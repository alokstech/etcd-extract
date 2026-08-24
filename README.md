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

### Listing Resources

```bash
# List all resource types in the database
etcd-extract --list snapshot.db

# List resource types in a specific namespace
etcd-extract --list --ns openshift-config snapshot.db

# List individual objects of a resource type
etcd-extract --list --resource configmaps --ns openshift-config snapshot.db

# List all secrets across all namespaces
etcd-extract --list --resource secrets snapshot.db
```

### Extracting Resources (YAML)

```bash
# Extract a specific configmap by name
etcd-extract -r configmaps -n openshift-config --name etcd-ca-bundle snapshot.db

# Extract all configmaps in a namespace
etcd-extract -r configmaps -n openshift-config snapshot.db

# Extract all secrets across all namespaces
etcd-extract -r secrets -A snapshot.db

# Extract cluster-scoped resources (no namespace needed)
etcd-extract -r namespaces snapshot.db
etcd-extract -r nodes snapshot.db
etcd-extract -r clusterroles snapshot.db

# Extract all resources across all namespaces
etcd-extract -A snapshot.db
```

### Extracting Resources (JSON)

```bash
# Extract a specific secret as JSON
etcd-extract -r secrets -n kube-system --name my-secret -o json snapshot.db

# Extract all deployments as JSON
etcd-extract -r deployments -A -o json snapshot.db
```

### Web GUI

```bash
# Start web GUI with a database
etcd-extract --serve snapshot.db

# Start web GUI on a custom port
etcd-extract --serve --port 9090 snapshot.db

# Start web GUI without a database (load via browser upload)
etcd-extract --serve
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
  db_file                 Path to etcd database file

options:
  -h, --help              Show help message
  -r, --resource TYPE     Resource type (e.g., secrets, configmaps, pods)
  -n, --ns, --namespace   Namespace (for namespaced resources)
  --name NAME             Object name
  -A, --all-namespaces    Extract from all namespaces
  -o, --output FORMAT     Output format: yaml or json (default: yaml)
  -l, --list              List available resources in the database
  --serve                 Start web GUI server
  --port PORT             Web server port (default: 8080)
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
