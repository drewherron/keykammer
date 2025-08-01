package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/hkdf"
	"google.golang.org/grpc"

	pb "keykammer/proto"
)

const (
	MaxKeyFileSize = 20 * 1024 * 1024 // 20MB
	// KeyDerivationSalt must remain constant for compatibility across all clients
	KeyDerivationSalt = "keykammer-v1-salt"
	// DefaultPort is the default port for keykammer servers (76667 spells "rooms")
	DefaultPort = 76667
	// DefaultDiscoveryServer is the default discovery server endpoint
	DefaultDiscoveryServer = "https://discovery.keykammer.com"
	// Discovery server timeout constants
	DiscoveryTimeout = 10 // seconds
	DiscoveryRetryDelay = 2 // seconds
	DefaultMaxRetries = 3 // number of retry attempts
)

// getFileSize returns the size of a file in bytes
func getFileSize(path string) (int64, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return stat.Size(), nil
}

// readFile reads the contents of a file and returns the raw bytes
func readFile(path string) ([]byte, error) {
	size, err := getFileSize(path)
	if err != nil {
		return nil, err
	}
	
	if size > MaxKeyFileSize {
		return nil, fmt.Errorf("file too large: %d bytes (max: %d bytes)", size, MaxKeyFileSize)
	}
	
	return os.ReadFile(path)
}

// hashContent returns the hex-encoded SHA-256 hash of the content
func hashContent(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// deriveEncryptionKey derives a 32-byte encryption key from file content and password using HKDF
func deriveEncryptionKey(fileContent []byte, password string) ([]byte, error) {
	salt := []byte(KeyDerivationSalt)
	info := []byte("keykammer-encryption-key")
	
	// Combine file content and password as input key material
	keyMaterial := append(fileContent, []byte(password)...)
	
	hkdf := hkdf.New(sha256.New, keyMaterial, salt, info)
	key := make([]byte, 32) // 32 bytes for AES-256
	
	_, err := io.ReadFull(hkdf, key)
	if err != nil {
		return nil, err
	}
	
	return key, nil
}

// deriveRoomID generates a room ID from file content and password using salted hash
func deriveRoomID(fileContent []byte, password string) string {
	salt := []byte(KeyDerivationSalt)
	// Concatenate salt + content + password
	salted := append(salt, fileContent...)
	salted = append(salted, []byte(password)...)
	hash := sha256.Sum256(salted)
	return hex.EncodeToString(hash[:])
}

// deriveLocalServerAddress derives a consistent local server address from file content and password
func deriveLocalServerAddress(fileContent []byte, password string, port int) string {
	// For localhost, we use 127.0.0.1 with the specified port
	// Later this can be extended for distributed/internet-wide addressing
	return fmt.Sprintf("127.0.0.1:%d", port)
}

// KeyInfo bundles room ID and encryption key derived from file content, plus discovery metadata
type KeyInfo struct {
	RoomID          string
	EncryptionKey   []byte
	MaxUsers        int
	DiscoveryStatus DiscoveryStatus
}

// Discovery server data structures

// RoomRegistration represents a room registration request to the discovery server
type RoomRegistration struct {
	RoomID        string `json:"room_id"`
	ServerAddress string `json:"server_address"`
	MaxUsers      int    `json:"max_users"`
	CurrentUsers  int    `json:"current_users"`
}

// RoomLookup represents a room lookup request
type RoomLookup struct {
	RoomID string `json:"room_id"`
}

// DiscoveryResponse represents the response from the discovery server
type DiscoveryResponse struct {
	ServerAddress string `json:"server_address"`
	CurrentUsers  int    `json:"current_users"`
	MaxUsers      int    `json:"max_users"`
	WillAutoDelete bool  `json:"will_auto_delete"`
	SlotsRemaining int   `json:"slots_remaining"`
}

// Discovery server status tracking

// DiscoveryStatus represents the current state of discovery server connection
type DiscoveryStatus int

const (
	DiscoveryUnknown DiscoveryStatus = iota
	DiscoveryConnected
	DiscoveryDisconnected
	DiscoveryRoomListed
	DiscoveryRoomDeleted
)

func (s DiscoveryStatus) String() string {
	switch s {
	case DiscoveryUnknown:
		return "Unknown"
	case DiscoveryConnected:
		return "Connected"
	case DiscoveryDisconnected:
		return "Disconnected"
	case DiscoveryRoomListed:
		return "Room Listed"
	case DiscoveryRoomDeleted:
		return "Room Deleted"
	default:
		return "Unknown"
	}
}

// Global discovery status tracking
var currentDiscoveryStatus DiscoveryStatus = DiscoveryUnknown

// getDiscoveryStatus returns the current discovery server status
func getDiscoveryStatus() DiscoveryStatus {
	return currentDiscoveryStatus
}

// setDiscoveryStatus updates the current discovery server status
func setDiscoveryStatus(status DiscoveryStatus) {
	currentDiscoveryStatus = status
}

// isDiscoveryServerAvailable tests if the discovery server is reachable using health check
func isDiscoveryServerAvailable(discoveryURL string) bool {
	err := checkDiscoveryHealth(discoveryURL)
	if err == nil {
		setDiscoveryStatus(DiscoveryConnected)
		return true
	}
	
	setDiscoveryStatus(DiscoveryDisconnected)
	return false
}

// Discovery server HTTP client functions

// createDiscoveryClient creates an HTTP client with reasonable timeouts for discovery operations
func createDiscoveryClient() *http.Client {
	return &http.Client{
		Timeout: time.Duration(DiscoveryTimeout) * time.Second,
	}
}

// registerRoom registers a new room with the discovery server
func registerRoom(discoveryURL, roomID, serverAddr string, maxUsers int) error {
	client := createDiscoveryClient()
	
	registration := RoomRegistration{
		RoomID:        roomID,
		ServerAddress: serverAddr,
		MaxUsers:      maxUsers,
		CurrentUsers:  1, // Starting with 1 user (the creator)
	}
	
	jsonData, err := json.Marshal(registration)
	if err != nil {
		return fmt.Errorf("failed to marshal registration: %v", err)
	}
	
	resp, err := client.Post(discoveryURL+"/api/rooms", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to register room: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		setDiscoveryStatus(DiscoveryDisconnected)
		return fmt.Errorf("registration failed with status: %d", resp.StatusCode)
	}
	
	setDiscoveryStatus(DiscoveryRoomListed)
	return nil
}

// lookupRoom looks up an existing room in the discovery server
func lookupRoom(discoveryURL, roomID string) (*DiscoveryResponse, error) {
	client := createDiscoveryClient()
	
	resp, err := client.Get(discoveryURL + "/api/rooms/" + roomID)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup room: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // Room not found, which is not an error
	}
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lookup failed with status: %d", resp.StatusCode)
	}
	
	var discovery DiscoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}
	
	return &discovery, nil
}

