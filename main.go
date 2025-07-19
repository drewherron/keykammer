package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"

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

