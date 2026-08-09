package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	bolt "go.etcd.io/bbolt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run analyze-format.go <db_file>")
		os.Exit(1)
	}

	dbPath := os.Args[1]

	db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true})
	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	fmt.Println("=== Analyzing etcd v3 format ===\n")

	err = db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("key"))
		if bucket == nil {
			fmt.Println("No 'key' bucket found")
			return nil
		}

		count := 0
		return bucket.ForEach(func(k, v []byte) error {
			if count < 5 {
				fmt.Printf("\n--- Entry %d ---\n", count+1)
				fmt.Printf("Raw key (hex): %x\n", k)
				fmt.Printf("Raw key (string): %q\n", string(k))
				fmt.Printf("Value length: %d bytes\n", len(v))

				// The value in etcd v3 contains the path
				// Try to find "/kubernetes.io/" or similar patterns
				vStr := string(v)

				// Look for the path pattern
				if idx := strings.Index(vStr, "/"); idx >= 0 {
					// Extract potential path
					pathStart := idx
					pathEnd := strings.Index(vStr[pathStart+1:], "\x00")
					if pathEnd < 0 {
						pathEnd = strings.Index(vStr[pathStart+1:], " ")
					}
					if pathEnd > 0 {
						path := vStr[pathStart : pathStart+1+pathEnd]
						fmt.Printf("Potential path: %s\n", path)
					}
				}

				// Try to find JSON
				if idx := strings.Index(vStr, "{\"kind\""); idx >= 0 {
					jsonStart := idx

					// Find the end of JSON
					for i := jsonStart; i < len(vStr); i++ {
						if vStr[i] == '}' {
							// Try to parse
							candidate := vStr[jsonStart : i+1]
							var obj map[string]interface{}
							if json.Unmarshal([]byte(candidate), &obj) == nil {
								fmt.Printf("Found valid JSON at offset %d\n", jsonStart)
								if kind, ok := obj["kind"].(string); ok {
									fmt.Printf("  Kind: %s\n", kind)
								}
								if md, ok := obj["metadata"].(map[string]interface{}); ok {
									if name, ok := md["name"].(string); ok {
										fmt.Printf("  Name: %s\n", name)
									}
									if ns, ok := md["namespace"].(string); ok {
										fmt.Printf("  Namespace: %s\n", ns)
									}
								}
								break
							}
						}
					}
				}

				// Show first 500 chars
				preview := string(v)
				if len(preview) > 500 {
					preview = preview[:500]
				}
				fmt.Printf("\nValue preview:\n%s\n", preview)
			}
			count++
			return nil
		})
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
