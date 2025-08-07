package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	pb "keykammer/proto"
)

// ClientInfo stores information about a connected client
type ClientInfo struct {
	Username string
	Stream   interface{} // pb.KeykammerService_ChatServer when streaming, nil for JoinRoom-only clients
}

// Server implementation
type server struct {
	pb.UnimplementedKeykammerServiceServer
	roomID       string
	port         int
	maxUsers     int // Maximum users allowed in room (0 = unlimited)
	currentUsers int // Current number of connected users
	clients      map[string]*ClientInfo
	usernames    map[string]string // username -> clientID
	discoveryURL string            // URL for discovery server (empty if none)
	mutex        sync.RWMutex
}

// newServer creates a new server instance
func newServer(roomID string, port int, maxUsers int, discoveryURL string) *server {
	return &server{
		roomID:       roomID,
		port:         port,
		maxUsers:     maxUsers,
		currentUsers: 0,
		clients:      make(map[string]*ClientInfo),
		usernames:    make(map[string]string),
		discoveryURL: discoveryURL,
	}
}

// Chat handles bidirectional streaming chat
func (s *server) Chat(stream pb.KeykammerService_ChatServer) error {
	// Generate client ID
	clientID := generateClientID()
	
	// Wait for first message to get username
	msg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("failed to receive initial message: %v", err)
	}
	
	// Validate room ID
	if msg.RoomId != s.roomID {
		return fmt.Errorf("invalid room ID: expected %s, got %s", s.roomID[:16]+"...", msg.RoomId[:16]+"...")
	}
	
	username := msg.Username
	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}
	
	// Find existing client by username and update with stream
	s.mutex.Lock()
	existingClientID := s.usernames[username]
	if existingClientID == "" {
		s.mutex.Unlock()
		return fmt.Errorf("username %s not found in room (must call JoinRoom first)", username)
	}
	
	// Update existing client with stream
	if clientInfo, exists := s.clients[existingClientID]; exists {
		clientInfo.Stream = stream
		clientID = existingClientID // Use the existing client ID
	} else {
		s.mutex.Unlock()
		return fmt.Errorf("client info not found for username %s", username)
	}
	
	// User already counted in JoinRoom
	s.mutex.Unlock()
	
	// Send user list update to the newly connected client
	s.notifyUserListChange()
	
	// Add defer function to remove client on disconnect
	defer func() {
		s.mutex.Lock()
		delete(s.clients, clientID)
		s.currentUsers--
		remainingUsers := s.currentUsers
		s.mutex.Unlock()
		
		// Notify all remaining clients about user list change
		s.notifyUserListChange()
		
		// Check if room should be deleted from discovery (if currentUsers == 0)
		if remainingUsers == 0 {
			if s.discoveryURL != "" {
				go func() {
					err := deleteRoomFromDiscoveryWithRetry(s.roomID, s.discoveryURL, DefaultMaxRetries)
					if err != nil {
						fmt.Printf("Failed to clean up empty room from discovery: %v\n", err)
					} else {
						fmt.Printf("Empty room cleaned up from discovery server\n")
					}
				}()
			}
		}
	}()
	
	// Start message receive loop
	for {
		msg, err := stream.Recv()
		if err != nil {
			break
		}
		
		// Broadcast message to other clients
		s.broadcast(msg, clientID)
	}
	
	return nil
}

