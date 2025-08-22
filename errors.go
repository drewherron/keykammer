package main

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

// KeykammerError represents application-specific errors with context
type KeykammerError struct {
	Code      string    // Error code for programmatic handling
	Message   string    // Human-readable error message
	Cause     error     // Underlying error
	Context   string    // Additional context (function, operation, etc.)
	Timestamp time.Time // When the error occurred
	File      string    // Source file where error occurred
	Line      int       // Source line where error occurred
}

func (e *KeykammerError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *KeykammerError) Unwrap() error {
	return e.Cause
}

// Error codes for consistent error handling
const (
	ErrCodeCrypto       = "CRYPTO_ERROR"
	ErrCodeNetwork      = "NETWORK_ERROR"
	ErrCodeFile         = "FILE_ERROR"
	ErrCodeConfig       = "CONFIG_ERROR"
	ErrCodeAuth         = "AUTH_ERROR"
	ErrCodeRoom         = "ROOM_ERROR"
	ErrCodeDiscovery    = "DISCOVERY_ERROR"
	ErrCodeTUI          = "TUI_ERROR"
	ErrCodeServer       = "SERVER_ERROR"
	ErrCodeClient       = "CLIENT_ERROR"
	ErrCodeValidation   = "VALIDATION_ERROR"
	ErrCodeShutdown     = "SHUTDOWN_ERROR"
)

// newKeykammerError creates a new KeykammerError with context information
func newKeykammerError(code, message string, cause error) *KeykammerError {
	// Get caller information
	_, file, line, _ := runtime.Caller(2)
	
	return &KeykammerError{
		Code:      code,
		Message:   message,
		Cause:     cause,
		Timestamp: time.Now(),
		File:      file,
		Line:      line,
	}
}

// newKeykammerErrorWithContext creates a new KeykammerError with additional context
func newKeykammerErrorWithContext(code, message, context string, cause error) *KeykammerError {
	err := newKeykammerError(code, message, cause)
	err.Context = context
	return err
}

// Error creation helpers for common scenarios

// CryptoError creates a cryptography-related error
func CryptoError(message string, cause error) error {
	return newKeykammerError(ErrCodeCrypto, message, cause)
}

// NetworkError creates a network-related error
func NetworkError(message string, cause error) error {
	return newKeykammerError(ErrCodeNetwork, message, cause)
}

// FileError creates a file-related error
func FileError(message string, cause error) error {
	return newKeykammerError(ErrCodeFile, message, cause)
}

// ConfigError creates a configuration-related error
func ConfigError(message string, cause error) error {
	return newKeykammerError(ErrCodeConfig, message, cause)
}

// AuthError creates an authentication-related error
func AuthError(message string, cause error) error {
	return newKeykammerError(ErrCodeAuth, message, cause)
}

// RoomError creates a room-related error
func RoomError(message string, cause error) error {
	return newKeykammerError(ErrCodeRoom, message, cause)
}

// DiscoveryError creates a discovery-related error
func DiscoveryError(message string, cause error) error {
	return newKeykammerError(ErrCodeDiscovery, message, cause)
}

// ValidationError creates a validation-related error
func ValidationError(message string, cause error) error {
	return newKeykammerError(ErrCodeValidation, message, cause)
}

// Context-aware error handling functions

// withTimeout wraps operations with timeout context and proper error handling
func withTimeout(ctx context.Context, timeout time.Duration, operation func(context.Context) error) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	done := make(chan error, 1)
	
	go func() {
		done <- operation(timeoutCtx)
	}()
	
	select {
	case err := <-done:
		return err
	case <-timeoutCtx.Done():
		return newKeykammerError(ErrCodeNetwork, 
			fmt.Sprintf("operation timed out after %v", timeout), 
			timeoutCtx.Err())
	}
}

// retryWithBackoff retries an operation with exponential backoff
func retryWithBackoff(ctx context.Context, maxRetries int, initialDelay time.Duration, operation func() error) error {
	var lastErr error
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * initialDelay
			logDebug("Retrying operation in %v (attempt %d/%d)", delay, attempt+1, maxRetries+1)
			
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return newKeykammerError(ErrCodeNetwork, "operation cancelled", ctx.Err())
			}
		}
		
		err := operation()
		if err == nil {
			return nil
		}
		lastErr = err
		
		// Check if we should continue retrying
		if !shouldRetry(err) {
			break
		}
	}
	
	return newKeykammerErrorWithContext(ErrCodeNetwork, 
		fmt.Sprintf("operation failed after %d retries", maxRetries+1),
		"retry", lastErr)
}

// shouldRetry determines if an error is retryable
func shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	
	// Check for specific error types that shouldn't be retried
	var keykammerErr *KeykammerError
	if fmt.Errorf("%w", err) != nil && err != context.Canceled {
		if keykammerErr != nil {
			switch keykammerErr.Code {
			case ErrCodeAuth, ErrCodeValidation, ErrCodeFile:
				return false // Don't retry auth, validation, or file errors
			}
		}
	}
	
	return true
}

// Enhanced error logging with context
func logErrorWithContext(err error, context string) {
	var keykammerErr *KeykammerError
	if fmt.Errorf("%w", err) != nil {
		if keykammerErr != nil {
			logError("[%s] %s (context: %s, file: %s:%d)", 
				keykammerErr.Code, keykammerErr.Message, context, 
				keykammerErr.File, keykammerErr.Line)
			if keykammerErr.Cause != nil {
				logDebug("Caused by: %v", keykammerErr.Cause)
			}
		} else {
			logError("%v (context: %s)", err, context)
		}
	} else {
		logError("%v (context: %s)", err, context)
	}
}

// Production error handling utilities

// recoverFromPanic recovers from panics and converts them to errors
func recoverFromPanic() error {
	if r := recover(); r != nil {
		// Get stack trace
		stack := make([]byte, 4096)
		length := runtime.Stack(stack, false)
		
		logError("Panic recovered: %v", r)
		logDebug("Stack trace:\n%s", stack[:length])
		
		return newKeykammerError(ErrCodeServer, 
			fmt.Sprintf("panic recovered: %v", r), nil)
	}
	return nil
}

// safeExecute executes a function safely with panic recovery
func safeExecute(fn func() error) (err error) {
	defer func() {
		if recoverErr := recoverFromPanic(); recoverErr != nil {
			err = recoverErr
		}
	}()
	
	return fn()
}

// validateRequired validates that required fields are not empty
func validateRequired(fields map[string]string) error {
	for name, value := range fields {
		if value == "" {
			return ValidationError(fmt.Sprintf("required field '%s' is empty", name), nil)
		}
	}
	return nil
}