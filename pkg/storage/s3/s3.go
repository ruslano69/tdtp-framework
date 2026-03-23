//go:build !nos3

// Package s3 provides S3-compatible object storage for the TDTP framework.
package s3

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

func init() {
	storage.Register("s3", NewDriver)
}

// Driver implements storage.ObjectStorage over an S3-compatible API.
type Driver struct {
	client   *s3.Client
	uploader *manager.Uploader //nolint:staticcheck // transfermanager is not yet stable
	bucket   string
}

// NewDriver creates an S3 driver from the given Config.
func NewDriver(cfg storage.Config) (storage.ObjectStorage, error) {
	if cfg.S3.Bucket == "" {
		return nil, fmt.Errorf("s3: bucket must not be empty")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.S3.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.S3.AccessKey, cfg.S3.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("s3: failed to load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		// Required for SeaweedFS, MinIO and other S3-compatible stores.
		o.UsePathStyle = true
		if cfg.S3.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3.Endpoint)
		}
	})

	// PartSize=5MB: packets are ≤3.8MB, so the uploader sends a single PutObject
	// (below multipart threshold). No temp files needed.
	uploader := manager.NewUploader(client, func(u *manager.Uploader) { //nolint:staticcheck // transfermanager is not yet stable
		u.PartSize = 5 * 1024 * 1024 // 5 MB
		u.Concurrency = 1            // parallelism at the worker level, not within a packet
	})

	return &Driver{
		client:   client,
		uploader: uploader,
		bucket:   cfg.S3.Bucket,
	}, nil
}

// Put streams reader to S3 key, attaching meta as x-amz-meta-tdtp-* headers.
func (d *Driver) Put(ctx context.Context, key string, reader io.Reader, meta map[string]string) error {
	s3meta := make(map[string]string, len(meta))
	for k, v := range meta {
		s3meta["tdtp-"+k] = v
	}
	_, err := d.uploader.Upload(ctx, &s3.PutObjectInput{ //nolint:staticcheck // transfermanager is not yet stable
		Bucket:   aws.String(d.bucket),
		Key:      aws.String(key),
		Body:     reader,
		Metadata: s3meta,
	})
	if err != nil {
		return fmt.Errorf("s3: Put %s: %w", key, err)
	}
	return nil
}

// Get returns a ReadCloser for the object at key. Caller must close it.
func (d *Driver) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	resp, err := d.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3: Get %s: %w", key, err)
	}
	return resp.Body, nil
}

// Stat returns metadata for the object at key.
func (d *Driver) Stat(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	resp, err := d.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3: Stat %s: %w", key, err)
	}

	info := &storage.ObjectInfo{
		Key:      key,
		Metadata: make(map[string]string, len(resp.Metadata)),
	}
	if resp.ContentLength != nil {
		info.Size = *resp.ContentLength
	}
	if resp.LastModified != nil {
		info.ModTime = *resp.LastModified
	}
	for k, v := range resp.Metadata {
		info.Metadata[k] = v
	}
	return info, nil
}

// List returns all objects with the given prefix.
func (d *Driver) List(ctx context.Context, prefix string) ([]storage.ObjectInfo, error) {
	var result []storage.ObjectInfo

	paginator := s3.NewListObjectsV2Paginator(d.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(d.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3: List prefix=%s: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			info := storage.ObjectInfo{
				Key: aws.ToString(obj.Key),
			}
			if obj.Size != nil {
				info.Size = *obj.Size
			}
			if obj.LastModified != nil {
				info.ModTime = *obj.LastModified
			}
			result = append(result, info)
		}
	}
	return result, nil
}

// Delete removes the object at key.
func (d *Driver) Delete(ctx context.Context, key string) error {
	_, err := d.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3: Delete %s: %w", key, err)
	}
	return nil
}

// Close is a no-op; the S3 client is stateless.
func (d *Driver) Close() error { return nil }
