package blob

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/appendblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
)

// WALBlob manages append blob operations for the write-ahead log
type WALBlob struct {
	client *appendblob.Client
}

// NewWALBlob creates a new WAL blob store
func NewWALBlob(credential azcore.TokenCredential, accountURL, containerName, blobName string) (*WALBlob, error) {
	serviceClient, err := azblob.NewClient(accountURL, credential, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create service client: %w", err)
	}

	// Create container if it doesn't exist
	_, err = serviceClient.CreateContainer(context.Background(), containerName, nil)
	if err != nil {
		// Ignore if container already exists
	}

	// Create append blob client
	appendBlobClient := serviceClient.ServiceClient().NewContainerClient(containerName).NewAppendBlobClient(blobName)

	return &WALBlob{
		client: appendBlobClient,
	}, nil
}

// Create initializes the append blob
func (wb *WALBlob) Create(ctx context.Context) error {
	_, err := wb.client.Create(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to create append blob: %w", err)
	}
	return nil
}

// Append appends data to the WAL blob
func (wb *WALBlob) Append(ctx context.Context, data []byte) (int64, error) {
	// Get current size before append to return the offset
	props, err := wb.client.GetProperties(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get blob properties: %w", err)
	}

	offset := *props.ContentLength

	_, err = wb.client.AppendBlock(ctx, streaming.NopCloser(bytes.NewReader(data)), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to append block: %w", err)
	}

	return offset, nil
}

// ReadFrom reads WAL data starting from the specified offset
func (wb *WALBlob) ReadFrom(ctx context.Context, offset int64) ([]byte, error) {
	// Get blob size first
	props, err := wb.client.GetProperties(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get blob properties: %w", err)
	}

	size := *props.ContentLength
	if offset >= size {
		return []byte{}, nil // No data to read
	}

	downloadResponse, err := wb.client.DownloadStream(ctx, &blob.DownloadStreamOptions{
		Range: blob.HTTPRange{
			Offset: offset,
			Count:  size - offset,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to download stream: %w", err)
	}

	data, err := io.ReadAll(downloadResponse.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read download response: %w", err)
	}

	return data, nil
}

// Size returns the current size of the WAL blob
func (wb *WALBlob) Size(ctx context.Context) (int64, error) {
	props, err := wb.client.GetProperties(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get blob properties: %w", err)
	}
	return *props.ContentLength, nil
}

// Delete removes the WAL blob
func (wb *WALBlob) Delete(ctx context.Context) error {
	_, err := wb.client.Delete(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to delete append blob: %w", err)
	}
	return nil
}
