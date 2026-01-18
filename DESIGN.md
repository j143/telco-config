# Ironclad-DB Design Document

## Executive Summary

Ironclad-DB is an ultra-reliable key-value store designed specifically for 5G Network Function (NF) state management and subscriber cache. It leverages Azure Page Blobs for durable storage with Write-Ahead Logging (WAL) to ensure crash safety and strong consistency.

## System Architecture

### Core Components

```
┌─────────────────────────────────────────────────────────┐
│                    gRPC/HTTP API                        │
│           (Put, Get, Delete, Scan, Health)             │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                   Shard Engine                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │  In-Memory   │  │     WAL      │  │   Page Blob  │ │
│  │    Index     │  │   Manager    │  │   Manager    │ │
│  └──────────────┘  └──────────────┘  └──────────────┘ │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│              Azure Blob Storage (Premium ZRS)           │
│  ┌────────────────────┐     ┌──────────────────────┐  │
│  │   Page Blob        │     │   Append Blob (WAL)  │  │
│  │   (Data Storage)   │     │   (Write-Ahead Log)  │  │
│  └────────────────────┘     └──────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### Design Principles

1. **WAL-First Writes**: All mutations are logged to the WAL before being applied to the page blob
2. **Crash Safety**: WAL replay ensures no data loss even on catastrophic failures
3. **Optimistic Concurrency**: Version-based conditional updates prevent lost updates
4. **Fixed Layout**: 8KB slots provide predictable performance without compaction

## Data Model

### Storage Layout

#### Page Blob Structure

```
Offset    Size     Description
------    ----     -----------
0         512B     Superblock (metadata)
512       8KB      Slot 0
8704      8KB      Slot 1
16896     8KB      Slot 2
...
```

#### Superblock Format (512 bytes)

```
Offset  Size  Field            Description
------  ----  -----            -----------
0       4     Version          Format version (currently 1)
4       4     ShardID          Shard identifier
8       8     LastAppliedLSN   Last applied log sequence number
16      496   Reserved         Reserved for future use
```

#### Slot Format (8KB = 8192 bytes)

```
Offset  Size  Field       Description
------  ----  -----       -----------
0       8     KeyHash     FNV-1a hash of key
8       2     KeyLen      Length of key in bytes
10      2     ValueLen    Length of value in bytes
12      8     Version     Version number for OCC
20      4     CRC32       CRC32 checksum (IEEE)
24      4     Flags       Flags (bit 0: tombstone)
28      12    Reserved    Reserved for future use
40      var   Key         Key bytes (max 256)
var     var   Value       Value bytes (max 7680)
var     var   Padding     Zero-padding to 8KB
```

**Key Constraints:**
- Maximum key size: 256 bytes
- Maximum value size: 7,680 bytes (8192 - 512 overhead)
- Slot size: 8,192 bytes (aligned to 512-byte pages)

#### WAL Record Format

```
Offset  Size  Field         Description
------  ----  -----         -----------
0       1     OpType        Operation (1=Put, 2=Delete)
1       8     KeyHash       FNV-1a hash of key
9       8     SlotOffset    Page blob slot offset
17      8     Version       Version number
25      2     KeyLen        Length of key
27      2     ValueLen      Length of value
29      4     CRC32         CRC32 checksum (IEEE)
33      var   Key           Key bytes
var     var   Value         Value bytes
```

### In-Memory Index

```go
type IndexEntry struct {
    SlotOffset int64   // Location in page blob
    Version    uint64  // Current version
    KeyHash    uint64  // Key hash for validation
}

