package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"google.golang.org/grpc"
	pb "keykammer/proto"
)

// runClient handles main client logic for connecting to a server
func runClient(serverAddr string, roomID string, encryptionKey []byte) {
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
			err := startChatSession(serverAddr, roomID, username, encryptionKey)
			if err != nil {
				// Provide specific error messages for chat session failures
				if strings.Contains(err.Error(), "connection refused") {
					fmt.Printf("Chat server disconnected: %v\n", err)
					fmt.Printf("  Tip: The server may have shut down. Try connecting again.\n")
				} else if strings.Contains(err.Error(), "stream") {
					fmt.Printf("Chat stream error: %v\n", err)
					fmt.Printf("  Tip: Connection interrupted. Try reconnecting.\n")
				} else {
					fmt.Printf("Chat session failed: %v\n", err)
				}
				
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
	
	fmt.Printf("Failed to connect after %d attempts. Check server status and try again.\n", maxAttempts)
}

// establishChatStream creates a bidirectional gRPC stream for chat
func establishChatStream(serverAddr string) (pb.KeykammerService_ChatClient, *grpc.ClientConn, error) {
	fmt.Printf("Connecting to chat server at %s...\n", serverAddr)
	
	// Create connection to server with retry logic
	conn, err := connectToServerWithRetry(serverAddr, 3) // 3 retries = 4 total attempts
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to server: %v", err)
	}
	
	fmt.Printf("Connection established, starting chat stream...\n")
	
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
func startChatSession(serverAddr, roomID, username string, encryptionKey []byte) error {
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
	
	// Set up TUI interface
	err = setupTUI(roomID, username)
	if err != nil {
		return fmt.Errorf("failed to setup TUI: %v", err)
	}
	
	// Display welcome message in chat
	addChatMessage("System", fmt.Sprintf("Successfully joined room %s as %s", roomID[:16]+"...", username))
	addChatMessage("System", "Commands: /quit to exit, /help for help")
	addChatMessage("System", "Use Tab to navigate between panes, Esc to return to input")
	
	// Initialize user list with current user (will be updated by server notifications)
	updateUserList([]string{username})
	
	// Start message receive handler in goroutine
	done := make(chan bool)
	go handleIncomingMessagesTUI(stream, encryptionKey, done)
	
	// Set up input handling for TUI
	setupTUIInputHandling(stream, roomID, username, encryptionKey, done)
	
	// Set up signal handling for Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		app.Stop()
		close(done)
	}()
	
	// Run the TUI application (this blocks until app.Stop() is called)
	err = app.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %v", err)
	}
	
	return nil
}

// connectToServer creates a gRPC connection to the specified address
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

// connectToServerWithRetry attempts to connect with retry logic
func connectToServerWithRetry(addr string, maxRetries int) (*grpc.ClientConn, error) {
	var lastErr error
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		conn, err := connectToServer(addr)
		if err == nil {
			if attempt > 0 {
				fmt.Printf("Connection succeeded after %d retries\n", attempt)
			}
			return conn, nil
		}
		
		lastErr = err
		if attempt < maxRetries {
			delay := time.Duration(1<<attempt) * time.Second // Exponential backoff: 1s, 2s, 4s, 8s...
			fmt.Printf("Connection attempt %d/%d failed: %v\n", attempt+1, maxRetries+1, err)
			fmt.Printf("Retrying in %v...\n", delay)
			time.Sleep(delay)
		}
	}
	
	// Provide specific error message based on failure type
	if strings.Contains(lastErr.Error(), "connection refused") {
		return nil, fmt.Errorf("connection failed after %d attempts: server refused connection to %s\nTip: Make sure the server is running and check the port number", maxRetries+1, addr)
	} else if strings.Contains(lastErr.Error(), "timeout") || strings.Contains(lastErr.Error(), "deadline exceeded") {
		return nil, fmt.Errorf("connection failed after %d attempts: connection timeout to %s\nTip: Check your network connection and firewall settings", maxRetries+1, addr)
	} else if strings.Contains(lastErr.Error(), "no such host") {
		return nil, fmt.Errorf("connection failed after %d attempts: cannot resolve hostname in %s\nTip: Check the server address", maxRetries+1, addr)
	} else {
		return nil, fmt.Errorf("connection failed after %d attempts: %v", maxRetries+1, lastErr)
	}
}

// tryConnectAsClient attempts to join a room as a client
func tryConnectAsClient(addr string, roomID string, username string) bool {
	// Add client logging
	fmt.Printf("Attempting to join room as user %s\n", username)
	fmt.Printf("Server address: %s\n", addr)
	
	// Create connection to server with retry logic
	conn, err := connectToServerWithRetry(addr, 2) // 2 retries = 3 total attempts (less for join attempts)
	if err != nil {
		fmt.Printf("Failed to connect to server: %v\n", err)
		return false
	}
	defer conn.Close()
	
	// Create gRPC client
	client := pb.NewKeykammerServiceClient(conn)
	
	// Call JoinRoom RPC
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Send username in join request using proper JoinRequest
	joinReq := &pb.JoinRequest{
		RoomId:   roomID,
		Username: username,
		Version:  1, // Protocol version
	}
	
	resp, err := client.JoinRoom(ctx, joinReq)
	if err != nil {
		fmt.Printf("Failed to join room: %v\n", err)
		return false
	}
	
	if resp.Success {
		fmt.Printf("Successfully joined room %s\n", roomID[:16]+"...")
		if resp.ClientCount > 0 {
			fmt.Printf("Room participants: %d users connected\n", resp.ClientCount)
		}
		return true
	} else {
		// Provide specific error messages based on the failure reason
		if strings.Contains(resp.Message, "full") || strings.Contains(resp.Message, "capacity") {
			fmt.Printf("Room is full: %s\n", resp.Message)
			fmt.Printf("  Tip: Try again later or use a different keyfile to create a new room\n")
		} else if strings.Contains(resp.Message, "taken") {
			fmt.Printf("Username already taken: %s\n", resp.Message)
			if len(resp.TakenUsernames) > 0 {
				fmt.Printf("  Taken usernames: %v\n", resp.TakenUsernames)
			}
			fmt.Printf("  Tip: Try a different username\n")
		} else if strings.Contains(resp.Message, "Invalid room") {
			fmt.Printf("Wrong room: %s\n", resp.Message)
			fmt.Printf("  Tip: Make sure you're using the same keyfile as other participants\n")
		} else {
			fmt.Printf("Room join rejected: %s\n", resp.Message)
		}
		return false
	}
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