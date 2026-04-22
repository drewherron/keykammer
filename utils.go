package main

import (
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

	// Note: No file size limit - any file can be used as a keyfile
	// Large files will take longer to process but provide the same security
	if size > 5*1024*1024*1024 { // 5GB warning threshold
		fmt.Printf("Warning: A large keyfile (%d bytes) will take longer to process...\n", size)
	}

	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read file: %v", err)
	}

	return content, nil
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

