package main

import (
	"bytes"
	"context"
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

// KeyInfo bundles room ID and encryption key derived from file content
type KeyInfo struct {
	RoomID        string
	EncryptionKey []byte
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

// isDiscoveryServerAvailable tests if the discovery server is reachable
func isDiscoveryServerAvailable(discoveryURL string) bool {
	client := createDiscoveryClient()
	
	req, err := http.NewRequest("HEAD", discoveryURL+"/api/health", nil)
	if err != nil {
		setDiscoveryStatus(DiscoveryDisconnected)
		return false
	}
	
	resp, err := client.Do(req)
	if err != nil {
		setDiscoveryStatus(DiscoveryDisconnected)
		return false
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == http.StatusOK {
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
	
	fmt.Printf("✓ Room registered with discovery server\n")
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
		fmt.Printf("✗ Room not found in discovery server\n")
		return "", nil
	}
	
	fmt.Printf("✓ Found existing room in discovery server\n")
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
	fmt.Printf("✓ Room deleted from discovery server\n")
	fmt.Printf("  Room ID: %s\n", roomID[:16]+"...")
	fmt.Printf("  Status: Room is now private and invisible\n")
	
	return nil
}

// triggerAutoDelete removes room from discovery when capacity is reached
func triggerAutoDelete(roomID, discoveryURL string, currentUsers, maxUsers int) error {
	if maxUsers > 0 && currentUsers >= maxUsers {
		fmt.Printf("🔒 Room capacity reached (%d/%d) - triggering auto-delete\n", currentUsers, maxUsers)
		return deleteRoomFromDiscovery(roomID, discoveryURL)
	}
	return nil
}

// checkDiscoveryAndFallback tests discovery server availability and logs fallback mode
func checkDiscoveryAndFallback(discoveryURL string) bool {
	fmt.Printf("Testing discovery server availability...\n")
	
	if isDiscoveryServerAvailable(discoveryURL) {
		fmt.Printf("✓ Discovery server available: %s\n", discoveryURL)
		return true
	}
	
	fmt.Printf("✗ Discovery server unavailable: %s\n", discoveryURL)
	fmt.Printf("⚠ Falling back to localhost-only mode\n")
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
		fmt.Printf("✗ Cannot join room %s: %v\n", roomID[:16]+"...", err)
		return false, err
	}
	
	if max > 0 {
		remaining := max - current
		fmt.Printf("✓ Room has space: %d/%d users (%d slots remaining)\n", current, max, remaining)
	} else {
		fmt.Printf("✓ Room has unlimited capacity (current: %d users)\n", current)
	}
	
	return true, nil
}

// deriveKeyInfo derives both room ID and encryption key from file content and password
func deriveKeyInfo(fileContent []byte, password string) (*KeyInfo, error) {
	roomID := deriveRoomID(fileContent, password)
	encryptionKey, err := deriveEncryptionKey(fileContent, password)
	if err != nil {
		return nil, err
	}
	
	return &KeyInfo{
		RoomID:        roomID,
		EncryptionKey: encryptionKey,
	}, nil
}

// Server implementation
type server struct {
	pb.UnimplementedChatServiceServer
}

func (s *server) SendMessage(ctx context.Context, req *pb.ChatMessage) (*pb.ChatResponse, error) {
	fmt.Printf("Received message: %s\n", req.Content)
	return &pb.ChatResponse{Success: true}, nil
}

func runServer() {
	lis, err := net.Listen("tcp", ":9999")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterChatServiceServer(s, &server{})

	fmt.Println("Server listening on :9999")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func runClient() {
	conn, err := grpc.Dial("localhost:9999", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewChatServiceClient(conn)

	// Send a single test message
	resp, err := client.SendMessage(context.Background(), &pb.ChatMessage{
		Content: "Hello from client!",
	})
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	fmt.Printf("Message sent successfully: %v\n", resp.Success)
}

func main() {
	serverMode := flag.Bool("server", false, "Run in server mode")
	keyfile := flag.String("keyfile", "", "Path to key file (required)")
	port := flag.Int("port", DefaultPort, "Port to use (default: 76667)")
	password := flag.String("password", "", "Optional password for server derivation (empty uses keyfile only)")
	size := flag.Int("size", 2, "Maximum users per room (default: 2 for maximum privacy, 0 = unlimited)")
	discoveryServer := flag.String("discovery-server", DefaultDiscoveryServer, "Discovery server URL")
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

	// Read keyfile and derive key info
	fileContent, err := readFile(*keyfile)
	if err != nil {
		fmt.Printf("Error reading keyfile: %v\n", err)
		os.Exit(1)
	}

	keyInfo, err := deriveKeyInfo(fileContent, *password)
	if err != nil {
		fmt.Printf("Error deriving key info: %v\n", err)
		os.Exit(1)
	}

	// Print derived values and exit (temporary)
	fmt.Printf("Room ID: %s\n", keyInfo.RoomID[:16])
	fmt.Printf("Key length: %d bytes\n", len(keyInfo.EncryptionKey))
	fmt.Printf("Port: %d\n", *port)
	if *size == 0 {
		fmt.Printf("Room size: unlimited\n")
	} else {
		fmt.Printf("Room size: %d users max\n", *size)
	}
	fmt.Printf("Discovery server: %s\n", *discoveryServer)
	serverAddr := deriveLocalServerAddress(fileContent, *password, *port)
	fmt.Printf("Server address: %s\n", serverAddr)
	os.Exit(0)

	if *serverMode {
		runServer()
	} else {
		runClient()
	}
}

