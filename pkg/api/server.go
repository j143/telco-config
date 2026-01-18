package api

// This file contains the gRPC server implementation.
// To use it, generate the protobuf code first:
//   make proto
// Then uncomment the implementation below.

/*
import (
	"context"
	"fmt"

	"github.com/j143/telco-config/pkg/engine"
)

// Server implements the IroncladDB gRPC service
type Server struct {
	UnimplementedIroncladDBServer
	shard *engine.Shard
}

// NewServer creates a new gRPC server instance
func NewServer(shard *engine.Shard) *Server {
	return &Server{
		shard: shard,
	}
}

// Put stores a key-value pair
func (s *Server) Put(ctx context.Context, req *PutRequest) (*PutResponse, error) {
	if len(req.Key) == 0 {
		return nil, fmt.Errorf("key cannot be empty")
	}

	err := s.shard.Put(ctx, req.Key, req.Value, req.ExpectedVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to put: %w", err)
	}

	// Get the new version
	_, version, err := s.shard.Get(ctx, req.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to get version after put: %w", err)
	}

	return &PutResponse{
		Version: version,
	}, nil
}

// Get retrieves a value by key
func (s *Server) Get(ctx context.Context, req *GetRequest) (*GetResponse, error) {
	if len(req.Key) == 0 {
		return nil, fmt.Errorf("key cannot be empty")
	}

	value, version, err := s.shard.Get(ctx, req.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to get: %w", err)
	}

	return &GetResponse{
		Value:   value,
		Version: version,
	}, nil
}

// Delete removes a key
func (s *Server) Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error) {
	if len(req.Key) == 0 {
		return nil, fmt.Errorf("key cannot be empty")
	}

	err := s.shard.Delete(ctx, req.Key, req.ExpectedVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to delete: %w", err)
	}

	return &DeleteResponse{
		Success: true,
	}, nil
}

// Scan retrieves keys with a given prefix (stub implementation)
func (s *Server) Scan(ctx context.Context, req *ScanRequest) (*ScanResponse, error) {
	// For now, return empty - full implementation would iterate through index
	return &ScanResponse{
		Items: []*KeyValue{},
	}, nil
}

// Health returns the health status of the service
func (s *Server) Health(ctx context.Context, req *HealthRequest) (*HealthResponse, error) {
	return &HealthResponse{
		Healthy: true,
		Status:  "OK",
	}, nil
}
*/
