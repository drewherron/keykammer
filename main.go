package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"

	pb "keykammer/proto"
)

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
	flag.Parse()

	if *serverMode {
		runServer()
	} else {
		runClient()
	}
}

