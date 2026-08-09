package main

import (
	"fmt"
	"os"

	bolt "go.etcd.io/bbolt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run debug-db.go <db_file>")
		os.Exit(1)
	}

	dbPath := os.Args[1]

	db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true})
	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	fmt.Println("=== BoltDB Structure ===\n")

	keyCount := 0
	err = db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(bucketName []byte, bucket *bolt.Bucket) error {
			fmt.Printf("Bucket: %s\n", string(bucketName))

			// Show first 10 keys from this bucket
			count := 0
			bucket.ForEach(func(key, value []byte) error {
				if count < 10 {
					keyStr := string(key)
					fmt.Printf("  Key: %s (value size: %d bytes)\n", keyStr, len(value))

					// Show first 200 chars of value
					if len(value) > 0 {
						preview := value
						if len(preview) > 200 {
							preview = preview[:200]
						}
						fmt.Printf("  Value preview: %s...\n", string(preview))
					}
					fmt.Println()
				}
				count++
				keyCount++
				return nil
			})

			fmt.Printf("  Total keys in bucket: %d\n", count)
			fmt.Println("---")
			return nil
		})
	})

	if err != nil {
		fmt.Printf("Error reading database: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nTotal keys across all buckets: %d\n", keyCount)
}
