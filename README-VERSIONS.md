# Version Guide

etcd-extract is available in two implementations:

## Go Version (Recommended) ⭐

**Location:** `main.go`  
**Build:** `./build-go.sh` or `make build`  
**Output:** `dist/etcd-extract` (2.7MB, static)

### Advantages
- ✓ Truly static binary (zero dependencies)
- ✓ 72% smaller (2.7MB vs 9.8MB)
- ✓ 10-100x faster performance
- ✓ Uses official bbolt library (same as etcd)
- ✓ Cross-compile for any platform
- ✓ Instant startup

### When to Use
- Production deployments
- Distribution to end users
- Performance-critical operations
- Any Linux system (static binary works everywhere)

## Python Version

**Location:** `etcd_extract.py`  
**Build:** `./build-executable.sh` or `make build-python`  
**Output:** `dist/etcd-extract` (9.8MB, dynamic)

### Advantages
- Can modify without recompiling
- Familiar to Python developers

### Disadvantages
- Requires glibc (dynamically linked)
- 3.6x larger (9.8MB vs 2.7MB)
- 10-100x slower performance
- Uses unofficial boltdb port
- Platform-specific binaries

### When to Use
- Quick prototyping in Python
- You strongly prefer Python
- You need to modify the code frequently

## Quick Comparison

| Feature | Go | Python |
|---------|-----|--------|
| **Size** | 2.7MB | 9.8MB |
| **Dependencies** | Zero (static) | glibc, etc. (dynamic) |
| **Performance** | Fast (native) | Slow (interpreted) |
| **Library** | Official (bbolt) | Unofficial (boltdb) |
| **Portability** | Any Linux | Same glibc version |
| **Build Time** | ~5s | ~30s |
| **Cross-compile** | Easy | Hard |

## Recommendation

**Use the Go version** unless you have a specific reason to use Python.

```bash
# Build Go version (recommended)
make build

# Or explicitly
./build-go.sh
```

See [COMPARISON.md](COMPARISON.md) for detailed analysis.

## Files

### Go Implementation
- `main.go` - Go source code
- `go.mod` - Go dependencies
- `build-go.sh` - Go build script
- `BUILD-GO.md` - Go build documentation

### Python Implementation  
- `etcd_extract.py` - Python source code
- `requirements.txt` - Python dependencies
- `build-executable.sh` - Python build script (PyInstaller)
- `install-deps.sh` - Python dependency installer
- `BUILD.md` - Python build documentation

### Documentation
- `README.md` - Main documentation
- `QUICKSTART.md` - Quick start guide
- `COMPARISON.md` - Detailed Go vs Python comparison
- `USAGE_SUMMARY.md` - Usage examples

### Build System
- `Makefile` - Build automation (defaults to Go)
- `.gitignore` - Git ignore rules
