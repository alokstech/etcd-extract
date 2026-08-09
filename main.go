package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

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

// Protobuf wire format parsing

type ProtoField struct {
	WireType int
	Bytes    []byte
	Varint   uint64
	Fixed64  uint64
	Fixed32  uint32
}

func readVarint(data []byte, offset int) (uint64, int, error) {
	if offset >= len(data) {
		return 0, offset, fmt.Errorf("unexpected end of data")
	}
	var val uint64
	var shift uint
	for i := offset; i < len(data) && i < offset+10; i++ {
		b := data[i]
		val |= uint64(b&0x7F) << shift
		if b < 0x80 {
			return val, i + 1, nil
		}
		shift += 7
	}
	return 0, offset, fmt.Errorf("unterminated varint")
}

func parseProtoMessage(data []byte) (map[int][]ProtoField, error) {
	fields := make(map[int][]ProtoField)
	offset := 0

	for offset < len(data) {
		tag, newOffset, err := readVarint(data, offset)
		if err != nil {
			return fields, err
		}
		offset = newOffset

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x7)
		if fieldNum == 0 {
			return fields, fmt.Errorf("invalid field number 0")
		}

		var field ProtoField
		field.WireType = wireType

		switch wireType {
		case 0: // varint
			val, newOffset, err := readVarint(data, offset)
			if err != nil {
				return fields, err
			}
			offset = newOffset
			field.Varint = val

		case 1: // 64-bit fixed
			if offset+8 > len(data) {
				return fields, fmt.Errorf("unexpected end for 64-bit field")
			}
			field.Fixed64 = binary.LittleEndian.Uint64(data[offset : offset+8])
			offset += 8

		case 2: // length-delimited
			length, newOffset, err := readVarint(data, offset)
			if err != nil {
				return fields, err
			}
			offset = newOffset
			end := offset + int(length)
			if end > len(data) || end < offset {
				return fields, fmt.Errorf("length %d exceeds data", length)
			}
			field.Bytes = data[offset:end]
			offset = end

		case 5: // 32-bit fixed
			if offset+4 > len(data) {
				return fields, fmt.Errorf("unexpected end for 32-bit field")
			}
			field.Fixed32 = binary.LittleEndian.Uint32(data[offset : offset+4])
			offset += 4

		default:
			return fields, fmt.Errorf("unsupported wire type %d", wireType)
		}

		fields[fieldNum] = append(fields[fieldNum], field)
	}

	return fields, nil
}

func protoString(fields map[int][]ProtoField, num int) string {
	if f, ok := fields[num]; ok && len(f) > 0 && f[0].WireType == 2 {
		return string(f[0].Bytes)
	}
	return ""
}

func protoInt64(fields map[int][]ProtoField, num int) int64 {
	if f, ok := fields[num]; ok && len(f) > 0 && f[0].WireType == 0 {
		return int64(f[0].Varint)
	}
	return 0
}

func protoBytes(fields map[int][]ProtoField, num int) []byte {
	if f, ok := fields[num]; ok && len(f) > 0 && f[0].WireType == 2 {
		return f[0].Bytes
	}
	return nil
}

func parseProtoStringMap(entries []ProtoField) map[string]string {
	m := make(map[string]string)
	for _, entry := range entries {
		if entry.WireType != 2 {
			continue
		}
		ef, err := parseProtoMessage(entry.Bytes)
		if err != nil {
			continue
		}
		key := protoString(ef, 1)
		val := protoString(ef, 2)
		if key != "" {
			m[key] = val
		}
	}
	return m
}

