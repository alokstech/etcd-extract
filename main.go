package main

import (
	"embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	serve         = flag.Bool("serve", false, "Start web GUI server")
	port          = flag.String("port", "8080", "Port for web server (used with --serve)")
)

//go:embed web
var webFS embed.FS

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

var k8sFieldNames = map[string]map[int]string{
	"Namespace":                    {2: "spec", 3: "status"},
	"Namespace.spec":               {1: "finalizers"},
	"Namespace.status":             {1: "phase", 2: "conditions"},
	"Node":                         {2: "spec", 3: "status"},
	"Node.spec":                    {1: "podCIDR", 3: "providerID", 4: "unschedulable", 5: "taints", 7: "podCIDRs"},
	"Node.status":                  {1: "capacity", 2: "allocatable", 3: "phase", 4: "conditions", 5: "addresses", 6: "daemonEndpoints", 7: "nodeInfo", 8: "images", 9: "volumesInUse", 10: "volumesAttached"},
	"PersistentVolume":             {2: "spec", 3: "status"},
	"PersistentVolume.spec":        {1: "capacity", 2: "persistentVolumeSource", 3: "accessModes", 4: "claimRef", 5: "persistentVolumeReclaimPolicy", 6: "storageClassName", 7: "mountOptions", 8: "volumeMode", 9: "nodeAffinity"},
	"PersistentVolume.status":      {1: "phase", 2: "message", 3: "reason"},
	"PersistentVolumeClaim":        {2: "spec", 3: "status"},
	"PersistentVolumeClaim.spec":   {1: "accessModes", 2: "resources", 3: "volumeName", 4: "selector", 5: "storageClassName", 6: "volumeMode"},
	"PersistentVolumeClaim.status": {1: "phase", 2: "accessModes", 3: "capacity", 4: "conditions"},
	"Pod":                          {2: "spec", 3: "status"},
	"Pod.spec":                     {1: "volumes", 2: "containers", 3: "restartPolicy", 4: "terminationGracePeriodSeconds", 6: "dnsPolicy", 7: "nodeSelector", 8: "serviceAccountName", 11: "nodeName", 14: "securityContext", 16: "imagePullSecrets", 17: "hostname", 19: "affinity", 20: "schedulerName", 21: "initContainers", 22: "tolerations", 24: "priority", 25: "priorityClassName", 28: "preemptionPolicy"},
	"Pod.status":                   {1: "phase", 2: "conditions", 3: "message", 4: "reason", 5: "hostIP", 6: "podIP", 7: "startTime", 8: "containerStatuses", 10: "initContainerStatuses", 14: "podIPs"},
	"Deployment":                   {2: "spec", 3: "status"},
	"Deployment.spec":              {1: "replicas", 2: "selector", 3: "template", 4: "strategy", 5: "minReadySeconds", 6: "revisionHistoryLimit", 9: "progressDeadlineSeconds"},
	"Deployment.status":            {1: "observedGeneration", 2: "replicas", 3: "updatedReplicas", 4: "readyReplicas", 5: "unavailableReplicas", 6: "conditions", 7: "availableReplicas"},
	"ReplicaSet":                   {2: "spec", 3: "status"},
	"ReplicaSet.spec":              {1: "replicas", 2: "selector", 3: "template", 4: "minReadySeconds"},
	"ReplicaSet.status":            {1: "replicas", 2: "fullyLabeledReplicas", 3: "readyReplicas", 4: "availableReplicas", 5: "observedGeneration", 6: "conditions"},
	"DaemonSet":                    {2: "spec", 3: "status"},
	"DaemonSet.spec":               {1: "selector", 2: "template", 3: "updateStrategy", 4: "minReadySeconds", 5: "revisionHistoryLimit"},
	"DaemonSet.status":             {1: "currentNumberScheduled", 2: "numberMisscheduled", 3: "desiredNumberScheduled", 4: "numberReady", 5: "observedGeneration", 6: "updatedNumberScheduled", 7: "numberAvailable", 8: "numberUnavailable", 9: "conditions"},
	"StatefulSet":                  {2: "spec", 3: "status"},
	"StatefulSet.spec":             {1: "replicas", 2: "selector", 3: "template", 4: "volumeClaimTemplates", 5: "serviceName", 6: "podManagementPolicy", 7: "updateStrategy", 8: "revisionHistoryLimit", 9: "minReadySeconds"},
	"StatefulSet.status":           {1: "observedGeneration", 2: "replicas", 3: "readyReplicas", 4: "currentReplicas", 5: "updatedReplicas", 6: "currentRevision", 7: "updateRevision", 9: "conditions", 10: "availableReplicas"},
	"Service":                      {2: "spec", 3: "status"},
	"Service.spec":                 {1: "ports", 2: "selector", 3: "clusterIP", 4: "type", 5: "externalIPs", 6: "sessionAffinity", 7: "loadBalancerIP", 14: "ipFamilies", 15: "ipFamilyPolicy", 19: "clusterIPs"},
	"Service.status":               {1: "loadBalancer", 2: "conditions"},
	"Endpoints":                    {2: "subsets"},
	"Job":                          {2: "spec", 3: "status"},
	"Job.spec":                     {1: "parallelism", 2: "completions", 3: "activeDeadlineSeconds", 4: "selector", 5: "manualSelector", 6: "template", 7: "backoffLimit", 8: "ttlSecondsAfterFinished"},
	"Job.status":                   {1: "conditions", 2: "startTime", 3: "completionTime", 4: "active", 5: "succeeded", 6: "failed"},
	"CronJob":                      {2: "spec", 3: "status"},
	"CronJob.spec":                 {1: "schedule", 2: "startingDeadlineSeconds", 3: "concurrencyPolicy", 4: "suspend", 5: "jobTemplate", 6: "successfulJobsHistoryLimit", 7: "failedJobsHistoryLimit"},
	"CronJob.status":               {1: "active", 4: "lastScheduleTime", 5: "lastSuccessfulTime"},
	"Ingress":                      {2: "spec", 3: "status"},
	"Ingress.spec":                 {1: "ingressClassName", 2: "defaultBackend", 3: "tls", 4: "rules"},
	"Ingress.status":               {1: "loadBalancer"},
	"ClusterRole":                  {2: "rules", 3: "aggregationRule"},
	"ClusterRole.rules":            {1: "verbs", 2: "apiGroups", 3: "resources", 4: "resourceNames", 5: "nonResourceURLs"},
	"ClusterRoleBinding":           {2: "subjects", 3: "roleRef"},
	"ClusterRoleBinding.subjects":  {1: "kind", 2: "apiGroup", 3: "name", 4: "namespace"},
	"ClusterRoleBinding.roleRef":   {1: "apiGroup", 2: "kind", 3: "name"},
	"Role":                         {2: "rules"},
	"Role.rules":                   {1: "verbs", 2: "apiGroups", 3: "resources", 4: "resourceNames"},
	"RoleBinding":                  {2: "subjects", 3: "roleRef"},
	"RoleBinding.subjects":         {1: "kind", 2: "apiGroup", 3: "name", 4: "namespace"},
	"RoleBinding.roleRef":          {1: "apiGroup", 2: "kind", 3: "name"},
	"ServiceAccount":               {2: "secrets", 3: "imagePullSecrets", 4: "automountServiceAccountToken"},
	"StorageClass":                 {2: "provisioner", 3: "parameters", 4: "reclaimPolicy", 5: "mountOptions", 6: "allowVolumeExpansion", 7: "volumeBindingMode", 8: "allowedTopologies"},
	"CSINode":                      {2: "spec"},
	"CSINode.spec":                 {1: "drivers"},
	"LimitRange":                   {2: "spec"},
	"ResourceQuota":                {2: "spec", 3: "status"},
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

func isLikelyString(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
			return false
		}
	}
	return true
}

