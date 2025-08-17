# Keykammer Build System

# Version information
VERSION ?= 1.0.0
BUILD_TIME := $(shell date -u '+%Y-%m-%d %H:%M:%S UTC')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GO_VERSION := $(shell go version | cut -d' ' -f3)

# Build flags
LDFLAGS := -ldflags "\
	-X 'main.Version=$(VERSION)' \
	-X 'main.BuildTime=$(BUILD_TIME)' \
	-X 'main.GitCommit=$(GIT_COMMIT)'"

# Default target
.PHONY: all
all: build

# Build the application
.PHONY: build
build:
	go build $(LDFLAGS) -o keykammer *.go

# Build for release (optimized)
.PHONY: release
release:
	CGO_ENABLED=0 go build $(LDFLAGS) -a -installsuffix cgo -o keykammer *.go

# Build for all platforms
.PHONY: build-all
build-all: clean-dist
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/keykammer-linux-amd64 *.go
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/keykammer-windows-amd64.exe *.go
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/keykammer-macos-intel *.go
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/keykammer-macos-arm64 *.go
	@echo "Built binaries:"
	@ls -la dist/

# Build release versions for all platforms (optimized)
.PHONY: release-all
release-all: clean-dist
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -a -installsuffix cgo -o dist/keykammer-linux-amd64 *.go
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -a -installsuffix cgo -o dist/keykammer-windows-amd64.exe *.go
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -a -installsuffix cgo -o dist/keykammer-macos-intel *.go
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -a -installsuffix cgo -o dist/keykammer-macos-arm64 *.go
	@echo "Built optimized release binaries:"
	@ls -la dist/

# Clean build artifacts
.PHONY: clean
clean:
	rm -f keykammer

# Clean distribution folder
.PHONY: clean-dist
clean-dist:
	rm -rf dist/

# Install dependencies
.PHONY: deps
deps:
	go mod tidy
	go mod download

# Run tests
.PHONY: test
test:
	go test -v ./...

# Show version that would be built
.PHONY: version
version:
	@echo "Version: $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Git Commit: $(GIT_COMMIT)"
	@echo "Go Version: $(GO_VERSION)"

# Development build (without version info)
.PHONY: dev
dev:
	go build -o keykammer *.go

# Help
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build       - Build keykammer with version information"
	@echo "  build-all   - Build for all platforms (Linux, Windows, macOS)"
	@echo "  release     - Build optimized release version"
	@echo "  release-all - Build optimized releases for all platforms"
	@echo "  dev         - Quick development build"
	@echo "  clean       - Remove build artifacts"
	@echo "  clean-dist  - Remove distribution folder"
	@echo "  deps        - Install/update dependencies"
	@echo "  test        - Run tests"
	@echo "  version     - Show version information"
	@echo "  help        - Show this help message"
	@echo ""
	@echo "Environment variables:"
	@echo "  VERSION  - Override version number (default: 1.0.0)"