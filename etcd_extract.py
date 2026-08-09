#!/usr/bin/env python3
"""
etcd-extract: Tool to extract Kubernetes objects from etcd database files

Usage: ./etcd_extract.py [options] <db_file>
Dependencies: pip install 'git+https://github.com/qingyunha/boltdb.git' PyYAML
"""

import argparse
import json
import sys
from pathlib import Path
from typing import Optional, List, Dict, Any

# Check dependencies
missing_deps = []

try:
    import boltdb
except ImportError:
    missing_deps.append('boltdb')

try:
    import yaml
except ImportError:
    missing_deps.append('PyYAML')

if missing_deps:
    print(f"Error: Missing required dependencies: {', '.join(missing_deps)}", file=sys.stderr)
    print(f"\nInstall with: pip install {' '.join(missing_deps)}", file=sys.stderr)
    print("Or: pip install boltdb PyYAML", file=sys.stderr)
    sys.exit(1)


# Cluster-scoped resources (no namespace)
CLUSTER_SCOPED_RESOURCES = {
    'namespaces', 'nodes', 'persistentvolumes', 'clusterroles',
    'clusterrolebindings', 'storageclasses', 'ingressclasses',
    'customresourcedefinitions', 'priorityclasses', 'runtimeclasses',
    'volumesnapshotclasses', 'csidrivers', 'csinodes', 'csistoragecapacities'
}


def parse_etcd_key(key: str) -> Dict[str, Optional[str]]:
    """
    Parse etcd key to extract resource type, namespace, and name.

    Kubernetes etcd keys follow patterns:
    - /registry/<resource>/<namespace>/<name> (namespaced)
    - /registry/<resource>/<name> (cluster-scoped)
    """
    parts = key.strip('/').split('/')

    if len(parts) < 3 or parts[0] != 'registry':
        return {'resource': None, 'namespace': None, 'name': None}

    resource = parts[1]

    # Determine if cluster-scoped or namespaced
    if resource in CLUSTER_SCOPED_RESOURCES or len(parts) == 3:
        # Cluster-scoped: /registry/<resource>/<name>
        return {
            'resource': resource,
            'namespace': None,
            'name': parts[2] if len(parts) >= 3 else None
        }
    else:
        # Namespaced: /registry/<resource>/<namespace>/<name>
        return {
            'resource': resource,
            'namespace': parts[2] if len(parts) >= 3 else None,
            'name': parts[3] if len(parts) >= 4 else None
        }


def decode_value(value: bytes) -> Optional[Dict[str, Any]]:
    """Decode etcd value (protobuf or json) to dict."""
    try:
        # Try JSON first
        return json.loads(value.decode('utf-8'))
    except (json.JSONDecodeError, UnicodeDecodeError):
        pass

    # Try to parse as protobuf (k8s.io/apimachinery/pkg/runtime serialization)
    # The format is typically: k8s\x00 + protobuf data
    if value.startswith(b'k8s\x00'):
        # This is protobuf format - would need protobuf definitions
        # For now, return None and handle with better error message
        return None

    return None


