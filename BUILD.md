# Building Standalone Executable

This guide shows how to build a fully standalone executable that bundles all dependencies.

## Quick Build

```bash
./build-executable.sh
```

This will:
1. Install PyInstaller if needed
2. Install build dependencies
3. Create a standalone executable at `dist/etcd-extract`

## Using the Standalone Executable

The executable in `dist/etcd-extract` is completely self-contained:

```bash
# Run directly
./dist/etcd-extract --help

# Copy to your PATH
sudo cp dist/etcd-extract /usr/local/bin/
etcd-extract --help

# Or copy to user bin
mkdir -p ~/bin
cp dist/etcd-extract ~/bin/
~/bin/etcd-extract --help
```

## Benefits

- ✓ No Python installation required
- ✓ No pip dependencies to install
- ✓ Single binary file
- ✓ Copy and run anywhere
- ✓ Same Linux architecture

## Manual Build

If you prefer to build manually:

```bash
# Install PyInstaller
pip install pyinstaller

# Install dependencies
pip install bolt-python PyYAML

# Build
pyinstaller --onefile --name etcd-extract --clean etcd_extract.py

# Result
./dist/etcd-extract --help
```

## Distribution

You can distribute the `dist/etcd-extract` binary to other users with the same OS/architecture. They can run it without installing anything.

## Size

The standalone executable is typically 10-20MB due to bundled Python runtime and dependencies.

## Platform Notes

- Linux builds work on same architecture (x86_64, ARM, etc.)
- For different platforms, build on target OS
- For cross-platform distribution, build on each platform separately
