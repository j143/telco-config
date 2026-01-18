package engine

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"

	"github.com/j143/telco-config/pkg/blob"
)

// IndexEntry represents an entry in the in-memory index
type IndexEntry struct {
	SlotOffset int64
	Version    uint64
	KeyHash    uint64
}

// Shard represents a single shard with its own page blob and WAL
type Shard struct {
	shardID    uint32
	pageBlob   *blob.PageBlobStore
	walBlob    *blob.WALBlob
	index      map[uint64]*IndexEntry // key_hash -> IndexEntry
	indexMu    sync.RWMutex           // Protects the index
	nextSlot   int64                  // Next available slot offset
	superblock *Superblock
	sbMu       sync.Mutex // Protects superblock updates
}

// NewShard creates a new shard instance
func NewShard(shardID uint32, pageBlob *blob.PageBlobStore, walBlob *blob.WALBlob) *Shard {
	return &Shard{
		shardID:  shardID,
		pageBlob: pageBlob,
		walBlob:  walBlob,
		index:    make(map[uint64]*IndexEntry),
		nextSlot: SuperblockSize, // Start after superblock
		superblock: &Superblock{
			Version:        SuperblockVersion,
			ShardID:        shardID,
			LastAppliedLSN: 0,
		},
	}
}

// Initialize initializes the shard by creating blobs and writing initial superblock
func (s *Shard) Initialize(ctx context.Context) error {
	// Create page blob
	if err := s.pageBlob.Create(ctx); err != nil {
		return fmt.Errorf("failed to create page blob: %w", err)
	}

	// Create WAL blob
	if err := s.walBlob.Create(ctx); err != nil {
		return fmt.Errorf("failed to create WAL blob: %w", err)
	}

	// Write initial superblock
	if err := s.writeSuperblock(ctx); err != nil {
		return fmt.Errorf("failed to write superblock: %w", err)
	}

	return nil
}

// Put stores a key-value pair with optimistic concurrency control
func (s *Shard) Put(ctx context.Context, key, value []byte, expectedVersion uint64) error {
	keyHash := hashKey(key)

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	// Check version for optimistic concurrency
	if entry, exists := s.index[keyHash]; exists {
		if expectedVersion != 0 && entry.Version != expectedVersion {
			return fmt.Errorf("version mismatch: expected %d, got %d", expectedVersion, entry.Version)
		}
	} else if expectedVersion != 0 {
		return fmt.Errorf("key does not exist, cannot update with expected version")
	}

	// Allocate or reuse slot
	var slotOffset int64
	var newVersion uint64
	if entry, exists := s.index[keyHash]; exists {
		slotOffset = entry.SlotOffset
		newVersion = entry.Version + 1
	} else {
		slotOffset = s.allocateSlot()
		newVersion = 1
	}

	// Create WAL record
	walRecord := &WALRecord{
		OpType:     OpTypePut,
		KeyHash:    keyHash,
		SlotOffset: slotOffset,
		Version:    newVersion,
		KeyLen:     uint16(len(key)),
		ValueLen:   uint16(len(value)),
		Key:        key,
		Value:      value,
	}

	// Marshal WAL record
	walData, err := MarshalWALRecord(walRecord)
	if err != nil {
		return fmt.Errorf("failed to marshal WAL record: %w", err)
	}

	// Append to WAL
	lsn, err := s.walBlob.Append(ctx, walData)
	if err != nil {
		return fmt.Errorf("failed to append to WAL: %w", err)
	}

	// Marshal slot data
	slotData, err := MarshalSlot(keyHash, key, value, newVersion, 0)
	if err != nil {
		return fmt.Errorf("failed to marshal slot: %w", err)
	}

	// Write to page blob
	if err := s.pageBlob.UploadPages(ctx, slotOffset, slotData); err != nil {
		return fmt.Errorf("failed to upload pages: %w", err)
	}

	// Update index
	s.index[keyHash] = &IndexEntry{
		SlotOffset: slotOffset,
		Version:    newVersion,
		KeyHash:    keyHash,
	}

	// Update last applied LSN
	s.sbMu.Lock()
	s.superblock.LastAppliedLSN = uint64(lsn + int64(len(walData)))
	s.sbMu.Unlock()

	return nil
}

// Get retrieves a value by key
func (s *Shard) Get(ctx context.Context, key []byte) ([]byte, uint64, error) {
	keyHash := hashKey(key)

	s.indexMu.RLock()
	entry, exists := s.index[keyHash]
	s.indexMu.RUnlock()

	if !exists {
		return nil, 0, fmt.Errorf("key not found")
	}

	// Read slot from page blob
	slotData, err := s.pageBlob.DownloadRange(ctx, entry.SlotOffset, SlotSize)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read slot: %w", err)
	}

	// Unmarshal slot
	_, slotKey, value, version, flags, err := UnmarshalSlot(slotData)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal slot: %w", err)
	}

	// Check if tombstoned
	if flags&FlagTombstone != 0 {
		return nil, 0, fmt.Errorf("key deleted")
	}

	// Verify key matches
	if string(key) != string(slotKey) {
		return nil, 0, fmt.Errorf("key hash collision detected")
	}

	return value, version, nil
}

