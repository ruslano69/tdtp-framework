package brokers

import (
	"context"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
)

// Kafka реализует MessageBroker для Apache Kafka
type Kafka struct {
	config      Config
	writer      *kafka.Writer
	reader      *kafka.Reader
	lastMessage *kafka.Message // Последнее полученное сообщение (для manual commit)
}

// NewKafka создает новый Kafka брокер
func NewKafka(cfg Config) (*Kafka, error) {
	if cfg.Topic == "" {
		return nil, fmt.Errorf("topic name is required for Kafka")
	}
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("at least one broker address is required for Kafka")
	}
	if cfg.ConsumerGroup == "" {
		cfg.ConsumerGroup = "tdtp-consumer-group"
	}

	return &Kafka{
		config: cfg,
	}, nil
}

// Connect устанавливает соединение с Kafka
func (k *Kafka) Connect(ctx context.Context) error {
	// Создаем Writer для отправки сообщений
	k.writer = &kafka.Writer{
		Addr:         kafka.TCP(k.config.Brokers...),
		Topic:        k.config.Topic,
		Balancer:     &kafka.LeastBytes{}, // Балансировка по наименьшей загруженности
		RequiredAcks: kafka.RequireAll,    // Ждем подтверждения от всех реплик
		Async:        false,               // Синхронная отправка для надежности
		Compression:  kafka.Snappy,        // Сжатие данных
		MaxAttempts:  3,                   // Повторные попытки
		WriteTimeout: 10 * time.Second,
	}

	// Создаем Reader для получения сообщений
	k.reader = kafka.NewReader(kafka.ReaderConfig{
		Brokers:        k.config.Brokers,
		GroupID:        k.config.ConsumerGroup,
		Topic:          k.config.Topic,
		MinBytes:       1,                // Минимальный размер batch
		MaxBytes:       10e6,             // 10MB максимальный размер
		CommitInterval: 0,                // Manual commit
		StartOffset:    kafka.LastOffset, // Начинаем с последнего offset (новые сообщения)
		MaxWait:        1 * time.Second,  // Максимальное время ожидания
		ReadBackoffMin: 100 * time.Millisecond,
		ReadBackoffMax: 1 * time.Second,
	})

	// Проверяем подключение
	return k.Ping(ctx)
}

// Close закрывает соединение с Kafka
func (k *Kafka) Close() error {
	var errs []error

	if k.writer != nil {
		if err := k.writer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close writer: %w", err))
		}
	}

	if k.reader != nil {
		if err := k.reader.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close reader: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during close: %v", errs)
	}

	return nil
}

// Send отправляет сообщение в Kafka topic
func (k *Kafka) Send(ctx context.Context, message []byte) error {
	if k.writer == nil {
		return fmt.Errorf("not connected to Kafka")
	}

	msg := kafka.Message{
		Key:   []byte(fmt.Sprintf("tdtp-%d", time.Now().UnixNano())), // Уникальный ключ
		Value: message,
		Time:  time.Now(),
		Headers: []kafka.Header{
			{Key: "content-type", Value: []byte("application/xml")}, // TDTP пакеты в XML
			{Key: "protocol", Value: []byte("tdtp")},
		},
	}

	err := k.writer.WriteMessages(ctx, msg)
	if err != nil {
		return fmt.Errorf("failed to write message to Kafka: %w", err)
	}

	return nil
}

// Receive получает сообщение из Kafka topic
// ВАЖНО: offset НЕ коммитится автоматически!
// Нужно вызвать CommitLast() после успешной обработки
func (k *Kafka) Receive(ctx context.Context) ([]byte, error) {
	if k.reader == nil {
		return nil, fmt.Errorf("not connected to Kafka")
	}

	msg, err := k.reader.FetchMessage(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch message: %w", err)
	}

	// Сохраняем сообщение для последующего commit
	k.lastMessage = &msg
	return msg.Value, nil
}

// CommitLast подтверждает последнее полученное сообщение (commit offset)
// Вызывайте ТОЛЬКО после успешной обработки сообщения!
func (k *Kafka) CommitLast(ctx context.Context) error {
	if k.lastMessage == nil {
		return fmt.Errorf("no message to commit")
	}

	err := k.reader.CommitMessages(ctx, *k.lastMessage)
	if err != nil {
		return fmt.Errorf("failed to commit message: %w", err)
	}

	k.lastMessage = nil // Очищаем после commit
	return nil
}

// Ping проверяет доступность Kafka
func (k *Kafka) Ping(ctx context.Context) error {
	// Создаем временный connection для проверки доступности
	conn, err := kafka.DialContext(ctx, "tcp", k.config.Brokers[0])
	if err != nil {
		return fmt.Errorf("failed to dial Kafka broker: %w", err)
	}
	defer conn.Close()

	// Проверяем, что можем получить метаданные
	_, err = conn.ReadPartitions(k.config.Topic)
	if err != nil {
		return fmt.Errorf("failed to read topic partitions: %w", err)
	}

	return nil
}

// GetBrokerType возвращает тип брокера
func (k *Kafka) GetBrokerType() string {
	return "kafka"
}

// GetStats возвращает статистику Kafka reader/writer
func (k *Kafka) GetStats() (readerStats kafka.ReaderStats, writerStats kafka.WriterStats) {
	if k.reader != nil {
		readerStats = k.reader.Stats()
	}
	if k.writer != nil {
		writerStats = k.writer.Stats()
	}
	return
}

// SetOffset устанавливает offset для чтения (полезно для replay)
// offset: kafka.FirstOffset, kafka.LastOffset, или конкретное значение
func (k *Kafka) SetOffset(offset int64) error {
	if k.reader == nil {
		return fmt.Errorf("not connected to Kafka")
	}
	return k.reader.SetOffset(offset)
}