def extract_objects(
    db_path: str,
    resource: Optional[str] = None,
    namespace: Optional[str] = None,
    name: Optional[str] = None,
    all_namespaces: bool = False
) -> List[Dict[str, Any]]:
    """Extract objects from etcd database file."""

    if not Path(db_path).exists():
        print(f"Error: Database file not found: {db_path}", file=sys.stderr)
        sys.exit(1)

    results = []

    try:
        db = boltdb.BoltDB(db_path, readonly=True)

        # Iterate through all keys in the database
        for bucket_name in db.buckets():
            bucket = db.bucket(bucket_name)

            for key, value in bucket.items():
                key_str = key.decode('utf-8') if isinstance(key, bytes) else key

                # Parse the key
                parsed = parse_etcd_key(key_str)

                # Apply filters
                if resource and parsed['resource'] != resource:
                    continue

                # Handle namespace filtering
                if parsed['namespace'] is not None:  # Namespaced resource
                    if namespace and parsed['namespace'] != namespace:
                        continue
                    if not all_namespaces and namespace is None:
                        # Skip namespaced resources if no namespace specified and not --all-namespaces
                        continue
                elif namespace:
                    # Cluster-scoped resource but namespace filter specified
                    continue

                if name and parsed['name'] != name:
                    continue

                # Decode value
                obj = decode_value(value)

                if obj:
                    results.append({
                        'key': key_str,
                        'resource': parsed['resource'],
                        'namespace': parsed['namespace'],
                        'name': parsed['name'],
                        'object': obj
                    })
                else:
                    # Handle protobuf-encoded objects
                    print(f"Warning: Could not decode object at {key_str} (likely protobuf format)", file=sys.stderr)

        db.close()

    except Exception as e:
        print(f"Error reading database: {e}", file=sys.stderr)
        sys.exit(1)

    return results


def main():
    parser = argparse.ArgumentParser(
        description='Extract Kubernetes objects from etcd database files',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Extract all secrets from namespace 'default'
  %(prog)s --resource secrets --ns default db.etcd

  # Extract a specific secret
  %(prog)s --resource secrets --ns kube-system --name my-secret db.etcd

  # Extract all namespaces (cluster-scoped)
  %(prog)s --resource namespaces db.etcd

  # List all secrets across all namespaces
  %(prog)s --resource secrets --all-namespaces db.etcd

  # Extract specific ConfigMap
  %(prog)s --resource configmaps --ns default --name app-config db.etcd
        """
    )

    parser.add_argument('db_file', help='Path to etcd database file')
    parser.add_argument('-r', '--resource', help='Resource type (e.g., secrets, configmaps, pods)')
    parser.add_argument('-n', '--ns', '--namespace', dest='namespace',
                       help='Namespace (for namespaced resources)')
    parser.add_argument('--name', help='Object name')
    parser.add_argument('-A', '--all-namespaces', action='store_true',
                       help='Extract from all namespaces')
    parser.add_argument('-o', '--output', choices=['yaml', 'json'], default='yaml',
                       help='Output format (default: yaml)')
    parser.add_argument('-l', '--list', action='store_true',
                       help='List available resources in the database')

    args = parser.parse_args()

    if args.list:
        # List all resources in the database
        print("Scanning database for resources...", file=sys.stderr)
        all_objects = extract_objects(args.db_file, all_namespaces=True)

        resources = {}
        for obj in all_objects:
            res = obj['resource']
            if res not in resources:
                resources[res] = {'total': 0, 'namespaced': False}
            resources[res]['total'] += 1
            if obj['namespace']:
                resources[res]['namespaced'] = True

        print("\nAvailable resources:")
        print(f"{'Resource':<30} {'Type':<20} {'Count':<10}")
        print("-" * 60)
        for res, info in sorted(resources.items()):
            scope = 'namespaced' if info['namespaced'] else 'cluster-scoped'
            print(f"{res:<30} {scope:<20} {info['total']:<10}")

        return

    # Extract objects
    results = extract_objects(
        args.db_file,
        resource=args.resource,
        namespace=args.namespace,
        name=args.name,
        all_namespaces=args.all_namespaces
    )

    if not results:
        print("No objects found matching the criteria", file=sys.stderr)
        sys.exit(0)

    # Output results
    for result in results:
        if args.output == 'yaml':
            print(f"# Key: {result['key']}")
            if result['namespace']:
                print(f"# Namespace: {result['namespace']}")
            print(f"# Resource: {result['resource']}")
            print(f"# Name: {result['name']}")
            print("---")
            print(yaml.dump(result['object'], default_flow_style=False, sort_keys=False))
        else:  # json
            print(json.dumps(result, indent=2))

    print(f"\n# Extracted {len(results)} object(s)", file=sys.stderr)


if __name__ == '__main__':
    main()