// Delete removes a key
func (s *Shard) Delete(ctx context.Context, key []byte, expectedVersion uint64) error {
	keyHash := hashKey(key)

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	entry, exists := s.index[keyHash]
	if !exists {
		return fmt.Errorf("key not found")
	}

	// Check version
	if expectedVersion != 0 && entry.Version != expectedVersion {
		return fmt.Errorf("version mismatch: expected %d, got %d", expectedVersion, entry.Version)
	}

	newVersion := entry.Version + 1

	// Create WAL record for deletion
	walRecord := &WALRecord{
		OpType:     OpTypeDelete,
		KeyHash:    keyHash,
		SlotOffset: entry.SlotOffset,
		Version:    newVersion,
		KeyLen:     uint16(len(key)),
		ValueLen:   0,
		Key:        key,
		Value:      nil,
	}

	walData, err := MarshalWALRecord(walRecord)
	if err != nil {
		return fmt.Errorf("failed to marshal WAL record: %w", err)
	}

	// Append to WAL
	lsn, err := s.walBlob.Append(ctx, walData)
	if err != nil {
		return fmt.Errorf("failed to append to WAL: %w", err)
	}

	// Mark slot as tombstone
	slotData, err := MarshalSlot(keyHash, key, nil, newVersion, FlagTombstone)
	if err != nil {
		return fmt.Errorf("failed to marshal slot: %w", err)
	}

	if err := s.pageBlob.UploadPages(ctx, entry.SlotOffset, slotData); err != nil {
		return fmt.Errorf("failed to upload pages: %w", err)
	}

	// Remove from index
	delete(s.index, keyHash)

	// Update last applied LSN
	s.sbMu.Lock()
	s.superblock.LastAppliedLSN = uint64(lsn + int64(len(walData)))
	s.sbMu.Unlock()

	return nil
}

// Recover replays the WAL to rebuild the index after a crash
func (s *Shard) Recover(ctx context.Context) error {
	// Read superblock
	sbData, err := s.pageBlob.DownloadRange(ctx, 0, SuperblockSize)
	if err != nil {
		return fmt.Errorf("failed to read superblock: %w", err)
	}

	sb, err := UnmarshalSuperblock(sbData)
	if err != nil {
		return fmt.Errorf("failed to unmarshal superblock: %w", err)
	}

	s.superblock = sb

	// Read WAL from last applied LSN
	walData, err := s.walBlob.ReadFrom(ctx, int64(sb.LastAppliedLSN))
	if err != nil {
		return fmt.Errorf("failed to read WAL: %w", err)
	}

	// Replay WAL records
	offset := 0
	for offset < len(walData) {
		record, size, err := UnmarshalWALRecord(walData[offset:])
		if err != nil {
			return fmt.Errorf("failed to unmarshal WAL record at offset %d: %w", offset, err)
		}

		// Apply record
		if err := s.applyWALRecord(ctx, record); err != nil {
			return fmt.Errorf("failed to apply WAL record: %w", err)
		}

		offset += size
	}

	// Update superblock
	s.superblock.LastAppliedLSN = uint64(len(walData))
	if err := s.writeSuperblock(ctx); err != nil {
		return fmt.Errorf("failed to update superblock: %w", err)
	}

	return nil
}

// applyWALRecord applies a single WAL record to the page blob and index
func (s *Shard) applyWALRecord(ctx context.Context, record *WALRecord) error {
	switch record.OpType {
	case OpTypePut:
		slotData, err := MarshalSlot(record.KeyHash, record.Key, record.Value, record.Version, 0)
		if err != nil {
			return fmt.Errorf("failed to marshal slot: %w", err)
		}

		if err := s.pageBlob.UploadPages(ctx, record.SlotOffset, slotData); err != nil {
			return fmt.Errorf("failed to upload pages: %w", err)
		}

		s.index[record.KeyHash] = &IndexEntry{
			SlotOffset: record.SlotOffset,
			Version:    record.Version,
			KeyHash:    record.KeyHash,
		}

	case OpTypeDelete:
		slotData, err := MarshalSlot(record.KeyHash, record.Key, nil, record.Version, FlagTombstone)
		if err != nil {
			return fmt.Errorf("failed to marshal slot: %w", err)
		}

		if err := s.pageBlob.UploadPages(ctx, record.SlotOffset, slotData); err != nil {
			return fmt.Errorf("failed to upload pages: %w", err)
		}

		delete(s.index, record.KeyHash)
	}

	return nil
}

// allocateSlot allocates a new slot and returns its offset
func (s *Shard) allocateSlot() int64 {
	offset := s.nextSlot
	s.nextSlot += SlotSize
	return offset
}

// writeSuperblock writes the current superblock to the page blob
func (s *Shard) writeSuperblock(ctx context.Context) error {
	s.sbMu.Lock()
	defer s.sbMu.Unlock()

	data, err := MarshalSuperblock(s.superblock)
	if err != nil {
		return fmt.Errorf("failed to marshal superblock: %w", err)
	}

	if err := s.pageBlob.UploadPages(ctx, 0, data); err != nil {
		return fmt.Errorf("failed to upload superblock: %w", err)
	}

	return nil
}

// hashKey computes a hash of the key
func hashKey(key []byte) uint64 {
	h := fnv.New64a()
	h.Write(key)
	return h.Sum64()
}
