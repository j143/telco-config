package blob

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/pageblob"
)

// PageBlobStore manages page blob operations for the segment storage
type PageBlobStore struct {
	client *pageblob.Client
	size   int64
}

// NewPageBlobStore creates a new page blob store
func NewPageBlobStore(credential azcore.TokenCredential, accountURL, containerName, blobName string, size int64) (*PageBlobStore, error) {
	// Ensure size is multiple of 512 bytes (page blob requirement)
	if size%512 != 0 {
		return nil, fmt.Errorf("page blob size must be multiple of 512 bytes, got %d", size)
	}

	serviceClient, err := azblob.NewClient(accountURL, credential, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create service client: %w", err)
	}

	// Create container if it doesn't exist
	_, err = serviceClient.CreateContainer(context.Background(), containerName, nil)
	if err != nil {
		// Ignore if container already exists
	}

	// Create page blob client
	pageBlobClient := serviceClient.ServiceClient().NewContainerClient(containerName).NewPageBlobClient(blobName)

	return &PageBlobStore{
		client: pageBlobClient,
		size:   size,
	}, nil
}

// Create provisions the page blob with the specified size
func (pbs *PageBlobStore) Create(ctx context.Context) error {
	_, err := pbs.client.Create(ctx, pbs.size, nil)
	if err != nil {
		return fmt.Errorf("failed to create page blob: %w", err)
	}
	return nil
}

// UploadPages writes data to the page blob at the specified offset
// Data must be aligned to 512-byte boundaries
func (pbs *PageBlobStore) UploadPages(ctx context.Context, offset int64, data []byte) error {
	if offset%512 != 0 {
		return fmt.Errorf("offset must be aligned to 512 bytes, got %d", offset)
	}
	if len(data)%512 != 0 {
		return fmt.Errorf("data size must be multiple of 512 bytes, got %d", len(data))
	}

	_, err := pbs.client.UploadPages(ctx, streaming.NopCloser(bytes.NewReader(data)), blob.HTTPRange{
		Offset: offset,
		Count:  int64(len(data)),
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to upload pages: %w", err)
	}
	return nil
}

// DownloadRange reads data from the page blob at the specified offset
func (pbs *PageBlobStore) DownloadRange(ctx context.Context, offset, length int64) ([]byte, error) {
	downloadResponse, err := pbs.client.DownloadStream(ctx, &blob.DownloadStreamOptions{
		Range: blob.HTTPRange{
			Offset: offset,
			Count:  length,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to download range: %w", err)
	}

	data, err := io.ReadAll(downloadResponse.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read download response: %w", err)
	}

	return data, nil
}

// Size returns the size of the page blob
func (pbs *PageBlobStore) Size() int64 {
	return pbs.size
}

// Delete removes the page blob
func (pbs *PageBlobStore) Delete(ctx context.Context) error {
	_, err := pbs.client.Delete(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to delete page blob: %w", err)
	}
	return nil
}
