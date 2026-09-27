package main

// storage_test.go — s3:// wiring that needs no live SeaweedFS: driver
// registration, URI→config resolution, and the no-config refusal.
// Object movement itself is proven E2E (tests/cli T8 against weed).

import (
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

func TestStorage_DriverRegistered(t *testing.T) {
	// Without drivers_s3.go this fails with "unknown storage type".
	_, err := storage.New(storage.Config{Type: "s3", S3: storage.S3Config{
		Endpoint: "http://127.0.0.1:1",
		Bucket:   "nope",
	}})
	if err != nil {
		t.Fatalf("s3 driver not registered: %v", err)
	}
}

func TestRemoteStorage_BucketOverride(t *testing.T) {
	base := storage.Config{Type: "s3", S3: storage.S3Config{
		Endpoint: "http://127.0.0.1:8333", Bucket: "from-file",
	}}
	cfg, key := remoteStorage(base, "s3://from-uri/ci/t8/u.xml")
	if cfg.Type != "s3" || cfg.S3.Bucket != "from-uri" || key != "ci/t8/u.xml" {
		t.Errorf("got %+v key %q, want bucket from-uri key ci/t8/u.xml", cfg, key)
	}
	// No bucket in the URI: the file's bucket stands.
	cfg, key = remoteStorage(base, "s3://from-file/k.xml")
	if cfg.S3.Bucket != "from-file" || key != "k.xml" {
		t.Errorf("got %+v key %q", cfg, key)
	}
	// The file's endpoint travels untouched.
	if cfg.S3.Endpoint != "http://127.0.0.1:8333" {
		t.Errorf("endpoint lost: %+v", cfg.S3)
	}
}

func TestTestCmd_RemoteNeedsConfig(t *testing.T) {
	// s3:// without --config is UsageError (exit 2), not a crash and not
	// the old "wave 2" refusal: the message must say what is missing.
	code, _, stderr := runApp(t, "test", "s3://b/k.xml")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "--config") {
		t.Errorf("stderr should ask for --config, got %q", stderr)
	}
}
