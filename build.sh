#!/bin/bash

# Keykammer Build Script
# Usage: ./build.sh [version]

set -e

# Default version
VERSION="${1:-0.3.0-alpha}"
BUILD_TIME=$(date -u '+%Y-%m-%d %H:%M:%S UTC')
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

echo "Building Keykammer..."
echo "Version: $VERSION"
echo "Build Time: $BUILD_TIME"
echo "Git Commit: $GIT_COMMIT"
echo ""

# Build with version information
go build \
    -ldflags "\
        -X 'main.Version=$VERSION' \
        -X 'main.BuildTime=$BUILD_TIME' \
        -X 'main.GitCommit=$GIT_COMMIT'" \
    -o keykammer-$VERSION *.go

echo "Build complete: ./keykammer-$VERSION"
echo "Run './keykammer-$VERSION -version' to verify build information"