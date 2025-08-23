package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

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

// DiscoveryResponse contains the server address and metadata for a room lookup
type DiscoveryResponse struct {
	ServerAddress  string `json:"server_address"`
	CurrentUsers   int    `json:"current_users"`
	MaxUsers       int    `json:"max_users"`
	WillAutoDelete bool   `json:"will_auto_delete"`
	SlotsRemaining int    `json:"slots_remaining"`
}

// Global discovery status tracking
var currentDiscoveryStatus DiscoveryStatus = DiscoveryUnknown

// In-memory room storage for discovery server mode
var (
	discoveryRooms = make(map[string]*RoomRegistration)
	discoveryMutex = sync.RWMutex{}
)

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

// Global HTTP client with optimized settings for discovery operations
var discoveryHTTPClient *http.Client

func init() {
	// Create optimized HTTP client for discovery operations
	transport := &http.Transport{
		// Connection pooling settings
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     30 * time.Second,
		
		// Timeout settings
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		
		// Keep-alive settings
		DisableKeepAlives: false,
		
		// Compression
		DisableCompression: false,
	}
	
	discoveryHTTPClient = &http.Client{
		Transport: transport,
		Timeout:   time.Duration(DiscoveryTimeout) * time.Second,
	}
}

// createDiscoveryClient returns the optimized HTTP client for discovery operations
func createDiscoveryClient() *http.Client {
	return discoveryHTTPClient
}

// registerRoom registers a new room with the discovery server
func registerRoom(discoveryURL, roomID, serverAddr string, maxUsers int) error {
	// Validate inputs
	if err := validateRequired(map[string]string{
		"discoveryURL": discoveryURL,
		"roomID":       roomID,
		"serverAddr":   serverAddr,
	}); err != nil {
		return err
	}

	client := createDiscoveryClient()

	registration := RoomRegistration{
		RoomID:        roomID,
		ServerAddress: serverAddr,
		MaxUsers:      maxUsers,
		CurrentUsers:  1, // Starting with 1 user (the creator)
	}

	jsonData, err := json.Marshal(registration)
	if err != nil {
		return ConfigError("failed to serialize room registration", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(DiscoveryTimeout)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", discoveryURL+"/api/rooms", bytes.NewBuffer(jsonData))
	if err != nil {
		return NetworkError("failed to create registration request", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		setDiscoveryStatus(DiscoveryDisconnected)
		return NetworkError("failed to register room with discovery server", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		setDiscoveryStatus(DiscoveryDisconnected)
		return DiscoveryError(fmt.Sprintf("registration failed with HTTP status %d", resp.StatusCode), nil)
	}

	setDiscoveryStatus(DiscoveryRoomListed)
	logDebug("Room registered successfully with discovery server")
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

// getPublicIP attempts to get the public IP address using external services
func getPublicIP() (string, error) {
	// List of public IP services to try
	services := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
	}

	client := &http.Client{Timeout: 5 * time.Second}

	for _, service := range services {
		resp, err := client.Get(service)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				continue
			}

			ip := strings.TrimSpace(string(body))
			if ip != "" {
				return ip, nil
			}
		}
	}

	return "", fmt.Errorf("failed to get public IP from any service")
}

// registerWithDiscovery registers a new room with the discovery server using KeyInfo
func registerWithDiscovery(keyInfo *KeyInfo, discoveryURL string, port int, maxUsers int) error {
	// Get public IP address for internet-wide server registration
	publicIP, err := getPublicIP()
	if err != nil {
		fmt.Printf("Warning: Could not get public IP (%v), using localhost (local network only)\n", err)
		publicIP = "127.0.0.1"
	}

	serverAddr := fmt.Sprintf("%s:%d", publicIP, port)

	regErr := registerRoom(discoveryURL, keyInfo.RoomID, serverAddr, maxUsers)
	if regErr != nil {
		return fmt.Errorf("failed to register with discovery server: %v", regErr)
	}

	// Room registered silently for maximum privacy
	return nil
}

// lookupRoomInDiscovery looks up an existing room via discovery server and returns server address
func lookupRoomInDiscovery(roomID, discoveryURL string) (string, error) {
	discovery, err := lookupRoom(discoveryURL, roomID)
	if err != nil {
		return "", fmt.Errorf("failed to lookup room in discovery server: %v", err)
	}

	if discovery == nil {
		// Room not found - silent for privacy
		return "", nil
	}

	// Room found - silent for privacy

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
	// Room deleted successfully - no output to avoid TUI corruption
	return nil
}

// triggerAutoDelete removes room from discovery when capacity is reached
func triggerAutoDelete(roomID, discoveryURL string, currentUsers, maxUsers int) error {
	if maxUsers > 0 && currentUsers >= maxUsers {
		// Room at capacity - delete from discovery (no output to avoid TUI corruption)
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
			// Operation succeeded - silent for privacy
			return nil
		}

		lastErr = err
		if attempt < maxRetries {
			delay := time.Duration(DiscoveryRetryDelay*(1<<attempt)) * time.Second
			// Retry silently for privacy
			time.Sleep(delay)
		}
	}

	// All retries failed - silent for privacy
	return fmt.Errorf("operation failed after %d retries: %v", maxRetries+1, lastErr)
}

// checkDiscoveryHealth performs a health check on the discovery server
func checkDiscoveryHealth(discoveryURL string) error {
	client := createDiscoveryClient()

	resp, err := client.Get(discoveryURL + "/health")
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
func checkDiscoveryAndFallback(discoveryURL string, port int) bool {

	if isDiscoveryServerAvailable(discoveryURL) {
		fmt.Printf("Discovery server available: %s\n", discoveryURL)
		return true
	}

	fmt.Printf("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!\n")
	fmt.Printf("Discovery server unavailable: %s\n", discoveryURL)
	fmt.Printf("Falling back to direct connection mode\n")
	fmt.Printf("  Note: Only users with your IP address will be able to connect\n")
	fmt.Printf("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!\n")

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
		// Room validation failed - silent for privacy
		return false, err
	}

	// Room has space - silent for privacy
	return true, nil
}

// Discovery Server HTTP Handlers

// runDiscoveryServer starts the HTTP discovery server with graceful shutdown
func runDiscoveryServer(port int) error {
	// Create HTTP server with explicit configuration
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/rooms", handleRooms)
	mux.HandleFunc("/api/rooms/", handleSpecificRoom)

	addr := fmt.Sprintf(":%d", port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
		// Production timeouts
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Register graceful shutdown for HTTP server
	RegisterShutdownCallback(func() error {
		logDebug("Shutting down discovery HTTP server")
		
		// Create context with timeout for shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		return server.Shutdown(ctx)
	})

	logInfo("Discovery server listening on %s", addr)
	logInfo("Endpoints:")
	logInfo("  GET  /health        - Health check")
	logInfo("  POST /api/rooms     - Register room")
	logInfo("  GET  /api/rooms/{id} - Lookup room")
	logInfo("  DELETE /api/rooms/{id} - Delete room")

	err := server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// handleHealth provides a simple health check endpoint
func handleHealth(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers for website status checking
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type")
	
	// Handle preflight requests
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	
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

	// Room registration processed silently for maximum privacy

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

	// Room lookup processed silently for maximum privacy

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

	// Room deletion processed silently for maximum privacy

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}
