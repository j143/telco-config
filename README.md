# Ironclad-DB

Ultra-reliable key-value store for 5G Network Function (NF) state and subscriber cache, using Azure page blobs with Write-Ahead Logging (WAL).

## Overview

Ironclad-DB is designed for telecommunications workloads requiring:
- **Ultra-reliability**: Crash-safe with WAL-based durability
- **Strong consistency**: Optimistic concurrency control with versioning
- **AZ-level resilience**: Built on Azure Premium ZRS storage
- **5G NF workloads**: Optimized for 10-100k UE/session records per shard

## Architecture

### Components

- **Page Blob Storage**: Fixed-size slots (8KB) for key-value data, aligned to 512-byte boundaries
- **WAL (Write-Ahead Log)**: Append blob ensuring durability before applying changes
- **In-Memory Index**: Fast lookup mapping key hashes to slot offsets
- **Sharding**: Horizontal scaling across multiple pods/instances

### Design Principles

1. **WAL-first writes**: All mutations logged before applying to page blob
2. **Crash recovery**: Replay WAL from last checkpoint on startup
3. **Optimistic concurrency**: Version-based conditional updates
4. **Fixed slot layout**: Predictable performance, no compaction during operation

## Quick Start

### Prerequisites

- Go 1.21 or later
- Azure Storage Account (Premium with ZRS recommended)
- Azure credentials configured (Azure CLI, Managed Identity, or Service Principal)

### Installation

```bash
# Clone the repository
git clone https://github.com/j143/telco-config.git
cd telco-config

# Install dependencies
make deps

# Build the application
make build
```

### Configuration

Set environment variables:

```bash
export STORAGE_ACCOUNT_URL="https://your-account.blob.core.windows.net"
export CONTAINER_NAME="ironclad-db"
export PAGE_BLOB_NAME="shard-0.blob"
export WAL_BLOB_NAME="shard-0.wal"
export INIT_MODE="true"  # Set to true for first-time initialization
```

### Running

```bash
# Initialize and run
make run

# Or run directly
./bin/ironclad-db
```

## API

### gRPC Service

The service exposes the following operations:

- **Put(key, value, expected_version)**: Store or update a key-value pair
  - Returns new version number
  - Optional optimistic concurrency control via expected_version
  
- **Get(key)**: Retrieve a value and its version
  
- **Delete(key, expected_version)**: Remove a key
  - Optional version check for concurrency control
  
- **Scan(prefix, limit)**: List keys with a given prefix (management operation)

- **Health()**: Health check endpoint

### HTTP Endpoints

- `GET /health` - Health check
- `GET /ready` - Readiness check

## Storage Layout

### Page Blob Structure

```
[Superblock: 512 bytes]
  - Format version
  - Shard ID
  - Last applied LSN
  
[Slot 0: 8KB]
[Slot 1: 8KB]
[Slot 2: 8KB]
...
```

### Slot Format (8KB)

```
[Header: 40 bytes]
  - Key hash (8 bytes)
  - Key length (2 bytes)
  - Value length (2 bytes)
  - Version (8 bytes)
  - CRC32 (4 bytes)
  - Flags (4 bytes)
  - Reserved (12 bytes)
  
[Key: variable]
[Value: variable]
[Padding: to 8KB]
```

### WAL Record Format

```
[OpType: 1 byte]      # PUT or DELETE
[Key hash: 8 bytes]
[Slot offset: 8 bytes]
[Version: 8 bytes]
[Key length: 2 bytes]
[Value length: 2 bytes]
[Reserved: 3 bytes]
[CRC32: 4 bytes]
[Key: variable]
[Value: variable]
```

## Development

### Building

```bash
# Build binary
make build

# Run tests
make test

# Generate protobuf code
make proto

# Format code
make fmt

# Clean build artifacts
make clean
```

### Testing

```bash
# Run all tests
make test

# Run with coverage
make test-coverage
```

## Deployment

### Kubernetes

Example deployment manifests are provided in the `k8s/` directory.

```bash
# Deploy to Kubernetes
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
```

### Configuration

Key configuration options:

- **STORAGE_ACCOUNT_URL**: Azure Storage account endpoint
- **CONTAINER_NAME**: Blob container name
- **PAGE_BLOB_NAME**: Name for the page blob (data storage)
- **WAL_BLOB_NAME**: Name for the WAL append blob
- **INIT_MODE**: Set to "true" for initial setup, "false" for recovery mode
- **GRPC_PORT**: gRPC service port (default: 9090)
- **HTTP_PORT**: HTTP health check port (default: 8080)

## Architecture Details

### Write Path

1. Hash the key using FNV-1a
2. Lock entry in index (fine-grained)
3. Check version for optimistic concurrency
4. Allocate or reuse slot
5. Create WAL record
6. **Append to WAL blob** (durability point)
7. Write to page blob slot
8. Update in-memory index
9. Update last applied LSN

### Read Path

1. Hash the key
2. Lookup in index (read lock)
3. Read slot from page blob (or cache)
4. Verify CRC and key
5. Return value and version

### Recovery

1. Read superblock for last applied LSN
2. Stream WAL from that offset
3. Replay each record:
   - Apply to page blob
   - Update index
4. Write updated superblock
5. Ready to serve requests

### Compaction (Future)

Periodic checkpoint and WAL rotation:
1. Scan all slots, rebuild index
2. Write new compacted page blob
3. Create new WAL with base LSN
4. Delete old WAL

## Performance Considerations

- **Slot size**: 8KB chosen to balance space efficiency and Azure page blob alignment
- **Batching**: WAL records can be batched to reduce API calls
- **Caching**: In-memory index eliminates reads for lookups
- **Sharding**: Horizontal scaling via consistent hashing

## Future Enhancements

- [ ] Full Scan implementation with streaming
- [ ] Checkpoint and WAL rotation
- [ ] Background compaction/GC
- [ ] Multi-shard coordinator
- [ ] Prometheus metrics
- [ ] Distributed tracing
- [ ] Read replicas

## References

- [Azure Page Blobs](https://learn.microsoft.com/en-us/azure/storage/blobs/storage-blob-pageblob-overview)
- [Azure Blob Storage Go SDK](https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/storage/azblob)
- [5G NF Stateful Workloads on AKS](https://learn.microsoft.com/en-us/azure/aks/stateful-workloads-overview)

## License

See LICENSE file for details.

## Contributing

Contributions welcome! Please open an issue or PR.