// registerWithDiscovery registers a new room with the discovery server using KeyInfo
func registerWithDiscovery(keyInfo *KeyInfo, discoveryURL string, port int, maxUsers int) error {
	// Get local IP address for server registration
	serverAddr := fmt.Sprintf("127.0.0.1:%d", port)
	
	// For internet use, we'd need to get the actual public IP
	// This is a placeholder for localhost development
	
	err := registerRoom(discoveryURL, keyInfo.RoomID, serverAddr, maxUsers)
	if err != nil {
		return fmt.Errorf("failed to register with discovery server: %v", err)
	}
	
	fmt.Printf("Room registered with discovery server\n")
	fmt.Printf("  Room ID: %s\n", keyInfo.RoomID[:16]+"...")
	fmt.Printf("  Server: %s\n", serverAddr)
	fmt.Printf("  Max users: %d\n", maxUsers)
	
	return nil
}

// lookupRoomInDiscovery looks up an existing room via discovery server and returns server address
func lookupRoomInDiscovery(roomID, discoveryURL string) (string, error) {
	discovery, err := lookupRoom(discoveryURL, roomID)
	if err != nil {
		return "", fmt.Errorf("failed to lookup room in discovery server: %v", err)
	}
	
	if discovery == nil {
		// Room not found
		fmt.Printf("Room not found in discovery server\n")
		return "", nil
	}
	
	fmt.Printf("Found existing room in discovery server\n")
	fmt.Printf("  Room ID: %s\n", roomID[:16]+"...")
	fmt.Printf("  Server: %s\n", discovery.ServerAddress)
	
	// Validate room capacity using the new validation function
	canJoin, err := checkRoomJoinability(roomID, discovery.CurrentUsers, discovery.MaxUsers)
	if !canJoin {
		return "", err
	}
	
	return discovery.ServerAddress, nil
}

// deleteRoomFromDiscovery removes a room from the discovery server
func deleteRoomFromDiscovery(roomID, discoveryURL string) error {
	client := createDiscoveryClient()
	
	req, err := http.NewRequest("DELETE", discoveryURL+"/api/rooms/"+roomID, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %v", err)
	}
	
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete room: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("deletion failed with status: %d", resp.StatusCode)
	}
	
	setDiscoveryStatus(DiscoveryRoomDeleted)
	fmt.Printf("Room deleted from discovery server\n")
	fmt.Printf("  Room ID: %s\n", roomID[:16]+"...")
	fmt.Printf("  Status: Room is now private and invisible\n")
	
	return nil
}