func parseObjectMeta(data []byte) map[string]interface{} {
	meta := make(map[string]interface{})
	fields, err := parseProtoMessage(data)
	if err != nil {
		return meta
	}

	if v := protoString(fields, 1); v != "" {
		meta["name"] = v
	}
	if v := protoString(fields, 2); v != "" {
		meta["generateName"] = v
	}
	if v := protoString(fields, 3); v != "" {
		meta["namespace"] = v
	}
	if v := protoString(fields, 5); v != "" {
		meta["uid"] = v
	}
	if v := protoString(fields, 6); v != "" {
		meta["resourceVersion"] = v
	}
	if v := protoInt64(fields, 7); v != 0 {
		meta["generation"] = v
	}

	// creationTimestamp (field 8)
	if tsBytes := protoBytes(fields, 8); tsBytes != nil {
		tsFields, err := parseProtoMessage(tsBytes)
		if err == nil {
			seconds := protoInt64(tsFields, 1)
			nanos := protoInt64(tsFields, 2)
			if seconds != 0 {
				meta["creationTimestamp"] = time.Unix(seconds, nanos).UTC().Format(time.RFC3339)
			}
		}
	}

	// labels (field 11, map<string,string>)
	if entries, ok := fields[11]; ok {
		labels := parseProtoStringMap(entries)
		if len(labels) > 0 {
			meta["labels"] = labels
		}
	}

	// annotations (field 12, map<string,string>)
	if entries, ok := fields[12]; ok {
		annotations := parseProtoStringMap(entries)
		if len(annotations) > 0 {
			meta["annotations"] = annotations
		}
	}

	// ownerReferences (field 13, repeated message)
	if entries, ok := fields[13]; ok {
		var ownerRefs []map[string]interface{}
		for _, e := range entries {
			if e.WireType != 2 {
				continue
			}
			orFields, err := parseProtoMessage(e.Bytes)
			if err != nil {
				continue
			}
			ref := make(map[string]interface{})
			if v := protoString(orFields, 1); v != "" {
				ref["apiVersion"] = v
			}
			if v := protoString(orFields, 3); v != "" {
				ref["kind"] = v
			}
			if v := protoString(orFields, 4); v != "" {
				ref["name"] = v
			}
			if v := protoString(orFields, 5); v != "" {
				ref["uid"] = v
			}
			if len(ref) > 0 {
				ownerRefs = append(ownerRefs, ref)
			}
		}
		if len(ownerRefs) > 0 {
			meta["ownerReferences"] = ownerRefs
		}
	}

	// finalizers (field 14, repeated string)
	if entries, ok := fields[14]; ok {
		var finalizers []string
		for _, e := range entries {
			if e.WireType == 2 && len(e.Bytes) > 0 {
				finalizers = append(finalizers, string(e.Bytes))
			}
		}
		if len(finalizers) > 0 {
			meta["finalizers"] = finalizers
		}
	}

	return meta
}

func decodeK8sProtobuf(data []byte) (map[string]interface{}, error) {
	if len(data) < 4 || string(data[:4]) != "k8s\x00" {
		return nil, fmt.Errorf("not k8s protobuf format")
	}

	wrapperFields, err := parseProtoMessage(data[4:])
	if err != nil {
		return nil, fmt.Errorf("failed to parse Unknown wrapper: %w", err)
	}

	result := make(map[string]interface{})

	// TypeMeta (field 1)
	if tmBytes := protoBytes(wrapperFields, 1); tmBytes != nil {
		tmFields, err := parseProtoMessage(tmBytes)
		if err == nil {
			if v := protoString(tmFields, 1); v != "" {
				result["apiVersion"] = v
			}
			if v := protoString(tmFields, 2); v != "" {
				result["kind"] = v
			}
		}
	}

	// Raw object (field 2)
	rawBytes := protoBytes(wrapperFields, 2)
	if rawBytes == nil {
		return result, nil
	}

	innerFields, err := parseProtoMessage(rawBytes)
	if err != nil {
		return result, nil
	}

	// ObjectMeta (field 1)
	if metaBytes := protoBytes(innerFields, 1); metaBytes != nil {
		result["metadata"] = parseObjectMeta(metaBytes)
	}

	kind, _ := result["kind"].(string)
	switch kind {
	case "ConfigMap":
		// data (field 2, map<string,string>)
		if entries, ok := innerFields[2]; ok {
			d := parseProtoStringMap(entries)
			if len(d) > 0 {
				result["data"] = d
			}
		}

	case "Secret":
		// data (field 2, map<string,bytes>)
		if entries, ok := innerFields[2]; ok {
			secretData := make(map[string]string)
			for _, entry := range entries {
				if entry.WireType != 2 {
					continue
				}
				ef, err := parseProtoMessage(entry.Bytes)
				if err != nil {
					continue
				}
				key := protoString(ef, 1)
				val := protoBytes(ef, 2)
				if key != "" {
					secretData[key] = base64.StdEncoding.EncodeToString(val)
				}
			}
			if len(secretData) > 0 {
				result["data"] = secretData
			}
		}
		// type (field 3)
		if v := protoString(innerFields, 3); v != "" {
			result["type"] = v
		}
	}

	return result, nil
}

// parseEtcdV3Value parses a mvccpb.KeyValue protobuf to extract the key path and decoded object
func parseEtcdV3Value(value []byte) (string, map[string]interface{}, error) {
	// Parse the mvccpb.KeyValue protobuf envelope
	kvFields, err := parseProtoMessage(value)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse KeyValue: %w", err)
	}

	// Field 1: etcd key (the Kubernetes path)
	path := protoString(kvFields, 1)
	if path == "" {
		return "", nil, fmt.Errorf("no key/path found")
	}

	// Field 5: value (the Kubernetes object)
	objData := protoBytes(kvFields, 5)
	if objData == nil {
		return path, nil, fmt.Errorf("no object data found")
	}

	// Try JSON first
	var obj map[string]interface{}
	if json.Unmarshal(objData, &obj) == nil {
		return path, obj, nil
	}

	// Try Kubernetes protobuf
	obj, err = decodeK8sProtobuf(objData)
	if err != nil {
		return path, nil, fmt.Errorf("failed to decode object: %w", err)
	}

	return path, obj, nil
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

