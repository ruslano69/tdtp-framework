package storage

import (
	"context"
	"io"
	"time"
)

// ObjectInfo holds metadata about a stored object.
type ObjectInfo struct {
	Key      string
	Size     int64
	ModTime  time.Time
	Metadata map[string]string
}

// ObjectStorage is the interface for object storage backends (S3, SeaweedFS, etc.).
type ObjectStorage interface {
	Put(ctx context.Context, key string, reader io.Reader, meta map[string]string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Stat(ctx context.Context, key string) (*ObjectInfo, error)
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)
	Delete(ctx context.Context, key string) error
	Close() error
}
