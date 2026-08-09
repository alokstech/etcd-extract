package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	bolt "go.etcd.io/bbolt"
	"gopkg.in/yaml.v3"
)

var (
	resource      = flag.String("resource", "", "Resource type (e.g., secrets, configmaps, pods)")
	resourceShort = flag.String("r", "", "Resource type (short form)")
	namespace     = flag.String("namespace", "", "Namespace (for namespaced resources)")
	namespaceShort = flag.String("n", "", "Namespace (short form)")
	nsFlag        = flag.String("ns", "", "Namespace (alternative)")
	name          = flag.String("name", "", "Object name")
	allNamespaces = flag.Bool("all-namespaces", false, "Extract from all namespaces")
	allNsShort    = flag.Bool("A", false, "Extract from all namespaces (short form)")
	output        = flag.String("output", "yaml", "Output format (yaml or json)")
	outputShort   = flag.String("o", "", "Output format (short form)")
	list          = flag.Bool("list", false, "List available resources in the database")
	listShort     = flag.Bool("l", false, "List available resources (short form)")
)

// ClusterScopedResources defines resources that don't have a namespace
var clusterScopedResources = map[string]bool{
	"namespaces":                   true,
	"nodes":                        true,
	"persistentvolumes":            true,
	"clusterroles":                 true,
	"clusterrolebindings":          true,
	"storageclasses":               true,
	"ingressclasses":               true,
	"customresourcedefinitions":    true,
	"priorityclasses":              true,
	"runtimeclasses":               true,
	"volumesnapshotclasses":        true,
	"csidrivers":                   true,
	"csinodes":                     true,
	"csistoragecapacities":         true,
}

type ParsedKey struct {
	Resource  string
	Namespace string
	Name      string
	FullPath  string
}