func decodeGenericField(f ProtoField, depth int) interface{} {
	switch f.WireType {
	case 0:
		return f.Varint
	case 1:
		return f.Fixed64
	case 2:
		sub, err := parseProtoMessage(f.Bytes)
		if err == nil && len(sub) > 0 {
			return decodeGenericProto(f.Bytes, depth+1)
		}
		if isLikelyString(f.Bytes) {
			return string(f.Bytes)
		}
		return base64.StdEncoding.EncodeToString(f.Bytes)
	case 5:
		return f.Fixed32
	}
	return nil
}

func isMapEntries(entries []ProtoField) bool {
	if len(entries) == 0 {
		return false
	}
	for _, e := range entries {
		if e.WireType != 2 {
			return false
		}
		ef, err := parseProtoMessage(e.Bytes)
		if err != nil || len(ef) != 2 {
			return false
		}
		f1, ok1 := ef[1]
		_, ok2 := ef[2]
		if !ok1 || !ok2 || len(f1) != 1 || f1[0].WireType != 2 {
			return false
		}
		if !isLikelyString(f1[0].Bytes) {
			return false
		}
	}
	return true
}

func decodeProtoFields(data []byte, names map[int]string, depth int) interface{} {
	if depth > 8 {
		if isLikelyString(data) {
			return string(data)
		}
		return base64.StdEncoding.EncodeToString(data)
	}

	fields, err := parseProtoMessage(data)
	if err != nil || len(fields) == 0 {
		if isLikelyString(data) {
			return string(data)
		}
		return base64.StdEncoding.EncodeToString(data)
	}

	// Detect timestamp pattern (field 1=seconds, optional field 2=nanos)
	if len(fields) <= 2 {
		if f1, ok := fields[1]; ok && len(f1) == 1 && f1[0].WireType == 0 {
			seconds := int64(f1[0].Varint)
			if seconds > 946684800 && seconds < 2524608000 {
				var nanos int64
				if f2, ok := fields[2]; ok && len(f2) > 0 && f2[0].WireType == 0 {
					nanos = int64(f2[0].Varint)
				}
				return time.Unix(seconds, nanos).UTC().Format(time.RFC3339)
			}
		}
	}

	result := make(map[string]interface{})
	for num, entries := range fields {
		key := fmt.Sprintf("field_%d", num)
		if names != nil {
			if name, ok := names[num]; ok {
				key = name
			}
		}
		if isMapEntries(entries) {
			m := make(map[string]interface{})
			for _, e := range entries {
				ef, _ := parseProtoMessage(e.Bytes)
				k := protoString(ef, 1)
				if k != "" && len(ef[2]) > 0 {
					m[k] = decodeGenericField(ef[2][0], depth+1)
				}
			}
			result[key] = m
		} else if len(entries) == 1 {
			result[key] = decodeGenericField(entries[0], depth)
		} else {
			var vals []interface{}
			for _, e := range entries {
				vals = append(vals, decodeGenericField(e, depth))
			}
			result[key] = vals
		}
	}
	return result
}

