package main

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
	"gopkg.in/yaml.v3"
)

const (
	maxUploadSize = 500 * 1024 * 1024 // 500MB
	uploadDir     = "./uploads"
	sessionTTL    = 1 * time.Hour
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

type Session struct {
	ID        string
	DBPath    string
	CreatedAt time.Time
}

type WebServer struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

type ListResourcesResponse struct {
	Resources []ResourceInfo `json:"resources"`
}

type ResourceInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Count      int    `json:"count"`
	Namespaced bool   `json:"namespaced"`
}

type ExtractRequest struct {
	SessionID     string `json:"sessionId"`
	Resource      string `json:"resource"`
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	AllNamespaces bool   `json:"allNamespaces"`
	Format        string `json:"format"` // "yaml" or "json"
}

type ExtractResponse struct {
	Objects []ExtractedObject `json:"objects"`
	Count   int               `json:"count"`
}

// parseEtcdV3Value extracts the path and object from etcd v3 value format
func parseEtcdV3Value(value []byte) (string, map[string]interface{}, error) {
	vStr := string(value)

	// Find the Kubernetes path (starts with /)
	pathStart := strings.Index(vStr, "/")
	if pathStart < 0 {
		return "", nil, fmt.Errorf("no path found")
	}

	// Find the end of the path
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
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) < 2 {
		return ParsedKey{FullPath: path}
	}

	// Skip "kubernetes.io" prefix if present
	startIdx := 0
	if parts[0] == "kubernetes.io" {
		startIdx = 1
	}

	if startIdx >= len(parts) {
		return ParsedKey{FullPath: path}
	}

	resource := ""
	namespaceIdx := -1
	nameIdx := -1

	if len(parts) > startIdx {
		resource = parts[startIdx]
	}

	// Check if next part looks like a group (contains dots)
	if startIdx+1 < len(parts) && strings.Contains(parts[startIdx+1], ".") {
		if startIdx+2 < len(parts) {
			resource = parts[startIdx+2]
		}
		namespaceIdx = startIdx + 3
		nameIdx = startIdx + 4
	} else {
		namespaceIdx = startIdx + 1
		nameIdx = startIdx + 2
	}

	result := ParsedKey{
		Resource: resource,
		FullPath: path,
	}

	// Determine if cluster-scoped
	if clusterScopedResources[resource] {
		if namespaceIdx < len(parts) {
			result.Name = parts[namespaceIdx]
		}
	} else {
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
		bucket := tx.Bucket([]byte("key"))
		if bucket == nil {
			return fmt.Errorf("no 'key' bucket found")
		}

		return bucket.ForEach(func(key, value []byte) error {
			path, obj, err := parseEtcdV3Value(value)
			if err != nil {
				return nil
			}

			parsed := parseEtcdPath(path)

			if resourceFilter != "" && parsed.Resource != resourceFilter {
				return nil
			}

			if parsed.Namespace != "" {
				if namespaceFilter != "" && parsed.Namespace != namespaceFilter {
					return nil
				}
				if !allNs && namespaceFilter == "" {
					return nil
				}
			} else if namespaceFilter != "" {
				return nil
			}

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

func NewWebServer() *WebServer {
	ws := &WebServer{
		sessions: make(map[string]*Session),
	}

	go ws.cleanupSessions()

	return ws
}

func (ws *WebServer) cleanupSessions() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		ws.mu.Lock()
		now := time.Now()
		for id, session := range ws.sessions {
			if now.Sub(session.CreatedAt) > sessionTTL {
				os.Remove(session.DBPath)
				delete(ws.sessions, id)
				log.Printf("Cleaned up expired session: %s", id)
			}
		}
		ws.mu.Unlock()
	}
}

func generateSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (ws *WebServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("dbfile")
	if err != nil {
		http.Error(w, "Error retrieving file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}

	sessionID := generateSessionID()
	dbPath := filepath.Join(uploadDir, sessionID+"-"+header.Filename)

	dst, err := os.Create(dbPath)
	if err != nil {
		http.Error(w, "Error saving file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(dbPath)
		http.Error(w, "Error saving file", http.StatusInternalServerError)
		return
	}

	db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true, Timeout: 5 * time.Second})
	if err != nil {
		os.Remove(dbPath)
		http.Error(w, "Invalid etcd database file", http.StatusBadRequest)
		return
	}
	db.Close()

	session := &Session{
		ID:        sessionID,
		DBPath:    dbPath,
		CreatedAt: time.Now(),
	}

	ws.mu.Lock()
	ws.sessions[sessionID] = session
	ws.mu.Unlock()

	log.Printf("New session created: %s for file: %s", sessionID, header.Filename)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"sessionId": sessionID,
		"filename":  header.Filename,
	})
}

