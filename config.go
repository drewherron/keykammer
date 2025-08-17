package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	// Connection settings
	Port            int    `yaml:"port" json:"port"`
	DiscoveryServer string `yaml:"discovery_server" json:"discovery_server"`
	ConnectDirect   string `yaml:"connect_direct" json:"connect_direct"`
	
	// File and security settings
	Keyfile  string `yaml:"keyfile" json:"keyfile"`
	Password string `yaml:"password" json:"password"`
	
	// Room settings
	MaxUsers int `yaml:"max_users" json:"max_users"`
	
	// Server mode settings
	DiscoveryServerMode bool `yaml:"discovery_server_mode" json:"discovery_server_mode"`
	
	// Application settings
	ConfigFile string `yaml:"-" json:"-"` // Not serialized, used internally
}

// NewDefaultConfig returns a Config with default values
func NewDefaultConfig() *Config {
	return &Config{
		Port:                DefaultPort,
		DiscoveryServer:     DefaultDiscoveryServer,
		ConnectDirect:       "",
		Keyfile:             "",
		Password:            "",
		MaxUsers:            2,
		DiscoveryServerMode: false,
		ConfigFile:          "",
	}
}

// LoadConfig loads configuration from file and returns a Config struct
func LoadConfig(configPath string) (*Config, error) {
	config := NewDefaultConfig()
	
	// If no config path specified, try default locations
	if configPath == "" {
		// Try current directory first
		if _, err := os.Stat("keykammer.yaml"); err == nil {
			configPath = "keykammer.yaml"
		} else if _, err := os.Stat("keykammer.yml"); err == nil {
			configPath = "keykammer.yml"
		} else {
			// No config file found, return default config
			return config, nil
		}
	}
	
	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", configPath)
	}
	
	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}
	
	// Parse YAML
	err = yaml.Unmarshal(data, config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	
	config.ConfigFile = configPath
	return config, nil
}

// SaveConfig saves the current configuration to a YAML file
func (c *Config) SaveConfig(configPath string) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}
	
	// Marshal to YAML
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}
	
	// Write to file
	err = os.WriteFile(configPath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}
	
	return nil
}

const (
	// KeyDerivationSalt must remain constant for compatibility across all clients
	KeyDerivationSalt = "keykammer-v1-salt"
	// DefaultPort is the default port for keykammer servers (53952 spells "KEYKA")
	DefaultPort = 53952
	// DefaultDiscoveryServer is the default discovery server endpoint
	DefaultDiscoveryServer = "https://discovery.keykammer.com"
	// Discovery server timeout constants
	DiscoveryTimeout = 10 // seconds
	DiscoveryRetryDelay = 2 // seconds
	DefaultMaxRetries = 3 // number of retry attempts
	// AES key size for AES-256
	AESKeySize = 32 // 32 bytes for AES-256
	// TUI configuration
	MaxChatHistory = 1000 // Maximum number of chat messages to keep in memory
)

// KeyInfo bundles room ID and encryption key derived from file content
type KeyInfo struct {
	RoomID        string
	EncryptionKey []byte
	MaxUsers      int // Maximum users for this room (0 = unlimited)
	DiscoveryStatus DiscoveryStatus
}

// DiscoveryStatus represents the current status of discovery server communication
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