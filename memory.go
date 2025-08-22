package main

import (
	"os"
	"runtime"
	"sync"
	"time"
)

// Memory optimization constants
const (
	// Buffer pool sizes for efficient memory reuse
	SmallBufferSize  = 1024     // 1KB buffers for small messages
	MediumBufferSize = 8192     // 8KB buffers for medium messages
	LargeBufferSize  = 65536    // 64KB buffers for large messages
	
	// Memory management thresholds
	MaxMemoryUsageMB     = 100  // Trigger cleanup if memory usage exceeds this
	GCIntervalSeconds    = 30   // Seconds between forced garbage collection
	BufferPoolMaxSize    = 100  // Maximum number of buffers to keep in pools
	
	// Chat history limits for memory efficiency
	MaxChatHistoryMemory = 500  // Reduced from 1000 for better memory usage
)

// Global buffer pools for memory efficiency
var (
	smallBufferPool  = sync.Pool{New: func() interface{} { return make([]byte, SmallBufferSize) }}
	mediumBufferPool = sync.Pool{New: func() interface{} { return make([]byte, MediumBufferSize) }}
	largeBufferPool  = sync.Pool{New: func() interface{} { return make([]byte, LargeBufferSize) }}
	
	// String builder pool for efficient string operations
	stringBuilderPool = sync.Pool{New: func() interface{} { 
		// Pre-allocate with reasonable capacity
		builder := make([]byte, 0, 256)
		return &builder
	}}
	
	// Slice pools for frequently allocated slices
	stringSlicePool = sync.Pool{New: func() interface{} {
		slice := make([]string, 0, 16) // Pre-allocate capacity for typical user lists
		return &slice
	}}
)

// getBuffer retrieves an appropriately sized buffer from pool
func getBuffer(size int) []byte {
	switch {
	case size <= SmallBufferSize:
		return smallBufferPool.Get().([]byte)[:size]
	case size <= MediumBufferSize:
		return mediumBufferPool.Get().([]byte)[:size]
	case size <= LargeBufferSize:
		return largeBufferPool.Get().([]byte)[:size]
	default:
		// For very large buffers, allocate directly (not pooled)
		return make([]byte, size)
	}
}

// putBuffer returns a buffer to the appropriate pool
func putBuffer(buf []byte) {
	if cap(buf) == 0 {
		return // Don't pool zero-capacity buffers
	}
	
	switch cap(buf) {
	case SmallBufferSize:
		smallBufferPool.Put(buf[:cap(buf)])
	case MediumBufferSize:
		mediumBufferPool.Put(buf[:cap(buf)])
	case LargeBufferSize:
		largeBufferPool.Put(buf[:cap(buf)])
	default:
		// Don't pool non-standard sizes
	}
}

// getStringSlice retrieves a string slice from pool
func getStringSlice() *[]string {
	slice := stringSlicePool.Get().(*[]string)
	*slice = (*slice)[:0] // Reset length but keep capacity
	return slice
}

// putStringSlice returns a string slice to pool
func putStringSlice(slice *[]string) {
	if cap(*slice) > 100 { // Don't pool excessively large slices
		return
	}
	stringSlicePool.Put(slice)
}

// Memory monitoring and cleanup functions
func getCurrentMemoryUsage() (uint64, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// Return allocated memory in MB
	return m.Alloc / 1024 / 1024, nil
}

// startMemoryMonitor begins periodic memory monitoring and cleanup
func startMemoryMonitor() {
	go func() {
		ticker := time.NewTicker(time.Duration(GCIntervalSeconds) * time.Second)
		defer ticker.Stop()
		
		for range ticker.C {
			memUsageMB, err := getCurrentMemoryUsage()
			if err != nil {
				logError("Failed to get memory usage: %v", err)
				continue
			}
			
			logDebug("Current memory usage: %d MB", memUsageMB)
			
			// Trigger garbage collection if memory usage is high
			if memUsageMB > MaxMemoryUsageMB {
				logDebug("High memory usage detected (%d MB), triggering GC", memUsageMB)
				runtime.GC()
				
				// Force release unused memory back to OS
				runtime.GC()
				
				// Log memory usage after GC
				memUsageAfterMB, _ := getCurrentMemoryUsage()
				logDebug("Memory usage after GC: %d MB", memUsageAfterMB)
			}
		}
	}()
}

// optimizeForProduction sets runtime parameters for production use
func optimizeForProduction() {
	// Set garbage collection target percentage (lower = more frequent GC, less memory)
	runtime.GOMAXPROCS(runtime.NumCPU()) // Use all available CPUs
	
	// Set initial heap size to reduce early garbage collection pressure
	debug := os.Getenv("KEYKAMMER_DEBUG_GC")
	if debug == "1" || debug == "true" {
		logDebug("GC optimization enabled for production")
	}
	
	// Start memory monitoring
	startMemoryMonitor()
}

// cleanupResources performs cleanup of pooled resources
func cleanupResources() {
	// Clear buffer pools to free memory
	smallBufferPool = sync.Pool{New: func() interface{} { return make([]byte, SmallBufferSize) }}
	mediumBufferPool = sync.Pool{New: func() interface{} { return make([]byte, MediumBufferSize) }}
	largeBufferPool = sync.Pool{New: func() interface{} { return make([]byte, LargeBufferSize) }}
	stringBuilderPool = sync.Pool{New: func() interface{} { 
		builder := make([]byte, 0, 256)
		return &builder
	}}
	stringSlicePool = sync.Pool{New: func() interface{} {
		slice := make([]string, 0, 16)
		return &slice
	}}
	
	// Force garbage collection
	runtime.GC()
	runtime.GC() // Call twice to ensure full cleanup
	
	logDebug("Resource cleanup completed")
}

// Efficient string operations for reducing allocations
func efficientStringJoin(parts []string, separator string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	
	// Calculate total length to pre-allocate
	totalLen := len(separator) * (len(parts) - 1)
	for _, part := range parts {
		totalLen += len(part)
	}
	
	// Use buffer from pool
	var result []byte
	if totalLen <= SmallBufferSize {
		result = getBuffer(totalLen)
		defer putBuffer(result)
	} else {
		result = make([]byte, 0, totalLen)
	}
	
	// Build string efficiently
	for i, part := range parts {
		if i > 0 {
			result = append(result, separator...)
		}
		result = append(result, part...)
	}
	
	return string(result)
}