func decodeGenericProto(data []byte, depth int) interface{} {
	return decodeProtoFields(data, nil, depth)
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

	// Try JSON decode on raw inner content
	var jsonObj map[string]interface{}
	if json.Unmarshal(rawBytes, &jsonObj) == nil {
		for k, v := range jsonObj {
			result[k] = v
		}
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

	default:
		topNames := k8sFieldNames[kind]
		for fieldNum, entries := range innerFields {
			if fieldNum == 1 {
				continue
			}
			key := fmt.Sprintf("field_%d", fieldNum)
			if topNames != nil {
				if name, ok := topNames[fieldNum]; ok {
					key = name
				}
			} else if fieldNum == 2 {
				key = "spec"
			} else if fieldNum == 3 {
				key = "status"
			}

			sectionNames := k8sFieldNames[kind+"."+key]

			if len(entries) == 1 {
				if entries[0].WireType == 2 && sectionNames != nil {
					result[key] = decodeProtoFields(entries[0].Bytes, sectionNames, 0)
				} else {
					result[key] = decodeGenericField(entries[0], 0)
				}
			} else {
				var vals []interface{}
				for _, e := range entries {
					if e.WireType == 2 && sectionNames != nil {
						vals = append(vals, decodeProtoFields(e.Bytes, sectionNames, 0))
					} else {
						vals = append(vals, decodeGenericField(e, 0))
					}
				}
				result[key] = vals
			}
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
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) < 2 {
		return ParsedKey{FullPath: path}
	}

	startIdx := 0
	if parts[0] == "kubernetes.io" || parts[0] == "registry" {
		startIdx = 1
	}

	if startIdx >= len(parts) {
		return ParsedKey{FullPath: path}
	}

	remaining := parts[startIdx:]
	result := ParsedKey{
		Resource: remaining[0],
		FullPath: path,
	}

	if strings.Contains(remaining[0], ".") {
		// API group path: <group>/<resource>/[<namespace>/]<name>
		// Use segment count: 3 = cluster-scoped, 4+ = namespaced
		switch len(remaining) {
		case 3:
			result.Name = remaining[2]
		case 4:
			result.Namespace = remaining[2]
			result.Name = remaining[3]
		default:
			if len(remaining) >= 5 {
				result.Namespace = remaining[2]
				result.Name = remaining[3]
			}
		}
	} else {
		// Core resource path: <resource>/[<namespace>/]<name>
		// Use segment count: 2 = cluster-scoped, 3+ = namespaced
		switch len(remaining) {
		case 2:
			result.Name = remaining[1]
		case 3:
			result.Namespace = remaining[1]
			result.Name = remaining[2]
		default:
			if len(remaining) >= 4 {
				result.Namespace = remaining[1]
				result.Name = remaining[2]
			}
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

// Web GUI server

type webServer struct {
	dbPath string
	mu     sync.RWMutex
}

func (ws *webServer) jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (ws *webServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	ws.jsonResponse(w, map[string]interface{}{"loaded": ws.dbPath != "", "path": ws.dbPath})
}

func (ws *webServer) handleLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(req.Path); os.IsNotExist(err) {
		http.Error(w, "File not found: "+req.Path, http.StatusBadRequest)
		return
	}
	db, err := bolt.Open(req.Path, 0600, &bolt.Options{ReadOnly: true, Timeout: 5 * time.Second})
	if err != nil {
		http.Error(w, "Invalid etcd database: "+err.Error(), http.StatusBadRequest)
		return
	}
	db.Close()

	ws.mu.Lock()
	ws.dbPath = req.Path
	ws.mu.Unlock()

	ws.jsonResponse(w, map[string]interface{}{"success": true, "path": req.Path})
}

func (ws *webServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.ParseMultipartForm(512 << 20) // 512MB max
	file, header, err := r.FormFile("dbfile")
	if err != nil {
		http.Error(w, "Failed to read uploaded file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	tmpDir, err := os.MkdirTemp("", "etcd-extract-*")
	if err != nil {
		http.Error(w, "Failed to create temp directory", http.StatusInternalServerError)
		return
	}

	tmpPath := filepath.Join(tmpDir, header.Filename)
	dst, err := os.Create(tmpPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		os.RemoveAll(tmpDir)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	dst.Close()

	db, err := bolt.Open(tmpPath, 0600, &bolt.Options{ReadOnly: true, Timeout: 5 * time.Second})
	if err != nil {
		os.RemoveAll(tmpDir)
		http.Error(w, "Invalid etcd database: "+err.Error(), http.StatusBadRequest)
		return
	}
	db.Close()

	ws.mu.Lock()
	ws.dbPath = tmpPath
	ws.mu.Unlock()

	ws.jsonResponse(w, map[string]interface{}{"success": true, "filename": header.Filename, "path": tmpPath})
}

func (ws *webServer) withDB(w http.ResponseWriter, fn func(dbPath string)) {
	ws.mu.RLock()
	dbPath := ws.dbPath
	ws.mu.RUnlock()
	if dbPath == "" {
		http.Error(w, "No database loaded", http.StatusBadRequest)
		return
	}
	fn(dbPath)
}

func (ws *webServer) handleNamespaces(w http.ResponseWriter, r *http.Request) {
	ws.withDB(w, func(dbPath string) {
		db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer db.Close()

		nsSet := make(map[string]bool)
		db.View(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("key"))
			if bucket == nil {
				return nil
			}
			return bucket.ForEach(func(key, value []byte) error {
				path, _, err := parseEtcdV3Value(value)
				if err != nil {
					return nil
				}
				parsed := parseEtcdPath(path)
				if parsed.Namespace != "" {
					nsSet[parsed.Namespace] = true
				}
				return nil
			})
		})

		var namespaces []string
		for ns := range nsSet {
			namespaces = append(namespaces, ns)
		}
		sort.Strings(namespaces)
		ws.jsonResponse(w, map[string]interface{}{"namespaces": namespaces})
	})
}

func (ws *webServer) handleResources(w http.ResponseWriter, r *http.Request) {
	ws.withDB(w, func(dbPath string) {
		nsFilter := r.URL.Query().Get("namespace")

		db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer db.Close()

		type resSummary struct {
			Count      int  `json:"count"`
			Namespaced bool `json:"namespaced"`
		}
		resMap := make(map[string]*resSummary)

		db.View(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("key"))
			if bucket == nil {
				return nil
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
				if nsFilter != "" && parsed.Namespace != nsFilter {
					return nil
				}
				if _, exists := resMap[parsed.Resource]; !exists {
					resMap[parsed.Resource] = &resSummary{}
				}
				resMap[parsed.Resource].Count++
				if parsed.Namespace != "" {
					resMap[parsed.Resource].Namespaced = true
				}
				return nil
			})
		})

		type resInfo struct {
			Name       string `json:"name"`
			Count      int    `json:"count"`
			Namespaced bool   `json:"namespaced"`
		}
		var resources []resInfo
		for n, s := range resMap {
			resources = append(resources, resInfo{Name: n, Count: s.Count, Namespaced: s.Namespaced})
		}
		sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
		ws.jsonResponse(w, map[string]interface{}{"resources": resources})
	})
}

func (ws *webServer) handleObjects(w http.ResponseWriter, r *http.Request) {
	ws.withDB(w, func(dbPath string) {
		resourceFilter := r.URL.Query().Get("resource")
		nsFilter := r.URL.Query().Get("namespace")
		if resourceFilter == "" {
			http.Error(w, "resource parameter required", http.StatusBadRequest)
			return
		}

		db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer db.Close()

		type objEntry struct {
			Key       string `json:"key"`
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		}
		var objects []objEntry

		db.View(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("key"))
			if bucket == nil {
				return nil
			}
			return bucket.ForEach(func(key, value []byte) error {
				path, _, err := parseEtcdV3Value(value)
				if err != nil {
					return nil
				}
				parsed := parseEtcdPath(path)
				if parsed.Resource != resourceFilter {
					return nil
				}
				if nsFilter != "" && parsed.Namespace != nsFilter {
					return nil
				}
				objects = append(objects, objEntry{Key: parsed.FullPath, Namespace: parsed.Namespace, Name: parsed.Name})
				return nil
			})
		})

		sort.Slice(objects, func(i, j int) bool {
			if objects[i].Namespace != objects[j].Namespace {
				return objects[i].Namespace < objects[j].Namespace
			}
			return objects[i].Name < objects[j].Name
		})
		ws.jsonResponse(w, map[string]interface{}{"objects": objects})
	})
}

