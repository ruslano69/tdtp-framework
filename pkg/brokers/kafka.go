//go:build !nokafka

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

	// limits — предел размера сообщения, полученный у брокера (kafka_limits.go).
	// Спрашивается один раз на соединение.
	limits limitCache
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

// Connect устанавливает соединение с Kafka.
// Reader создаётся лениво при первом вызове Receive() —
// это убирает ~3-секундный блок в Close() когда брокер используется только для записи.
func (k *Kafka) Connect(ctx context.Context) error {
	k.ConnectLazy()

	// Проверяем подключение без создания Reader
	return k.Ping(ctx)
}

// ConnectLazy готовит writer, ничего не отправляя и никуда не подключаясь.
//
// kafka.Writer открывает соединение при первой записи, и для отправителя,
// которому нечего делать до появления данных, это единственно верное
// поведение: спул может быть создан заранее, а брокер подняться позже.
// Connect с его Ping остаётся для случаев, где доступность надо знать сразу.
//
// Разница не косметическая: spool раньше создавал writer напрямую и был
// ленивым. Перевод его на Connect сделал конструктор сетевым, и первый же
// прогон это показал — Ping резолвит localhost в ::1 и падает там, где
// отложенная запись прошла бы по IPv4.
func (k *Kafka) ConnectLazy() {
	k.writer = NewKafkaWriter(k.config.Brokers, k.config.Topic)
}

// NewKafkaWriter — единственное место, где настраивается kafka.Writer.
//
// Раньше эта конфигурация существовала в двух экземплярах: здесь и в
// pkg/etl/kafka_spool.go. Десять полей, совпадавших значение в значение —
// не два writer'а под разные задачи, а копия. Spool завёл свой, чтобы не
// создавать Reader (его Close() блокирует на секунды); позже Reader здесь
// сделали ленивым — ensureReader, и Close() проверяет reader != nil, —
// то есть причина отпала, а копия осталась.
//
// Цена копии выяснилась предметно: предупреждение о превышении
// message.max.bytes, добавленное в Send/SendBatch, не действовало на
// spool-путь — то есть на основной путь выгрузки в Kafka.
func NewKafkaWriter(brokerAddrs []string, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:         kafka.TCP(brokerAddrs...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
		Async:        false,
		Compression:  kafka.Snappy,
		MaxAttempts:  3,
		WriteTimeout: 30 * time.Second,

		// Предел того, что kafka-go соберёт в один запрос. К брокеру
		// отношения не имеет: тот меряет сжатый record batch по своему
		// message.max.bytes и отвергает независимо от этого числа. Высокое
		// значение здесь лишь разрешает собрать крупный запрос — примет его
		// брокер или нет, решают kafka_limits.go и kafka_diag.go.
		BatchBytes:   100 * 1024 * 1024,
		BatchTimeout: 5 * time.Millisecond, // Не ждать накопления — отправлять сразу
	}
}

// ensureReader создаёт Reader при первом обращении к Receive().
func (k *Kafka) ensureReader() {
	if k.reader != nil {
		return
	}
	k.reader = kafka.NewReader(kafka.ReaderConfig{
		Brokers:           k.config.Brokers,
		GroupID:           k.config.ConsumerGroup,
		Topic:             k.config.Topic,
		MinBytes:          1,
		MaxBytes:          10e6,
		CommitInterval:    0,                 // Manual commit
		StartOffset:       kafka.FirstOffset, // earliest
		MaxWait:           1 * time.Second,
		ReadBackoffMin:    100 * time.Millisecond,
		ReadBackoffMax:    1 * time.Second,
		HeartbeatInterval: 200 * time.Millisecond, // Close() ждёт не более одного интервала
		SessionTimeout:    6 * time.Second,        // брокер считает консьюмера мёртвым через 6с
	})
}

// Close закрывает соединение с Kafka.
// reader.Close() может блокировать до MaxWait (fetch long-poll), поэтому
// закрываем его в горутине с таймаутом 400 мс — этого достаточно для
// нормального LeaveGroup + выхода heartbeat-цикла (HeartbeatInterval=200ms).
func (k *Kafka) Close() error {
	var errs []error

	if k.writer != nil {
		if err := k.writer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close writer: %w", err))
		}
	}

	if k.reader != nil {
		done := make(chan error, 1)
		go func() { done <- k.reader.Close() }()
		select {
		case err := <-done:
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to close reader: %w", err))
			}
		case <-time.After(400 * time.Millisecond):
			// reader.Close() завис в long-poll — игнорируем, сессия истечёт на брокере
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
		Key:   []byte(fmt.Sprintf("tdtp-%d", time.Now().UnixNano())),
		Value: message,
		Time:  time.Now(),
		Headers: []kafka.Header{
			{Key: "content-type", Value: []byte("application/xml")},
			{Key: "protocol", Value: []byte("tdtp")},
		},
	}

	k.warnIfOversized(ctx, [][]byte{message})

	if err := k.writer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("failed to write message to Kafka: %w", err)
	}
	return nil
}

// ContentTypeXML — обычный TDTP-пакет.
// ContentTypeXMLZstd — пакет, сжатый перед отправкой (spool-путь).
const (
	ContentTypeXML     = "application/xml"
	ContentTypeXMLZstd = "application/xml+zstd"
)

// SendBatch отправляет все сообщения одним вызовом WriteMessages —
// все пакеты уходят в одном roundtrip вместо N последовательных.
func (k *Kafka) SendBatch(ctx context.Context, messages [][]byte) error {
	return k.SendBatchAs(ctx, messages, ContentTypeXML)
}

// SendBatchAs — то же самое с явным content-type.
//
// Существует потому, что spool отправляет уже сжатые пакеты и помечает их
// application/xml+zstd: потребитель по этому заголовку понимает, надо ли
// распаковывать. При переводе spool на этот writer заголовок нельзя было
// потерять — иначе сжатые пакеты уехали бы под видом обычных.
func (k *Kafka) SendBatchAs(ctx context.Context, messages [][]byte, contentType string) error {
	if k.writer == nil {
		return fmt.Errorf("not connected to Kafka")
	}

	now := time.Now()
	msgs := make([]kafka.Message, len(messages))
	for i, m := range messages {
		msgs[i] = kafka.Message{
			Key:   []byte(fmt.Sprintf("tdtp-%d-%d", now.UnixNano(), i)),
			Value: m,
			Time:  now,
			Headers: []kafka.Header{
				{Key: "content-type", Value: []byte(contentType)},
				{Key: "protocol", Value: []byte("tdtp")},
			},
		}
	}

	k.warnIfOversized(ctx, messages)

	if err := k.writer.WriteMessages(ctx, msgs...); err != nil {
		return classifyWriteError(err, messages, k.limits.value)
	}
	return nil
}

// Receive получает сообщение из Kafka topic.
// ВАЖНО: offset НЕ коммитится автоматически!
// Нужно вызвать CommitLast() после успешной обработки.
func (k *Kafka) Receive(ctx context.Context) ([]byte, error) {
	k.ensureReader()

	msg, err := k.reader.FetchMessage(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch message: %w", err)
	}

	// Сохраняем сообщение для последующего commit
	k.lastMessage = &msg
	return msg.Value, nil
}

// CommitLast подтверждает последнее полученное сообщение (commit offset).
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
	defer func() { _ = conn.Close() }()

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
