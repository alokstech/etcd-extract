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

var podSpecFields = map[int]string{
	1: "volumes", 2: "containers", 3: "restartPolicy", 4: "terminationGracePeriodSeconds",
	5: "activeDeadlineSeconds", 6: "dnsPolicy", 7: "nodeSelector", 8: "serviceAccountName",
	9: "serviceAccount", 10: "nodeName", 11: "hostNetwork", 12: "hostPID", 13: "hostIPC",
	14: "securityContext", 15: "imagePullSecrets", 16: "hostname", 17: "subdomain",
	18: "affinity", 19: "schedulerName", 20: "initContainers", 21: "automountServiceAccountToken",
	22: "tolerations", 23: "hostAliases", 24: "priorityClassName", 25: "priority",
	26: "dnsConfig", 27: "shareProcessNamespace", 28: "readinessGates",
	29: "runtimeClassName", 30: "enableServiceLinks", 31: "preemptionPolicy",
	32: "overhead", 33: "topologySpreadConstraints", 34: "ephemeralContainers",
	35: "setHostnameAsFQDN", 36: "os", 37: "hostUsers", 38: "schedulingGates",
}

var podTemplateFields = map[int]string{1: "metadata", 2: "spec"}

var containerFields = map[int]string{
	1: "name", 2: "image", 3: "command", 4: "args", 5: "workingDir",
	6: "ports", 7: "env", 8: "resources", 9: "volumeMounts",
	10: "livenessProbe", 11: "readinessProbe", 12: "lifecycle",
	13: "terminationMessagePath", 14: "imagePullPolicy", 15: "securityContext",
	16: "stdin", 17: "stdinOnce", 18: "tty", 19: "envFrom",
	20: "terminationMessagePolicy", 21: "volumeDevices", 22: "startupProbe",
	23: "resizePolicy", 24: "restartPolicy",
}

var containerPortFields = map[int]string{
	1: "name", 2: "hostPort", 3: "containerPort", 4: "protocol", 5: "hostIP",
}

var envVarFields = map[int]string{1: "name", 2: "value", 3: "valueFrom"}

var envVarSourceFields = map[int]string{
	1: "fieldRef", 2: "resourceFieldRef", 3: "configMapKeyRef", 4: "secretKeyRef",
}

var fieldRefFields = map[int]string{1: "apiVersion", 2: "fieldPath"}

var configMapKeyRefFields = map[int]string{1: "name", 2: "key", 3: "optional"}

var secretKeyRefFields = map[int]string{1: "name", 2: "key", 3: "optional"}

var volumeMountFields = map[int]string{
	1: "name", 2: "readOnly", 3: "mountPath", 4: "subPath",
	5: "mountPropagation", 6: "subPathExpr", 7: "recursiveReadOnly",
}

var resourceRequirementsFields = map[int]string{1: "limits", 2: "requests"}

var probeFields = map[int]string{
	1: "handler", 2: "initialDelaySeconds", 3: "timeoutSeconds",
	4: "periodSeconds", 5: "successThreshold", 6: "failureThreshold",
}

var probeHandlerFields = map[int]string{1: "exec", 2: "httpGet", 3: "tcpSocket"}

var execActionFields = map[int]string{1: "command"}

var httpGetFields = map[int]string{
	1: "path", 2: "port", 3: "host", 4: "scheme", 5: "httpHeaders",
}

var tcpSocketFields = map[int]string{1: "port", 2: "host"}

var containerSecurityContextFields = map[int]string{
	1: "capabilities", 2: "privileged", 3: "seLinuxOptions", 4: "runAsUser",
	6: "procMount", 7: "runAsNonRoot", 8: "readOnlyRootFilesystem", 10: "windowsOptions",
	11: "seccompProfile", 15: "allowPrivilegeEscalation", 18: "runAsGroup", 22: "appArmorProfile",
}

var capabilitiesFields = map[int]string{1: "add", 2: "drop"}

var seLinuxOptionsFields = map[int]string{1: "user", 2: "role", 3: "type", 4: "level"}

var seccompProfileFields = map[int]string{1: "type", 2: "localhostProfile"}

var appArmorProfileFields = map[int]string{1: "type", 2: "localhostProfile"}

var podSecurityContextFields = map[int]string{
	1: "seLinuxOptions", 2: "runAsUser", 3: "runAsNonRoot",
	4: "supplementalGroups", 5: "fsGroup", 6: "runAsGroup",
	7: "sysctls", 9: "fsGroupChangePolicy", 10: "seccompProfile",
	11: "appArmorProfile",
}

var envFromSourceFields = map[int]string{1: "prefix", 2: "configMapRef", 3: "secretRef"}

var configMapEnvSourceFields = map[int]string{1: "name", 2: "optional"}

var secretEnvSourceFields = map[int]string{1: "name", 2: "optional"}

var lifecycleFields = map[int]string{1: "postStart", 2: "preStop"}

var volumeFields = map[int]string{1: "name", 2: "volumeSource"}

var volumeSourceFields = map[int]string{
	1: "hostPath", 2: "emptyDir", 3: "gcePersistentDisk", 4: "awsElasticBlockStore",
	5: "gitRepo", 6: "secret", 7: "nfs", 8: "iscsi", 9: "glusterfs",
	10: "persistentVolumeClaim", 11: "rbd", 12: "flexVolume", 13: "cinder",
	14: "cephfs", 15: "flocker", 16: "downwardAPI", 17: "fc", 18: "azureFile",
	19: "configMap", 20: "vsphereVolume", 22: "azureDisk", 26: "projected",
	27: "storageos", 28: "csi", 29: "ephemeral", 30: "image",
}

var secretVolumeSourceFields = map[int]string{
	1: "secretName", 2: "items", 3: "defaultMode", 4: "optional",
}

var configMapVolumeSourceFields = map[int]string{
	1: "name", 2: "items", 3: "defaultMode", 4: "optional",
}

var keyToPathFields = map[int]string{1: "key", 2: "path", 3: "mode"}

var projectedVolumeSourceFields = map[int]string{1: "sources", 2: "defaultMode"}

var projectionFields = map[int]string{
	1: "secret", 2: "downwardAPI", 3: "configMap", 4: "serviceAccountToken",
}