// triggerAutoDelete removes room from discovery when capacity is reached
func triggerAutoDelete(roomID, discoveryURL string, currentUsers, maxUsers int) error {
	if maxUsers > 0 && currentUsers >= maxUsers {
		fmt.Printf("Room capacity reached (%d/%d) - triggering auto-delete\n", currentUsers, maxUsers)
		return deleteRoomFromDiscovery(roomID, discoveryURL)
	}
	return nil
}

// retryDiscoveryOperation retries a discovery server operation with exponential backoff
func retryDiscoveryOperation(operation func() error, maxRetries int) error {
	var lastErr error
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := operation()
		if err == nil {
			if attempt > 0 {
				fmt.Printf("Discovery operation succeeded after %d retries\n", attempt)
			}
			return nil
		}
		
		lastErr = err
		if attempt < maxRetries {
			delay := time.Duration(DiscoveryRetryDelay * (1 << attempt)) * time.Second
			fmt.Printf("Discovery operation failed (attempt %d/%d): %v\n", attempt+1, maxRetries+1, err)
			fmt.Printf("  Retrying in %v...\n", delay)
			time.Sleep(delay)
		}
	}
	
	fmt.Printf("✗ Discovery operation failed after %d retries: %v\n", maxRetries+1, lastErr)
	return fmt.Errorf("operation failed after %d retries: %v", maxRetries+1, lastErr)
}

// checkDiscoveryHealth performs a health check on the discovery server
func checkDiscoveryHealth(discoveryURL string) error {
	client := createDiscoveryClient()
	
	resp, err := client.Get(discoveryURL + "/api/health")
	if err != nil {
		return fmt.Errorf("health check request failed: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discovery server unhealthy (status: %d)", resp.StatusCode)
	}
	
	// Optionally read and validate response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read health check response: %v", err)
	}
	
	// Basic validation that server is responding properly
	if len(body) == 0 {
		return fmt.Errorf("empty health check response")
	}
	
	return nil
}

// registerWithDiscoveryWithRetry registers a room with retry logic
func registerWithDiscoveryWithRetry(keyInfo *KeyInfo, discoveryURL string, port int, maxUsers int, maxRetries int) error {
	return retryDiscoveryOperation(func() error {
		return registerWithDiscovery(keyInfo, discoveryURL, port, maxUsers)
	}, maxRetries)
}

// lookupRoomInDiscoveryWithRetry looks up a room with retry logic
func lookupRoomInDiscoveryWithRetry(roomID, discoveryURL string, maxRetries int) (string, error) {
	var result string
	var resultErr error
	
	err := retryDiscoveryOperation(func() error {
		addr, err := lookupRoomInDiscovery(roomID, discoveryURL)
		result = addr
		resultErr = err
		return err
	}, maxRetries)
	
	if err != nil {
		return "", err
	}
	return result, resultErr
}

// deleteRoomFromDiscoveryWithRetry deletes a room with retry logic
func deleteRoomFromDiscoveryWithRetry(roomID, discoveryURL string, maxRetries int) error {
	return retryDiscoveryOperation(func() error {
		return deleteRoomFromDiscovery(roomID, discoveryURL)
	}, maxRetries)
}

// checkDiscoveryAndFallback tests discovery server availability and logs fallback mode
func checkDiscoveryAndFallback(discoveryURL string) bool {
	fmt.Printf("Testing discovery server availability...\n")
	
	if isDiscoveryServerAvailable(discoveryURL) {
		fmt.Printf("Discovery server available: %s\n", discoveryURL)
		return true
	}
	
	fmt.Printf("Discovery server unavailable: %s\n", discoveryURL)
	fmt.Printf("Falling back to localhost-only mode\n")
	fmt.Printf("  Note: Only local connections will work in this mode\n")
	
	return false
}

// validateRoomCapacity checks if a room has space for new users
func validateRoomCapacity(current, max int) error {
	if max <= 0 {
		// Unlimited room (max = 0), always allow
		return nil
	}
	
	if current < 0 {
		return fmt.Errorf("invalid current user count: %d", current)
	}
	
	if current >= max {
		return fmt.Errorf("room is full (%d/%d users)", current, max)
	}
	
	return nil
}

// checkRoomJoinability validates if a user can join a room based on capacity
func checkRoomJoinability(roomID string, current, max int) (bool, error) {
	err := validateRoomCapacity(current, max)
	if err != nil {
		fmt.Printf("Cannot join room %s: %v\n", roomID[:16]+"...", err)
		return false, err
	}
	
	if max > 0 {
		remaining := max - current
		fmt.Printf("Room has space: %d/%d users (%d slots remaining)\n", current, max, remaining)
	} else {
		fmt.Printf("Room has unlimited capacity (current: %d users)\n", current)
	}
	
	return true, nil
}

