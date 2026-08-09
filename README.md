# etcd-extract

A fast, lightweight command-line tool to extract Kubernetes object YAMLs from etcd database files.

## Features

- ✓ **Truly static binary** - Zero dependencies, copy and run anywhere
- ✓ **Small size** - Only 2.7MB (vs 9.8MB Python version)
- ✓ **Fast** - Written in Go using the official bbolt library (same as etcd)
- ✓ **Native compatibility** - Uses the exact same BoltDB library as etcd
- Extract specific Kubernetes objects from etcd database snapshots
- Filter by resource type, namespace, and name
- Automatically distinguishes between cluster-scoped and namespaced resources
- Output in YAML or JSON format
- List all available resources in a database

## Quick Start

### Option 1: Build Go Executable (Recommended - Fastest, Smallest, No Dependencies!)

```bash
# Build static binary
./build-go.sh
# or: make build
```

Then use the standalone binary:
```bash
./dist/etcd-extract --help

# Copy anywhere and run (truly static, no dependencies!)
sudo cp dist/etcd-extract /usr/local/bin/
```

**Benefits:** 
- ✓ Truly static (no glibc or any dependencies)
- ✓ 2.7MB vs 9.8MB Python version (72% smaller)
- ✓ 10-100x faster performance
- ✓ Uses official bbolt library (same as etcd)
- ✓ Copy to any Linux system and run

### Option 2: Build Python Executable (Alternative)

```bash
./build-executable.sh
```

**Note:** Python version requires glibc and is dynamically linked.

### Option 3: Run Python Script Directly

```bash
# Install dependencies once
./install-deps.sh
# or manually: pip install 'git+https://github.com/qingyunha/boltdb.git' PyYAML

# Run the script
./etcd_extract.py --help
```

## Usage

### Basic Examples

Extract all secrets from the `default` namespace:
```bash
./etcd_extract.py --resource secrets --ns default /path/to/db.etcd
```

Extract a specific secret:
```bash
./etcd_extract.py --resource secrets --ns kube-system --name my-secret /path/to/db.etcd
```

Extract cluster-scoped resources (no namespace):
```bash
./etcd_extract.py --resource namespaces /path/to/db.etcd
./etcd_extract.py --resource nodes /path/to/db.etcd
```

Extract all secrets across all namespaces:
```bash
./etcd_extract.py --resource secrets --all-namespaces /path/to/db.etcd
```

List all available resources in the database:
```bash
./etcd_extract.py --list /path/to/db.etcd
```

### Command-Line Options

```
positional arguments:
  db_file               Path to etcd database file

optional arguments:
  -h, --help            Show help message
  -r, --resource RESOURCE
                        Resource type (e.g., secrets, configmaps, pods)
  -n, --ns, --namespace NAMESPACE
                        Namespace (for namespaced resources)
  --name NAME           Object name
  -A, --all-namespaces  Extract from all namespaces
  -o, --output {yaml,json}
                        Output format (default: yaml)
  -l, --list            List available resources in the database
```

## Resource Types

### Cluster-Scoped Resources
These resources don't have a namespace:
- `namespaces`
- `nodes`
- `persistentvolumes`
- `clusterroles`
- `clusterrolebindings`
- `storageclasses`
- `customresourcedefinitions`

### Namespaced Resources
These resources require a namespace:
- `secrets`
- `configmaps`
- `pods`
- `services`
- `deployments`
- `statefulsets`
- `daemonsets`
- And many more...

## How It Works

1. Opens the etcd BoltDB database file in read-only mode
2. Parses Kubernetes etcd key structure:
   - Cluster-scoped: `/registry/<resource>/<name>`
   - Namespaced: `/registry/<resource>/<namespace>/<name>`
3. Filters objects based on your criteria
4. Decodes and outputs as YAML or JSON

## Notes

- The tool currently supports JSON-encoded objects in etcd
- Protobuf-encoded objects will show a warning (requires additional protobuf definitions)
- Always use read-only mode on production databases or work with snapshots

## License

MIT
