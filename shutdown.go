package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// GracefulShutdown manages coordinated shutdown of all application components
type GracefulShutdown struct {
	mu        sync.RWMutex
	callbacks []func() error
	done      chan struct{}
	started   bool
	timeout   time.Duration
}

// Global shutdown manager
var shutdownManager *GracefulShutdown

func init() {
	shutdownManager = &GracefulShutdown{
		done:    make(chan struct{}),
		timeout: 30 * time.Second, // 30 second timeout for graceful shutdown
	}
}

// RegisterShutdownCallback adds a callback to be executed during shutdown
func RegisterShutdownCallback(callback func() error) {
	shutdownManager.mu.Lock()
	defer shutdownManager.mu.Unlock()
	
	shutdownManager.callbacks = append(shutdownManager.callbacks, callback)
}

// StartGracefulShutdownHandler begins listening for shutdown signals
func StartGracefulShutdownHandler() {
	shutdownManager.mu.Lock()
	if shutdownManager.started {
		shutdownManager.mu.Unlock()
		return // Already started
	}
	shutdownManager.started = true
	shutdownManager.mu.Unlock()
	
	go func() {
		sigChan := make(chan os.Signal, 1)
		
		// Listen for multiple shutdown signals
		signal.Notify(sigChan, 
			os.Interrupt,    // SIGINT (Ctrl+C)
			syscall.SIGTERM, // SIGTERM (systemd/docker stop)
			syscall.SIGQUIT, // SIGQUIT
		)
		
		sig := <-sigChan
		logInfo("Received shutdown signal: %v", sig)
		
		// Perform graceful shutdown
		err := performGracefulShutdown()
		if err != nil {
			logError("Error during graceful shutdown: %v", err)
			os.Exit(1)
		}
		
		logInfo("Graceful shutdown completed successfully")
		os.Exit(0)
	}()
}

// performGracefulShutdown executes all registered shutdown callbacks with timeout
func performGracefulShutdown() error {
	shutdownManager.mu.RLock()
	callbacks := make([]func() error, len(shutdownManager.callbacks))
	copy(callbacks, shutdownManager.callbacks)
	shutdownManager.mu.RUnlock()
	
	// Create context with timeout for shutdown operations
	ctx, cancel := context.WithTimeout(context.Background(), shutdownManager.timeout)
	defer cancel()
	
	// Channel to signal completion of all callbacks
	done := make(chan error, 1)
	
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logError("Panic during shutdown: %v", r)
				done <- nil // Continue with shutdown even if panic occurs
			}
		}()
		
		logInfo("Executing %d shutdown callbacks", len(callbacks))
		
		// Execute callbacks in reverse order (LIFO)
		for i := len(callbacks) - 1; i >= 0; i-- {
			callback := callbacks[i]
			if callback != nil {
				logDebug("Executing shutdown callback %d", len(callbacks)-i)
				if err := callback(); err != nil {
					logWarn("Shutdown callback failed: %v", err)
					// Continue with other callbacks even if one fails
				}
			}
		}
		
		// Cleanup resources
		cleanupResources()
		
		done <- nil
	}()
	
	// Wait for completion or timeout
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		logWarn("Graceful shutdown timed out after %v, forcing exit", shutdownManager.timeout)
		return ctx.Err()
	}
}

// TriggerShutdown manually triggers a graceful shutdown (for testing or programmatic shutdown)
func TriggerShutdown() error {
	close(shutdownManager.done)
	return performGracefulShutdown()
}

// IsShuttingDown returns true if shutdown has been initiated
func IsShuttingDown() bool {
	select {
	case <-shutdownManager.done:
		return true
	default:
		return false
	}
}

// SetShutdownTimeout configures the maximum time to wait for graceful shutdown
func SetShutdownTimeout(timeout time.Duration) {
	shutdownManager.mu.Lock()
	defer shutdownManager.mu.Unlock()
	shutdownManager.timeout = timeout
}

// Production-specific shutdown callbacks

// registerServerShutdown registers cleanup for gRPC server
func registerServerShutdown(server interface{}) {
	RegisterShutdownCallback(func() error {
		logDebug("Shutting down gRPC server")
		
		// Type assertion for different server types
		switch s := server.(type) {
		case interface{ GracefulStop() }:
			// Give graceful stop a chance
			done := make(chan struct{})
			go func() {
				s.GracefulStop()
				close(done)
			}()
			
			// Wait up to 5 seconds for graceful stop
			select {
			case <-done:
				logDebug("gRPC server stopped gracefully")
			case <-time.After(5 * time.Second):
				logWarn("gRPC server graceful stop timed out")
				if forceStop, ok := s.(interface{ Stop() }); ok {
					forceStop.Stop()
				}
			}
		}
		return nil
	})
}

// registerDiscoveryCleanup registers cleanup for discovery server registration
func registerDiscoveryCleanup(roomID, discoveryURL string) {
	if discoveryURL == "" {
		return
	}
	
	RegisterShutdownCallback(func() error {
		logDebug("Cleaning up discovery server registration")
		
		// Use shorter timeout for discovery cleanup during shutdown
		err := deleteRoomFromDiscoveryWithRetry(roomID, discoveryURL, 1)
		if err != nil {
			logWarn("Failed to cleanup discovery registration: %v", err)
		} else {
			logDebug("Discovery registration cleaned up successfully")
		}
		return nil // Don't fail shutdown even if discovery cleanup fails
	})
}

// registerTUICleanup registers cleanup for TUI components
func registerTUICleanup() {
	RegisterShutdownCallback(func() error {
		logDebug("Shutting down TUI")
		
		// Cleanup TUI components if they exist
		if app != nil {
			app.Stop()
		}
		return nil
	})
}

// registerClientCleanup registers cleanup for client connections
func registerClientCleanup(cleanup func()) {
	if cleanup == nil {
		return
	}
	
	RegisterShutdownCallback(func() error {
		logDebug("Cleaning up client resources")
		cleanup()
		return nil
	})
}