var serviceAccountTokenProjectionFields = map[int]string{
	1: "audience", 2: "expirationSeconds", 3: "path",
}

var persistentVolumeClaimVolumeSourceFields = map[int]string{1: "claimName", 2: "readOnly"}

var hostPathVolumeSourceFields = map[int]string{1: "path", 2: "type"}

var nfsVolumeSourceFields = map[int]string{1: "server", 2: "path", 3: "readOnly"}

var localVolumeSourceFields = map[int]string{1: "path", 2: "fsType"}

var downwardAPIVolumeSourceFields = map[int]string{1: "items"}

var downwardAPIVolumeFileFields = map[int]string{
	1: "path", 2: "fieldRef", 3: "resourceFieldRef", 4: "mode",
}

var tolerationFields = map[int]string{
	1: "key", 2: "operator", 3: "value", 4: "effect", 5: "tolerationSeconds",
}

var imagePullSecretFields = map[int]string{1: "name"}

var affinityFields = map[int]string{1: "nodeAffinity", 2: "podAffinity", 3: "podAntiAffinity"}

var nodeAffinityFields = map[int]string{1: "required", 2: "preferred"}

var nodeSelectorFields = map[int]string{1: "nodeSelectorTerms"}

var preferredSchedulingTermFields = map[int]string{1: "weight", 2: "preference"}

var nodeSelectorTermFields = map[int]string{1: "matchExpressions", 2: "matchFields"}

var nodeSelectorRequirementFields = map[int]string{1: "key", 2: "operator", 3: "values"}

var topologySpreadConstraintFields = map[int]string{
	1: "maxSkew", 2: "topologyKey", 3: "whenUnsatisfiable", 4: "labelSelector",
	5: "minDomains", 6: "nodeAffinityPolicy", 7: "nodeTaintsPolicy", 8: "matchLabelKeys",
}

var labelSelectorFields = map[int]string{1: "matchLabels", 2: "matchExpressions"}

var matchExpressionsFields = map[int]string{1: "key", 2: "operator", 3: "values"}

var containerStatusFields = map[int]string{
	1: "name", 2: "state", 3: "lastState", 4: "ready", 5: "restartCount",
	6: "image", 7: "imageID", 8: "containerID", 9: "started",
	10: "allocatedResources", 11: "resources", 12: "volumeMounts", 13: "user",
}

var containerStateFields = map[int]string{1: "waiting", 2: "running", 3: "terminated"}

var containerUserFields = map[int]string{1: "linux"}

var linuxContainerUserFields = map[int]string{1: "uid", 2: "gid", 3: "supplementalGroups"}

var volumeMountStatusFields = map[int]string{
	1: "name", 2: "mountPath", 3: "readOnly", 4: "recursiveReadOnly",
}

var containerStateWaitingFields = map[int]string{1: "reason", 2: "message"}

var containerStateRunningFields = map[int]string{1: "startedAt"}

var containerStateTerminatedFields = map[int]string{
	1: "exitCode", 2: "signal", 3: "reason", 4: "message",
	5: "startedAt", 6: "finishedAt", 7: "containerID",
}

var podConditionFields = map[int]string{
	1: "type", 2: "status", 3: "lastProbeTime", 4: "lastTransitionTime",
	5: "reason", 6: "message", 7: "observedGeneration",
}

var nodeConditionFields = map[int]string{
	1: "type", 2: "status", 3: "lastHeartbeatTime", 4: "lastTransitionTime",
	5: "reason", 6: "message",
}

var replicaSetConditionFields = map[int]string{
	1: "type", 2: "status", 3: "lastTransitionTime", 4: "reason", 5: "message",
}

var metaConditionFields = map[int]string{
	1: "type", 2: "status", 3: "observedGeneration", 4: "lastTransitionTime",
	5: "reason", 6: "message",
}

var flowControlConditionFields = map[int]string{
	1: "type", 2: "status", 3: "lastTransitionTime", 4: "reason", 5: "message",
}

var deploymentStrategyFields = map[int]string{1: "type", 2: "rollingUpdate"}

var deploymentRollingUpdateFields = map[int]string{1: "maxUnavailable", 2: "maxSurge"}

var statefulSetRollingUpdateFields = map[int]string{1: "partition", 2: "maxUnavailable"}

var policyRuleFields = map[int]string{
	1: "verbs", 2: "apiGroups", 3: "resources", 4: "resourceNames", 5: "nonResourceURLs",
}

var rbacSubjectFields = map[int]string{1: "kind", 2: "apiGroup", 3: "name", 4: "namespace"}

var roleRefFields = map[int]string{1: "apiGroup", 2: "kind", 3: "name"}

var nodeAddressFields = map[int]string{1: "type", 2: "address"}

var nodeInfoFields = map[int]string{
	1: "machineID", 2: "systemUUID", 3: "bootID", 4: "kernelVersion",
	5: "osImage", 6: "containerRuntimeVersion", 7: "kubeletVersion",
	8: "kubeProxyVersion", 9: "operatingSystem", 10: "architecture",
}

var taintFields = map[int]string{1: "key", 2: "value", 3: "effect", 4: "timeAdded"}

var daemonEndpointsFields = map[int]string{1: "kubeletEndpoint"}

var kubeletEndpointFields = map[int]string{1: "port"}

var nodeImageFields = map[int]string{1: "names", 2: "sizeBytes"}

var runtimeHandlerFields = map[int]string{1: "name", 2: "features"}

var runtimeHandlerFeaturesFields = map[int]string{1: "recursiveReadOnlyMounts", 2: "userNamespaces"}

var nodeFeaturesFields = map[int]string{1: "supplementalGroupsPolicy"}

var endpointSubsetFields = map[int]string{1: "addresses", 2: "notReadyAddresses", 3: "ports"}

var endpointAddressFields = map[int]string{1: "ip", 2: "targetRef", 3: "hostname", 4: "nodeName"}

var endpointPortFields = map[int]string{1: "name", 2: "port", 3: "protocol", 4: "appProtocol"}

var servicePortFields = map[int]string{
	1: "name", 2: "protocol", 3: "port", 4: "targetPort", 5: "nodePort",
}

