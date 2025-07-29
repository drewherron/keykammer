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

// Server implementation
type server struct {
	pb.UnimplementedChatServiceServer
	roomID  string
	port    int
	clients map[string]interface{}
	mutex   sync.RWMutex
}

// newServer creates a new server instance
func newServer(roomID string, port int) *server {
	return &server{
		roomID:  roomID,
		port:    port,
		clients: make(map[string]interface{}),
	}
}

func (s *server) SendMessage(ctx context.Context, req *pb.ChatMessage) (*pb.ChatResponse, error) {
	fmt.Printf("Received message: %s\n", req.Content)
	return &pb.ChatResponse{Success: true}, nil
}

// JoinRoom handles client room join requests (stub implementation)
func (s *server) JoinRoom(ctx context.Context, req *pb.ChatMessage) (*pb.ChatResponse, error) {
	fmt.Printf("Client attempting to join room: %s\n", req.Content)
	
	// Step 31: Room ID validation
	if req.Content != s.roomID {
		fmt.Printf("Room ID mismatch: expected %s, got %s\n", s.roomID[:16]+"...", req.Content[:16]+"...")
		return &pb.ChatResponse{Success: false}, nil
	}
	
	// Step 44: Add client to tracking and count clients
	clientID := generateClientID()
	
	s.mutex.Lock()
	s.clients[clientID] = struct{}{} // Add client to tracking
	clientCount := len(s.clients)
	s.mutex.Unlock()
	
	fmt.Printf("Client %s successfully joined room (total clients: %d)\n", clientID[:8], clientCount)
	return &pb.ChatResponse{Success: true}, nil
}

// runServer starts a gRPC server for the specified room and port (Steps 32-35)
func runServer(roomID string, port int) {
	// Step 33: Server startup logging
	fmt.Printf("Starting server on port %d for room %s\n", port, roomID[:16]+"...")
	
	// Step 32: Create TCP listener and gRPC server
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Failed to listen on port %d: %v", port, err)
	}

	// Create server instance with room ID and port
	serverInstance := newServer(roomID, port)
	
	// Step 35: Graceful shutdown handling
	grpcServer := grpc.NewServer()
	pb.RegisterChatServiceServer(grpcServer, serverInstance)

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
	
	// Try to connect to the server and join the room
	success := tryConnectAsClient(serverAddr, roomID)
	
	if success {
		fmt.Printf("Client connected successfully to room\n")
		// In full implementation, would start chat loop here
	} else {
		fmt.Printf("Failed to connect to server or join room\n")
		os.Exit(1)
	}
}

// determineMode returns "server" if serverAddr is empty, "client" if provided
func determineMode(serverAddr string) string {
	if serverAddr == "" {
		return "server"
	}
	return "client"
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
func tryConnectAsClient(addr string, roomID string) bool {
	// Step 38: Add client logging
	fmt.Printf("Attempting to connect to %s\n", addr)
	
	// Create connection to server
	conn, err := connectToServer(addr)
	if err != nil {
		fmt.Printf("Failed to connect to server: %v\n", err)
		return false
	}
	defer conn.Close()
	
	// Create gRPC client
	client := pb.NewChatServiceClient(conn)
	
	// Call JoinRoom RPC (using ChatMessage as request type due to proto limitations)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Use the roomID as content since we don't have proper JoinRequest type yet
	resp, err := client.SendMessage(ctx, &pb.ChatMessage{Content: roomID})
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
	serverMode := flag.Bool("server", false, "Run in server mode")
	keyfile := flag.String("keyfile", "", "Path to key file (required)")
	port := flag.Int("port", DefaultPort, "Port to use")
	password := flag.String("password", "", "Optional password for server derivation (empty uses keyfile only)")
	size := flag.Int("size", 2, "Maximum users per room (2 for maximum privacy, 0 = unlimited)")
	discoveryServer := flag.String("discovery-server", DefaultDiscoveryServer, "Discovery server URL")
	discoveryMode := flag.String("discovery-mode", "auto", "Discovery mode: auto, discovery-only, local-only")
	flag.Parse()

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

	// Validate discovery mode parameter
	switch *discoveryMode {
	case "auto", "discovery-only", "local-only":
		// Valid modes
	default:
		fmt.Printf("Error: invalid discovery mode '%s'. Must be one of: auto, discovery-only, local-only\n", *discoveryMode)
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
	
	// Determine operation mode
	var serverAddr string
	if *serverMode {
		serverAddr = ""
	} else {
		serverAddr = "some-address" // Will be determined later
	}
	mode := determineMode(serverAddr)
	fmt.Printf("Operation mode: %s\n", mode)
	fmt.Printf("\n")
	
	// Check discovery mode and server availability
	fmt.Printf("Discovery server: %s\n", *discoveryServer)
	fmt.Printf("Discovery mode: %s\n", *discoveryMode)
	
	var discoveryAvailable bool
	
	switch *discoveryMode {
	case "local-only":
		fmt.Printf("Local-only mode selected, skipping discovery server\n")
		discoveryAvailable = false
	case "discovery-only":
		fmt.Printf("Discovery-only mode selected, discovery server required\n")
		discoveryAvailable = checkDiscoveryAndFallback(*discoveryServer)
		if !discoveryAvailable {
			fmt.Printf("Error: Discovery server required but unavailable\n")
			os.Exit(1)
		}
	case "auto":
		discoveryAvailable = checkDiscoveryAndFallback(*discoveryServer)
	}
	
	if discoveryAvailable {
		// Try room lookup before creating new room
		fmt.Printf("\nLooking for existing room...\n")
		existingServerAddr, err := lookupRoomInDiscoveryWithRetry(keyInfo.RoomID, *discoveryServer, DefaultMaxRetries)
		
		if err != nil {
			fmt.Printf("Error during room lookup: %v\n", err)
			fmt.Printf("Proceeding to create new room...\n")
		} else if existingServerAddr != "" {
			// Step 40: Connect to existing room as client
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
				// Step 34: Start server when in server mode
				if mode == "server" {
					runServer(keyInfo.RoomID, *port)
					return
				}
			}
		}
	} else {
		// Operating in localhost-only mode
		fmt.Printf("\nOperating in localhost-only mode\n")
		serverAddr := deriveLocalServerAddress(fileContent, *password, *port)
		fmt.Printf("Server address: %s\n", serverAddr)
		
		// Step 40: Check for existing local server first
		if !*serverMode && isServerRunning(*port) {
			fmt.Printf("Found existing server on localhost:%d, connecting as client\n", *port)
			runClient(fmt.Sprintf("localhost:%d", *port), keyInfo.RoomID)
			return
		}
		
		// Start server if in server mode or no existing server found
		if mode == "server" || !*serverMode {
			runServer(keyInfo.RoomID, *port)
			return
		}
	}
	
	fmt.Printf("\nDiscovery flow complete. Client mode not yet implemented.\n")
}

