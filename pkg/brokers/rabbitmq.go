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
	config       Config
	conn         *amqp.Connection
	channel      *amqp.Channel
	queue        amqp.Queue
	lastDelivery *amqp.Delivery       // Последнее полученное сообщение (для manual ack)
	deliveryChan <-chan amqp.Delivery // Канал для блокирующего получения сообщений
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
	// Reset consumer state so startConsuming() re-registers after reconnect.
	// Without this, deliveryChan stays non-nil (pointing to the closed channel
	// of the dead connection), startConsuming() returns early, and every
	// subsequent Receive() reads from a closed channel — infinite error loop.
	r.deliveryChan = nil
	r.lastDelivery = nil

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

	// DialConfig with explicit 10s heartbeat: amqp.Dial() inherits the broker
	// default (~60s), meaning a network partition stays silent for up to 60s
	// before Receive() surfaces an error. 10s matches production bridge behaviour.
	dialCfg := amqp.Config{
		Heartbeat: 10 * time.Second,
		Locale:    "en_US",
	}
	if r.config.UseTLS {
		dialCfg.TLSClientConfig = &tls.Config{
			ServerName:         r.config.Host,
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: r.config.TLSSkipVerify, //nolint:gosec // controlled by config
		}
	}

	var err error
	r.conn, err = amqp.DialConfig(connStr, dialCfg)

	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	r.channel, err = r.conn.Channel()
	if err != nil {
		_ = r.conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	// Объявляем очередь или проверяем существующую
	if r.config.PassiveDeclare {
		// PassiveDeclare: только проверить что очередь существует, не менять её параметры.
		// Используется когда очередь создана сторонним сервисом с другими параметрами.
		// Если очередь не существует — вернёт ошибку 404.
		r.queue, err = r.channel.QueueDeclarePassive(r.config.Queue, false, false, false, false, nil)
		if err != nil {
			_ = r.channel.Close()
			_ = r.conn.Close()
			return fmt.Errorf("queue '%s' not found (passive_declare=true): %w", r.config.Queue, err)
		}
	} else {
		// Обычный declare: создать если не существует.
		// ВАЖНО: параметры durable/auto_delete/exclusive должны совпадать с существующей очередью,
		// иначе RabbitMQ вернёт 406 PRECONDITION_FAILED.
		r.queue, err = r.channel.QueueDeclare(
			r.config.Queue,      // name
			r.config.Durable,    // durable
			r.config.AutoDelete, // auto-delete
			r.config.Exclusive,  // exclusive
			false,               // no-wait
			nil,                 // arguments
		)
		if err != nil {
			_ = r.channel.Close()
			_ = r.conn.Close()
			return fmt.Errorf("failed to declare queue '%s': %w", r.config.Queue, err)
		}
	}

	return nil
}

// startConsuming регистрирует consumer на канале — вызывается лениво при первом Receive.
// Разделение Connect и Consume необходимо: если вызвать Consume при отправке,
// RabbitMQ начинает пушить unacked deliveries обратно; никто их не читает,
// TCP-буфер забивается и следующий PublishWithContext блокируется навсегда.
func (r *RabbitMQ) startConsuming() error {
	if r.deliveryChan != nil {
		return nil // уже зарегистрирован
	}

	// prefetch=1: broker delivers one message at a time and waits for ACK before
	// the next. Prevents unbounded in-memory buffering when upserts are slow and
	// limits unacknowledged exposure to a single message on crash.
	// For --workers N mode (Sprint 9), set prefetch to N.
	if err := r.channel.Qos(1, 0, false); err != nil {
		return fmt.Errorf("failed to set QoS prefetch: %w", err)
	}

	var err error
	r.deliveryChan, err = r.channel.Consume(
		r.config.Queue, // queue
		"",             // consumer tag (auto-generated)
		false,          // auto-ack = false — MANUAL ACK
		false,          // exclusive
		false,          // no-local
		false,          // no-wait
		nil,            // args
	)
	if err != nil {
		return fmt.Errorf("failed to start consuming: %w", err)
	}
	return nil
}

// Close закрывает соединение с RabbitMQ
func (r *RabbitMQ) Close() error {
	// Nil out the delivery channel first so Receive() unblocks and returns
	// immediately on the next select iteration rather than blocking on the
	// dying AMQP delivery channel until the library closes it.
	r.deliveryChan = nil
	r.lastDelivery = nil

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
		exchange,   // exchange (пустая строка = default exchange)
		routingKey, // routing key
		false,      // mandatory
		false,      // immediate
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

// SendBatch отправляет несколько сообщений последовательно.
// RabbitMQ не имеет нативного batch API, поэтому это N вызовов Send.
func (r *RabbitMQ) SendBatch(ctx context.Context, messages [][]byte) error {
	for i, msg := range messages {
		if err := r.Send(ctx, msg); err != nil {
			return fmt.Errorf("SendBatch: message %d/%d: %w", i+1, len(messages), err)
		}
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
	if err := r.startConsuming(); err != nil {
		return nil, err
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
