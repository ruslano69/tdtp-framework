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
	lastDelivery  *amqp.Delivery     // Последнее полученное сообщение (для manual ack)
	deliveryChan  <-chan amqp.Delivery // Канал для блокирующего получения сообщений
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
	// Сначала пробуем пассивно проверить существование очереди
	r.queue, err = r.channel.QueueDeclarePassive(
		r.config.Queue,      // name
		r.config.Durable,    // durable
		r.config.AutoDelete, // auto-delete
		r.config.Exclusive,  // exclusive
		false,               // no-wait
		nil,                 // arguments
	)

	if err != nil {
		// Если очередь не существует, создаем её
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
	}

	// Начинаем потребление сообщений (блокирующий режим)
	r.deliveryChan, err = r.channel.Consume(
		r.config.Queue, // queue
		"",             // consumer tag (пустая строка = auto-generated)
		false,          // auto-ack = false - MANUAL ACK!
		false,          // exclusive
		false,          // no-local
		false,          // no-wait
		nil,            // args
	)
	if err != nil {
		r.channel.Close()
		r.conn.Close()
		return fmt.Errorf("failed to start consuming: %w", err)
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

	// Определяем exchange (default = "")
	exchange := r.config.Exchange

	// Определяем routing key (default = имя очереди)
	routingKey := r.config.RoutingKey
	if routingKey == "" {
		routingKey = r.config.Queue
	}

	err := r.channel.PublishWithContext(
		ctx,
		exchange,    // exchange (пустая строка = default exchange)
		routingKey,  // routing key
		false,       // mandatory
		false,       // immediate
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
	if r.deliveryChan == nil {
		return nil, fmt.Errorf("not connected to RabbitMQ (deliveryChan is nil)")
	}

	// Блокирующее получение сообщения из канала
	// Ждем пока не придет сообщение или не отменят контекст
	select {
	case delivery, ok := <-r.deliveryChan:
		if !ok {
			// Канал закрыт (соединение разорвано)
			return nil, fmt.Errorf("delivery channel closed (connection lost)")
		}

		// Сохраняем delivery для последующего подтверждения
		r.lastDelivery = &delivery
		return delivery.Body, nil

	case <-ctx.Done():
		// Контекст отменен
		return nil, ctx.Err()
	}
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
