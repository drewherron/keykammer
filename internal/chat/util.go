package chat

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// generateClientID creates a unique identifier for each client
func generateClientID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("client_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// formatRoomID formats a room ID as first8...last8 digits (crypto address style)
func formatRoomID(roomID string) string {
	if len(roomID) <= 16 {
		return roomID
	}
	return fmt.Sprintf("%s...%s", roomID[:8], roomID[len(roomID)-8:])
}
