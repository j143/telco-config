package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/j143/telco-config/pkg/blob"
	"github.com/j143/telco-config/pkg/engine"
	"google.golang.org/grpc"
)

// Config holds the application configuration
type Config struct {
	StorageAccountURL string
	ContainerName     string
	ShardID           uint32
	PageBlobName      string
	WALBlobName       string
	PageBlobSize      int64
	GRPCPort          int
	HTTPPort          int
}

// DefaultConfig returns a configuration with defaults from environment variables
func DefaultConfig() *Config {
	return &Config{
		StorageAccountURL: getEnv("STORAGE_ACCOUNT_URL", "https://your-account.blob.core.windows.net"),
		ContainerName:     getEnv("CONTAINER_NAME", "ironclad-db"),
		ShardID:           0,
		PageBlobName:      getEnv("PAGE_BLOB_NAME", "shard-0.blob"),
		WALBlobName:       getEnv("WAL_BLOB_NAME", "shard-0.wal"),
		PageBlobSize:      8 * 1024 * 1024 * 1024, // 8 GB default
		GRPCPort:          9090,
		HTTPPort:          8080,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	log.Println("Starting Ironclad-DB...")

	config := DefaultConfig()

	// Create Azure credential
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("Failed to create credential: %v", err)
	}

	// Create blob stores
	pageBlob, err := blob.NewPageBlobStore(
		credential,
		config.StorageAccountURL,
		config.ContainerName,
		config.PageBlobName,
		config.PageBlobSize,
	)
	if err != nil {
		log.Fatalf("Failed to create page blob store: %v", err)
	}

	walBlob, err := blob.NewWALBlob(
		credential,
		config.StorageAccountURL,
		config.ContainerName,
		config.WALBlobName,
	)
	if err != nil {
		log.Fatalf("Failed to create WAL blob: %v", err)
	}

	// Create shard
	shard := engine.NewShard(config.ShardID, pageBlob, walBlob)

	// Check if we need to initialize or recover
	ctx := context.Background()
	if os.Getenv("INIT_MODE") == "true" {
		log.Println("Initializing new shard...")
		if err := shard.Initialize(ctx); err != nil {
			log.Fatalf("Failed to initialize shard: %v", err)
		}
		log.Println("Shard initialized successfully")
	} else {
		log.Println("Recovering shard from existing state...")
		if err := shard.Recover(ctx); err != nil {
			log.Printf("Recovery failed, trying to initialize: %v", err)
			if err := shard.Initialize(ctx); err != nil {
				log.Fatalf("Failed to initialize shard: %v", err)
			}
		}
		log.Println("Shard recovered successfully")
	}

	// Start gRPC server (disabled for now - would need generated proto code)
	// grpcServer := startGRPCServer(config.GRPCPort, shard)

	// Start HTTP health server
	httpServer := startHTTPServer(config.HTTPPort)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	// grpcServer.GracefulStop()
	httpServer.Shutdown(ctx)
	log.Println("Shutdown complete")
}

func startGRPCServer(port int, shard *engine.Shard) *grpc.Server {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Failed to listen on port %d: %v", port, err)
	}

	grpcServer := grpc.NewServer()
	// Note: Would register API server here once proto is generated
	// api.RegisterIroncladDBServer(grpcServer, api.NewServer(shard))

	go func() {
		log.Printf("gRPC server listening on :%d", port)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve gRPC: %v", err)
		}
	}()

	return grpcServer
}

func startHTTPServer(port int) *http.Server {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	// Readiness check endpoint
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		log.Printf("HTTP server listening on :%d", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to serve HTTP: %v", err)
		}
	}()

	return server
}
