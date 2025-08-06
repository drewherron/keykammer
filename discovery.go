package main

import (
	"bytes"
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
	ServerAddress string `json:"server_address"`
	CurrentUsers  int    `json:"current_users"`
	MaxUsers      int    `json:"max_users"`
	WillAutoDelete bool  `json:"will_auto_delete"`
	SlotsRemaining int   `json:"slots_remaining"`
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
	if discovery.MaxUsers > 0 {
		fmt.Printf("  Capacity: %d/%d users (%d slots remaining)\n", 
			discovery.CurrentUsers, discovery.MaxUsers, discovery.SlotsRemaining)
	} else {
		fmt.Printf("  Capacity: %d users (unlimited)\n", discovery.CurrentUsers)
	}
	
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
func checkDiscoveryAndFallback(discoveryURL string) bool {
	fmt.Printf("Testing discovery server availability...\n")
	
	if isDiscoveryServerAvailable(discoveryURL) {
		fmt.Printf("Discovery server available: %s\n", discoveryURL)
		return true
	}
	
	fmt.Printf("Discovery server unavailable: %s\n", discoveryURL)
	fmt.Printf("Falling back to localhost-only mode\n")
	fmt.Printf("  Note: Only connections on this computer will work\n")
	fmt.Printf("  Note: To chat across the internet, ensure discovery server is running\n")
	fmt.Printf("  Note: You can run your own discovery server with: keykammer -discovery-server-mode\n")
	
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

// Discovery Server HTTP Handlers

// runDiscoveryServer starts the HTTP discovery server
func runDiscoveryServer(port int) error {
	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/api/rooms", handleRooms)
	http.HandleFunc("/api/rooms/", handleSpecificRoom)
	
	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("Discovery server listening on %s\n", addr)
	fmt.Printf("Endpoints:\n")
	fmt.Printf("  GET  /health        - Health check\n")
	fmt.Printf("  POST /api/rooms     - Register room\n")
	fmt.Printf("  GET  /api/rooms/{id} - Lookup room\n")
	fmt.Printf("  DELETE /api/rooms/{id} - Delete room\n")
	
	return http.ListenAndServe(addr, nil)
}

// handleHealth provides a simple health check endpoint
func handleHealth(w http.ResponseWriter, r *http.Request) {
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
	
	fmt.Printf("Registered room %s at %s (%d/%d users)\n", 
		registration.RoomID[:16]+"...", registration.ServerAddress, 
		registration.CurrentUsers, registration.MaxUsers)
	
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
	
	fmt.Printf("Room lookup: %s -> %s (%d/%d users)\n", 
		roomID[:16]+"...", room.ServerAddress, room.CurrentUsers, room.MaxUsers)
	
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
	
	fmt.Printf("Deleted room %s from discovery\n", roomID[:16]+"...")
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}