type ExtractedObject struct {
	Key       string                 `json:"key" yaml:"key"`
	Resource  string                 `json:"resource" yaml:"resource"`
	Namespace string                 `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string                 `json:"name" yaml:"name"`
	Object    map[string]interface{} `json:"object" yaml:"object"`
}

type ResourceSummary struct {
	Total      int
	Namespaced bool
}

// parseEtcdV3Value extracts the path and object from etcd v3 value format
func parseEtcdV3Value(value []byte) (string, map[string]interface{}, error) {
	vStr := string(value)

	// Find the Kubernetes path (starts with /)
	pathStart := strings.Index(vStr, "/")
	if pathStart < 0 {
		return "", nil, fmt.Errorf("no path found")
	}

	// Find the end of the path (usually before JSON or binary data)
	pathEnd := pathStart + 1
	for pathEnd < len(vStr) && vStr[pathEnd] != ' ' && vStr[pathEnd] != '\x00' && vStr[pathEnd] != '*' {
		pathEnd++
	}

	path := vStr[pathStart:pathEnd]

	// Find JSON object
	jsonStart := strings.Index(vStr, "{\"kind\"")
	if jsonStart < 0 {
		return path, nil, fmt.Errorf("no JSON found")
	}

	// Parse JSON
	var obj map[string]interface{}
	// Try to find complete JSON by looking for matching braces
	for i := jsonStart; i < len(value); i++ {
		if value[i] == '}' {
			candidate := value[jsonStart : i+1]
			if json.Unmarshal(candidate, &obj) == nil {
				return path, obj, nil
			}
		}
	}

	return path, nil, fmt.Errorf("failed to parse JSON")
}

func parseEtcdPath(path string) ParsedKey {
	// Path format: /kubernetes.io/<resource>/<namespace>/<name>
	// or: /kubernetes.io/<group>/<resource>/<namespace>/<name>

	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) < 2 {
		return ParsedKey{FullPath: path}
	}

	// Skip known prefixes
	startIdx := 0
	if parts[0] == "kubernetes.io" || parts[0] == "registry" {
		startIdx = 1
	}

	if startIdx >= len(parts) {
		return ParsedKey{FullPath: path}
	}

	// Determine resource type
	resource := ""
	namespaceIdx := -1
	nameIdx := -1

	// Simple resources: /kubernetes.io/<resource>/<namespace>/<name>
	// or /kubernetes.io/<resource>/<name> for cluster-scoped
	if len(parts) > startIdx {
		resource = parts[startIdx]
	}

	// Check if next part looks like a group (contains dots)
	if startIdx+1 < len(parts) && strings.Contains(parts[startIdx+1], ".") {
		// This is a group: /kubernetes.io/<group>/<resource>/...
		if startIdx+2 < len(parts) {
			resource = parts[startIdx+2]
		}
		namespaceIdx = startIdx + 3
		nameIdx = startIdx + 4
	} else {
		// No group: /kubernetes.io/<resource>/...
		namespaceIdx = startIdx + 1
		nameIdx = startIdx + 2
	}

	result := ParsedKey{
		Resource: resource,
		FullPath: path,
	}

	// Determine if cluster-scoped
	if clusterScopedResources[resource] {
		// Cluster-scoped: name comes directly after resource
		if namespaceIdx < len(parts) {
			result.Name = parts[namespaceIdx]
		}
	} else {
		// Namespaced resource
		if namespaceIdx < len(parts) {
			result.Namespace = parts[namespaceIdx]
		}
		if nameIdx < len(parts) {
			result.Name = parts[nameIdx]
		}
	}

	return result
}

func extractObjects(dbPath, resourceFilter, namespaceFilter, nameFilter string, allNs bool) ([]ExtractedObject, error) {
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	var results []ExtractedObject

	err = db.View(func(tx *bolt.Tx) error {
		// etcd v3 stores everything in the "key" bucket
		bucket := tx.Bucket([]byte("key"))
		if bucket == nil {
			return fmt.Errorf("no 'key' bucket found - this may not be an etcd v3 database")
		}

		return bucket.ForEach(func(key, value []byte) error {
			// Parse etcd v3 value format
			path, obj, err := parseEtcdV3Value(value)
			if err != nil {
				// Skip entries we can't parse
				return nil
			}

			parsed := parseEtcdPath(path)

			// Apply resource filter
			if resourceFilter != "" && parsed.Resource != resourceFilter {
				return nil
			}

			// Handle namespace filtering
			if parsed.Namespace != "" {
				// Namespaced resource
				if namespaceFilter != "" && parsed.Namespace != namespaceFilter {
					return nil
				}
				if !allNs && namespaceFilter == "" {
					// Skip namespaced resources if no namespace specified and not --all-namespaces
					return nil
				}
			} else if namespaceFilter != "" {
				// Cluster-scoped resource but namespace filter specified
				return nil
			}

			// Apply name filter
			if nameFilter != "" && parsed.Name != nameFilter {
				return nil
			}

			results = append(results, ExtractedObject{
				Key:       parsed.FullPath,
				Resource:  parsed.Resource,
				Namespace: parsed.Namespace,
				Name:      parsed.Name,
				Object:    obj,
			})

			return nil
		})
	})

	return results, err
}

func listResources(dbPath string) error {
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	fmt.Fprintln(os.Stderr, "Scanning database for resources...")

	resources := make(map[string]*ResourceSummary)

	err = db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("key"))
		if bucket == nil {
			return fmt.Errorf("no 'key' bucket found")
		}

		return bucket.ForEach(func(key, value []byte) error {
			path, _, err := parseEtcdV3Value(value)
			if err != nil {
				return nil
			}

			parsed := parseEtcdPath(path)
			if parsed.Resource == "" {
				return nil
			}

			if _, exists := resources[parsed.Resource]; !exists {
				resources[parsed.Resource] = &ResourceSummary{}
			}

			resources[parsed.Resource].Total++
			if parsed.Namespace != "" {
				resources[parsed.Resource].Namespaced = true
			}

			return nil
		})
	})

	if err != nil {
		return err
	}

	// Sort resources by name
	var resourceNames []string
	for name := range resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Strings(resourceNames)

	// Print table
	fmt.Println("\nAvailable resources:")
	fmt.Printf("%-30s %-20s %-10s\n", "Resource", "Type", "Count")
	fmt.Println(strings.Repeat("-", 60))

	for _, name := range resourceNames {
		info := resources[name]
		scope := "cluster-scoped"
		if info.Namespaced {
			scope = "namespaced"
		}
		fmt.Printf("%-30s %-20s %-10d\n", name, scope, info.Total)
	}

	return nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <db_file>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Extract Kubernetes objects from etcd v3 database files\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Extract all secrets from namespace 'default'\n")
		fmt.Fprintf(os.Stderr, "  %s --resource secrets --ns default db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Extract a specific secret\n")
		fmt.Fprintf(os.Stderr, "  %s --resource secrets --ns kube-system --name my-secret db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Extract all namespaces (cluster-scoped)\n")
		fmt.Fprintf(os.Stderr, "  %s --resource namespaces db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # List all secrets across all namespaces\n")
		fmt.Fprintf(os.Stderr, "  %s --resource secrets --all-namespaces db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Extract specific ConfigMap\n")
		fmt.Fprintf(os.Stderr, "  %s --resource configmaps --ns openshift-config --name app-config db.etcd\n", os.Args[0])
	}

	flag.Parse()

	// Handle flag merging (short and long forms)
	if *resourceShort != "" {
		*resource = *resourceShort
	}
	if *namespaceShort != "" {
		*namespace = *namespaceShort
	}
	if *nsFlag != "" {
		*namespace = *nsFlag
	}
	if *outputShort != "" {
		*output = *outputShort
	}
	if *listShort {
		*list = true
	}
	if *allNsShort {
		*allNamespaces = true
	}

	// Get database file path
	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Error: database file required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	dbPath := flag.Arg(0)

	// Check if file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: Database file not found: %s\n", dbPath)
		os.Exit(1)
	}

	// Handle list mode
	if *list {
		if err := listResources(dbPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Extract objects
	results, err := extractObjects(dbPath, *resource, *namespace, *name, *allNamespaces)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Fprintln(os.Stderr, "No objects found matching the criteria")
		return
	}

	// Output results
	for _, result := range results {
		if *output == "yaml" {
			fmt.Printf("# Key: %s\n", result.Key)
			if result.Namespace != "" {
				fmt.Printf("# Namespace: %s\n", result.Namespace)
			}
			fmt.Printf("# Resource: %s\n", result.Resource)
			fmt.Printf("# Name: %s\n", result.Name)
			fmt.Println("---")

			yamlData, err := yaml.Marshal(result.Object)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error marshaling YAML: %v\n", err)
				continue
			}
			fmt.Println(string(yamlData))
		} else {
			jsonData, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
				continue
			}
			fmt.Println(string(jsonData))
		}
	}

	fmt.Fprintf(os.Stderr, "\n# Extracted %d object(s)\n", len(results))
}