var intOrStringFields = map[int]string{1: "type", 2: "intVal", 3: "strVal"}

var objectMetaFields = map[int]string{
	1: "name", 2: "generateName", 3: "namespace", 4: "selfLink", 5: "uid",
	6: "resourceVersion", 7: "generation", 8: "creationTimestamp",
	9: "deletionTimestamp", 10: "deletionGracePeriodSeconds",
	11: "labels", 12: "annotations", 13: "ownerReferences", 14: "finalizers",
}

var configMapProjectionFields = map[int]string{1: "name", 2: "items", 3: "optional"}

var secretProjectionFields = map[int]string{1: "name", 2: "items", 3: "optional"}

var podAffinityFields = map[int]string{
	1: "requiredDuringSchedulingIgnoredDuringExecution",
	2: "preferredDuringSchedulingIgnoredDuringExecution",
}

var weightedPodAffinityTermFields = map[int]string{1: "weight", 2: "podAffinityTerm"}

var podAffinityTermFields = map[int]string{
	1: "labelSelector", 2: "namespaces", 3: "topologyKey",
	4: "namespaceSelector", 5: "matchLabelKeys", 6: "mismatchLabelKeys",
}

var flowSchemaSubjectFields = map[int]string{
	1: "kind", 2: "user", 3: "group", 4: "serviceAccount",
}

var priorityLevelConfigRefFields = map[int]string{1: "name"}

var distinguisherMethodFields = map[int]string{1: "type"}

var limitedFields = map[int]string{
	1: "nominalConcurrencyShares", 2: "limitResponse",
	3: "lendablePercent", 4: "borrowingLimitPercent",
}

var limitResponseFields = map[int]string{1: "type", 2: "queuing"}

var queuingFields = map[int]string{1: "queues", 2: "handSize", 3: "queueLengthLimit"}

var resourcePolicyRuleFields = map[int]string{
	1: "verbs", 2: "apiGroups", 3: "resources", 4: "clusterScope", 5: "namespaces",
}

var nonResourcePolicyRuleFields = map[int]string{1: "verbs", 6: "nonResourceURLs"}

var clientConfigFields = map[int]string{1: "service", 2: "caBundle", 3: "url"}

var webhookServiceFields = map[int]string{1: "namespace", 2: "name", 3: "path", 4: "port"}

var ruleFields = map[int]string{1: "apiGroups", 2: "apiVersions", 3: "resources", 4: "scope"}

var csiNodeDriverFields = map[int]string{
	1: "name", 2: "nodeID", 3: "topologyKeys", 4: "allocatable",
}

var csiPVSourceFields = map[int]string{
	1: "driver", 2: "volumeHandle", 3: "readOnly", 4: "fsType",
	5: "volumeAttributes", 6: "controllerPublishSecretRef",
	7: "nodeStageSecretRef", 8: "nodePublishSecretRef",
}

var pvSourceFields = map[int]string{
	1: "gcePersistentDisk", 2: "awsElasticBlockStore", 3: "hostPath",
	4: "glusterfs", 5: "nfs", 6: "rbd", 7: "iscsi", 8: "cinder", 9: "cephfs",
	14: "fc", 18: "flexVolume", 19: "azureDisk", 20: "local", 21: "storageos", 22: "csi",
}

var objectReferenceFields = map[int]string{
	1: "kind", 2: "namespace", 3: "name", 4: "uid",
	5: "apiVersion", 6: "resourceVersion", 7: "fieldPath",
}

var jobSpecFields = map[int]string{
	1: "parallelism", 2: "completions", 3: "activeDeadlineSeconds",
	4: "selector", 5: "manualSelector", 6: "template",
	7: "backoffLimit", 8: "ttlSecondsAfterFinished",
}

var jobTemplateSpecFields = map[int]string{1: "metadata", 2: "spec"}

var ingressTLSFields = map[int]string{1: "hosts", 2: "secretName"}

var limitRangeItemFields = map[int]string{
	1: "type", 2: "max", 3: "min", 4: "default",
	5: "defaultRequest", 6: "maxLimitRequestRatio",
}

