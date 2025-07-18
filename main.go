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

// deriveEncryptionKey derives a 32-byte encryption key from file content using HKDF
func deriveEncryptionKey(fileContent []byte) ([]byte, error) {
	salt := []byte(KeyDerivationSalt)
	info := []byte("keykammer-encryption-key")
	
	hkdf := hkdf.New(sha256.New, fileContent, salt, info)
	key := make([]byte, 32) // 32 bytes for AES-256
	
	_, err := io.ReadFull(hkdf, key)
	if err != nil {
		return nil, err
	}
	
	return key, nil
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
	flag.Parse()

	if *keyfile == "" {
		fmt.Println("Error: -keyfile is required")
		flag.Usage()
		os.Exit(1)
	}

	if *serverMode {
		runServer()
	} else {
		runClient()
	}
}

