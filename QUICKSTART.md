# Quick Start Guide

Get up and running with etcd-extract in under 30 seconds.

## 🚀 Recommended: Go Version (Fast, Static, 2.7MB)

```bash
# Build once (requires Go 1.21+)
./build-go.sh
# or: make build

# Use anywhere - truly static binary!
./dist/etcd-extract --help
```

**Why Go?** Truly static (zero deps), 72% smaller, 10-100x faster. See [COMPARISON.md](COMPARISON.md).

## 🐍 Alternative: Python Version

```bash
# Build Python version
./build-executable.sh

# Use (requires glibc)
./dist/etcd-extract --help
```

## Basic Usage

```bash
# Set your tool path
TOOL="./dist/etcd-extract"

# Get help
$TOOL --help

# List all resources in database
$TOOL --list /path/to/db.etcd

# Extract a secret from specific namespace
$TOOL --resource secrets --ns kube-system /path/to/db.etcd

# Extract a specific secret by name
$TOOL --resource secrets --ns default --name my-secret /path/to/db.etcd

# Extract all secrets across all namespaces
$TOOL --resource secrets --all-namespaces /path/to/db.etcd

# Extract cluster-scoped resources (no namespace needed)
$TOOL --resource namespaces /path/to/db.etcd
$TOOL --resource nodes /path/to/db.etcd

# Output as JSON instead of YAML
$TOOL --resource secrets --ns default -o json /path/to/db.etcd
```

## Short Flags

```bash
# These are equivalent
$TOOL --resource secrets --namespace default --all-namespaces --output json --list

# Same using short flags
$TOOL -r secrets -n default -A -o json -l
```

## Common Resource Types

**Namespaced** (require `-n` or `-A`):
- `secrets` - Kubernetes secrets
- `configmaps` - Configuration data
- `pods` - Running pods
- `services` - Service definitions
- `deployments` - Deployments
- `statefulsets` - StatefulSets
- `daemonsets` - DaemonSets
- `replicasets` - ReplicaSets
- `jobs` - Jobs
- `cronjobs` - CronJobs

**Cluster-scoped** (no namespace):
- `namespaces` - All namespaces
- `nodes` - Cluster nodes
- `clusterroles` - Cluster roles
- `clusterrolebindings` - Cluster role bindings
- `persistentvolumes` - PVs
- `storageclasses` - Storage classes
- `customresourcedefinitions` - CRDs

## Real-World Examples

### Recover Deleted Secret
```bash
# List all secrets to find the one you need
./dist/etcd-extract -r secrets -A db.etcd | grep -A5 "Name: my-deleted-secret"

# Extract specific secret
./dist/etcd-extract -r secrets -n production -n my-deleted-secret db.etcd > recovered-secret.yaml

# Apply back to cluster
kubectl apply -f recovered-secret.yaml
```

### Audit All Secrets
```bash
# Extract all secrets from all namespaces
./dist/etcd-extract -r secrets -A db.etcd > all-secrets.yaml

# Count secrets per namespace
grep "Namespace:" all-secrets.yaml | sort | uniq -c
```

### Export Namespace Configuration
```bash
# Extract all configmaps from a namespace
./dist/etcd-extract -r configmaps -n myapp db.etcd > myapp-configs.yaml

# Extract all secrets from a namespace
./dist/etcd-extract -r secrets -n myapp db.etcd > myapp-secrets.yaml
```

### Inspect etcd Snapshot
```bash
# See what's in the snapshot
./dist/etcd-extract --list snapshot.db

# Extract specific resource type
./dist/etcd-extract -r namespaces snapshot.db
```

## Installation for Daily Use

```bash
# System-wide (requires sudo)
sudo cp dist/etcd-extract /usr/local/bin/
etcd-extract --help

# User installation
mkdir -p ~/bin
cp dist/etcd-extract ~/bin/
~/bin/etcd-extract --help

# Or add to PATH
export PATH=$PATH:$(pwd)/dist
etcd-extract --help
```

## Tips & Best Practices

1. **Always use snapshots** - Never run against a live etcd database
2. **List first** - Use `--list` to discover what resources are available
3. **Filter early** - Use `-r`, `-n`, and `--name` to avoid extracting everything
4. **JSON for scripting** - Use `-o json` when processing output programmatically
5. **YAML for humans** - Default YAML output is easier to read and includes comments
6. **Backup regularly** - Create etcd snapshots regularly so you can recover data

## Output Format

### YAML Output (default)
```yaml
# Key: /registry/secrets/kube-system/my-secret
# Namespace: kube-system
# Resource: secrets
# Name: my-secret
---
apiVersion: v1
kind: Secret
metadata:
  name: my-secret
  namespace: kube-system
...
```

### JSON Output
```json
{
  "key": "/registry/secrets/kube-system/my-secret",
  "resource": "secrets",
  "namespace": "kube-system",
  "name": "my-secret",
  "object": {
    "apiVersion": "v1",
    "kind": "Secret",
    ...
  }
}
```

## Troubleshooting

### "Database file not found"
```bash
# Check file path
ls -lh /path/to/db.etcd

# Use absolute path
./dist/etcd-extract -r secrets -n default $(pwd)/db.etcd
```

### "No objects found"
```bash
# List resources first to see what's available
./dist/etcd-extract --list db.etcd

# Try without namespace filter for namespaced resources
./dist/etcd-extract -r secrets -A db.etcd
```

### "Could not decode object (protobuf format)"
```
# Some etcd databases use protobuf encoding
# Currently only JSON-encoded objects are supported
# This is a known limitation
```

### Go version not found
```bash
# Install Go
wget https://go.dev/dl/go1.26.3.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.26.3.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Rebuild
./build-go.sh
```

## Next Steps

- **Full documentation**: [README.md](README.md)
- **Build details**: [BUILD-GO.md](BUILD-GO.md)
- **Go vs Python**: [COMPARISON.md](COMPARISON.md)
- **Usage examples**: Run `etcd-extract --help`

## Performance Note

The **Go version is 10-100x faster** than the Python version for large databases:

| Database Size | Go | Python |
|--------------|-----|--------|
| 100 MB | ~1s | ~10s |
| 1 GB | ~10s | ~2min |
| 10 GB | ~1min | ~20min |

For large databases, the Go version is **strongly recommended**.