// broadcast sends a message to all connected clients including the sender
func (s *server) broadcast(msg *pb.ChatMessage, senderID string) {
	s.mutex.RLock()
	
	var failedClients []string
	
	// Iterate through all clients
	for clientID, clientInfo := range s.clients {
		
		// Skip clients without active streams
		if clientInfo.Stream == nil {
			continue
		}
		
		// Send message to client stream
		if stream, ok := clientInfo.Stream.(pb.KeykammerService_ChatServer); ok {
			err := stream.Send(msg)
			if err != nil {
				// Mark client for removal
				failedClients = append(failedClients, clientID)
			}
		}
	}
	
	s.mutex.RUnlock()
	
	// Remove failed clients in a separate goroutine to avoid deadlock
	if len(failedClients) > 0 {
		go func() {
			s.mutex.Lock()
			defer s.mutex.Unlock()
			
			for _, clientID := range failedClients {
				delete(s.clients, clientID)
				s.currentUsers--
			}
			
			// Notify about user list change if any clients were removed
			if len(failedClients) > 0 {
				s.notifyUserListChange()
			}
		}()
	}
}

// JoinRoom handles room join requests and validates usernames
func (s *server) JoinRoom(ctx context.Context, req *pb.JoinRequest) (*pb.JoinResponse, error) {
	var clientCount, maxCount int
	var discoveryURL, roomID string
	
	// Room ID validation
	if req.RoomId != s.roomID {
		fmt.Printf("Room ID mismatch: expected %s, got %s\n", s.roomID[:16]+"...", req.RoomId[:16]+"...")
		return &pb.JoinResponse{
			Success: false,
			Message: "Invalid room ID",
		}, nil
	}
	
	// Check capacity before accepting new connections
	s.mutex.RLock()
	currentCount := s.currentUsers
	maxCount = s.maxUsers
	s.mutex.RUnlock()
	
	if maxCount > 0 && currentCount >= maxCount {
		fmt.Printf("Room at capacity (%d/%d) - rejecting new connection\n", currentCount, maxCount)
		return &pb.JoinResponse{
			Success: false,
			Message: fmt.Sprintf("Room is full (%d/%d users)", currentCount, maxCount),
		}, nil
	}
	
	// Extract and validate username
	username := req.Username
	
	// Validate username format
	if err := validateUsername(username); err != nil {
		fmt.Printf("Username validation failed: %v\n", err)
		return &pb.JoinResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid username: %v", err),
		}, nil
	}
	
	// Check if username is available
	if !s.isUsernameAvailable(username) {
		// Get taken usernames and return them in response
		takenUsernames := s.getTakenUsernames()
		return &pb.JoinResponse{
			Success:        false,
			Message:        fmt.Sprintf("Username '%s' is already taken", username),
			TakenUsernames: takenUsernames,
		}, nil
	}
	
	// Add client to tracking and register username
	clientID := generateClientID()
	
	s.mutex.Lock()
	s.clients[clientID] = &ClientInfo{
		Username: username,
		Stream:   nil, // Stream will be set when client calls Chat method
	}
	// Register username mapping
	s.usernames[username] = clientID
	// Increment user count when user actually joins
	s.currentUsers++
	clientCount = s.currentUsers
	maxCount = s.maxUsers
	discoveryURL = s.discoveryURL
	roomID = s.roomID
	s.mutex.Unlock()
	
	// Notify all clients about user list change
	s.notifyUserListChange()
	
	// Check if room reaches capacity and trigger auto-delete
	if maxCount > 0 && clientCount >= maxCount {
		if discoveryURL != "" {
			go func() {
				err := deleteRoomFromDiscoveryWithRetry(roomID, discoveryURL, DefaultMaxRetries)
				if err != nil {
					fmt.Printf("Room full but failed to delete from discovery: %v\n", err)
				} else {
					// Send system message to all clients about room being closed
					systemMsg := &pb.ChatMessage{
						RoomId:           roomID,
						Username:         "System",
						EncryptedContent: []byte(fmt.Sprintf("Room at capacity (%d/%d) - no longer accepting new connections", clientCount, maxCount)),
						Timestamp:        time.Now().UnixNano(),
					}
					s.broadcast(systemMsg, "")
				}
			}()
		}
	}
	
	return &pb.JoinResponse{
		Success:     true,
		Message:     fmt.Sprintf("Successfully joined room as %s", username),
		ClientCount: int32(clientCount),
	}, nil
}

