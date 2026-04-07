//go:build nokafka

// Stub used when building without the kafka-go dependency (e.g. offline builds).
// Provides the same Kafka type so the rest of the package compiles, but all
// operations return an "not supported" error at runtime.

package brokers

import (
	"context"
	"fmt"
)

// Kafka stub — kafka-go not compiled in this build.
type Kafka struct{}

// NewKafka returns an error stub in nokafka builds.
func NewKafka(_ Config) (*Kafka, error) {
	return nil, fmt.Errorf("Kafka support not compiled in this build (nokafka tag)")
}

// Connect always returns an error in nokafka builds.
func (k *Kafka) Connect(_ context.Context) error { return fmt.Errorf("kafka not available") }

// Close is a no-op stub.
func (k *Kafka) Close() error { return nil }

// Send always returns an error in nokafka builds.
func (k *Kafka) Send(_ context.Context, _ []byte) error {
	return fmt.Errorf("kafka not available")
}

// Receive always returns an error in nokafka builds.
func (k *Kafka) Receive(_ context.Context) ([]byte, error) {
	return nil, fmt.Errorf("kafka not available")
}

// CommitLast always returns an error in nokafka builds.
func (k *Kafka) CommitLast(_ context.Context) error { return fmt.Errorf("kafka not available") }

// Ping always returns an error in nokafka builds.
func (k *Kafka) Ping(_ context.Context) error { return fmt.Errorf("kafka not available") }

// GetBrokerType returns "kafka".
func (k *Kafka) GetBrokerType() string { return "kafka" }

// SetOffset always returns an error in nokafka builds.
func (k *Kafka) SetOffset(_ int64) error { return fmt.Errorf("kafka not available") }