// deriveKeyInfo derives both room ID and encryption key from file content and password, plus discovery metadata
func deriveKeyInfo(fileContent []byte, password string, maxUsers int) (*KeyInfo, error) {
	roomID := deriveRoomID(fileContent, password)
	encryptionKey, err := deriveEncryptionKey(fileContent, password)
	if err != nil {
		return nil, err
	}
	
	return &KeyInfo{
		RoomID:          roomID,
		EncryptionKey:   encryptionKey,
		MaxUsers:        maxUsers,
		DiscoveryStatus: getDiscoveryStatus(),
	}, nil
}

// deriveKeyInfoLegacy provides backward compatibility for existing code that doesn't specify maxUsers
func deriveKeyInfoLegacy(fileContent []byte, password string) (*KeyInfo, error) {
	return deriveKeyInfo(fileContent, password, 2) // Default to 2 users for backward compatibility
}

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
	mutex        sync.RWMutex
}

// newServer creates a new server instance
func newServer(roomID string, port int, maxUsers int) *server {
	return &server{
		roomID:       roomID,
		port:         port,
		maxUsers:     maxUsers,
		currentUsers: 0,
		clients:      make(map[string]*ClientInfo),
		usernames:    make(map[string]string),
	}
}

// Chat handles bidirectional streaming chat
func (s *server) Chat(stream pb.KeykammerService_ChatServer) error {
	// Receive first message from stream for room validation
	firstMsg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("failed to receive first message: %v", err)
	}
	
	// Check room ID matches
	if firstMsg.RoomId != s.roomID {
		return fmt.Errorf("room ID mismatch: expected %s, got %s", 
			s.roomID[:16]+"...", firstMsg.RoomId[:16]+"...")
	}
	
	// Room ID validation passed
	fmt.Printf("Client stream connected to room %s\n", s.roomID[:16]+"...")
	
	// Generate client ID
	clientID := generateClientID()
	
	// Lock mutex and register client
	s.mutex.Lock()
	s.clients[clientID] = &ClientInfo{
		Username: "", // Will be set from username in message or join flow
		Stream:   stream,
	}
	s.currentUsers++
	clientCount := s.currentUsers
	s.mutex.Unlock()
	
	fmt.Printf("Client %s registered for streaming (total clients: %d)\n", clientID[:8], clientCount)
	
	// Check if room reaches capacity and trigger auto-delete
	if s.maxUsers > 0 && clientCount >= s.maxUsers {
		fmt.Printf("Room capacity reached (%d/%d) - triggering auto-delete from discovery\n", clientCount, s.maxUsers)
		// In full implementation, would call deleteRoomFromDiscovery(s.roomID)
		// For now, just log the event
	}
	
	// Add defer function to remove client on disconnect
	defer func() {
		s.mutex.Lock()
		delete(s.clients, clientID)
		s.currentUsers--
		remainingUsers := s.currentUsers
		s.mutex.Unlock()
		
		fmt.Printf("Client %s disconnected (remaining clients: %d)\n", clientID[:8], remainingUsers)
		
		// Check if room should be deleted from discovery (if currentUsers == 0)
		if remainingUsers == 0 {
			fmt.Printf("Room is now empty - would trigger discovery cleanup in full implementation\n")
		}
	}()
	
	// Message receive loop
	for {
		msg, err := stream.Recv()
		if err != nil {
			fmt.Printf("Client %s stream receive error: %v\n", clientID[:8], err)
			break
		}
		
		// Log received message for now
		fmt.Printf("Received message from client %s: room=%s, username=%s\n", 
			clientID[:8], msg.RoomId[:16]+"...", msg.Username)
		
		// Broadcast message to other clients
		s.broadcast(msg, clientID)
	}
	
	return nil
}

// broadcast sends a message to all connected clients except the sender
func (s *server) broadcast(msg *pb.ChatMessage, senderID string) {
	s.mutex.RLock()
	
	var failedClients []string
	
	// Iterate through all clients
	for clientID, clientInfo := range s.clients {
		// Skip sender
		if clientID == senderID {
			continue
		}
		
		// Skip clients without active streams
		if clientInfo.Stream == nil {
			continue
		}
		
		// Send message to client stream
		if stream, ok := clientInfo.Stream.(pb.KeykammerService_ChatServer); ok {
			err := stream.Send(msg)
			if err != nil {
				fmt.Printf("Failed to send message to client %s: %v\n", clientID[:8], err)
				// Mark client for removal
				failedClients = append(failedClients, clientID)
			} else {
				fmt.Printf("Broadcasted message to client %s\n", clientID[:8])
			}
		}
	}
	
	s.mutex.RUnlock()
	
	// Remove failed clients in a goroutine to avoid deadlock
	if len(failedClients) > 0 {
		go func() {
			s.mutex.Lock()
			defer s.mutex.Unlock()
			
			for _, clientID := range failedClients {
				delete(s.clients, clientID)
				s.currentUsers--
				fmt.Printf("Removed failed client %s (remaining clients: %d)\n", clientID[:8], s.currentUsers)
			}
		}()
	}
}

