package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/hkdf"
	pb "keykammer/proto"
)

// deriveEncryptionKey generates a 256-bit AES key from file content and password using HKDF
func deriveEncryptionKey(fileContent []byte, password string) ([]byte, error) {
	salt := []byte(KeyDerivationSalt)
	info := []byte("keykammer-encryption-key")
	
	// Combine file content and password as input key material
	keyMaterial := append(fileContent, []byte(password)...)
	
	hkdf := hkdf.New(sha256.New, keyMaterial, salt, info)
	key := make([]byte, 32) // 32 bytes for AES-256
	
	_, err := io.ReadFull(hkdf, key)
	if err != nil {
		return nil, err
	}
	
	return key, nil
}

// deriveRoomID generates a room ID from file content and password using salted hash
func deriveRoomID(fileContent []byte, password string) string {
	salt := []byte(KeyDerivationSalt)
	// Concatenate salt + content + password
	salted := append(salt, fileContent...)
	salted = append(salted, []byte(password)...)
	hash := sha256.Sum256(salted)
	return hex.EncodeToString(hash[:])
}

// deriveLocalServerAddress derives a consistent local server address from file content and password
func deriveLocalServerAddress(fileContent []byte, password string, port int) string {
	// For localhost, we use 127.0.0.1 with the specified port
	// Later this can be extended for distributed/internet-wide addressing
	return fmt.Sprintf("127.0.0.1:%d", port)
}

// deriveKeyInfo bundles room ID and encryption key derived from file content
func deriveKeyInfo(fileContent []byte, password string, maxUsers int) (*KeyInfo, error) {
	roomID := deriveRoomID(fileContent, password)
	encryptionKey, err := deriveEncryptionKey(fileContent, password)
	if err != nil {
		return nil, err
	}
	
	return &KeyInfo{
		RoomID:          roomID,
		EncryptionKey:   encryptionKey,
		MaxUsers:        maxUsers,
		DiscoveryStatus: getDiscoveryStatus(),
	}, nil
}

// deriveKeyInfoLegacy provides backward compatibility for existing code that doesn't specify maxUsers
func deriveKeyInfoLegacy(fileContent []byte, password string) (*KeyInfo, error) {
	return deriveKeyInfo(fileContent, password, 2) // Default to 2 users for backward compatibility
}

// encrypt encrypts plaintext using AES-256-GCM with the provided key
func encrypt(plaintext []byte, key []byte) ([]byte, error) {
	// Validate key size
	if len(key) != AESKeySize {
		return nil, fmt.Errorf("invalid key size: %d bytes (expected %d)", len(key), AESKeySize)
	}
	
	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %v", err)
	}
	
	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %v", err)
	}
	
	// Generate random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %v", err)
	}
	
	// Encrypt and authenticate
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	
	return ciphertext, nil
}

// decrypt decrypts ciphertext using AES-256-GCM with the provided key
func decrypt(ciphertext []byte, key []byte) ([]byte, error) {
	// Validate key size
	if len(key) != AESKeySize {
		return nil, fmt.Errorf("invalid key size: %d bytes (expected %d)", len(key), AESKeySize)
	}
	
	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %v", err)
	}
	
	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %v", err)
	}
	
	// Check minimum ciphertext length
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short: %d bytes (minimum %d)", len(ciphertext), nonceSize)
	}
	
	// Extract nonce and ciphertext
	nonce := ciphertext[:nonceSize]
	ciphertext = ciphertext[nonceSize:]
	
	// Decrypt and verify
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %v", err)
	}
	
	return plaintext, nil
}

// createEncryptedMessage creates a ChatMessage with encrypted content
func createEncryptedMessage(roomID, username, content string, key []byte) (*pb.ChatMessage, error) {
	// Encrypt the content
	encryptedContent, err := encrypt([]byte(content), key)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt message: %v", err)
	}
	
	// Create the message
	msg := &pb.ChatMessage{
		RoomId:           roomID,
		Username:         username,
		EncryptedContent: encryptedContent,
		Timestamp:        time.Now().UnixNano(),
	}
	
	return msg, nil
}

// decryptMessageContent decrypts the content of a ChatMessage
func decryptMessageContent(msg *pb.ChatMessage, key []byte) (string, error) {
	// Handle empty content (like initial validation messages)
	if len(msg.EncryptedContent) == 0 {
		return "", nil
	}
	
	// Decrypt the content
	plaintext, err := decrypt(msg.EncryptedContent, key)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt message: %v", err)
	}
	
	return string(plaintext), nil
}

// validateEncryptionKey checks if an encryption key is valid
func validateEncryptionKey(key []byte) error {
	if len(key) != AESKeySize {
		return fmt.Errorf("invalid encryption key size: %d bytes (expected %d)", len(key), AESKeySize)
	}
	return nil
}