var k8sFieldNames = map[string]map[int]string{
	"Namespace":                    {2: "spec", 3: "status"},
	"Namespace.spec":               {1: "finalizers"},
	"Namespace.status":             {1: "phase", 2: "conditions"},
	"Node":                         {2: "spec", 3: "status"},
	"Node.spec":                    {1: "podCIDR", 2: "externalID", 3: "providerID", 4: "unschedulable", 5: "taints", 7: "podCIDRs"},
	"Node.status":                  {1: "capacity", 2: "allocatable", 3: "phase", 4: "conditions", 5: "addresses", 6: "daemonEndpoints", 7: "nodeInfo", 8: "images", 9: "volumesInUse", 10: "volumesAttached", 11: "config", 12: "runtimeHandlers", 13: "features"},
	"PersistentVolume":             {2: "spec", 3: "status"},
	"PersistentVolume.spec":                       {1: "capacity", 2: "persistentVolumeSource", 3: "accessModes", 4: "claimRef", 5: "persistentVolumeReclaimPolicy", 6: "storageClassName", 7: "mountOptions", 8: "volumeMode", 9: "nodeAffinity"},
	"PersistentVolume.spec.persistentVolumeSource": {1: "gcePersistentDisk", 2: "awsElasticBlockStore", 3: "hostPath", 4: "glusterfs", 5: "nfs", 6: "rbd", 7: "iscsi", 8: "cinder", 9: "cephfs", 14: "fc", 18: "flexVolume", 19: "azureDisk", 20: "local", 21: "storageos", 22: "csi"},
	"PersistentVolume.status":      {1: "phase", 2: "message", 3: "reason", 4: "lastPhaseTransitionTime"},
	"PersistentVolumeClaim":        {2: "spec", 3: "status"},
	"PersistentVolumeClaim.spec":   {1: "accessModes", 2: "resources", 3: "volumeName", 4: "selector", 5: "storageClassName", 6: "volumeMode"},
	"PersistentVolumeClaim.status": {1: "phase", 2: "accessModes", 3: "capacity", 4: "conditions"},
	"Pod":                          {2: "spec", 3: "status"},
	"Pod.spec":                     podSpecFields,
	"Pod.status":                   {1: "phase", 2: "conditions", 3: "message", 4: "reason", 5: "hostIP", 6: "podIP", 7: "startTime", 8: "containerStatuses", 9: "qosClass", 10: "initContainerStatuses", 11: "nominatedNodeName", 12: "podIPs", 13: "ephemeralContainerStatuses", 14: "resize", 16: "hostIPs", 17: "observedGeneration"},
	"Deployment":                   {2: "spec", 3: "status"},
	"Deployment.spec":              {1: "replicas", 2: "selector", 3: "template", 4: "strategy", 5: "minReadySeconds", 6: "revisionHistoryLimit", 7: "paused", 9: "progressDeadlineSeconds"},
	"Deployment.spec.template":     podTemplateFields,
	"Deployment.spec.template.spec": podSpecFields,
	"Deployment.status":            {1: "observedGeneration", 2: "replicas", 3: "updatedReplicas", 4: "availableReplicas", 5: "unavailableReplicas", 6: "conditions", 7: "readyReplicas", 8: "collisionCount"},
	"Deployment.status.conditions": {1: "type", 2: "status", 4: "reason", 5: "message", 6: "lastUpdateTime", 7: "lastTransitionTime"},
	"ReplicaSet":                   {2: "spec", 3: "status"},
	"ReplicaSet.spec":              {1: "replicas", 2: "selector", 3: "template", 4: "minReadySeconds"},
	"ReplicaSet.spec.template":     podTemplateFields,
	"ReplicaSet.spec.template.spec": podSpecFields,
	"ReplicaSet.status":            {1: "replicas", 2: "fullyLabeledReplicas", 3: "readyReplicas", 4: "availableReplicas", 5: "observedGeneration", 6: "conditions"},
	"DaemonSet":                    {2: "spec", 3: "status"},
	"DaemonSet.spec":               {1: "selector", 2: "template", 3: "updateStrategy", 4: "minReadySeconds", 5: "revisionHistoryLimit"},
	"DaemonSet.spec.template":      podTemplateFields,
	"DaemonSet.spec.template.spec": podSpecFields,
	"DaemonSet.status":             {1: "currentNumberScheduled", 2: "numberMisscheduled", 3: "desiredNumberScheduled", 4: "numberReady", 5: "observedGeneration", 6: "updatedNumberScheduled", 7: "numberAvailable", 8: "numberUnavailable", 9: "conditions"},
	"StatefulSet":                  {2: "spec", 3: "status"},
	"StatefulSet.spec":             {1: "replicas", 2: "selector", 3: "template", 4: "volumeClaimTemplates", 5: "serviceName", 6: "podManagementPolicy", 7: "updateStrategy", 8: "revisionHistoryLimit", 9: "minReadySeconds"},
	"StatefulSet.spec.template":    podTemplateFields,
	"StatefulSet.spec.template.spec": podSpecFields,
	"StatefulSet.status":           {1: "observedGeneration", 2: "replicas", 3: "readyReplicas", 4: "currentReplicas", 5: "updatedReplicas", 6: "currentRevision", 7: "updateRevision", 9: "conditions", 10: "availableReplicas"},
	"Service":                      {2: "spec", 3: "status"},
	"Service.spec":                 {1: "ports", 2: "selector", 3: "clusterIP", 4: "type", 5: "externalIPs", 6: "sessionAffinity", 7: "loadBalancerIP", 8: "sessionAffinityConfig", 9: "loadBalancerSourceRanges", 10: "externalName", 11: "externalTrafficPolicy", 12: "healthCheckNodePort", 13: "publishNotReadyAddresses", 17: "ipFamilyPolicy", 18: "clusterIPs", 19: "ipFamilies", 22: "internalTrafficPolicy", 23: "allocateLoadBalancerNodePorts", 24: "loadBalancerClass", 25: "trafficDistribution"},
	"Service.spec.ports":           {1: "name", 2: "protocol", 3: "port", 4: "targetPort", 5: "nodePort"},
	"Service.status":               {1: "loadBalancer", 2: "conditions"},
	"Endpoints":                          {2: "subsets"},
	"Endpoints.subsets":                   {1: "addresses", 2: "notReadyAddresses", 3: "ports"},
	"Endpoints.subsets.addresses":         {1: "ip", 2: "targetRef", 3: "hostname", 4: "nodeName"},
	"Endpoints.subsets.notReadyAddresses": {1: "ip", 2: "targetRef", 3: "hostname", 4: "nodeName"},
	"Endpoints.subsets.ports":             {1: "name", 2: "port", 3: "protocol", 4: "appProtocol"},
	"Job":                          {2: "spec", 3: "status"},
	"Job.spec":                     {1: "parallelism", 2: "completions", 3: "activeDeadlineSeconds", 4: "selector", 5: "manualSelector", 6: "template", 7: "backoffLimit", 8: "ttlSecondsAfterFinished"},
	"Job.spec.template":            podTemplateFields,
	"Job.spec.template.spec":       podSpecFields,
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
	"CSINode":                            {2: "spec"},
	"CSINode.spec":                       {1: "drivers"},
	"CSINode.spec.drivers.allocatable":   {1: "count"},
	"CSIDriver":                    {2: "spec"},
	"CSIDriver.spec":               {1: "attachRequired", 2: "podInfoOnMount", 3: "volumeLifecycleModes", 4: "storageCapacity", 5: "fsGroupPolicy", 6: "tokenRequests", 7: "requiresRepublish", 8: "seLinuxMount", 9: "nodeAllocatableUpdatePeriodSeconds"},
	"PriorityClass":                {2: "value", 3: "globalDefault", 4: "description", 5: "preemptionPolicy"},
	"IngressClass":                 {2: "spec"},
	"IngressClass.spec":            {1: "controller", 2: "parameters"},
	"IngressClass.spec.parameters": {1: "apiGroup", 2: "kind", 3: "name", 4: "scope", 5: "namespace"},
	"FlowSchema":                   {2: "spec", 3: "status"},
	"FlowSchema.spec":              {1: "priorityLevelConfiguration", 2: "matchingPrecedence", 3: "distinguisherMethod", 4: "rules"},
	"FlowSchema.spec.rules":        {1: "subjects", 2: "resourceRules", 3: "nonResourceRules"},
	"FlowSchema.status":            {1: "conditions"},
	"PriorityLevelConfiguration":          {2: "spec", 3: "status"},
	"PriorityLevelConfiguration.spec":     {1: "type", 2: "limited", 3: "exempt"},
	"PriorityLevelConfiguration.status":   {1: "conditions"},
	"PriorityLevelConfiguration.spec.exempt": {1: "nominalConcurrencyShares", 2: "lendablePercent"},
	"ValidatingWebhookConfiguration":              {2: "webhooks"},
	"ValidatingWebhookConfiguration.webhooks":          {1: "name", 2: "clientConfig", 3: "rules", 4: "failurePolicy", 5: "namespaceSelector", 6: "sideEffects", 7: "timeoutSeconds", 8: "admissionReviewVersions", 9: "matchPolicy", 10: "objectSelector", 11: "matchConditions"},
	"ValidatingWebhookConfiguration.webhooks.rules":    {1: "operations", 2: "rule"},
	"ValidatingWebhookConfiguration.webhooks.rules.rule": {1: "apiGroups", 2: "apiVersions", 3: "resources", 4: "scope"},
	"MutatingWebhookConfiguration":                {2: "webhooks"},
	"MutatingWebhookConfiguration.webhooks":            {1: "name", 2: "clientConfig", 3: "rules", 4: "failurePolicy", 5: "namespaceSelector", 6: "sideEffects", 7: "timeoutSeconds", 8: "admissionReviewVersions", 9: "matchPolicy", 10: "reinvocationPolicy", 11: "objectSelector", 12: "matchConditions"},
	"MutatingWebhookConfiguration.webhooks.rules":      {1: "operations", 2: "rule"},
	"MutatingWebhookConfiguration.webhooks.rules.rule": {1: "apiGroups", 2: "apiVersions", 3: "resources", 4: "scope"},
	"LimitRange":                   {2: "spec"},
	"ResourceQuota":                {2: "spec", 3: "status"},
}