// JoinRoom handles client room join requests with username validation
func (s *server) JoinRoom(ctx context.Context, req *pb.JoinRequest) (*pb.JoinResponse, error) {
	fmt.Printf("Client attempting to join room: %s with username: %s\n", req.RoomId, req.Username)
	
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
	maxCount := s.maxUsers
	s.mutex.RUnlock()
	
	if maxCount > 0 && currentCount >= maxCount {
		fmt.Printf("Room at capacity (%d/%d) - rejecting new connection\n", currentCount, maxCount)
		return &pb.JoinResponse{
			Success: false,
			Message: fmt.Sprintf("Room is full (%d/%d users)", currentCount, maxCount),
		}, nil
	}
	
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
		fmt.Printf("Username %s is already taken. Taken usernames: %v\n", username, takenUsernames)
		return &pb.JoinResponse{
			Success:        false,
			Message:        "Username is already taken",
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
	clientCount := len(s.clients)
	s.mutex.Unlock()
	
	fmt.Printf("Client %s (%s) successfully joined room (total clients: %d)\n", clientID[:8], username, clientCount)
	return &pb.JoinResponse{
		Success:     true,
		Message:     "Successfully joined room",
		ClientCount: int32(clientCount),
	}, nil
}

// isUsernameAvailable checks if a username is available for use
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

// removeClient cleans up a client on disconnect (Step 62)
// In full implementation, this would be called when:
// - gRPC stream disconnects
// - Client explicitly leaves  
// - Heartbeat/keepalive fails
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
	
	// Log the disconnect
	fmt.Printf("Client %s (%s) left the room (remaining clients: %d)\n", 
		clientID[:8], username, len(s.clients))
}

// getUserList returns a formatted list of connected users (Step 64)
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

// handleUserListCommand processes /users or /who commands (Step 64)
func (s *server) handleUserListCommand() {
	userList := s.getUserList()
	displayMessage("System", userList)
}

// displayMessage formats and displays a chat message with username (Step 61)
func displayMessage(username, message string) {
	timestamp := time.Now().Format("15:04:05")
	fmt.Printf("[%s] %s: %s\n", timestamp, username, message)
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
	serverInstance := newServer(roomID, port, maxUsers)
	
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

// runClient handles main client logic for connecting to a server
func runClient(serverAddr string, roomID string) {
	fmt.Printf("Starting client mode\n")
	
	// Username prompt with retry logic for taken usernames
	var username string
	maxAttempts := 3
	
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Get and validate username format
		for {
			username = promptUsername()
			if err := validateUsername(username); err != nil {
				fmt.Printf("Invalid username: %v\n", err)
				continue
			}
			break
		}
		
		fmt.Printf("Connecting as user: %s (attempt %d/%d)\n", username, attempt, maxAttempts)
		
		// Try to connect with username and handle retries
		success := tryConnectAsClient(serverAddr, roomID, username)
		
		if success {
			fmt.Printf("Client connected successfully to room as %s\n", username)
			
			// Establish chat stream and start chat
			err := startChatSession(serverAddr, roomID, username)
			if err != nil {
				fmt.Printf("Failed to start chat session: %v\n", err)
				if attempt < maxAttempts {
					fmt.Printf("Retrying with different username...\n")
					continue
				}
			} else {
				return // Chat session completed normally
			}
		} else if attempt < maxAttempts {
			fmt.Printf("Username may be taken or connection failed. Try a different username.\n")
		}
	}
	
	fmt.Printf("Failed to join room after %d attempts\n", maxAttempts)
	os.Exit(1)
}

// establishChatStream creates a bidirectional gRPC stream for chat
func establishChatStream(serverAddr string) (pb.KeykammerService_ChatClient, *grpc.ClientConn, error) {
	// Create connection to server
	conn, err := connectToServer(serverAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to server: %v", err)
	}
	
	// Create gRPC client
	client := pb.NewKeykammerServiceClient(conn)
	
	// Start chat stream
	ctx := context.Background()
	stream, err := client.Chat(ctx)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("failed to start chat stream: %v", err)
	}
	
	return stream, conn, nil
}

