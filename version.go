package main

import (
	"fmt"
	"runtime"
)

// Version information - these will be set at build time using ldflags
var (
	Version   = "dev"                    // Version number
	BuildTime = "unknown"                // Build timestamp
	GitCommit = "unknown"                // Git commit hash
	GoVersion = runtime.Version()        // Go version used to build
)

// printVersion displays version information and exits
func printVersion() {
	fmt.Printf("Keykammer %s\n", Version)
	fmt.Printf("Built: %s\n", BuildTime)
	fmt.Printf("Commit: %s\n", GitCommit)
	fmt.Printf("Go: %s\n", GoVersion)
	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

// getVersionString returns a formatted version string
func getVersionString() string {
	return fmt.Sprintf("Keykammer %s (built %s)", Version, BuildTime)
}

// getBuildInfo returns detailed build information
func getBuildInfo() map[string]string {
	return map[string]string{
		"version":   Version,
		"buildTime": BuildTime,
		"gitCommit": GitCommit,
		"goVersion": GoVersion,
		"platform":  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}