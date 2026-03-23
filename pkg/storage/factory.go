// Package storage provides functionality for the TDTP framework.
package storage

import (
	"fmt"
	"strings"
	"sync"
)

// S3Config holds S3-compatible storage settings.
type S3Config struct {
	Endpoint   string `yaml:"endpoint"`
	Region     string `yaml:"region"`
	Bucket     string `yaml:"bucket"`
	AccessKey  string `yaml:"access_key"`
	SecretKey  string `yaml:"secret_key"`
	DisableSSL bool   `yaml:"disable_ssl,omitempty"`
}

// Config is the top-level storage configuration passed to the factory.
type Config struct {
	Type string   `yaml:"type"`
	S3   S3Config `yaml:"s3,omitempty"`
}

// StorageConstructor creates an ObjectStorage from a Config.
type StorageConstructor func(cfg Config) (ObjectStorage, error)

var (
	mu       sync.RWMutex
	registry = make(map[string]StorageConstructor)
)

// Register registers a storage constructor for a given storage type.
// Called from init() functions of driver packages.
func Register(storageType string, fn StorageConstructor) {
	mu.Lock()
	defer mu.Unlock()
	registry[storageType] = fn
}

// ParseURI parses a remote storage URI like "s3://bucket/path/to/key".
// Returns (scheme, bucket, key, true) for known remote schemes;
// ("", "", "", false) for local paths.
func ParseURI(uri string) (scheme, bucket, key string, remote bool) {
	for _, pfx := range []string{"s3://"} {
		if strings.HasPrefix(uri, pfx) {
			scheme = pfx[:len(pfx)-3]
			rest := uri[len(pfx):]
			if idx := strings.Index(rest, "/"); idx >= 0 {
				return scheme, rest[:idx], rest[idx+1:], true
			}
			return scheme, rest, "", true
		}
	}
	return "", "", "", false
}

// IsRemote returns true if the path is a supported remote storage URI (e.g. s3://).
func IsRemote(path string) bool {
	_, _, _, ok := ParseURI(path)
	return ok
}

// New creates an ObjectStorage instance for the given Config.
func New(cfg Config) (ObjectStorage, error) {
	mu.RLock()
	fn, ok := registry[cfg.Type]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown storage type: %q (no driver registered; add blank import for the driver package)", cfg.Type)
	}
	return fn(cfg)
}