func listResources(dbPath, resourceFilter, namespaceFilter string) error {
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	fmt.Fprintln(os.Stderr, "Scanning database...")

	type ObjectEntry struct {
		Namespace string
		Name      string
	}

	resources := make(map[string]*ResourceSummary)
	var entries []ObjectEntry
	listObjects := resourceFilter != ""

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

			if namespaceFilter != "" && parsed.Namespace != namespaceFilter {
				return nil
			}

			if resourceFilter != "" && parsed.Resource != resourceFilter {
				return nil
			}

			if _, exists := resources[parsed.Resource]; !exists {
				resources[parsed.Resource] = &ResourceSummary{}
			}

			resources[parsed.Resource].Total++
			if parsed.Namespace != "" {
				resources[parsed.Resource].Namespaced = true
			}

			if listObjects {
				entries = append(entries, ObjectEntry{
					Namespace: parsed.Namespace,
					Name:      parsed.Name,
				})
			}

			return nil
		})
	})

	if err != nil {
		return err
	}

	if listObjects {
		// List individual objects for a specific resource type
		if len(entries) == 0 {
			fmt.Fprintln(os.Stderr, "No objects found matching the criteria")
			return nil
		}

		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Namespace != entries[j].Namespace {
				return entries[i].Namespace < entries[j].Namespace
			}
			return entries[i].Name < entries[j].Name
		})

		hasNamespace := false
		for _, e := range entries {
			if e.Namespace != "" {
				hasNamespace = true
				break
			}
		}

		if hasNamespace {
			fmt.Printf("%-40s %-30s\n", "NAMESPACE", "NAME")
			fmt.Println(strings.Repeat("-", 70))
			for _, e := range entries {
				fmt.Printf("%-40s %-30s\n", e.Namespace, e.Name)
			}
		} else {
			fmt.Printf("%-30s\n", "NAME")
			fmt.Println(strings.Repeat("-", 30))
			for _, e := range entries {
				fmt.Printf("%-30s\n", e.Name)
			}
		}

		fmt.Fprintf(os.Stderr, "\nTotal: %d object(s)\n", len(entries))
		return nil
	}

	// List resource types summary
	var resourceNames []string
	for name := range resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Strings(resourceNames)

	header := "\nAvailable resources:"
	if namespaceFilter != "" {
		header = fmt.Sprintf("\nResources in namespace %q:", namespaceFilter)
	}
	fmt.Println(header)
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

func reorderArgs() {
	var flags, positional []string
	args := os.Args[1:]

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}

		flags = append(flags, arg)

		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") {
			continue
		}

		f := flag.Lookup(name)
		if f == nil {
			continue
		}

		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			continue
		}

		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}

	os.Args = append([]string{os.Args[0]}, append(flags, positional...)...)
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <db_file>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Extract Kubernetes objects from etcd v3 database files\n\n")
		fmt.Fprintf(os.Stderr, "Note: Flags can be used with single dash (-ns) or double dash (--ns).\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")

		fmt.Fprintf(os.Stderr, "\n  Listing:\n\n")
		fmt.Fprintf(os.Stderr, "  # List all resource types in the database\n")
		fmt.Fprintf(os.Stderr, "  %s --list db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # List all resource types in a specific namespace\n")
		fmt.Fprintf(os.Stderr, "  %s --list --ns openshift-config db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # List individual configmaps in a namespace\n")
		fmt.Fprintf(os.Stderr, "  %s --list --resource configmaps --ns openshift-config db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # List all secrets across all namespaces\n")
		fmt.Fprintf(os.Stderr, "  %s --list --resource secrets db.etcd\n\n", os.Args[0])

		fmt.Fprintf(os.Stderr, "  Extracting:\n\n")
		fmt.Fprintf(os.Stderr, "  # Extract a specific configmap by name\n")
		fmt.Fprintf(os.Stderr, "  %s --resource configmaps --ns openshift-config --name etcd-ca-bundle db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Extract a specific secret by name\n")
		fmt.Fprintf(os.Stderr, "  %s --resource secrets --ns kube-system --name my-secret db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Extract all configmaps in a namespace\n")
		fmt.Fprintf(os.Stderr, "  %s --resource configmaps --ns openshift-config db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Extract all secrets across all namespaces\n")
		fmt.Fprintf(os.Stderr, "  %s --resource secrets --all-namespaces db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Extract all namespaces (cluster-scoped resource)\n")
		fmt.Fprintf(os.Stderr, "  %s --resource namespaces db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Extract as JSON instead of YAML\n")
		fmt.Fprintf(os.Stderr, "  %s --resource configmaps --ns default --output json db.etcd\n", os.Args[0])
	}

	reorderArgs()
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
		if err := listResources(dbPath, *resource, *namespace); err != nil {
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
