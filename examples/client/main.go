package main

import (
	"context"
	"log"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/j143/telco-config/pkg/blob"
	"github.com/j143/telco-config/pkg/engine"
)

// Example demonstrates basic usage of Ironclad-DB components
func main() {
	log.Println("Ironclad-DB Example Client")

	// Configuration
	storageAccountURL := "https://your-account.blob.core.windows.net"
	containerName := "ironclad-db-test"
	pageBlobName := "example.blob"
	walBlobName := "example.wal"
	pageBlobSize := int64(64 * 1024 * 1024) // 64 MB for example

	// Create Azure credential
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("Failed to create credential: %v", err)
	}

	// Create blob stores
	pageBlob, err := blob.NewPageBlobStore(
		credential,
		storageAccountURL,
		containerName,
		pageBlobName,
		pageBlobSize,
	)
	if err != nil {
		log.Fatalf("Failed to create page blob store: %v", err)
	}

	walBlob, err := blob.NewWALBlob(
		credential,
		storageAccountURL,
		containerName,
		walBlobName,
	)
	if err != nil {
		log.Fatalf("Failed to create WAL blob: %v", err)
	}

	// Create shard
	shard := engine.NewShard(0, pageBlob, walBlob)

	ctx := context.Background()

	// Initialize the shard
	log.Println("Initializing shard...")
	if err := shard.Initialize(ctx); err != nil {
		log.Fatalf("Failed to initialize shard: %v", err)
	}

	// Example 1: Put a key-value pair
	log.Println("\n=== Example 1: Put ===")
	key1 := []byte("user:12345")
	value1 := []byte(`{"name":"Alice","status":"active","cell_id":"TAC-001"}`)

	if err := shard.Put(ctx, key1, value1, 0); err != nil {
		log.Fatalf("Failed to put: %v", err)
	}
	log.Printf("PUT: %s -> %s", key1, value1)

	// Example 2: Get the value
	log.Println("\n=== Example 2: Get ===")
	value, version, err := shard.Get(ctx, key1)
	if err != nil {
		log.Fatalf("Failed to get: %v", err)
	}
	log.Printf("GET: %s -> %s (version: %d)", key1, value, version)

	// Example 3: Update with version check (optimistic concurrency)
	log.Println("\n=== Example 3: Update with version check ===")
	value2 := []byte(`{"name":"Alice","status":"idle","cell_id":"TAC-002"}`)

	if err := shard.Put(ctx, key1, value2, version); err != nil {
		log.Fatalf("Failed to update: %v", err)
	}
	log.Printf("UPDATE: %s -> %s (expected version: %d)", key1, value2, version)

	// Verify update
	value, newVersion, err := shard.Get(ctx, key1)
	if err != nil {
		log.Fatalf("Failed to get after update: %v", err)
	}
	log.Printf("GET after update: %s -> %s (version: %d)", key1, value, newVersion)

	// Example 4: Try to update with wrong version (should fail)
	log.Println("\n=== Example 4: Update with wrong version (should fail) ===")
	value3 := []byte(`{"name":"Alice","status":"invalid"}`)

	if err := shard.Put(ctx, key1, value3, version); err != nil {
		log.Printf("Expected error: %v", err)
	} else {
		log.Println("ERROR: Should have failed with version mismatch!")
	}

	// Example 5: Put another key
	log.Println("\n=== Example 5: Put another key ===")
	key2 := []byte("session:abc123")
	value4 := []byte(`{"ue_id":"12345","session_type":"voice","duration":300}`)

	if err := shard.Put(ctx, key2, value4, 0); err != nil {
		log.Fatalf("Failed to put: %v", err)
	}
	log.Printf("PUT: %s -> %s", key2, value4)

	// Example 6: Delete a key
	log.Println("\n=== Example 6: Delete ===")
	value, version, err = shard.Get(ctx, key2)
	if err != nil {
		log.Fatalf("Failed to get before delete: %v", err)
	}

	if err := shard.Delete(ctx, key2, version); err != nil {
		log.Fatalf("Failed to delete: %v", err)
	}
	log.Printf("DELETED: %s (version: %d)", key2, version)

	// Verify deletion
	_, _, err = shard.Get(ctx, key2)
	if err != nil {
		log.Printf("Expected error after delete: %v", err)
	} else {
		log.Println("ERROR: Should have failed - key should be deleted!")
	}

	// Example 7: Simulate crash recovery
	log.Println("\n=== Example 7: Crash recovery simulation ===")
	log.Println("Creating new shard instance (simulating restart)...")

	// Create new instances
	pageBlob2, _ := blob.NewPageBlobStore(
		credential,
		storageAccountURL,
		containerName,
		pageBlobName,
		pageBlobSize,
	)
	walBlob2, _ := blob.NewWALBlob(
		credential,
		storageAccountURL,
		containerName,
		walBlobName,
	)
	shard2 := engine.NewShard(0, pageBlob2, walBlob2)

	// Recover
	if err := shard2.Recover(ctx); err != nil {
		log.Fatalf("Failed to recover: %v", err)
	}
	log.Println("Recovery complete!")

	// Verify data after recovery
	value, version, err = shard2.Get(ctx, key1)
	if err != nil {
		log.Fatalf("Failed to get after recovery: %v", err)
	}
	log.Printf("GET after recovery: %s -> %s (version: %d)", key1, value, version)

	log.Println("\n=== All examples completed successfully! ===")

	// Cleanup (optional)
	// pageBlob.Delete(ctx)
	// walBlob.Delete(ctx)
}
