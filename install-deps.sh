#!/bin/bash
# Quick dependency installer for etcd-extract

echo "Installing dependencies for etcd-extract..."
pip install git+https://github.com/qingyunha/boltdb.git PyYAML

if [ $? -eq 0 ]; then
    echo ""
    echo "✓ Dependencies installed successfully!"
    echo ""
    echo "You can now run: ./etcd_extract.py --help"
else
    echo ""
    echo "✗ Installation failed. Try manually:"
    echo "  pip install git+https://github.com/qingyunha/boltdb.git PyYAML"
    exit 1
fi
