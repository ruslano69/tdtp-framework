package storage

import (
	"fmt"
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