func init() {
	podSpecPaths := []string{
		"Pod.spec",
		"Deployment.spec.template.spec",
		"ReplicaSet.spec.template.spec",
		"DaemonSet.spec.template.spec",
		"StatefulSet.spec.template.spec",
		"Job.spec.template.spec",
		"CronJob.spec.jobTemplate.spec.template.spec",
	}
	for _, p := range podSpecPaths {
		registerPodSpecChildren(p)
	}

	registerContainerStatusPaths("Pod.status")

	// Template metadata sub-paths
	templateMetaPaths := []string{
		"Deployment.spec.template.metadata",
		"ReplicaSet.spec.template.metadata",
		"DaemonSet.spec.template.metadata",
		"StatefulSet.spec.template.metadata",
		"Job.spec.template.metadata",
		"CronJob.spec.jobTemplate.spec.template.metadata",
		"CronJob.spec.jobTemplate.metadata",
	}
	for _, p := range templateMetaPaths {
		k8sFieldNames[p] = objectMetaFields
	}

	// CronJob jobTemplate sub-paths
	k8sFieldNames["CronJob.spec.jobTemplate"] = jobTemplateSpecFields
	k8sFieldNames["CronJob.spec.jobTemplate.spec"] = jobSpecFields
	k8sFieldNames["CronJob.spec.jobTemplate.spec.template"] = podTemplateFields
	k8sFieldNames["CronJob.spec.jobTemplate.spec.template.spec"] = podSpecFields
	k8sFieldNames["CronJob.spec.jobTemplate.spec.selector"] = labelSelectorFields
	k8sFieldNames["CronJob.spec.jobTemplate.spec.selector.matchExpressions"] = matchExpressionsFields

	// Selector sub-paths
	for _, p := range []string{
		"Deployment.spec.selector",
		"ReplicaSet.spec.selector",
		"DaemonSet.spec.selector",
		"StatefulSet.spec.selector",
		"Job.spec.selector",
		"PersistentVolumeClaim.spec.selector",
	} {
		k8sFieldNames[p] = labelSelectorFields
		k8sFieldNames[p+".matchExpressions"] = matchExpressionsFields
	}

	// Strategy sub-paths
	k8sFieldNames["Deployment.spec.strategy"] = deploymentStrategyFields
	k8sFieldNames["Deployment.spec.strategy.rollingUpdate"] = deploymentRollingUpdateFields
	k8sFieldNames["Deployment.spec.strategy.rollingUpdate.maxUnavailable"] = intOrStringFields
	k8sFieldNames["Deployment.spec.strategy.rollingUpdate.maxSurge"] = intOrStringFields
	k8sFieldNames["DaemonSet.spec.updateStrategy"] = deploymentStrategyFields
	k8sFieldNames["DaemonSet.spec.updateStrategy.rollingUpdate"] = deploymentRollingUpdateFields
	k8sFieldNames["DaemonSet.spec.updateStrategy.rollingUpdate.maxUnavailable"] = intOrStringFields
	k8sFieldNames["DaemonSet.spec.updateStrategy.rollingUpdate.maxSurge"] = intOrStringFields
	k8sFieldNames["StatefulSet.spec.updateStrategy"] = deploymentStrategyFields
	k8sFieldNames["StatefulSet.spec.updateStrategy.rollingUpdate"] = statefulSetRollingUpdateFields
	k8sFieldNames["StatefulSet.spec.updateStrategy.rollingUpdate.maxUnavailable"] = intOrStringFields

	// Condition sub-paths (each type has different field layouts)
	k8sFieldNames["Pod.status.conditions"] = podConditionFields
	k8sFieldNames["Node.status.conditions"] = nodeConditionFields
	k8sFieldNames["Namespace.status.conditions"] = metaConditionFields
	k8sFieldNames["ReplicaSet.status.conditions"] = replicaSetConditionFields
	k8sFieldNames["DaemonSet.status.conditions"] = replicaSetConditionFields
	k8sFieldNames["StatefulSet.status.conditions"] = replicaSetConditionFields
	k8sFieldNames["Job.status.conditions"] = podConditionFields
	k8sFieldNames["PersistentVolumeClaim.status.conditions"] = podConditionFields
	k8sFieldNames["FlowSchema.status.conditions"] = flowControlConditionFields
	k8sFieldNames["PriorityLevelConfiguration.status.conditions"] = flowControlConditionFields
	k8sFieldNames["Service.status.conditions"] = metaConditionFields

	// RBAC sub-paths
	k8sFieldNames["ClusterRole.rules"] = policyRuleFields
	k8sFieldNames["Role.rules"] = policyRuleFields
	for _, kind := range []string{"ClusterRoleBinding", "RoleBinding"} {
		k8sFieldNames[kind+".subjects"] = rbacSubjectFields
		k8sFieldNames[kind+".roleRef"] = roleRefFields
	}

	// Node sub-paths
	k8sFieldNames["Node.spec.taints"] = taintFields
	k8sFieldNames["Node.status.addresses"] = nodeAddressFields
	k8sFieldNames["Node.status.nodeInfo"] = nodeInfoFields
	k8sFieldNames["Node.status.images"] = nodeImageFields
	k8sFieldNames["Node.status.daemonEndpoints"] = daemonEndpointsFields
	k8sFieldNames["Node.status.daemonEndpoints.kubeletEndpoint"] = kubeletEndpointFields
	k8sFieldNames["Node.status.runtimeHandlers"] = runtimeHandlerFields
	k8sFieldNames["Node.status.runtimeHandlers.features"] = runtimeHandlerFeaturesFields
	k8sFieldNames["Node.status.features"] = nodeFeaturesFields

	// PersistentVolume sub-paths
	k8sFieldNames["PersistentVolume.spec.persistentVolumeSource"] = pvSourceFields
	k8sFieldNames["PersistentVolume.spec.claimRef"] = objectReferenceFields
	k8sFieldNames["PersistentVolume.spec.nodeAffinity"] = nodeAffinityFields
	k8sFieldNames["PersistentVolume.spec.nodeAffinity.required"] = nodeSelectorFields
	k8sFieldNames["PersistentVolume.spec.nodeAffinity.required.nodeSelectorTerms"] = nodeSelectorTermFields
	k8sFieldNames["PersistentVolume.spec.nodeAffinity.required.nodeSelectorTerms.matchExpressions"] = nodeSelectorRequirementFields
	k8sFieldNames["PersistentVolume.spec.nodeAffinity.required.nodeSelectorTerms.matchFields"] = nodeSelectorRequirementFields
	k8sFieldNames["PersistentVolume.spec.persistentVolumeSource.nfs"] = nfsVolumeSourceFields
	k8sFieldNames["PersistentVolume.spec.persistentVolumeSource.hostPath"] = hostPathVolumeSourceFields
	k8sFieldNames["PersistentVolume.spec.persistentVolumeSource.local"] = localVolumeSourceFields
	k8sFieldNames["PersistentVolume.spec.persistentVolumeSource.csi"] = csiPVSourceFields

	// PersistentVolumeClaim sub-paths
	k8sFieldNames["PersistentVolumeClaim.spec.resources"] = resourceRequirementsFields

	// Endpoints sub-paths
	k8sFieldNames["Endpoints.subsets"] = endpointSubsetFields
	k8sFieldNames["Endpoints.subsets.addresses"] = endpointAddressFields
	k8sFieldNames["Endpoints.subsets.notReadyAddresses"] = endpointAddressFields
	k8sFieldNames["Endpoints.subsets.ports"] = endpointPortFields

	// Service sub-paths
	k8sFieldNames["Service.spec.ports"] = servicePortFields
	k8sFieldNames["Service.spec.ports.targetPort"] = intOrStringFields

	// Ingress sub-paths
	k8sFieldNames["Ingress.spec.tls"] = ingressTLSFields

	// FlowSchema sub-paths
	k8sFieldNames["FlowSchema.spec.priorityLevelConfiguration"] = priorityLevelConfigRefFields
	k8sFieldNames["FlowSchema.spec.distinguisherMethod"] = distinguisherMethodFields
	k8sFieldNames["FlowSchema.spec.rules.subjects"] = flowSchemaSubjectFields
	k8sFieldNames["FlowSchema.spec.rules.resourceRules"] = resourcePolicyRuleFields
	k8sFieldNames["FlowSchema.spec.rules.nonResourceRules"] = nonResourcePolicyRuleFields

	// PriorityLevelConfiguration sub-paths
	k8sFieldNames["PriorityLevelConfiguration.spec.limited"] = limitedFields
	k8sFieldNames["PriorityLevelConfiguration.spec.limited.limitResponse"] = limitResponseFields
	k8sFieldNames["PriorityLevelConfiguration.spec.limited.limitResponse.queuing"] = queuingFields

	// Webhook sub-paths
	for _, kind := range []string{"ValidatingWebhookConfiguration", "MutatingWebhookConfiguration"} {
		k8sFieldNames[kind+".webhooks.clientConfig"] = clientConfigFields
		k8sFieldNames[kind+".webhooks.clientConfig.service"] = webhookServiceFields
		k8sFieldNames[kind+".webhooks.namespaceSelector"] = labelSelectorFields
		k8sFieldNames[kind+".webhooks.namespaceSelector.matchExpressions"] = matchExpressionsFields
		k8sFieldNames[kind+".webhooks.objectSelector"] = labelSelectorFields
		k8sFieldNames[kind+".webhooks.objectSelector.matchExpressions"] = matchExpressionsFields
	}

	// CSINode sub-paths
	k8sFieldNames["CSINode.spec.drivers"] = csiNodeDriverFields
	k8sFieldNames["CSINode.spec.drivers.allocatable"] = map[int]string{1: "count"}

	// ServiceAccount sub-paths
	k8sFieldNames["ServiceAccount.secrets"] = objectReferenceFields
	k8sFieldNames["ServiceAccount.imagePullSecrets"] = imagePullSecretFields

	// LimitRange sub-paths
	k8sFieldNames["LimitRange.spec"] = map[int]string{1: "limits"}
	k8sFieldNames["LimitRange.spec.limits"] = limitRangeItemFields

	// ResourceQuota sub-paths
	k8sFieldNames["ResourceQuota.spec"] = map[int]string{1: "hard", 2: "scopes", 3: "scopeSelector"}
	k8sFieldNames["ResourceQuota.status"] = map[int]string{1: "hard", 2: "used"}
}