index: map[uint64]*IndexEntry  // KeyHash -> IndexEntry
```

## Operations

### Write Path (Put)

1. **Hash Key**: Compute FNV-1a hash of the key
2. **Lock**: Acquire fine-grained lock for this key hash
3. **Version Check**: If expectedVersion != 0, verify current version matches
4. **Allocate Slot**: Allocate new slot or reuse existing slot offset
5. **Create WAL Record**: Build WAL record with operation metadata
6. **Append to WAL**: Write WAL record to append blob (durability point)
7. **Apply to Page Blob**: Write slot data to page blob at slot offset
8. **Update Index**: Update in-memory index with new version
9. **Update LSN**: Update last applied LSN in superblock (in memory)
10. **Unlock**: Release lock

**Durability Guarantee**: After step 6 completes, the operation is durable and will be replayed on recovery.

### Read Path (Get)

1. **Hash Key**: Compute FNV-1a hash of the key
2. **Lookup Index**: Check in-memory index for slot offset
3. **Read Slot**: Download slot data from page blob
4. **Verify CRC**: Validate CRC32 checksum
5. **Verify Key**: Ensure key matches (detect hash collisions)
6. **Check Tombstone**: Return error if slot is tombstoned
7. **Return Value**: Return value and version

**Performance**: Hot keys can be cached in memory to avoid blob reads.

### Delete Path

1. **Hash Key**: Compute FNV-1a hash of the key
2. **Lock**: Acquire fine-grained lock for this key hash
3. **Version Check**: If expectedVersion != 0, verify current version matches
4. **Create WAL Record**: Build delete WAL record
5. **Append to WAL**: Write WAL record to append blob
6. **Mark Tombstone**: Write slot with tombstone flag set
7. **Remove from Index**: Remove entry from in-memory index
8. **Unlock**: Release lock

### Recovery Path

1. **Read Superblock**: Load superblock from page blob offset 0
2. **Get Last LSN**: Extract LastAppliedLSN from superblock
3. **Stream WAL**: Read WAL from LastAppliedLSN to end
4. **Replay Records**: For each WAL record:
   - Apply to page blob
   - Update in-memory index
5. **Update Superblock**: Write new LastAppliedLSN to page blob
6. **Ready**: Service is ready to accept requests

## Consistency and Concurrency

### Optimistic Concurrency Control (OCC)

Every key-value pair has a version number that increments on each update:

```go
// Client ensures they're updating the expected version
err := shard.Put(ctx, key, newValue, expectedVersion)
if err != nil {
    // Handle version mismatch - retry or abort
}
```

**Use Cases:**
- Session state updates: Ensure no intermediate updates were lost
- Subscriber data: Prevent conflicting updates from different NFs
- Configuration: Atomic compare-and-swap operations

### Locking Strategy

- **Fine-grained locks**: Per key-hash mutex
- **Read-write locks**: Allow concurrent reads on index
- **Lock ordering**: Always acquire locks in hash order to prevent deadlocks

## Failure Scenarios

### Crash During Write

**Scenario**: Process crashes after WAL append but before page blob update

**Recovery**: 
1. WAL record exists and is durable
2. Replay applies the record to page blob
3. Index is rebuilt
4. Consistency restored

**Result**: No data loss ✓

### Partial WAL Write

**Scenario**: Network failure during WAL append

**Recovery**:
1. WAL record is incomplete or missing CRC
2. Replay detects CRC mismatch
3. Record is skipped
4. Client receives error on original request
5. Client retries with new version

**Result**: At-most-once semantics ✓

### Page Blob Write Failure

**Scenario**: Azure API failure during page blob update

**Recovery**:
1. WAL record exists
2. Replay attempts page blob write again
3. Operation completes successfully

**Result**: WAL provides retry capability ✓

## Performance Characteristics

### Latency (Estimated)

Operation      | Latency (P50) | Latency (P99)
---------------|---------------|---------------
Put            | 20-30ms       | 50-80ms
Get (cached)   | <1ms          | 2-5ms
Get (uncached) | 10-20ms       | 30-50ms
Delete         | 20-30ms       | 50-80ms

**Factors:**
- Azure Premium Storage provides <10ms latency
- Network RTT to Azure region: ~5-15ms
- WAL append + page blob write: ~2 round trips

### Throughput (Estimated)

- **Single shard**: 1,000-5,000 ops/sec
- **With sharding**: Linear scaling (10 shards = 10,000-50,000 ops/sec)
- **Read-heavy workload**: 10,000+ ops/sec (with caching)

### Capacity

Per shard:
- Page blob size: 8-64 GB (configurable)
- Slots per shard: ~1M - 8M slots (8GB - 64GB)
- Records per shard: 10K - 100K UE/session records (typical)

## Deployment

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ironclad-db
spec:
  replicas: 3  # Multiple shards
  template:
    spec:
      containers:
      - name: ironclad-db
        image: ironclad-db:latest
        env:
        - name: STORAGE_ACCOUNT_URL
          value: "https://account.blob.core.windows.net"
        - name: SHARD_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name  # Pod name determines shard
```

### Azure Configuration

**Recommended:**
- Storage Account: Premium tier
- Replication: Zone-Redundant Storage (ZRS)
- Authentication: Managed Identity (Workload Identity)
- Network: Private endpoint for AKS VNet

## Future Enhancements

### Phase 2: Optimization
- [ ] Slot caching layer (in-memory LRU)
- [ ] Batch WAL writes (reduce API calls)
- [ ] Background compaction (reclaim deleted slots)
- [ ] Read replicas (scale reads independently)

### Phase 3: Advanced Features
- [ ] Multi-shard coordinator (consistent hashing)
- [ ] Cross-shard transactions
- [ ] Snapshot isolation
- [ ] Point-in-time recovery

### Phase 4: Observability
- [ ] Prometheus metrics (latency, throughput, errors)
- [ ] Distributed tracing (OpenTelemetry)
- [ ] Structured logging (JSON)
- [ ] Health dashboard

## References

- [Azure Page Blobs Overview](https://learn.microsoft.com/en-us/azure/storage/blobs/storage-blob-pageblob-overview)
- [Azure Blob Storage Go SDK](https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/storage/azblob)
- [5G NF Stateful Workloads](https://learn.microsoft.com/en-us/azure/aks/stateful-workloads-overview)
- [Write-Ahead Logging](https://en.wikipedia.org/wiki/Write-ahead_logging)
- [Optimistic Concurrency Control](https://en.wikipedia.org/wiki/Optimistic_concurrency_control)

## Authors

- Initial Design: @j143
- Implementation: Copilot

## License

See LICENSE file.
