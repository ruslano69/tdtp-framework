package brokers

import (
	"context"
	"fmt"
)

// MessageBroker представляет универсальный интерфейс для работы с очередями сообщений
// Поддерживает RabbitMQ, MSMQ и Apache Kafka
type MessageBroker interface {
	// Connect устанавливает соединение с брокером
	Connect(ctx context.Context) error

	// Close закрывает соединение с брокером
	Close() error

	// Send отправляет сообщение в очередь
	// message - тело сообщения (обычно XML содержимое TDTP пакета)
	Send(ctx context.Context, message []byte) error

	// Receive получает сообщение из очереди
	// Блокирующий вызов - ждет пока не придет сообщение или не истечет timeout
	Receive(ctx context.Context) ([]byte, error)

	// Ping проверяет доступность брокера
	Ping(ctx context.Context) error

	// GetBrokerType возвращает тип брокера (rabbitmq, msmq, kafka)
	GetBrokerType() string
}

// Config содержит параметры подключения к message broker
type Config struct {
	Type       string // rabbitmq, msmq, kafka
	Host       string // Хост (для RabbitMQ)
	Port       int    // Порт (для RabbitMQ)
	User       string // Пользователь (для RabbitMQ)
	Password   string // Пароль (для RabbitMQ)
	Queue      string // Имя очереди (для RabbitMQ, MSMQ)
	VHost      string // Virtual host (для RabbitMQ, по умолчанию "/")
	UseTLS     bool   // Использовать TLS/SSL (amqps://) для RabbitMQ
	Exchange   string // RabbitMQ exchange (пустая строка = default exchange)
	RoutingKey string // RabbitMQ routing key (если пустой, используется имя очереди)

	// RabbitMQ параметры очереди (ВАЖНО: должны совпадать с существующей очередью!)
	Durable    bool // Очередь переживает перезапуск RabbitMQ
	AutoDelete bool // Очередь удаляется когда нет consumer'ов
	Exclusive  bool // Очередь доступна только одному соединению

	// MSMQ специфичные параметры (Windows only)
	QueuePath string // Путь к очереди MSMQ (например: ".\\private$\\tdtp_export")

	// Kafka специфичные параметры
	Brokers       []string // Список Kafka brokers (например: ["localhost:9092", "localhost:9093"])
	Topic         string   // Имя Kafka topic
	ConsumerGroup string   // Consumer group ID (по умолчанию "tdtp-consumer-group")
}

// New создает новый MessageBroker на основе конфигурации
func New(cfg Config) (MessageBroker, error) {
	switch cfg.Type {
	case "rabbitmq":
		return NewRabbitMQ(cfg)
	case "msmq":
		return NewMSMQ(cfg)
	case "kafka":
		return NewKafka(cfg)
	default:
		return nil, fmt.Errorf("unsupported broker type: %s (supported: rabbitmq, msmq, kafka)", cfg.Type)
	}
}