func registerPodSpecChildren(base string) {
	for _, cField := range []string{"containers", "initContainers", "ephemeralContainers"} {
		c := base + "." + cField
		k8sFieldNames[c] = containerFields
		k8sFieldNames[c+".ports"] = containerPortFields
		k8sFieldNames[c+".env"] = envVarFields
		k8sFieldNames[c+".env.valueFrom"] = envVarSourceFields
		k8sFieldNames[c+".env.valueFrom.fieldRef"] = fieldRefFields
		k8sFieldNames[c+".env.valueFrom.configMapKeyRef"] = configMapKeyRefFields
		k8sFieldNames[c+".env.valueFrom.secretKeyRef"] = secretKeyRefFields
		k8sFieldNames[c+".volumeMounts"] = volumeMountFields
		k8sFieldNames[c+".resources"] = resourceRequirementsFields
		for _, probe := range []string{"livenessProbe", "readinessProbe", "startupProbe"} {
			k8sFieldNames[c+"."+probe] = probeFields
			k8sFieldNames[c+"."+probe+".handler"] = probeHandlerFields
			k8sFieldNames[c+"."+probe+".handler.exec"] = execActionFields
			k8sFieldNames[c+"."+probe+".handler.httpGet"] = httpGetFields
			k8sFieldNames[c+"."+probe+".handler.httpGet.port"] = intOrStringFields
			k8sFieldNames[c+"."+probe+".handler.tcpSocket"] = tcpSocketFields
			k8sFieldNames[c+"."+probe+".handler.tcpSocket.port"] = intOrStringFields
		}
		k8sFieldNames[c+".securityContext"] = containerSecurityContextFields
		k8sFieldNames[c+".securityContext.capabilities"] = capabilitiesFields
		k8sFieldNames[c+".securityContext.seLinuxOptions"] = seLinuxOptionsFields
		k8sFieldNames[c+".securityContext.seccompProfile"] = seccompProfileFields
		k8sFieldNames[c+".securityContext.appArmorProfile"] = appArmorProfileFields
		k8sFieldNames[c+".envFrom"] = envFromSourceFields
		k8sFieldNames[c+".envFrom.configMapRef"] = configMapEnvSourceFields
		k8sFieldNames[c+".envFrom.secretRef"] = secretEnvSourceFields
		k8sFieldNames[c+".lifecycle"] = lifecycleFields
	}

	v := base + ".volumes"
	k8sFieldNames[v] = volumeFields
	k8sFieldNames[v+".volumeSource"] = volumeSourceFields
	k8sFieldNames[v+".volumeSource.secret"] = secretVolumeSourceFields
	k8sFieldNames[v+".volumeSource.secret.items"] = keyToPathFields
	k8sFieldNames[v+".volumeSource.configMap"] = configMapVolumeSourceFields
	k8sFieldNames[v+".volumeSource.configMap.items"] = keyToPathFields
	k8sFieldNames[v+".volumeSource.projected"] = projectedVolumeSourceFields
	k8sFieldNames[v+".volumeSource.projected.sources"] = projectionFields
	k8sFieldNames[v+".volumeSource.projected.sources.serviceAccountToken"] = serviceAccountTokenProjectionFields
	k8sFieldNames[v+".volumeSource.persistentVolumeClaim"] = persistentVolumeClaimVolumeSourceFields
	k8sFieldNames[v+".volumeSource.hostPath"] = hostPathVolumeSourceFields
	k8sFieldNames[v+".volumeSource.nfs"] = nfsVolumeSourceFields
	k8sFieldNames[v+".volumeSource.downwardAPI"] = downwardAPIVolumeSourceFields
	k8sFieldNames[v+".volumeSource.downwardAPI.items"] = downwardAPIVolumeFileFields
	k8sFieldNames[v+".volumeSource.downwardAPI.items.fieldRef"] = fieldRefFields
	k8sFieldNames[v+".volumeSource.projected.sources.configMap"] = configMapProjectionFields
	k8sFieldNames[v+".volumeSource.projected.sources.configMap.items"] = keyToPathFields
	k8sFieldNames[v+".volumeSource.projected.sources.secret"] = secretProjectionFields
	k8sFieldNames[v+".volumeSource.projected.sources.secret.items"] = keyToPathFields
	k8sFieldNames[v+".volumeSource.projected.sources.downwardAPI"] = downwardAPIVolumeSourceFields
	k8sFieldNames[v+".volumeSource.projected.sources.downwardAPI.items"] = downwardAPIVolumeFileFields
	k8sFieldNames[v+".volumeSource.projected.sources.downwardAPI.items.fieldRef"] = fieldRefFields

	// Pod affinity/anti-affinity
	for _, aField := range []string{"podAffinity", "podAntiAffinity"} {
		a := base + ".affinity." + aField
		k8sFieldNames[a] = podAffinityFields
		k8sFieldNames[a+".requiredDuringSchedulingIgnoredDuringExecution"] = podAffinityTermFields
		k8sFieldNames[a+".requiredDuringSchedulingIgnoredDuringExecution.labelSelector"] = labelSelectorFields
		k8sFieldNames[a+".requiredDuringSchedulingIgnoredDuringExecution.labelSelector.matchExpressions"] = matchExpressionsFields
		k8sFieldNames[a+".preferredDuringSchedulingIgnoredDuringExecution"] = weightedPodAffinityTermFields
		k8sFieldNames[a+".preferredDuringSchedulingIgnoredDuringExecution.podAffinityTerm"] = podAffinityTermFields
		k8sFieldNames[a+".preferredDuringSchedulingIgnoredDuringExecution.podAffinityTerm.labelSelector"] = labelSelectorFields
		k8sFieldNames[a+".preferredDuringSchedulingIgnoredDuringExecution.podAffinityTerm.labelSelector.matchExpressions"] = matchExpressionsFields
	}

	k8sFieldNames[base+".tolerations"] = tolerationFields
	k8sFieldNames[base+".imagePullSecrets"] = imagePullSecretFields
	k8sFieldNames[base+".securityContext"] = podSecurityContextFields
	k8sFieldNames[base+".securityContext.seLinuxOptions"] = seLinuxOptionsFields
	k8sFieldNames[base+".securityContext.seccompProfile"] = seccompProfileFields
	k8sFieldNames[base+".securityContext.appArmorProfile"] = appArmorProfileFields
	k8sFieldNames[base+".affinity"] = affinityFields
	k8sFieldNames[base+".affinity.nodeAffinity"] = nodeAffinityFields
	k8sFieldNames[base+".affinity.nodeAffinity.required"] = nodeSelectorFields
	k8sFieldNames[base+".affinity.nodeAffinity.required.nodeSelectorTerms"] = nodeSelectorTermFields
	k8sFieldNames[base+".affinity.nodeAffinity.required.nodeSelectorTerms.matchExpressions"] = nodeSelectorRequirementFields
	k8sFieldNames[base+".affinity.nodeAffinity.required.nodeSelectorTerms.matchFields"] = nodeSelectorRequirementFields
	k8sFieldNames[base+".affinity.nodeAffinity.preferred"] = preferredSchedulingTermFields
	k8sFieldNames[base+".affinity.nodeAffinity.preferred.preference"] = nodeSelectorTermFields
	k8sFieldNames[base+".affinity.nodeAffinity.preferred.preference.matchExpressions"] = nodeSelectorRequirementFields
	k8sFieldNames[base+".affinity.nodeAffinity.preferred.preference.matchFields"] = nodeSelectorRequirementFields
	k8sFieldNames[base+".topologySpreadConstraints"] = topologySpreadConstraintFields
	k8sFieldNames[base+".topologySpreadConstraints.labelSelector"] = labelSelectorFields
	k8sFieldNames[base+".topologySpreadConstraints.labelSelector.matchExpressions"] = matchExpressionsFields
}