func (ws *WebServer) handleListResources(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	ws.mu.RLock()
	session, exists := ws.sessions[sessionID]
	ws.mu.RUnlock()

	if !exists {
		http.Error(w, "Session not found or expired", http.StatusNotFound)
		return
	}

	resources, err := ws.listResources(session.DBPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ListResourcesResponse{Resources: resources})
}

func (ws *WebServer) listResources(dbPath string) ([]ResourceInfo, error) {
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	resourceMap := make(map[string]*ResourceSummary)

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

			if _, exists := resourceMap[parsed.Resource]; !exists {
				resourceMap[parsed.Resource] = &ResourceSummary{}
			}

			resourceMap[parsed.Resource].Total++
			if parsed.Namespace != "" {
				resourceMap[parsed.Resource].Namespaced = true
			}

			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	var resources []ResourceInfo
	for name, summary := range resourceMap {
		resourceType := "cluster-scoped"
		if summary.Namespaced {
			resourceType = "namespaced"
		}
		resources = append(resources, ResourceInfo{
			Name:       name,
			Type:       resourceType,
			Count:      summary.Total,
			Namespaced: summary.Namespaced,
		})
	}

	return resources, nil
}

func (ws *WebServer) handleExtract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ExtractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ws.mu.RLock()
	session, exists := ws.sessions[req.SessionID]
	ws.mu.RUnlock()

	if !exists {
		http.Error(w, "Session not found or expired", http.StatusNotFound)
		return
	}

	results, err := extractObjects(session.DBPath, req.Resource, req.Namespace, req.Name, req.AllNamespaces)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ExtractResponse{
		Objects: results,
		Count:   len(results),
	})
}

func (ws *WebServer) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ExtractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ws.mu.RLock()
	session, exists := ws.sessions[req.SessionID]
	ws.mu.RUnlock()

	if !exists {
		http.Error(w, "Session not found or expired", http.StatusNotFound)
		return
	}

	results, err := extractObjects(session.DBPath, req.Resource, req.Namespace, req.Name, req.AllNamespaces)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(results) == 0 {
		http.Error(w, "No objects found", http.StatusNotFound)
		return
	}

	format := req.Format
	if format == "" {
		format = "yaml"
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=etcd-extract-%s.zip", time.Now().Format("20060102-150405")))

	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	for i, result := range results {
		filename := fmt.Sprintf("%s-%s-%d.%s", result.Resource, result.Name, i, format)
		if result.Namespace != "" {
			filename = fmt.Sprintf("%s-%s-%s-%d.%s", result.Resource, result.Namespace, result.Name, i, format)
		}

		fileWriter, err := zipWriter.Create(filename)
		if err != nil {
			log.Printf("Error creating zip entry: %v", err)
			continue
		}

		var content []byte
		if format == "yaml" {
			metadata := fmt.Sprintf("# Key: %s\n# Resource: %s\n", result.Key, result.Resource)
			if result.Namespace != "" {
				metadata += fmt.Sprintf("# Namespace: %s\n", result.Namespace)
			}
			metadata += fmt.Sprintf("# Name: %s\n---\n", result.Name)

			yamlData, err := yaml.Marshal(result.Object)
			if err != nil {
				log.Printf("Error marshaling YAML: %v", err)
				continue
			}
			content = append([]byte(metadata), yamlData...)
		} else {
			jsonData, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				log.Printf("Error marshaling JSON: %v", err)
				continue
			}
			content = jsonData
		}

		fileWriter.Write(content)
	}
}

func (ws *WebServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "web/index.html")
}

func main() {
	os.MkdirAll(uploadDir, 0755)

	ws := NewWebServer()

	http.HandleFunc("/api/upload", ws.handleUpload)
	http.HandleFunc("/api/resources", ws.handleListResources)
	http.HandleFunc("/api/extract", ws.handleExtract)
	http.HandleFunc("/api/download", ws.handleDownload)

	http.HandleFunc("/", ws.handleIndex)
	http.Handle("/web/", http.StripPrefix("/web/", http.FileServer(http.Dir("web"))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting etcd-extract web server on http://localhost:%s", port)
	log.Printf("Upload directory: %s", uploadDir)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
