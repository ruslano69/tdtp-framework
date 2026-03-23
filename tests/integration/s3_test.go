//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ruslano69/tdtp-framework/pkg/storage"
	_ "github.com/ruslano69/tdtp-framework/pkg/storage/s3"
)

// ─── S3 test configuration ────────────────────────────────────────────────────

const (
	s3TestEndpoint  = "http://localhost:8333"
	s3TestRegion    = "us-east-1"
	s3TestBucket    = "tdtp-test"
	s3TestAccessKey = "tdtp_test_key"
	s3TestSecretKey = "tdtp_test_secret_2025"
)

// s3Available is set in TestMain; skips all S3 tests when SeaweedFS is not running.
var s3Available bool

// ─── TestMain ─────────────────────────────────────────────────────────────────

func TestMain(m *testing.M) {
	s3Available = setupS3()
	if !s3Available {
		fmt.Fprintln(os.Stderr, "⚠ SeaweedFS not available — S3 tests will be skipped (run: make start-s3)")
	}

	code := m.Run()

	if s3Available {
		cleanupS3TestObjects()
	}
	os.Exit(code)
}

// setupS3 checks if SeaweedFS is reachable and creates the test bucket.
// Returns true when the environment is ready.
func setupS3() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := newRawS3Client()

	// ListBuckets is a lightweight probe: succeeds ↔ server is up & authenticated.
	_, err := client.ListBuckets(ctx, &awss3.ListBucketsInput{})
	if err != nil {
		return false
	}

	// Ensure the test bucket exists.
	_, err = client.CreateBucket(ctx, &awss3.CreateBucketInput{
		Bucket: aws.String(s3TestBucket),
	})
	if err != nil && !strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") &&
		!strings.Contains(err.Error(), "BucketAlreadyExists") {
		fmt.Fprintf(os.Stderr, "S3 setup: CreateBucket %q: %v\n", s3TestBucket, err)
		return false
	}

	return true
}

// cleanupS3TestObjects removes all objects under the "integration-test/" prefix.
func cleanupS3TestObjects() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	store, err := storage.New(testStorageCfg())
	if err != nil {
		return
	}
	defer store.Close()

	objs, err := store.List(ctx, "integration-test/")
	if err != nil {
		return
	}
	for _, obj := range objs {
		_ = store.Delete(ctx, obj.Key)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// testStorageCfg returns the storage.Config for the local SeaweedFS instance.
func testStorageCfg() storage.Config {
	return storage.Config{
		Type: "s3",
		S3: storage.S3Config{
			Endpoint:  s3TestEndpoint,
			Region:    s3TestRegion,
			Bucket:    s3TestBucket,
			AccessKey: s3TestAccessKey,
			SecretKey: s3TestSecretKey,
		},
	}
}

// newRawS3Client returns a raw AWS S3 client pointed at the test SeaweedFS.
// Used for bucket-level operations (CreateBucket, ListBuckets) that are not
// part of the storage.ObjectStorage interface.
func newRawS3Client() *awss3.Client {
	awsCfg, _ := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(s3TestRegion),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(s3TestAccessKey, s3TestSecretKey, ""),
		),
	)
	return awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String(s3TestEndpoint)
	})
}

// testKey builds a unique object key under the integration-test/ prefix.
func testKey(name string) string {
	return fmt.Sprintf("integration-test/%s/%s", name, time.Now().Format("20060102-150405.000"))
}

