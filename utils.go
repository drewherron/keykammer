package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"time"
)

// getFileSize returns the size of a file in bytes
func getFileSize(path string) (int64, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return stat.Size(), nil
}

// readFile reads a keyfile into memory with size validation
func readFile(path string) ([]byte, error) {
	// Check file size first
	size, err := getFileSize(path)
	if err != nil {
		return nil, fmt.Errorf("cannot access file: %v", err)
	}
	
	if size > MaxKeyFileSize {
		return nil, fmt.Errorf("file too large: %d bytes (maximum %d bytes)", size, MaxKeyFileSize)
	}
	
	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read file: %v", err)
	}
	
	return content, nil
}

// generateClientID creates a unique identifier for each client
func generateClientID() string {
	// Generate 8 random bytes (will result in 16 hex characters)
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if random fails
		return fmt.Sprintf("client_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
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

// formatRoomID formats a room ID as first8...last8 digits (crypto address style)
func formatRoomID(roomID string) string {
	if len(roomID) <= 16 {
		return roomID
	}
	return fmt.Sprintf("%s...%s", roomID[:8], roomID[len(roomID)-8:])
}