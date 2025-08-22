package main

import (
	"sync"
	"time"
)

// Rate limiting for security
type RateLimiter struct {
	requests map[string][]time.Time
	mutex    sync.RWMutex
	limit    int
	window   time.Duration
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

// Allow checks if a request from the given key is allowed
func (rl *RateLimiter) Allow(key string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	
	now := time.Now()
	cutoff := now.Add(-rl.window)
	
	// Get existing requests for this key
	requests := rl.requests[key]
	
	// Remove requests outside the window
	filtered := make([]time.Time, 0, len(requests))
	for _, req := range requests {
		if req.After(cutoff) {
			filtered = append(filtered, req)
		}
	}
	
	// Check if we're under the limit
	if len(filtered) >= rl.limit {
		rl.requests[key] = filtered
		return false
	}
	
	// Add this request
	filtered = append(filtered, now)
	rl.requests[key] = filtered
	
	return true
}

// Production security enhancements
var (
	// Rate limiter for discovery operations (10 requests per minute per IP)
	discoveryRateLimiter = NewRateLimiter(10, time.Minute)
	
	// Rate limiter for room operations (5 requests per minute per room)
	roomRateLimiter = NewRateLimiter(5, time.Minute)
)

// secureString safely redacts sensitive information for logging
func secureString(s string, visibleChars int) string {
	if len(s) <= visibleChars {
		return "***"
	}
	return s[:visibleChars] + "..."
}

// secureRoomID returns a safe-to-log version of room ID
func secureRoomID(roomID string) string {
	return secureString(roomID, 8)
}

// Security validation functions
func validateRoomID(roomID string) error {
	if len(roomID) < 16 {
		return ValidationError("room ID too short", nil)
	}
	if len(roomID) > 128 {
		return ValidationError("room ID too long", nil)
	}
	return nil
}

func validateUsernameSecure(username string) error {
	if len(username) == 0 {
		return ValidationError("username cannot be empty", nil)
	}
	if len(username) > MaxUsernameLength {
		return ValidationError("username too long", nil)
	}
	// Check for control characters
	for _, r := range username {
		if r < 32 || r == 127 {
			return ValidationError("username contains invalid characters", nil)
		}
	}
	return nil
}

// Production hardening constants
const (
	MaxMessageSize    = 4096  // 4KB max message size
	MaxUsernameLength = 50    // 50 character max username
	MaxRoomUsers      = 100   // 100 user max per room for DoS protection
)