// isUsernameAvailable checks if a username is not already taken
func (s *server) isUsernameAvailable(username string) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	_, exists := s.usernames[username]
	return !exists
}

// getTakenUsernames returns a list of currently taken usernames
func (s *server) getTakenUsernames() []string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	taken := make([]string, 0, len(s.usernames))
	for username := range s.usernames {
		taken = append(taken, username)
	}
	return taken
}

// removeClient cleans up a client on disconnect
func (s *server) removeClient(clientID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	// Get client info before removing
	clientInfo, exists := s.clients[clientID]
	if !exists {
		return
	}
	
	username := clientInfo.Username
	
	// Remove from clients map
	delete(s.clients, clientID)
	
	// Remove from usernames map
	delete(s.usernames, username)
	
}

// getUserList returns a formatted list of connected users
func (s *server) getUserList() string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	if len(s.clients) == 0 {
		return "No users currently in the room."
	}
	
	var userList []string
	for _, clientInfo := range s.clients {
		userList = append(userList, clientInfo.Username)
	}
	
	return fmt.Sprintf("Users in room (%d): %s", len(s.clients), strings.Join(userList, ", "))
}

// handleUserListCommand processes /users or /who commands
func (s *server) handleUserListCommand() {
	userList := s.getUserList()
	displayMessage("System", userList)
}

// notifyUserListChange sends a user list update message to all clients
func (s *server) notifyUserListChange() {
	userList := s.getUsernameList()
	
	// Create a system message with user list info
	userListMsg := fmt.Sprintf("USERLIST:%s", strings.Join(userList, ","))
	
	// Broadcast to all clients as a system message
	systemMsg := &pb.ChatMessage{
		RoomId:           s.roomID,
		Username:         "System",
		EncryptedContent: []byte(userListMsg), // Store as plain text for system messages
		Timestamp:        time.Now().UnixNano(),
	}
	
	s.broadcast(systemMsg, "")
}

// getUsernameList returns just the list of usernames (for user list updates)
func (s *server) getUsernameList() []string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	var usernames []string
	for _, clientInfo := range s.clients {
		usernames = append(usernames, clientInfo.Username)
	}
	
	return usernames
}

