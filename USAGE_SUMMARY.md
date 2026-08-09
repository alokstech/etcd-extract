# etcd-extract Usage Summary

## One-Command Setup (Standalone Executable)

```bash
# Build the standalone executable (includes all dependencies)
./build-executable.sh

# That's it! Now use it:
./dist/etcd-extract --help
```

## Copy to System Path (Optional)

```bash
# Copy to system-wide location
sudo cp dist/etcd-extract /usr/local/bin/
etcd-extract --help

# Or copy to user directory
mkdir -p ~/bin
cp dist/etcd-extract ~/bin/
~/bin/etcd-extract --help
```

## Common Commands

### Discovery
```bash
# List all resource types in the database
./dist/etcd-extract --list db.etcd
```

### Extract Namespaced Resources
```bash
# Extract secrets from a namespace
./dist/etcd-extract --resource secrets --ns default db.etcd
./dist/etcd-extract --resource secrets --ns kube-system db.etcd

# Extract specific secret by name
./dist/etcd-extract --resource secrets --ns default --name my-secret db.etcd

# Extract all secrets from all namespaces
./dist/etcd-extract --resource secrets --all-namespaces db.etcd

# Other namespaced resources
./dist/etcd-extract --resource configmaps --ns default db.etcd
./dist/etcd-extract --resource pods --ns kube-system db.etcd
./dist/etcd-extract --resource deployments --ns production db.etcd
```

### Extract Cluster-Scoped Resources
```bash
# No namespace needed for cluster-scoped resources
./dist/etcd-extract --resource namespaces db.etcd
./dist/etcd-extract --resource nodes db.etcd
./dist/etcd-extract --resource clusterroles db.etcd
./dist/etcd-extract --resource persistentvolumes db.etcd
```

### Output Formats
```bash
# YAML output (default)
./dist/etcd-extract --resource secrets --ns default db.etcd

# JSON output
./dist/etcd-extract --resource secrets --ns default -o json db.etcd
```

## Real-World Examples

```bash
# Find all secrets in kube-system namespace
./dist/etcd-extract -r secrets --ns kube-system db.etcd

# Get a specific TLS certificate
./dist/etcd-extract -r secrets --ns ingress --name tls-cert db.etcd

# List all namespaces in the cluster
./dist/etcd-extract -r namespaces db.etcd

# Export all configmaps from an app namespace
./dist/etcd-extract -r configmaps --ns myapp db.etcd > myapp-configs.yaml

# Check what's in an etcd snapshot
./dist/etcd-extract --list snapshot.db
```

## Key Features

✓ **No dependencies** - Standalone executable includes everything
✓ **Namespace aware** - Automatically handles `--ns` for namespaced resources
✓ **Smart filtering** - Knows which resources are cluster-scoped vs namespaced
✓ **Multiple formats** - Output as YAML or JSON
✓ **Discovery mode** - Use `--list` to explore database contents
✓ **Safe** - Read-only operations on etcd snapshots

## File Locations

- `dist/etcd-extract` - Standalone executable (created after build)
- `etcd_extract.py` - Python script (alternative if you prefer)
- `build-executable.sh` - Build script
- `BUILD.md` - Build documentation
- `README.md` - Full documentation
- `QUICKSTART.md` - Quick reference

## Troubleshooting

**Build fails:**
```bash
# Install PyInstaller
pip install pyinstaller
# Try build again
./build-executable.sh
```

**Executable not working:**
```bash
# Make sure it's executable
chmod +x dist/etcd-extract
# Test it
./dist/etcd-extract --help
```

**Need to rebuild:**
```bash
make clean
make build
```