func (ws *webServer) handleObject(w http.ResponseWriter, r *http.Request) {
	ws.withDB(w, func(dbPath string) {
		resourceFilter := r.URL.Query().Get("resource")
		nsFilter := r.URL.Query().Get("namespace")
		nameFilter := r.URL.Query().Get("name")
		if resourceFilter == "" || nameFilter == "" {
			http.Error(w, "resource and name parameters required", http.StatusBadRequest)
			return
		}

		results, err := extractObjects(dbPath, resourceFilter, nsFilter, nameFilter, nsFilter == "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(results) == 0 {
			http.Error(w, "Object not found", http.StatusNotFound)
			return
		}

		obj := results[0]

		var yamlBuf strings.Builder
		fmt.Fprintf(&yamlBuf, "# Key: %s\n", obj.Key)
		if obj.Namespace != "" {
			fmt.Fprintf(&yamlBuf, "# Namespace: %s\n", obj.Namespace)
		}
		fmt.Fprintf(&yamlBuf, "# Resource: %s\n# Name: %s\n---\n", obj.Resource, obj.Name)
		yamlData, _ := yaml.Marshal(obj.Object)
		yamlBuf.Write(yamlData)

		jsonData, _ := json.MarshalIndent(obj.Object, "", "  ")

		ws.jsonResponse(w, map[string]interface{}{
			"key":       obj.Key,
			"resource":  obj.Resource,
			"namespace": obj.Namespace,
			"name":      obj.Name,
			"yaml":      yamlBuf.String(),
			"json":      string(jsonData),
		})
	})
}

