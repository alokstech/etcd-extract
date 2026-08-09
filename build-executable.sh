#!/bin/bash
# Build standalone executable for etcd-extract

set -e

echo "Building standalone executable for etcd-extract..."
echo ""

# Check if PyInstaller is installed
if ! python3 -m pip show pyinstaller &> /dev/null; then
    echo "PyInstaller not found. Installing..."
    python3 -m pip install --user pyinstaller
fi

# Install dependencies needed for build
echo "Installing dependencies for build..."
python3 -m pip install --user 'git+https://github.com/qingyunha/boltdb.git' PyYAML

# Clean previous builds
echo "Cleaning previous builds..."
rm -rf build/ dist/ *.spec

# Build the executable
echo "Building executable..."
python3 -m PyInstaller \
    --onefile \
    --name etcd-extract \
    --clean \
    etcd_extract.py

# Check if build succeeded
if [ -f "dist/etcd-extract" ]; then
    echo ""
    echo "✓ Build successful!"
    echo ""
    echo "Standalone executable created at: dist/etcd-extract"
    echo ""
    echo "Test it with: ./dist/etcd-extract --help"
    echo ""
    echo "You can copy this file anywhere and run it without dependencies:"
    echo "  cp dist/etcd-extract /usr/local/bin/"
    echo "  or"
    echo "  cp dist/etcd-extract ~/bin/"
    echo ""

    # Make executable (should already be, but just in case)
    chmod +x dist/etcd-extract

    # Show size
    SIZE=$(du -h dist/etcd-extract | cut -f1)
    echo "Executable size: $SIZE"
else
    echo "✗ Build failed!"
    exit 1
fi
