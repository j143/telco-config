package engine

import (
	"bytes"
	"testing"
)

func TestMarshalUnmarshalSuperblock(t *testing.T) {
	original := &Superblock{
		Version:        SuperblockVersion,
		ShardID:        42,
		LastAppliedLSN: 12345,
	}

	// Marshal
	data, err := MarshalSuperblock(original)
	if err != nil {
		t.Fatalf("Failed to marshal superblock: %v", err)
	}

	if len(data) != SuperblockSize {
		t.Fatalf("Expected superblock size %d, got %d", SuperblockSize, len(data))
	}

	// Unmarshal
	decoded, err := UnmarshalSuperblock(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal superblock: %v", err)
	}

	// Verify
	if decoded.Version != original.Version {
		t.Errorf("Version mismatch: expected %d, got %d", original.Version, decoded.Version)
	}
	if decoded.ShardID != original.ShardID {
		t.Errorf("ShardID mismatch: expected %d, got %d", original.ShardID, decoded.ShardID)
	}
	if decoded.LastAppliedLSN != original.LastAppliedLSN {
		t.Errorf("LastAppliedLSN mismatch: expected %d, got %d", original.LastAppliedLSN, decoded.LastAppliedLSN)
	}
}

func TestMarshalUnmarshalSlot(t *testing.T) {
	keyHash := uint64(0x1234567890ABCDEF)
	key := []byte("test-key")
	value := []byte("test-value-data")
	version := uint64(5)
	flags := uint32(0)

	// Marshal
	data, err := MarshalSlot(keyHash, key, value, version, flags)
	if err != nil {
		t.Fatalf("Failed to marshal slot: %v", err)
	}

	if len(data) != SlotSize {
		t.Fatalf("Expected slot size %d, got %d", SlotSize, len(data))
	}

	// Unmarshal
	decodedHash, decodedKey, decodedValue, decodedVersion, decodedFlags, err := UnmarshalSlot(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal slot: %v", err)
	}

	// Verify
	if decodedHash != keyHash {
		t.Errorf("KeyHash mismatch: expected %x, got %x", keyHash, decodedHash)
	}
	if !bytes.Equal(decodedKey, key) {
		t.Errorf("Key mismatch: expected %s, got %s", key, decodedKey)
	}
	if !bytes.Equal(decodedValue, value) {
		t.Errorf("Value mismatch: expected %s, got %s", value, decodedValue)
	}
	if decodedVersion != version {
		t.Errorf("Version mismatch: expected %d, got %d", version, decodedVersion)
	}
	if decodedFlags != flags {
		t.Errorf("Flags mismatch: expected %d, got %d", flags, decodedFlags)
	}
}

func TestMarshalUnmarshalWALRecord(t *testing.T) {
	record := &WALRecord{
		OpType:     OpTypePut,
		KeyHash:    0xABCDEF1234567890,
		SlotOffset: 1024,
		Version:    3,
		KeyLen:     8,
		ValueLen:   10,
		Key:        []byte("test-key"),
		Value:      []byte("test-value"),
	}

	// Marshal
	data, err := MarshalWALRecord(record)
	if err != nil {
		t.Fatalf("Failed to marshal WAL record: %v", err)
	}

	// Unmarshal
	decoded, size, err := UnmarshalWALRecord(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal WAL record: %v", err)
	}

	if size != len(data) {
		t.Errorf("Size mismatch: expected %d, got %d", len(data), size)
	}

	// Verify
	if decoded.OpType != record.OpType {
		t.Errorf("OpType mismatch: expected %d, got %d", record.OpType, decoded.OpType)
	}
	if decoded.KeyHash != record.KeyHash {
		t.Errorf("KeyHash mismatch: expected %x, got %x", record.KeyHash, decoded.KeyHash)
	}
	if decoded.SlotOffset != record.SlotOffset {
		t.Errorf("SlotOffset mismatch: expected %d, got %d", record.SlotOffset, decoded.SlotOffset)
	}
	if decoded.Version != record.Version {
		t.Errorf("Version mismatch: expected %d, got %d", record.Version, decoded.Version)
	}
	if !bytes.Equal(decoded.Key, record.Key) {
		t.Errorf("Key mismatch: expected %s, got %s", record.Key, decoded.Key)
	}
	if !bytes.Equal(decoded.Value, record.Value) {
		t.Errorf("Value mismatch: expected %s, got %s", record.Value, decoded.Value)
	}
}

func TestSlotCRCValidation(t *testing.T) {
	keyHash := uint64(0x1234567890ABCDEF)
	key := []byte("test-key")
	value := []byte("test-value")
	version := uint64(1)
	flags := uint32(0)

	// Marshal
	data, err := MarshalSlot(keyHash, key, value, version, flags)
	if err != nil {
		t.Fatalf("Failed to marshal slot: %v", err)
	}

	// Corrupt the data (change a byte in the value section)
	data[SlotHeaderSize+len(key)+2] ^= 0xFF

	// Unmarshal should fail due to CRC mismatch
	_, _, _, _, _, err = UnmarshalSlot(data)
	if err == nil {
		t.Fatal("Expected CRC error, but unmarshal succeeded")
	}
}

func TestWALRecordCRCValidation(t *testing.T) {
	record := &WALRecord{
		OpType:     OpTypePut,
		KeyHash:    0x1234567890ABCDEF,
		SlotOffset: 512,
		Version:    1,
		KeyLen:     4,
		ValueLen:   5,
		Key:        []byte("test"),
		Value:      []byte("value"),
	}

	// Marshal
	data, err := MarshalWALRecord(record)
	if err != nil {
		t.Fatalf("Failed to marshal WAL record: %v", err)
	}

	// Corrupt the data
	data[len(data)-1] ^= 0xFF

	// Unmarshal should fail due to CRC mismatch
	_, _, err = UnmarshalWALRecord(data)
	if err == nil {
		t.Fatal("Expected CRC error, but unmarshal succeeded")
	}
}