// startChatSession handles the full chat session lifecycle
func startChatSession(serverAddr, roomID, username string) error {
	fmt.Printf("Establishing chat stream to %s...\n", serverAddr)
	
	// Establish the gRPC stream
	stream, conn, err := establishChatStream(serverAddr)
	if err != nil {
		return err
	}
	defer conn.Close()
	
	fmt.Printf("Chat stream established, sending initial message...\n")
	
	// Send initial message with room ID and username for validation
	initialMsg := &pb.ChatMessage{
		RoomId:           roomID,
		Username:         username,
		EncryptedContent: []byte{}, // Empty content for initial validation message
		Timestamp:        time.Now().UnixNano(),
	}
	
	err = stream.Send(initialMsg)
	if err != nil {
		return fmt.Errorf("failed to send initial message: %v", err)
	}
	
	fmt.Printf("Successfully joined chat room %s as %s\n", roomID[:16]+"...", username)
	
	// Start message receive handler in goroutine
	done := make(chan bool)
	go handleIncomingMessages(stream, done)
	
	// Set up signal handling for Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	
	go func() {
		<-sigChan
		fmt.Printf("\nReceived interrupt signal, exiting chat...\n")
		close(done)
	}()
	
	fmt.Printf("Chat session ready. Type messages and press Enter. Type '/quit' to exit.\n")
	fmt.Printf("Press Ctrl+C to exit at any time.\n")
	
	// Start input loop for sending messages
	err = handleUserInput(stream, roomID, username, done)
	if err != nil {
		return fmt.Errorf("input handling error: %v", err)
	}
	
	return nil
}

// handleIncomingMessages receives and displays messages from the chat stream
func handleIncomingMessages(stream pb.KeykammerService_ChatClient, done chan bool) {
	for {
		select {
		case <-done:
			return
		default:
			// Receive message with context
			msg, err := stream.Recv()
			if err != nil {
				// Check if we're supposed to stop
				select {
				case <-done:
					return
				default:
					if err.Error() != "EOF" {
						fmt.Printf("\nConnection error: %v\n", err)
						fmt.Print("> ") // Restore input prompt
					}
					return
				}
			}
			
			// Display the received message
			fmt.Print("\r") // Clear the input prompt
			displayChatMessage(msg)
			fmt.Print("> ") // Restore input prompt
		}
	}
}

// displayChatMessage formats and displays a received chat message
func displayChatMessage(msg *pb.ChatMessage) {
	timestamp := time.Unix(0, msg.Timestamp).Format("15:04:05")
	
	// For now, treat encrypted content as plain text (will decrypt later)
	var content string
	if len(msg.EncryptedContent) > 0 {
		content = string(msg.EncryptedContent) // Temporary: will decrypt this later
	} else {
		content = "[empty message]"
	}
	
	fmt.Printf("[%s] %s: %s\n", timestamp, msg.Username, content)
}

// handleUserInput processes user input and sends messages
func handleUserInput(stream pb.KeykammerService_ChatClient, roomID, username string, done chan bool) error {
	scanner := bufio.NewScanner(os.Stdin)
	
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		
		input := strings.TrimSpace(scanner.Text())
		
		// Handle empty input
		if input == "" {
			continue
		}
		
		// Handle quit command
		if input == "/quit" {
			fmt.Printf("Exiting chat...\n")
			close(done)
			// Give the message handler a moment to stop
			time.Sleep(100 * time.Millisecond)
			return nil
		}
		
		// Send the message
		err := sendMessage(stream, roomID, username, input)
		if err != nil {
			fmt.Printf("Failed to send message: %v\n", err)
			continue
		}
	}
	
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("input scanner error: %v", err)
	}
	
	return nil
}

// sendMessage sends a chat message through the stream
func sendMessage(stream pb.KeykammerService_ChatClient, roomID, username, content string) error {
	// For now, send content as plain text in encrypted field (will encrypt later)
	msg := &pb.ChatMessage{
		RoomId:           roomID,
		Username:         username,
		EncryptedContent: []byte(content), // Temporary: will encrypt this later
		Timestamp:        time.Now().UnixNano(),
	}
	
	return stream.Send(msg)
}

// generateClientID creates a unique identifier for each client
func generateClientID() string {
	// Generate 8 random bytes (will result in 16 hex characters)
	bytes := make([]byte, 8)
	_, err := rand.Read(bytes)
	if err != nil {
		// Fallback to timestamp-based ID if random generation fails
		return fmt.Sprintf("client_%d", time.Now().UnixNano()%1000000000)
	}
	return hex.EncodeToString(bytes)
}

// promptUsername prompts the user to enter a username
func promptUsername() string {
	fmt.Print("Enter username: ")
	reader := bufio.NewReader(os.Stdin)
	username, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("Error reading username: %v", err)
		return "anonymous"
	}
	return strings.TrimSpace(username)
}

// validateUsername ensures the username meets requirements
func validateUsername(username string) error {
	if len(username) < 1 {
		return fmt.Errorf("username must be at least 1 character")
	}
	if len(username) > 20 {
		return fmt.Errorf("username must be 20 characters or less")
	}
	// Check for invalid characters (allow alphanumeric, underscore, dash)
	for _, char := range username {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || 
			 (char >= '0' && char <= '9') || char == '_' || char == '-') {
			return fmt.Errorf("username can only contain letters, numbers, underscore, and dash")
		}
	}
	return nil
}

