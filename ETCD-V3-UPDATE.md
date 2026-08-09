# etcd v3 Format Support - Update Summary

## What Was Fixed

### Issue
The original etcd-extract tool was designed for **etcd v2 format**, where data is stored with keys like:
```
/registry/configmaps/namespace/name
```

Your database uses **etcd v3 format**, where:
- All data is in the `key` bucket
- Keys are binary (revision information)
- Values contain both the path AND the JSON object
- Path format: `/kubernetes.io/<resource>/<namespace>/<name>`

### Solution
Updated both CLI and web tools to support etcd v3 format:
- New parser extracts path from value instead of key
- Handles the different storage structure
- Maintains all existing features

## Using the Updated Tools

### CLI Tool

The CLI tool now works with your database:

```bash
# List all available resources
./dist/etcd-extract --list ~/Downloads/db

# Extract configmaps from a specific namespace
./dist/etcd-extract --resource configmaps --ns openshift-kube-apiserver ~/Downloads/db

# Extract all configmaps from all namespaces
./dist/etcd-extract --resource configmaps --all-namespaces ~/Downloads/db

# Extract a specific object
./dist/etcd-extract --resource configmaps --ns openshift-kube-apiserver --name pod-config ~/Downloads/db

# Output as JSON instead of YAML
./dist/etcd-extract --resource secrets --all-namespaces -o json ~/Downloads/db
```

### Web GUI

The web interface is also updated:

```bash
# Start the web server
make run-web

# Or run directly
./dist/etcd-extract-web

# Then open in browser
http://localhost:8080
```

**Fixed GUI Issue**: Removed file extension restriction - you can now upload files named `db` (without `.db` or `.etcd` extension).

## Your Database

Based on the scan of your database at `~/Downloads/db`:

### Available Resources:
| Resource | Type | Count |
|----------|------|-------|
| apiextensions.k8s.io | namespaced | 185 |
| apiregistration.k8s.io | namespaced | 101 |
| clusterrolebindings | cluster-scoped | 4 |
| **configmaps** | namespaced | **38** |
| controllers | namespaced | 1 |
| openshift.io | namespaced | 2229 |
| operators.coreos.com | namespaced | 329 |
| pods | namespaced | 5 |
| rolebindings | namespaced | 52 |
| secrets | namespaced | 1 |
| serviceaccounts | namespaced | 5 |
| tekton.dev | namespaced | 27 |

### ConfigMap Namespaces
The 38 configmaps are in these namespaces:
- `openshift-kube-apiserver`
- `openshift-kube-controller-manager`
- `openshift-kube-scheduler`

**Note**: There are **no configmaps in `openshift-config`** namespace in this database. That's why:
```bash
./dist/etcd-extract --resource configmaps --ns openshift-config ~/Downloads/db
```
returned "No objects found" - the tool was working correctly, the namespace just doesn't have any configmaps.

## Example Workflows

### Extract All ConfigMaps
```bash
# See all configmaps across all namespaces
./dist/etcd-extract --resource configmaps --all-namespaces ~/Downloads/db > all-configmaps.yaml

# Count how many
./dist/etcd-extract --resource configmaps --all-namespaces ~/Downloads/db 2>&1 | tail -1
# Output: # Extracted 38 object(s)
```

### Extract from Specific Namespace
```bash
# Get configmaps from openshift-kube-apiserver
./dist/etcd-extract --resource configmaps --ns openshift-kube-apiserver ~/Downloads/db > apiserver-configmaps.yaml
```

### List Namespaces
```bash
# Extract all namespace objects to see what namespaces exist
./dist/etcd-extract --resource namespaces ~/Downloads/db | grep "name:"
```

### Extract OpenShift Resources
```bash
# The database has 2229 openshift.io resources
./dist/etcd-extract --resource openshift.io --all-namespaces ~/Downloads/db > openshift-resources.yaml
```

## Format Differences

### etcd v2 (old format)
```
Bucket: default or named buckets
Key: /registry/configmaps/namespace/name
Value: JSON object only
```

### etcd v3 (new format - your database)
```
Bucket: "key" (all data in one bucket)
Key: binary revision data
Value: /kubernetes.io/configmaps/namespace/name + JSON object
```

## Troubleshooting

### "No objects found"
This means the filters don't match any objects. Common reasons:
1. **Wrong namespace**: Use `--list` to see what resources exist, then `--all-namespaces` to see which namespaces they're in
2. **Wrong resource name**: The resource name must match exactly (e.g., `configmaps` not `configmap`)
3. **Empty database**: Check with `--list` first

### Find What Namespaces Have ConfigMaps
```bash
# Extract all and grep for namespaces
./dist/etcd-extract --resource configmaps --all-namespaces ~/Downloads/db 2>&1 | grep "# Namespace:" | sort -u
```

### Check File Format
```bash
# Verify it's a BoltDB file
file ~/Downloads/db

# Should output:
# /home/alosingh/Downloads/db: BoltDB database
```

## Web GUI Workflow

1. **Start server**: `make run-web` or `./dist/etcd-extract-web`
2. **Open browser**: http://localhost:8080
3. **Upload** your `db` file (drag & drop or click)
4. **Browse** the resource cards - you'll see configmaps (38 objects)
5. **Click** the configmaps card to select it
6. **Filter**:
   - Leave namespace empty and check "all namespaces" to see all 38
   - Or enter specific namespace: `openshift-kube-apiserver`
7. **Extract** and view results
8. **Download** as ZIP if needed

## Files Changed

### Core Files
- `main.go` - Updated with etcd v3 parsing logic
- `web-server.go` - Updated with etcd v3 parsing logic
- `web/index.html` - Removed file extension restriction

### Backup Files (if you need to revert)
- `main-v2.go.bak` - Original etcd v2 version
- `web-server-v2.go.bak` - Original web server

### Debug Tools (in `tools/` directory)
- `debug-db.go` - Inspect database structure
- `analyze-format.go` - Analyze etcd format
- `etcdv3-extract.go` - Standalone v3 extractor

## Build Commands

```bash
# Build CLI tool
make build

# Build web server
make build-web

# Build both
make build && make build-web

# Run web server
make run-web
```

## Verification

Test that everything works:

```bash
# Test CLI
./dist/etcd-extract --list ~/Downloads/db

# Test extraction
./dist/etcd-extract --resource configmaps --all-namespaces ~/Downloads/db | head -20

# Test web server (in another terminal)
./dist/etcd-extract-web
# Then open http://localhost:8080 and upload your db file
```

## Next Steps

1. **List resources** in your database: `./dist/etcd-extract --list ~/Downloads/db`
2. **Extract what you need**: Use `--all-namespaces` flag to see all objects
3. **Use the web GUI** for visual exploration: `make run-web`

## Support

If you encounter issues:
1. Verify database format: `file ~/Downloads/db`
2. Check what's available: `./dist/etcd-extract --list ~/Downloads/db`  
3. Try the debug tool: `go run tools/debug-db.go ~/Downloads/db | head -50`

The tools now fully support etcd v3 format! 🎉