// skipIfS3Unavailable marks the test as skipped when SeaweedFS is not running.
func skipIfS3Unavailable(t *testing.T) {
	t.Helper()
	if !s3Available {
		t.Skip("SeaweedFS not available")
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestS3Connection verifies that the storage driver can be created and List returns
// an empty result for a fresh prefix (no error means the connection is healthy).
func TestS3Connection(t *testing.T) {
	skipIfS3Unavailable(t)

	store, err := storage.New(testStorageCfg())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	objs, err := store.List(ctx, "integration-test/__probe__/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	t.Logf("S3 connection OK — objects under probe prefix: %d", len(objs))
}

// TestS3PutGet writes an object and reads it back, comparing bytes.
func TestS3PutGet(t *testing.T) {
	skipIfS3Unavailable(t)

	store, err := storage.New(testStorageCfg())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	defer store.Close()

	key := testKey("put-get")
	payload := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<TDTPPacket>hello S3</TDTPPacket>")
	meta := map[string]string{"table": "users", "rows": "1"}

	ctx := context.Background()

	if err := store.Put(ctx, key, bytes.NewReader(payload), meta); err != nil {
		t.Fatalf("Put: %v", err)
	}
	t.Logf("Put  %d bytes → s3://%s/%s", len(payload), s3TestBucket, key)

	rc, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	t.Logf("Got  %d bytes ← s3://%s/%s", len(got), s3TestBucket, key)

	if !bytes.Equal(payload, got) {
		t.Errorf("content mismatch\nwant: %q\n got: %q", payload, got)
	}
}

// TestS3Stat verifies that Stat returns correct size and key for a stored object.
func TestS3Stat(t *testing.T) {
	skipIfS3Unavailable(t)

	store, err := storage.New(testStorageCfg())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	defer store.Close()

	key := testKey("stat")
	payload := []byte("stat test content — 32 bytes ---")
	ctx := context.Background()

	if err := store.Put(ctx, key, bytes.NewReader(payload), nil); err != nil {
		t.Fatalf("Put: %v", err)
	}

	info, err := store.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info.Key != key {
		t.Errorf("Stat.Key = %q, want %q", info.Key, key)
	}
	if info.Size != int64(len(payload)) {
		t.Errorf("Stat.Size = %d, want %d", info.Size, len(payload))
	}
	t.Logf("Stat OK — key=%s size=%d modtime=%s", info.Key, info.Size, info.ModTime.Format(time.RFC3339))
}

// TestS3List puts three objects under a common prefix and verifies List finds all of them.
func TestS3List(t *testing.T) {
	skipIfS3Unavailable(t)

	store, err := storage.New(testStorageCfg())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	prefix := fmt.Sprintf("integration-test/list/%s/", time.Now().Format("150405"))

	// Put 3 objects under the same prefix.
	for i := 1; i <= 3; i++ {
		key := fmt.Sprintf("%spart_%d.xml", prefix, i)
		data := fmt.Sprintf("<part>%d</part>", i)
		if err := store.Put(ctx, key, strings.NewReader(data), nil); err != nil {
			t.Fatalf("Put part %d: %v", i, err)
		}
	}

	objs, err := store.List(ctx, prefix)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(objs) != 3 {
		t.Errorf("List returned %d objects, want 3", len(objs))
	}
	for _, o := range objs {
		t.Logf("  listed: %s (%d bytes)", o.Key, o.Size)
	}
}

// TestS3Delete puts an object, deletes it, and confirms Stat returns an error.
func TestS3Delete(t *testing.T) {
	skipIfS3Unavailable(t)

	store, err := storage.New(testStorageCfg())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	defer store.Close()

	key := testKey("delete")
	ctx := context.Background()

	if err := store.Put(ctx, key, strings.NewReader("to be deleted"), nil); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = store.Stat(ctx, key)
	if err == nil {
		t.Error("expected Stat to return error after Delete, got nil")
	} else {
		t.Logf("Stat after Delete correctly returned error: %v", err)
	}
}

// TestS3LargeObject verifies upload/download of a ~512 KB object (exercises the
// uploader buffer path in the S3 driver).
func TestS3LargeObject(t *testing.T) {
	skipIfS3Unavailable(t)

	store, err := storage.New(testStorageCfg())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	defer store.Close()

	const size = 512 * 1024
	payload := bytes.Repeat([]byte("x"), size)
	key := testKey("large")
	ctx := context.Background()

	if err := store.Put(ctx, key, bytes.NewReader(payload), nil); err != nil {
		t.Fatalf("Put: %v", err)
	}

	info, err := store.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size != int64(size) {
		t.Errorf("Stat.Size = %d, want %d", info.Size, size)
	}

	rc, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != size {
		t.Errorf("downloaded %d bytes, want %d", len(got), size)
	}
	t.Logf("Large object OK — %d KB round-tripped", size/1024)
}

// TestS3Metadata verifies that object metadata (tdtp-* headers) is stored and
// retrievable via Stat.
func TestS3Metadata(t *testing.T) {
	skipIfS3Unavailable(t)

	store, err := storage.New(testStorageCfg())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	defer store.Close()

	key := testKey("meta")
	meta := map[string]string{"table": "orders", "rows": "42", "compress": "zstd"}
	ctx := context.Background()

	if err := store.Put(ctx, key, strings.NewReader("metadata test"), meta); err != nil {
		t.Fatalf("Put: %v", err)
	}

	info, err := store.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	// SeaweedFS/S3 prefixes user metadata with "tdtp-" (set in the driver).
	for wantK, wantV := range meta {
		gotV, ok := info.Metadata["tdtp-"+wantK]
		if !ok {
			t.Errorf("metadata key %q not found in Stat response; available: %v", "tdtp-"+wantK, info.Metadata)
			continue
		}
		if gotV != wantV {
			t.Errorf("metadata[tdtp-%s] = %q, want %q", wantK, gotV, wantV)
		}
	}
	t.Logf("Metadata OK — %d keys verified", len(meta))
}