// isServerRunning checks if a server is running on the specified local port
func isServerRunning(port int) bool {
	timeout := time.Second * 2
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// connectToServer establishes a gRPC connection to the specified address
func connectToServer(addr string) (*grpc.ClientConn, error) {
	// Add timeout for connection attempt
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// Use insecure connection for now (will add TLS later)
	conn, err := grpc.DialContext(ctx, addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server at %s: %v", addr, err)
	}
	
	return conn, nil
}

// tryConnectAsClient attempts to connect to a server and join a room
func tryConnectAsClient(addr string, roomID string, username string) bool {
	// Add client logging
	fmt.Printf("Attempting to connect to %s as user %s\n", addr, username)
	
	// Create connection to server
	conn, err := connectToServer(addr)
	if err != nil {
		fmt.Printf("Failed to connect to server: %v\n", err)
		return false
	}
	defer conn.Close()
	
	// Create gRPC client
	client := pb.NewKeykammerServiceClient(conn)
	
	// Call JoinRoom RPC (using ChatMessage as request type due to proto limitations)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Send username in join request using proper JoinRequest
	resp, err := client.JoinRoom(ctx, &pb.JoinRequest{
		RoomId:   roomID,
		Version:  1,
		Username: username,
	})
	if err != nil {
		fmt.Printf("Failed to join room: %v\n", err)
		return false
	}
	
	if resp.Success {
		fmt.Printf("Successfully joined room %s\n", roomID[:16]+"...")
		return true
	} else {
		fmt.Printf("Room join rejected by server\n")
		return false
	}
}

func main() {
	keyfile := flag.String("keyfile", "", "Path to key file (required)")
	port := flag.Int("port", DefaultPort, "Port to use")
	password := flag.String("password", "", "Optional password for server derivation (empty uses keyfile only)")
	size := flag.Int("size", 2, "Maximum users per room (2 for maximum privacy, 0 = unlimited)")
	discoveryServer := flag.String("discovery-server", DefaultDiscoveryServer, "Discovery server URL")
	discoveryServerMode := flag.Bool("discovery-server-mode", false, "Run as HTTP discovery server")
	flag.Parse()

	// Handle discovery server mode
	if *discoveryServerMode {
		fmt.Printf("Starting discovery server on port %d\n", *port)
		err := runDiscoveryServer(*port)
		if err != nil {
			log.Fatalf("Discovery server failed: %v", err)
		}
		return
	}

	if *keyfile == "" {
		fmt.Println("Error: -keyfile is required")
		flag.Usage()
		os.Exit(1)
	}

	// Validate size parameter
	if *size < 0 {
		fmt.Println("Error: room size must be >= 0 (0 means unlimited)")
		os.Exit(1)
	}


	// Read keyfile and derive key info
	fileContent, err := readFile(*keyfile)
	if err != nil {
		fmt.Printf("Error reading keyfile: %v\n", err)
		os.Exit(1)
	}

	keyInfo, err := deriveKeyInfo(fileContent, *password, *size)
	if err != nil {
		fmt.Printf("Error deriving key info: %v\n", err)
		os.Exit(1)
	}

	// Print basic room information
	fmt.Printf("Room ID: %s\n", keyInfo.RoomID[:16]+"...")
	if keyInfo.MaxUsers == 0 {
		fmt.Printf("Room size: unlimited\n")
	} else {
		fmt.Printf("Room size: %d users max\n", keyInfo.MaxUsers)
	}
	
	// Server/client mode will be determined automatically based on room availability
	fmt.Printf("\n")
	
	// Check discovery server availability
	fmt.Printf("Discovery server: %s\n", *discoveryServer)
	
	discoveryAvailable := checkDiscoveryAndFallback(*discoveryServer)
	
	if discoveryAvailable {
		// Try room lookup before creating new room
		fmt.Printf("\nLooking for existing room...\n")
		existingServerAddr, err := lookupRoomInDiscoveryWithRetry(keyInfo.RoomID, *discoveryServer, DefaultMaxRetries)
		
		if err != nil {
			fmt.Printf("Error during room lookup: %v\n", err)
			fmt.Printf("Proceeding to create new room...\n")
		} else if existingServerAddr != "" {
			// Connect to existing room as client
			fmt.Printf("\nExisting room found! Connecting as client to %s\n", existingServerAddr)
			runClient(existingServerAddr, keyInfo.RoomID)
			return
		} else {
			// Register room if lookup fails (new room)
			fmt.Printf("\nNo existing room found, creating new room...\n")
			serverAddr := deriveLocalServerAddress(fileContent, *password, *port)
			
			err = registerWithDiscoveryWithRetry(keyInfo, *discoveryServer, *port, keyInfo.MaxUsers, DefaultMaxRetries)
			if err != nil {
				fmt.Printf("Failed to register room: %v\n", err)
				fmt.Printf("Falling back to localhost-only mode\n")
			} else {
				fmt.Printf("\nStarting new room server at %s\n", serverAddr)
				// Start server automatically since no existing room found
				runServer(keyInfo.RoomID, *port, *size)
				return
			}
		}
	} else {
		// Operating in localhost-only mode
		fmt.Printf("\nOperating in localhost-only mode\n")
		serverAddr := deriveLocalServerAddress(fileContent, *password, *port)
		fmt.Printf("Server address: %s\n", serverAddr)
		
		// Check for existing local server first
		if isServerRunning(*port) {
			fmt.Printf("Found existing server on localhost:%d, connecting as client\n", *port)
			runClient(fmt.Sprintf("localhost:%d", *port), keyInfo.RoomID)
			return
		}
		
		// Start server since no existing server found
		runServer(keyInfo.RoomID, *port, *size)
		return
	}
	
	fmt.Printf("\nDiscovery flow complete. Client mode not yet implemented.\n")
}

// In-memory room storage for discovery server
var (
	discoveryRooms = make(map[string]*RoomRegistration)
	discoveryMutex = sync.RWMutex{}
)

// runDiscoveryServer starts the HTTP discovery server
func runDiscoveryServer(port int) error {
	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/api/rooms", handleRooms)
	http.HandleFunc("/api/rooms/", handleSpecificRoom)
	
	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("Discovery server listening on %s\n", addr)
	fmt.Printf("Endpoints:\n")
	fmt.Printf("  GET  /health        - Health check\n")
	fmt.Printf("  POST /api/rooms     - Register room\n")
	fmt.Printf("  GET  /api/rooms/{id} - Lookup room\n")
	fmt.Printf("  DELETE /api/rooms/{id} - Delete room\n")
	
	return http.ListenAndServe(addr, nil)
}

// handleHealth provides a simple health check endpoint
func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// handleRooms handles POST requests to register new rooms
func handleRooms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var registration RoomRegistration
	if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	// Validate registration
	if registration.RoomID == "" || registration.ServerAddress == "" {
		http.Error(w, "Missing room_id or server_address", http.StatusBadRequest)
		return
	}
	
	discoveryMutex.Lock()
	discoveryRooms[registration.RoomID] = &registration
	discoveryMutex.Unlock()
	
	fmt.Printf("Registered room %s at %s (%d/%d users)\n", 
		registration.RoomID[:16]+"...", registration.ServerAddress, 
		registration.CurrentUsers, registration.MaxUsers)
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
}

