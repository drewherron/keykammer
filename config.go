package main

const (
	MaxKeyFileSize = 20 * 1024 * 1024 // 20MB
	// KeyDerivationSalt must remain constant for compatibility across all clients
	KeyDerivationSalt = "keykammer-v1-salt"
	// DefaultPort is the default port for keykammer servers
	DefaultPort = 76667
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