#!/bin/sh
set -e

echo "Installing Gauntlet..."

# Check for Go
if command -v go >/dev/null 2>&1; then
    echo "Using go install..."
    go install github.com/pmclSF/gauntlet/cmd/gauntlet@latest
    echo ""
    echo "Gauntlet installed to $(go env GOPATH)/bin/gauntlet"
    echo "Make sure $(go env GOPATH)/bin is in your PATH."
else
    echo "Error: Go is required to install Gauntlet."
    echo ""
    echo "Install Go: https://go.dev/dl/"
    echo "Then run: go install github.com/pmclSF/gauntlet/cmd/gauntlet@latest"
    exit 1
fi

echo ""
echo "Verify: gauntlet --version"
echo "Get started: gauntlet init"