func startWebServer(dbPath, listenPort string) {
	ws := &webServer{dbPath: dbPath}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", ws.handleStatus)
	mux.HandleFunc("/api/load", ws.handleLoad)
	mux.HandleFunc("/api/upload", ws.handleUpload)
	mux.HandleFunc("/api/namespaces", ws.handleNamespaces)
	mux.HandleFunc("/api/resources", ws.handleResources)
	mux.HandleFunc("/api/objects", ws.handleObjects)
	mux.HandleFunc("/api/object", ws.handleObject)

	subFS, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	addr := ":" + listenPort
	fmt.Fprintf(os.Stderr, "Starting etcd-extract web GUI at http://localhost%s\n", addr)
	if dbPath != "" {
		fmt.Fprintf(os.Stderr, "Database: %s\n", dbPath)
	}
	log.Fatal(http.ListenAndServe(addr, mux))
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
		fmt.Fprintf(os.Stderr, "  %s --resource configmaps --ns default --output json db.etcd\n\n", os.Args[0])

		fmt.Fprintf(os.Stderr, "  Web GUI:\n\n")
		fmt.Fprintf(os.Stderr, "  # Start web GUI with a database\n")
		fmt.Fprintf(os.Stderr, "  %s --serve db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Start web GUI on a custom port\n")
		fmt.Fprintf(os.Stderr, "  %s --serve --port 9090 db.etcd\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Start web GUI (load database via UI)\n")
		fmt.Fprintf(os.Stderr, "  %s --serve\n", os.Args[0])
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

	// Handle serve mode
	if *serve {
		initialDB := ""
		if flag.NArg() >= 1 {
			initialDB = flag.Arg(0)
		}
		startWebServer(initialDB, *port)
		return
	}

	// Get database file path
	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Error: database file required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	dbPath := flag.Arg(0)

	if flag.NArg() >= 2 && *name == "" {
		*name = flag.Arg(1)
	}

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
