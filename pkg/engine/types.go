package engine

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

// Constants for slot and superblock layout
const (
	SlotSize          = 8192 // 8 KB slots
	SuperblockSize    = 512  // First page (512 bytes)
	SuperblockVersion = 1    // Format version
	MaxKeySize        = 256  // Maximum key size in bytes
	MaxValueSize      = 7680 // Maximum value size (SlotSize - header overhead)
)

// Superblock represents the metadata stored at the beginning of the page blob
type Superblock struct {
	Version        uint32 // Format version
	ShardID        uint32 // Shard identifier
	LastAppliedLSN uint64 // Last applied log sequence number
	Reserved       [496]byte
}

// SlotHeader represents the header of a slot in the page blob
type SlotHeader struct {
	KeyHash  uint64 // Hash of the key
	KeyLen   uint16 // Length of the key
	ValueLen uint16 // Length of the value
	Version  uint64 // Version number for optimistic concurrency
	CRC      uint32 // CRC32 checksum of the slot data
	Flags    uint32 // Flags (e.g., tombstone marker)
	Reserved [8]byte
}

const (
	SlotHeaderSize = 40 // Size of SlotHeader in bytes
	FlagTombstone  = 1  // Slot is tombstoned (deleted)
)

// WALRecord represents a record in the write-ahead log
type WALRecord struct {
	OpType     uint8  // Operation type (Put, Delete)
	KeyHash    uint64 // Hash of the key
	SlotOffset int64  // Offset in the page blob
	Version    uint64 // Version number
	KeyLen     uint16 // Length of the key
	ValueLen   uint16 // Length of the value
	CRC        uint32 // CRC32 checksum
	Key        []byte // Key data
	Value      []byte // Value data
}

const (
	OpTypePut    = 1
	OpTypeDelete = 2
)

// MarshalSuperblock serializes a superblock to bytes
func MarshalSuperblock(sb *Superblock) ([]byte, error) {
	buf := make([]byte, SuperblockSize)
	binary.LittleEndian.PutUint32(buf[0:4], sb.Version)
	binary.LittleEndian.PutUint32(buf[4:8], sb.ShardID)
	binary.LittleEndian.PutUint64(buf[8:16], sb.LastAppliedLSN)
	copy(buf[16:], sb.Reserved[:])
	return buf, nil
}

// UnmarshalSuperblock deserializes a superblock from bytes
func UnmarshalSuperblock(data []byte) (*Superblock, error) {
	if len(data) < SuperblockSize {
		return nil, fmt.Errorf("insufficient data for superblock: got %d bytes, need %d", len(data), SuperblockSize)
	}

	sb := &Superblock{
		Version:        binary.LittleEndian.Uint32(data[0:4]),
		ShardID:        binary.LittleEndian.Uint32(data[4:8]),
		LastAppliedLSN: binary.LittleEndian.Uint64(data[8:16]),
	}
	copy(sb.Reserved[:], data[16:SuperblockSize])
	return sb, nil
}

// MarshalSlot serializes a slot (header + key + value) to bytes, aligned to 512
func MarshalSlot(keyHash uint64, key, value []byte, version uint64, flags uint32) ([]byte, error) {
	if len(key) > MaxKeySize {
		return nil, fmt.Errorf("key too large: %d bytes (max %d)", len(key), MaxKeySize)
	}
	if len(value) > MaxValueSize {
		return nil, fmt.Errorf("value too large: %d bytes (max %d)", len(value), MaxValueSize)
	}

	buf := make([]byte, SlotSize)

	// Write header
	binary.LittleEndian.PutUint64(buf[0:8], keyHash)
	binary.LittleEndian.PutUint16(buf[8:10], uint16(len(key)))
	binary.LittleEndian.PutUint16(buf[10:12], uint16(len(value)))
	binary.LittleEndian.PutUint64(buf[12:20], version)
	binary.LittleEndian.PutUint32(buf[24:28], flags)

	// Write key and value
	copy(buf[SlotHeaderSize:SlotHeaderSize+len(key)], key)
	copy(buf[SlotHeaderSize+len(key):SlotHeaderSize+len(key)+len(value)], value)

	// Calculate CRC over everything except the CRC field itself
	crc := crc32.ChecksumIEEE(buf[0:20])
	crc = crc32.Update(crc, crc32.IEEETable, buf[24:SlotHeaderSize+len(key)+len(value)])
	binary.LittleEndian.PutUint32(buf[20:24], crc)

	return buf, nil
}