// handleSpecificRoom handles GET and DELETE requests for specific rooms
func handleSpecificRoom(w http.ResponseWriter, r *http.Request) {
	// Extract room ID from URL path
	roomID := strings.TrimPrefix(r.URL.Path, "/api/rooms/")
	if roomID == "" {
		http.Error(w, "Missing room ID", http.StatusBadRequest)
		return
	}
	
	switch r.Method {
	case http.MethodGet:
		handleRoomLookup(w, roomID)
	case http.MethodDelete:
		handleRoomDelete(w, roomID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRoomLookup handles GET requests to lookup rooms
func handleRoomLookup(w http.ResponseWriter, roomID string) {
	discoveryMutex.RLock()
	room, exists := discoveryRooms[roomID]
	discoveryMutex.RUnlock()
	
	if !exists {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}
	
	response := DiscoveryResponse{
		ServerAddress:  room.ServerAddress,
		CurrentUsers:   room.CurrentUsers,
		MaxUsers:       room.MaxUsers,
		WillAutoDelete: room.MaxUsers > 0,
		SlotsRemaining: room.MaxUsers - room.CurrentUsers,
	}
	
	if room.MaxUsers > 0 && room.MaxUsers <= room.CurrentUsers {
		response.SlotsRemaining = 0
	}
	
	fmt.Printf("Room lookup: %s -> %s (%d/%d users)\n", 
		roomID[:16]+"...", room.ServerAddress, room.CurrentUsers, room.MaxUsers)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleRoomDelete handles DELETE requests to remove rooms
func handleRoomDelete(w http.ResponseWriter, roomID string) {
	discoveryMutex.Lock()
	_, exists := discoveryRooms[roomID]
	if exists {
		delete(discoveryRooms, roomID)
	}
	discoveryMutex.Unlock()
	
	if !exists {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}
	
	fmt.Printf("Deleted room %s from discovery\n", roomID[:16]+"...")
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

