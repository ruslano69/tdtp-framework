package brokers

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQ реализует MessageBroker для RabbitMQ
type RabbitMQ struct {
	config        Config
	conn          *amqp.Connection
	channel       *amqp.Channel
	queue         amqp.Queue
	lastDelivery  *amqp.Delivery // Последнее полученное сообщение (для manual ack)
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
		// Default port depends on TLS
		if cfg.UseTLS {
			cfg.Port = 5671 // amqps default
		} else {
			cfg.Port = 5672 // amqp default
		}
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
	// amqp://user:password@host:port/vhost  (без TLS)
	// amqps://user:password@host:port/vhost (с TLS)
	scheme := "amqp"
	if r.config.UseTLS {
		scheme = "amqps"
	}

	connStr := fmt.Sprintf("%s://%s:%s@%s:%d/%s",
		scheme,
		r.config.User,
		r.config.Password,
		r.config.Host,
		r.config.Port,
		r.config.VHost,
	)

	var err error
	if r.config.UseTLS {
		// Для TLS используем DialTLS с правильной конфигурацией
		tlsConfig := &tls.Config{
			ServerName: r.config.Host,
			MinVersion: tls.VersionTLS12,
		}
		r.conn, err = amqp.DialTLS(connStr, tlsConfig)
	} else {
		// Для обычного подключения используем Dial
		r.conn, err = amqp.Dial(connStr)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	r.channel, err = r.conn.Channel()
	if err != nil {
		r.conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	// Объявляем очередь (создается если не существует, идемпотентная операция)
	// ВАЖНО: Параметры должны совпадать с существующей очередью!
	r.queue, err = r.channel.QueueDeclare(
		r.config.Queue,      // name
		r.config.Durable,    // durable - очередь сохраняется при перезапуске RabbitMQ
		r.config.AutoDelete, // auto-delete
		r.config.Exclusive,  // exclusive
		false,               // no-wait
		nil,                 // arguments
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
// ВАЖНО: Сообщение НЕ удаляется из очереди автоматически!
// Нужно вызвать AckLast() после успешной обработки
func (r *RabbitMQ) Receive(ctx context.Context) ([]byte, error) {
	if r.channel == nil {
		return nil, fmt.Errorf("not connected to RabbitMQ")
	}

	// Используем Get() вместо Consume() для получения одного сообщения с manual ack
	// auto-ack = false означает что сообщение останется в очереди пока не подтвердим
	delivery, ok, err := r.channel.Get(
		r.config.Queue, // queue
		false,          // auto-ack = false - MANUAL ACK!
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	if !ok {
		// Нет сообщений в очереди - ждем немного и возвращаем ошибку timeout
		select {
		case <-time.After(1 * time.Second):
			return nil, fmt.Errorf("no messages available")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Сохраняем delivery для последующего подтверждения
	r.lastDelivery = &delivery
	return delivery.Body, nil
}

// AckLast подтверждает последнее полученное сообщение (удаляет из очереди)
// Вызывайте ТОЛЬКО после успешной обработки сообщения!
func (r *RabbitMQ) AckLast() error {
	if r.lastDelivery == nil {
		return fmt.Errorf("no message to acknowledge")
	}

	err := r.lastDelivery.Ack(false) // false = не подтверждать все предыдущие
	if err != nil {
		return fmt.Errorf("failed to acknowledge message: %w", err)
	}

	r.lastDelivery = nil // Очищаем после подтверждения
	return nil
}

// NackLast отклоняет последнее полученное сообщение (возвращает в очередь)
// Используйте если обработка не удалась и хотите попробовать позже
func (r *RabbitMQ) NackLast(requeue bool) error {
	if r.lastDelivery == nil {
		return fmt.Errorf("no message to reject")
	}

	err := r.lastDelivery.Nack(false, requeue) // false = только это сообщение, requeue = вернуть в очередь
	if err != nil {
		return fmt.Errorf("failed to reject message: %w", err)
	}

	r.lastDelivery = nil
	return nil
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
