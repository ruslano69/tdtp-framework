package brokers

import (
	"context"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQ реализует MessageBroker для RabbitMQ
type RabbitMQ struct {
	config Config
	conn   *amqp.Connection
	channel *amqp.Channel
	queue  amqp.Queue
}

// NewRabbitMQ создает новый RabbitMQ брокер
func NewRabbitMQ(cfg Config) (*RabbitMQ, error) {
	if cfg.Queue == "" {
		return nil, fmt.Errorf("queue name is required for RabbitMQ")
	}
	if cfg.Host == "" {
		cfg.Host = "localhost"
	}
	if cfg.Port == 0 {
		cfg.Port = 5672
	}
	if cfg.VHost == "" {
		cfg.VHost = "/"
	}

	return &RabbitMQ{
		config: cfg,
	}, nil
}

// Connect устанавливает соединение с RabbitMQ
func (r *RabbitMQ) Connect(ctx context.Context) error {
	// Формируем connection string
	// amqp://user:password@host:port/vhost
	connStr := fmt.Sprintf("amqp://%s:%s@%s:%d%s",
		r.config.User,
		r.config.Password,
		r.config.Host,
		r.config.Port,
		r.config.VHost,
	)

	var err error
	r.conn, err = amqp.Dial(connStr)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	r.channel, err = r.conn.Channel()
	if err != nil {
		r.conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	// Объявляем очередь (создается если не существует, идемпотентная операция)
	r.queue, err = r.channel.QueueDeclare(
		r.config.Queue, // name
		true,           // durable - очередь сохраняется при перезапуске RabbitMQ
		false,          // auto-delete
		false,          // exclusive
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		r.channel.Close()
		r.conn.Close()
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	return nil
}

// Close закрывает соединение с RabbitMQ
func (r *RabbitMQ) Close() error {
	if r.channel != nil {
		if err := r.channel.Close(); err != nil {
			return fmt.Errorf("failed to close channel: %w", err)
		}
	}
	if r.conn != nil {
		if err := r.conn.Close(); err != nil {
			return fmt.Errorf("failed to close connection: %w", err)
		}
	}
	return nil
}

// Send отправляет сообщение в RabbitMQ очередь
func (r *RabbitMQ) Send(ctx context.Context, message []byte) error {
	if r.channel == nil {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	err := r.channel.PublishWithContext(
		ctx,
		"",              // exchange (пустая строка = default exchange)
		r.config.Queue,  // routing key = имя очереди
		false,           // mandatory
		false,           // immediate
		amqp.Publishing{
			ContentType:  "application/xml", // TDTP пакеты в XML формате
			Body:         message,
			DeliveryMode: amqp.Persistent, // Сообщения сохраняются на диск
			Timestamp:    time.Now(),
		},
	)

	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

// Receive получает сообщение из RabbitMQ очереди
func (r *RabbitMQ) Receive(ctx context.Context) ([]byte, error) {
	if r.channel == nil {
		return nil, fmt.Errorf("not connected to RabbitMQ")
	}

	// Создаем consumer
	msgs, err := r.channel.Consume(
		r.config.Queue, // queue
		"",             // consumer tag (автогенерируется)
		true,           // auto-ack - автоматическое подтверждение получения
		false,          // exclusive
		false,          // no-local
		false,          // no-wait
		nil,            // args
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register consumer: %w", err)
	}

	// Ждем сообщение с учетом context timeout
	select {
	case msg, ok := <-msgs:
		if !ok {
			return nil, fmt.Errorf("channel closed")
		}
		return msg.Body, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Ping проверяет доступность RabbitMQ
func (r *RabbitMQ) Ping(ctx context.Context) error {
	if r.conn == nil || r.conn.IsClosed() {
		return fmt.Errorf("not connected to RabbitMQ")
	}
	if r.channel == nil {
		return fmt.Errorf("channel not open")
	}
	return nil
}

// GetBrokerType возвращает тип брокера
func (r *RabbitMQ) GetBrokerType() string {
	return "rabbitmq"
}