// runServer starts a gRPC server for the specified room and port
func runServer(roomID string, port int, maxUsers int) {
	// Server startup logging
	fmt.Printf("Starting server on port %d for room %s\n", port, roomID[:16]+"...")
	
	// Create TCP listener and gRPC server
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Failed to listen on port %d: %v", port, err)
	}
	// Create server instance with room ID, port, and max users
	serverInstance := newServer(roomID, port, maxUsers, "")
	
	// Graceful shutdown handling
	grpcServer := grpc.NewServer()
	pb.RegisterKeykammerServiceServer(grpcServer, serverInstance)
	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	
	go func() {
		<-sigChan
		fmt.Printf("\nReceived interrupt signal, shutting down server...\n")
		grpcServer.GracefulStop()
	}()
	
	fmt.Printf("Server ready and listening on :%d\n", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

// runServerWithTUI starts a server in background and then launches TUI for the server owner
func runServerWithTUI(roomID string, port int, maxUsers int, encryptionKey []byte, discoveryURL string) {
	fmt.Printf("Starting server on port %d for room %s\n", port, roomID[:16]+"...")
	
	// Variable to store UPnP mapping for cleanup
	var upnpMapping *UPnPMapping
	
	// Start server in background goroutine
	serverReady := make(chan bool, 1)
	go func() {
		// Create TCP listener and gRPC server
		lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			log.Fatalf("Failed to listen on port %d: %v", port, err)
		}
		
		// Attempt UPnP port forwarding setup
		if checkUPnPAvailability() {
			upnpMapping, err = setupUPnPPortForwarding(port, "Keykammer Chat Server")
			if err != nil {
				fmt.Printf("UPnP setup failed: %v\n", err)
				fmt.Printf("Manual port forwarding may be required for external access\n")
			}
		} else {
			fmt.Printf("UPnP not available on this network\n")
			fmt.Printf("Manual port forwarding may be required for external access\n")
		}
		
		// Create server instance with room ID, port, and max users
		serverInstance := newServer(roomID, port, maxUsers, discoveryURL)
		
		// Graceful shutdown handling
		grpcServer := grpc.NewServer()
		pb.RegisterKeykammerServiceServer(grpcServer, serverInstance)
		
		// Set up signal handling for graceful shutdown
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt)
		
		go func() {
			<-sigChan
			fmt.Printf("\nReceived interrupt signal, shutting down server...\n")
			
			// Clean up UPnP port forwarding
			if upnpMapping != nil {
				err := removeUPnPPortForwarding(upnpMapping)
				if err != nil {
					fmt.Printf("Failed to remove UPnP mapping: %v\n", err)
				}
			}
			
			// Use a timeout for graceful shutdown to avoid hanging
			go func() {
				time.Sleep(2 * time.Second) // Give 2 seconds for graceful shutdown
				fmt.Printf("Force stopping server...\n")
				grpcServer.Stop() // Force stop if graceful shutdown takes too long
			}()
			
			grpcServer.GracefulStop()
		}()
		
		fmt.Printf("Server ready and listening on :%d\n", port)
		serverReady <- true
		
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()
	
	// Wait for server to be ready
	<-serverReady
	
	// Give server a moment to fully initialize
	time.Sleep(500 * time.Millisecond)
	
	// Get public IP for connection info
	publicIP, err := getPublicIP()
	if err != nil {
		publicIP = "YOUR_EXTERNAL_IP" // Fallback placeholder
	}
	
	fmt.Printf("\nServer started successfully! You are now the room owner.\n")
	fmt.Printf("Connection options for others with the same keyfile:\n")
	if discoveryURL != "" {
		fmt.Printf("  Discovery (easiest): keykammer -keyfile SAME_FILE\n")
	}
	fmt.Printf("  Local network: keykammer -connect localhost:%d -keyfile SAME_FILE\n", port)
	fmt.Printf("  Internet: keykammer -connect %s:%d -keyfile SAME_FILE\n\n", publicIP, port)
	
	// Set up cleanup function for discovery server deletion and UPnP cleanup
	setGlobalCleanup(func() {
		// Clean up UPnP port forwarding
		if upnpMapping != nil {
			err := removeUPnPPortForwarding(upnpMapping)
			if err != nil {
				fmt.Printf("Failed to remove UPnP mapping: %v\n", err)
			}
		}
		
		// Clean up discovery server registration
		if discoveryURL != "" {
			fmt.Printf("Deleting room from discovery server...\n")
			err := deleteRoomFromDiscoveryWithRetry(roomID, discoveryURL, DefaultMaxRetries)
			if err != nil {
				fmt.Printf("Failed to delete room from discovery: %v\n", err)
			} else {
				fmt.Printf("Room cleanup completed\n")
			}
		}
	})
	
	// Connect to our own server as a client to show TUI
	serverAddr := fmt.Sprintf("localhost:%d", port)
	runClientAsServerOwner(serverAddr, roomID, encryptionKey, maxUsers)
	
	// When TUI exits (user quit), clean up the room from discovery server
	fmt.Printf("Server owner quit, cleaning up...\n")
	if discoveryURL != "" {
		err := deleteRoomFromDiscoveryWithRetry(roomID, discoveryURL, DefaultMaxRetries)
		if err != nil {
			fmt.Printf("Failed to delete room from discovery: %v\n", err)
		} else {
			fmt.Printf("Room cleanup completed (may have already been removed from discovery)\n")
		}
	}
	
	// TODO: Gracefully shutdown the server goroutine
	fmt.Printf("Shutting down server...\n")
	os.Exit(0) // For now, force exit to ensure clean shutdown
}