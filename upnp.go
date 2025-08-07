package main

import (
	"fmt"
	"net"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"
)

// UPnPMapping represents an active UPnP port forwarding mapping
type UPnPMapping struct {
	ExternalPort int
	InternalPort int
	InternalIP   string
	Protocol     string
	Description  string
	client       *internetgateway2.WANIPConnection1
}

// setupUPnPPortForwarding attempts to create a UPnP port forwarding rule
func setupUPnPPortForwarding(port int, description string) (*UPnPMapping, error) {
	fmt.Printf("Attempting UPnP port forwarding setup for port %d...\n", port)
	
	// Discover UPnP devices
	clients, _, err := internetgateway2.NewWANIPConnection1Clients()
	if err != nil {
		return nil, fmt.Errorf("failed to discover UPnP devices: %v", err)
	}
	
	if len(clients) == 0 {
		return nil, fmt.Errorf("no UPnP-enabled router found")
	}
	
	// Get local IP address
	localIP, err := getLocalIP()
	if err != nil {
		return nil, fmt.Errorf("failed to get local IP: %v", err)
	}
	
	// Try the first available client
	client := clients[0]
	
	// Create port forwarding mapping
	err = client.AddPortMapping(
		"",              // Remote host (empty for any)
		uint16(port),    // External port
		"TCP",           // Protocol
		uint16(port),    // Internal port
		localIP,         // Internal client IP
		true,            // Enabled
		description,     // Description
		uint32(3600),    // Lease duration (1 hour)
	)
	
	if err != nil {
		return nil, fmt.Errorf("failed to add port mapping: %v", err)
	}
	
	fmt.Printf("✓ UPnP port forwarding enabled: %s:%d -> %s:%d\n", 
		"external", port, localIP, port)
	
	return &UPnPMapping{
		ExternalPort: port,
		InternalPort: port,
		InternalIP:   localIP,
		Protocol:     "TCP",
		Description:  description,
		client:       client,
	}, nil
}

// removeUPnPPortForwarding removes the UPnP port forwarding rule
func removeUPnPPortForwarding(mapping *UPnPMapping) error {
	if mapping == nil || mapping.client == nil {
		return nil
	}
	
	fmt.Printf("Removing UPnP port forwarding for port %d...\n", mapping.ExternalPort)
	
	err := mapping.client.DeletePortMapping(
		"",                              // Remote host (empty for any)
		uint16(mapping.ExternalPort),    // External port
		mapping.Protocol,                // Protocol
	)
	
	if err != nil {
		return fmt.Errorf("failed to remove port mapping: %v", err)
	}
	
	fmt.Printf("✓ UPnP port forwarding removed for port %d\n", mapping.ExternalPort)
	return nil
}

// getLocalIP gets the local IP address used for internet connections
func getLocalIP() (string, error) {
	// Connect to a remote address to determine which local IP we use
	conn, err := net.DialTimeout("udp", "8.8.8.8:80", 5*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// checkUPnPAvailability tests if UPnP is available on the network
func checkUPnPAvailability() bool {
	clients, _, err := internetgateway2.NewWANIPConnection1Clients()
	if err != nil {
		return false
	}
	return len(clients) > 0
}

// refreshUPnPMapping refreshes the lease on an existing mapping
func refreshUPnPMapping(mapping *UPnPMapping) error {
	if mapping == nil || mapping.client == nil {
		return fmt.Errorf("invalid mapping")
	}
	
	// Remove and re-add the mapping to refresh the lease
	err := mapping.client.DeletePortMapping(
		"",
		uint16(mapping.ExternalPort),
		mapping.Protocol,
	)
	if err != nil {
		// Ignore errors when removing (mapping might not exist)
	}
	
	// Re-add the mapping
	err = mapping.client.AddPortMapping(
		"",
		uint16(mapping.ExternalPort),
		mapping.Protocol,
		uint16(mapping.InternalPort),
		mapping.InternalIP,
		true,
		mapping.Description,
		uint32(3600),
	)
	
	return err
}