// UnmarshalSlot deserializes a slot from bytes
func UnmarshalSlot(data []byte) (keyHash uint64, key, value []byte, version uint64, flags uint32, err error) {
	if len(data) < SlotHeaderSize {
		return 0, nil, nil, 0, 0, fmt.Errorf("insufficient data for slot header: got %d bytes", len(data))
	}

	keyHash = binary.LittleEndian.Uint64(data[0:8])
	keyLen := binary.LittleEndian.Uint16(data[8:10])
	valueLen := binary.LittleEndian.Uint16(data[10:12])
	version = binary.LittleEndian.Uint64(data[12:20])
	storedCRC := binary.LittleEndian.Uint32(data[20:24])
	flags = binary.LittleEndian.Uint32(data[24:28])

	if SlotHeaderSize+int(keyLen)+int(valueLen) > len(data) {
		return 0, nil, nil, 0, 0, fmt.Errorf("slot data truncated")
	}

	// Verify CRC
	crc := crc32.ChecksumIEEE(data[0:20])
	crc = crc32.Update(crc, crc32.IEEETable, data[24:SlotHeaderSize+int(keyLen)+int(valueLen)])
	if crc != storedCRC {
		return 0, nil, nil, 0, 0, fmt.Errorf("CRC mismatch: expected %x, got %x", storedCRC, crc)
	}

	key = make([]byte, keyLen)
	value = make([]byte, valueLen)
	copy(key, data[SlotHeaderSize:SlotHeaderSize+int(keyLen)])
	copy(value, data[SlotHeaderSize+int(keyLen):SlotHeaderSize+int(keyLen)+int(valueLen)])

	return keyHash, key, value, version, flags, nil
}

// MarshalWALRecord serializes a WAL record to bytes
func MarshalWALRecord(record *WALRecord) ([]byte, error) {
	recordSize := 32 + len(record.Key) + len(record.Value)
	buf := make([]byte, recordSize)

	buf[0] = record.OpType
	binary.LittleEndian.PutUint64(buf[1:9], record.KeyHash)
	binary.LittleEndian.PutUint64(buf[9:17], uint64(record.SlotOffset))
	binary.LittleEndian.PutUint64(buf[17:25], record.Version)
	binary.LittleEndian.PutUint16(buf[25:27], record.KeyLen)
	binary.LittleEndian.PutUint16(buf[27:29], record.ValueLen)

	copy(buf[32:32+len(record.Key)], record.Key)
	copy(buf[32+len(record.Key):], record.Value)

	// Calculate CRC
	crc := crc32.ChecksumIEEE(buf[0:29])
	crc = crc32.Update(crc, crc32.IEEETable, buf[32:])
	binary.LittleEndian.PutUint32(buf[29:32], crc)
	record.CRC = crc

	return buf, nil
}

// UnmarshalWALRecord deserializes a WAL record from bytes
func UnmarshalWALRecord(data []byte) (*WALRecord, int, error) {
	if len(data) < 32 {
		return nil, 0, fmt.Errorf("insufficient data for WAL record header")
	}

	record := &WALRecord{
		OpType:     data[0],
		KeyHash:    binary.LittleEndian.Uint64(data[1:9]),
		SlotOffset: int64(binary.LittleEndian.Uint64(data[9:17])),
		Version:    binary.LittleEndian.Uint64(data[17:25]),
		KeyLen:     binary.LittleEndian.Uint16(data[25:27]),
		ValueLen:   binary.LittleEndian.Uint16(data[27:29]),
		CRC:        binary.LittleEndian.Uint32(data[29:32]),
	}

	recordSize := 32 + int(record.KeyLen) + int(record.ValueLen)
	if len(data) < recordSize {
		return nil, 0, fmt.Errorf("WAL record data truncated")
	}

	// Verify CRC
	crc := crc32.ChecksumIEEE(data[0:29])
	crc = crc32.Update(crc, crc32.IEEETable, data[32:recordSize])
	if crc != record.CRC {
		return nil, 0, fmt.Errorf("WAL record CRC mismatch")
	}

	record.Key = make([]byte, record.KeyLen)
	record.Value = make([]byte, record.ValueLen)
	copy(record.Key, data[32:32+record.KeyLen])
	copy(record.Value, data[32+record.KeyLen:recordSize])

	return record, recordSize, nil
}
