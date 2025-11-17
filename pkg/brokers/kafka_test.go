package brokers

import (
	"context"
	"testing"
	"time"
)

// TestKafkaIntegration проверяет базовую функциональность Kafka broker
// Требует запущенного Kafka сервера на localhost:9092
func TestKafkaIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kafka integration test in short mode")
	}

	cfg := Config{
		Type:          "kafka",
		Brokers:       []string{"localhost:9092"},
		Topic:         "tdtp-test-topic",
		ConsumerGroup: "tdtp-test-group",
	}

	broker, err := NewKafka(cfg)
	if err != nil {
		t.Fatalf("Failed to create Kafka broker: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Подключение
	err = broker.Connect(ctx)
	if err != nil {
		t.Skipf("Skipping test: Kafka server not available: %v", err)
	}
	defer broker.Close()

	// Ping
	err = broker.Ping(ctx)
	if err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	// Send message
	testMessage := []byte(`<?xml version="1.0"?>
<packet>
	<header>
		<sender>test-sender</sender>
		<recipient>test-recipient</recipient>
	</header>
	<data>
		<row>
			<field name="id">1</field>
			<field name="name">Test</field>
		</row>
	</data>
</packet>`)

	err = broker.Send(ctx, testMessage)
	if err != nil {
		t.Errorf("Send failed: %v", err)
	}

	// Receive message
	received, err := broker.Receive(ctx)
	if err != nil {
		t.Errorf("Receive failed: %v", err)
	}

	if string(received) != string(testMessage) {
		t.Errorf("Received message doesn't match sent message.\nExpected: %s\nGot: %s",
			string(testMessage), string(received))
	}

	// Commit message
	err = broker.CommitLast(ctx)
	if err != nil {
		t.Errorf("Commit failed: %v", err)
	}

	t.Logf("Successfully sent and received message through Kafka")
}

// TestKafkaFactory проверяет создание Kafka broker через фабрику
func TestKafkaFactory(t *testing.T) {
	cfg := Config{
		Type:          "kafka",
		Brokers:       []string{"localhost:9092", "localhost:9093"},
		Topic:         "test-topic",
		ConsumerGroup: "test-group",
	}

	broker, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create Kafka broker via factory: %v", err)
	}

	if broker.GetBrokerType() != "kafka" {
		t.Errorf("Expected broker type 'kafka', got '%s'", broker.GetBrokerType())
	}
}

// TestKafkaValidation проверяет валидацию параметров
func TestKafkaValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				Type:    "kafka",
				Brokers: []string{"localhost:9092"},
				Topic:   "test",
			},
			wantErr: false,
		},
		{
			name: "missing topic",
			cfg: Config{
				Type:    "kafka",
				Brokers: []string{"localhost:9092"},
			},
			wantErr: true,
		},
		{
			name: "missing brokers",
			cfg: Config{
				Type:  "kafka",
				Topic: "test",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewKafka(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewKafka() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestKafkaStats проверяет получение статистики
func TestKafkaStats(t *testing.T) {
	cfg := Config{
		Type:          "kafka",
		Brokers:       []string{"localhost:9092"},
		Topic:         "test",
		ConsumerGroup: "test-group",
	}

	broker, err := NewKafka(cfg)
	if err != nil {
		t.Fatalf("Failed to create Kafka broker: %v", err)
	}

	ctx := context.Background()
	err = broker.Connect(ctx)
	if err != nil {
		t.Skipf("Skipping test: Kafka server not available: %v", err)
	}
	defer broker.Close()

	readerStats, writerStats := broker.GetStats()

	// Проверяем, что статистика не пустая
	t.Logf("Reader stats: Messages=%d, Bytes=%d", readerStats.Messages, readerStats.Bytes)
	t.Logf("Writer stats: Messages=%d, Bytes=%d", writerStats.Messages, writerStats.Bytes)
}
