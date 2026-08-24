# etcd-extract

A fast, lightweight tool to extract and browse Kubernetes/OpenShift objects from etcd v3 database snapshots. Fully decodes protobuf-encoded objects into human-readable YAML — matching `kubectl get -o yaml` output style.

## Features

- **Full protobuf decoding** — Decodes all Kubernetes and OpenShift protobuf-encoded objects with correct field names (zero `field_N` entries)
- **kubectl-identical YAML** — 2-space indentation and list style matching `kubectl get -o yaml`
- **Web GUI** — Built-in browser-based interface for exploring resources with search, filtering, and namespace selection
- **Truly static binary** — Zero dependencies, copy and run anywhere
- **Cross-platform** — Linux, macOS, and Windows (amd64 and arm64)
- **Fast** — Written in Go using the official bbolt library (same as etcd)
- Filter by resource type, namespace, and object name
- Output in YAML or JSON format

## Download

Download pre-built binaries from the [Releases](https://github.com/alokstech/etcd-extract/releases) page:

| Platform | Architecture | File |
|----------|-------------|------|
| Linux | amd64 | `etcd-extract-vX.X.X-linux-amd64.tar.gz` |
| Linux | arm64 | `etcd-extract-vX.X.X-linux-arm64.tar.gz` |
| macOS | Intel | `etcd-extract-vX.X.X-macos-amd64.tar.gz` |
| macOS | Apple Silicon | `etcd-extract-vX.X.X-macos-arm64.tar.gz` |
| Windows | amd64 | `etcd-extract-vX.X.X-windows-amd64.zip` |
| Windows | arm64 | `etcd-extract-vX.X.X-windows-arm64.zip` |

```bash
# Example: download and extract on Linux
tar xzf etcd-extract-v1.1.1-linux-amd64.tar.gz
sudo cp etcd-extract-v1.1.1-linux-amd64/etcd-extract /usr/local/bin/
```

## Build from Source

```bash
# Build static binary
make build
# or: ./build-go.sh

# Binary at dist/etcd-extract
./dist/etcd-extract --help
```

Requires Go 1.21+.

## Usage

### Command-Line

```bash
# List all resource types in the database
etcd-extract -l /path/to/snapshot.db

# Extract all resources (YAML)
etcd-extract -A /path/to/snapshot.db

# Extract specific resource type
etcd-extract -r secrets -A /path/to/snapshot.db

# Filter by namespace
etcd-extract -r pods -n kube-system /path/to/snapshot.db

# Filter by name
etcd-extract -r secrets -n default --name my-secret /path/to/snapshot.db

# JSON output
etcd-extract -r deployments -A -o json /path/to/snapshot.db
```

### Web GUI

```bash
# Launch web interface (opens browser automatically)
etcd-extract -w /path/to/snapshot.db

# Specify port
etcd-extract -w -p 9090 /path/to/snapshot.db
```

The web GUI provides:
- Sidebar with all resource types grouped by cluster-scoped and namespaced
- Resizable sidebar for long resource names
- Search bar to filter resources
- Namespace filter dropdown
- YAML/JSON toggle with copy and download buttons

### Command-Line Options

```
positional arguments:
  db_file               Path to etcd database file

options:
  -h, --help            Show help message
  -r, --resource TYPE   Resource type (e.g., secrets, configmaps, pods)
  -n, --namespace NS    Namespace (for namespaced resources)
  --name NAME           Object name
  -A, --all-namespaces  Extract from all namespaces
  -o, --output FORMAT   Output format: yaml or json (default: yaml)
  -l, --list            List available resources in the database
  -w, --web             Launch web GUI
  -p, --port PORT       Web server port (default: 8080)
```

## Supported Resource Types

All standard Kubernetes and OpenShift resource types are decoded with full field name resolution:

**Kubernetes:** Pod, Deployment, StatefulSet, DaemonSet, ReplicaSet, Job, CronJob, Service, ConfigMap, Secret, Ingress, IngressClass, NetworkPolicy, Node, Namespace, PersistentVolume, PersistentVolumeClaim, ServiceAccount, Role, ClusterRole, RoleBinding, ClusterRoleBinding, HorizontalPodAutoscaler, PodDisruptionBudget, CertificateSigningRequest, StorageClass, and more.

**OpenShift:** DeploymentConfig, BuildConfig, Build, Route, ImageStream, OAuthClient, OAuthAccessToken, OAuthClientAuthorization, Template, Identity, and more.

**CRDs:** Custom Resource Definitions are decoded from their JSON representation automatically.

## How It Works

1. Opens the etcd BoltDB database file in read-only mode
2. Scans the `key` bucket for Kubernetes-prefixed entries
3. Unwraps the `mvccpb.KeyValue` protobuf envelope
4. Detects encoding: protobuf (`k8s\x00` prefix) or JSON
5. For protobuf objects, recursively decodes using path-based field name lookup
6. Outputs kubectl-style YAML or JSON

## Security Note

etcd databases contain sensitive data including Secrets, credentials, and certificates. Run this tool locally and treat database files with the same security as cluster admin access.

## License

MIT
