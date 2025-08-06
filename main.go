package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	// Command line argument parsing
	keyfile := flag.String("keyfile", "", "Path to key file (required)")
	port := flag.Int("port", DefaultPort, "Port to use (default: 76667)")
	password := flag.String("password", "", "Optional password for key derivation (empty uses keyfile only)")
	size := flag.Int("size", 2, "Maximum users per room (default: 2 for maximum privacy)")
	discoveryServer := flag.String("discovery-server", DefaultDiscoveryServer, "Discovery server URL")
	discoveryServerMode := flag.Bool("discovery-server-mode", false, "Run as HTTP discovery server")
	connectDirect := flag.String("connect", "", "Connect directly to server at IP:PORT (bypasses discovery)")
	flag.Parse()
	
	// Handle discovery server mode
	if *discoveryServerMode {
		fmt.Printf("Starting in discovery server mode on port %d\n", *port)
		err := runDiscoveryServer(*port)
		if err != nil {
			fmt.Printf("Discovery server error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	
	// Validate required arguments
	if *keyfile == "" {
		fmt.Println("Error: -keyfile is required")
		flag.Usage()
		os.Exit(1)
	}
	
	// Validate connect flag format if provided
	if *connectDirect != "" {
		if !strings.Contains(*connectDirect, ":") {
			fmt.Printf("Error: -connect must be in IP:PORT format (got: %s)\n", *connectDirect)
			fmt.Printf("Example: -connect 192.168.1.100:76667\n")
			os.Exit(1)
		}
	}
	
	// Validate size parameter
	if *size < 0 {
		fmt.Println("Error: room size must be >= 0 (0 means unlimited)")
		os.Exit(1)
	}
	
	// Read and process keyfile
	fileContent, err := readFile(*keyfile)
	if err != nil {
		fmt.Printf("Error reading keyfile: %v\n", err)
		os.Exit(1)
	}
	
	// Derive room ID and encryption key from file content
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
	fmt.Printf("\n")
	
	// Handle direct connection mode
	if *connectDirect != "" {
		fmt.Printf("Direct connection mode: %s\n", *connectDirect)
		fmt.Printf("Bypassing discovery server, connecting directly...\n")
		runClient(*connectDirect, keyInfo.RoomID, keyInfo.EncryptionKey)
		return
	}
	
	// Check discovery server availability
	fmt.Printf("Discovery server: %s\n", *discoveryServer)
	fmt.Printf("Checking discovery server status...\n")
	
	if checkDiscoveryAndFallback(*discoveryServer) {
		// Discovery server is available - check for existing rooms
		existingServerAddr, err := lookupRoomInDiscoveryWithRetry(keyInfo.RoomID, *discoveryServer, DefaultMaxRetries)
		if err != nil {
			fmt.Printf("Room lookup failed: %v\n", err)
			if strings.Contains(err.Error(), "timeout") {
				fmt.Printf("  Tip: Discovery server may be slow. Try again or use localhost mode\n")
			} else if strings.Contains(err.Error(), "connection") {
				fmt.Printf("  Tip: Cannot reach discovery server. Proceeding with new room creation\n")
			}
			
			// Fallback to creating new room
			serverAddr := deriveLocalServerAddress(fileContent, *password, *port)
			fmt.Printf("Server will start at: %s\n", serverAddr)
			
			// Try to register with discovery server (may fail, that's ok)
			err = registerWithDiscoveryWithRetry(keyInfo, *discoveryServer, *port, keyInfo.MaxUsers, DefaultMaxRetries)
			if err != nil {
				fmt.Printf("Failed to register room: %v\n", err)
				fmt.Printf("Falling back to localhost-only mode\n")
			}
			
			runServer(keyInfo.RoomID, *port, keyInfo.MaxUsers)
		} else if existingServerAddr != "" {
			// Connect to existing room as client
			fmt.Printf("\nExisting room found! Connecting as client to %s\n", existingServerAddr)
			runClient(existingServerAddr, keyInfo.RoomID, keyInfo.EncryptionKey)
			return
		} else {
			// Register room if lookup fails (new room)
			fmt.Printf("\nNo existing room found. Creating new room...\n")
			
			serverAddr := deriveLocalServerAddress(fileContent, *password, *port)
			fmt.Printf("Server address: %s\n", serverAddr)
			
			err = registerWithDiscoveryWithRetry(keyInfo, *discoveryServer, *port, keyInfo.MaxUsers, DefaultMaxRetries)
			if err != nil {
				fmt.Printf("Failed to register room: %v\n", err)
				fmt.Printf("Starting in localhost-only mode\n")
			}
			
			runServer(keyInfo.RoomID, *port, keyInfo.MaxUsers)
		}
	} else {
		// Discovery server unavailable - fall back to localhost mode
		
		// Check for existing local server first
		if isServerRunning(*port) {
			fmt.Printf("Found existing server on localhost:%d, connecting as client\n", *port)
			runClient(fmt.Sprintf("localhost:%d", *port), keyInfo.RoomID, keyInfo.EncryptionKey)
			return
		}
		
		// Start new server on localhost
		fmt.Printf("Starting new server on localhost:%d\n", *port)
		runServer(keyInfo.RoomID, *port, keyInfo.MaxUsers)
	}
}