func registerContainerStatusPaths(statusBase string) {
	for _, csField := range []string{"containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"} {
		cs := statusBase + "." + csField
		k8sFieldNames[cs] = containerStatusFields
		k8sFieldNames[cs+".resources"] = resourceRequirementsFields
		k8sFieldNames[cs+".volumeMounts"] = volumeMountStatusFields
		k8sFieldNames[cs+".user"] = containerUserFields
		k8sFieldNames[cs+".user.linux"] = linuxContainerUserFields
		for _, stateField := range []string{"state", "lastState"} {
			s := cs + "." + stateField
			k8sFieldNames[s] = containerStateFields
			k8sFieldNames[s+".waiting"] = containerStateWaitingFields
			k8sFieldNames[s+".running"] = containerStateRunningFields
			k8sFieldNames[s+".terminated"] = containerStateTerminatedFields
		}
	}
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

func decodeGenericField(f ProtoField, pathPrefix string, depth int) interface{} {
	switch f.WireType {
	case 0:
		return f.Varint
	case 1:
		return f.Fixed64
	case 2:
		sub, err := parseProtoMessage(f.Bytes)
		if err == nil && len(sub) > 0 {
			return decodeProtoFields(f.Bytes, nil, pathPrefix, depth+1)
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

func decodeProtoFields(data []byte, names map[int]string, pathPrefix string, depth int) interface{} {
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

	// Detect Quantity-like wrapper: single field 1 that is a string (e.g. "20Gi", "100m")
	if len(fields) == 1 {
		if f1, ok := fields[1]; ok && len(f1) == 1 && f1[0].WireType == 2 && isLikelyString(f1[0].Bytes) {
			return string(f1[0].Bytes)
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

		childPath := pathPrefix
		if key != fmt.Sprintf("field_%d", num) && pathPrefix != "" {
			childPath = pathPrefix + "." + key
		} else if key != fmt.Sprintf("field_%d", num) {
			childPath = key
		}

		var childNames map[int]string
		if childPath != "" {
			childNames = k8sFieldNames[childPath]
		}

		if childNames == nil && isMapEntries(entries) {
			m := make(map[string]interface{})
			for _, e := range entries {
				ef, _ := parseProtoMessage(e.Bytes)
				k := protoString(ef, 1)
				if k != "" && len(ef[2]) > 0 {
					m[k] = decodeGenericField(ef[2][0], childPath, depth+1)
				}
			}
			result[key] = m
		} else if len(entries) == 1 {
			if entries[0].WireType == 2 && childNames != nil {
				result[key] = decodeProtoFields(entries[0].Bytes, childNames, childPath, depth+1)
			} else {
				result[key] = decodeGenericField(entries[0], childPath, depth)
			}
		} else {
			var vals []interface{}
			for _, e := range entries {
				if e.WireType == 2 && childNames != nil {
					vals = append(vals, decodeProtoFields(e.Bytes, childNames, childPath, depth+1))
				} else {
					vals = append(vals, decodeGenericField(e, childPath, depth))
				}
			}
			result[key] = vals
		}
	}
	return result
}

func decodeGenericProto(data []byte, depth int) interface{} {
	return decodeProtoFields(data, nil, "", depth)
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

			sectionPath := kind + "." + key
			sectionNames := k8sFieldNames[sectionPath]

			if sectionNames == nil && isMapEntries(entries) {
				m := make(map[string]interface{})
				for _, e := range entries {
					ef, _ := parseProtoMessage(e.Bytes)
					k := protoString(ef, 1)
					if k != "" && len(ef[2]) > 0 {
						m[k] = decodeGenericField(ef[2][0], sectionPath, 1)
					}
				}
				result[key] = m
			} else if len(entries) == 1 {
				if entries[0].WireType == 2 && sectionNames != nil {
					result[key] = decodeProtoFields(entries[0].Bytes, sectionNames, sectionPath, 0)
				} else {
					result[key] = decodeGenericField(entries[0], sectionPath, 0)
				}
			} else {
				var vals []interface{}
				for _, e := range entries {
					if e.WireType == 2 && sectionNames != nil {
						vals = append(vals, decodeProtoFields(e.Bytes, sectionNames, sectionPath, 0))
					} else {
						vals = append(vals, decodeGenericField(e, sectionPath, 0))
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
		if len(remaining) >= 2 {
			result.Resource = remaining[0] + "/" + remaining[1]
		}
		// Use segment count: 3 = cluster-scoped, 4 = namespaced
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
