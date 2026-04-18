package logging

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// LogLevel represents the logging level
type LogLevel int

const (
	LogLevelSilent LogLevel = iota
	LogLevelError
	LogLevelWarn
	LogLevelInfo
	LogLevelDebug
)

var (
	currentLogLevel LogLevel = LogLevelInfo
	enableColors    bool     = true
)

func init() {
	// Check environment variables for log configuration
	if level := os.Getenv("KEYKAMMER_LOG_LEVEL"); level != "" {
		switch strings.ToLower(level) {
		case "silent":
			currentLogLevel = LogLevelSilent
		case "error":
			currentLogLevel = LogLevelError
		case "warn", "warning":
			currentLogLevel = LogLevelWarn
		case "info":
			currentLogLevel = LogLevelInfo
		case "debug":
			currentLogLevel = LogLevelDebug
		}
	}

	// Check if colors should be disabled
	if os.Getenv("NO_COLOR") != "" || os.Getenv("KEYKAMMER_NO_COLOR") != "" {
		enableColors = false
	}

	// Disable standard log prefixes for cleaner output
	log.SetFlags(0)
}

// Production logging functions
func Error(format string, args ...interface{}) {
	if currentLogLevel >= LogLevelError {
		if enableColors {
			fmt.Printf("\033[31m[ERROR]\033[0m "+format+"\n", args...)
		} else {
			fmt.Printf("[ERROR] "+format+"\n", args...)
		}
	}
}

func Warn(format string, args ...interface{}) {
	if currentLogLevel >= LogLevelWarn {
		if enableColors {
			fmt.Printf("\033[33m[WARN]\033[0m "+format+"\n", args...)
		} else {
			fmt.Printf("[WARN] "+format+"\n", args...)
		}
	}
}

func Info(format string, args ...interface{}) {
	if currentLogLevel >= LogLevelInfo {
		if enableColors {
			fmt.Printf("\033[36m[INFO]\033[0m "+format+"\n", args...)
		} else {
			fmt.Printf("[INFO] "+format+"\n", args...)
		}
	}
}

func Debug(format string, args ...interface{}) {
	if currentLogLevel >= LogLevelDebug {
		if enableColors {
			fmt.Printf("\033[35m[DEBUG]\033[0m "+format+"\n", args...)
		} else {
			fmt.Printf("[DEBUG] "+format+"\n", args...)
		}
	}
}

// User-facing messages (always shown regardless of log level)
func UserInfo(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

func UserError(format string, args ...interface{}) {
	if enableColors {
		fmt.Printf("\033[31m"+format+"\033[0m\n", args...)
	} else {
		fmt.Printf(format+"\n", args...)
	}
}

func UserSuccess(format string, args ...interface{}) {
	if enableColors {
		fmt.Printf("\033[32m"+format+"\033[0m\n", args...)
	} else {
		fmt.Printf(format+"\n", args...)
	}
}
