#!/bin/bash
# Build standalone Go executable for etcd-extract

set -e

echo "Building Go standalone executable for etcd-extract..."
echo ""

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed"
    echo "Install Go from: https://go.dev/dl/"
    exit 1
fi

echo "Go version: $(go version)"
echo ""

# Download dependencies
echo "Downloading dependencies..."
go mod download

# Clean previous builds
echo "Cleaning previous builds..."
rm -f dist/etcd-extract

# Build static binary (no CGO, stripped and optimized)
echo "Building static binary..."
CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/etcd-extract main.go

# Check if build succeeded
if [ -f "dist/etcd-extract" ]; then
    echo ""
    echo "✓ Build successful!"
    echo ""
    echo "Standalone executable created at: dist/etcd-extract"
    echo ""

    # Show file info
    echo "File info:"
    file dist/etcd-extract
    echo ""

    # Show size
    SIZE=$(du -h dist/etcd-extract | cut -f1)
    echo "Executable size: $SIZE"
    echo ""

    # Verify it's static
    if ldd dist/etcd-extract 2>&1 | grep -q "not a dynamic executable"; then
        echo "✓ Truly static binary (no dependencies!)"
    else
        echo "⚠ Warning: Binary has dynamic dependencies"
        ldd dist/etcd-extract
    fi

    echo ""
    echo "Test it with: ./dist/etcd-extract --help"
    echo ""
    echo "You can copy this file anywhere and run it without dependencies:"
    echo "  sudo cp dist/etcd-extract /usr/local/bin/"
    echo "  or"
    echo "  cp dist/etcd-extract ~/bin/"
    echo ""
else
    echo "✗ Build failed!"
    exit 1
fi
