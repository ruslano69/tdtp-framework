package brokers

import (
	"context"
	"fmt"
)

// MessageBroker представляет универсальный интерфейс для работы с очередями сообщений
// Поддерживает RabbitMQ и MSMQ
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

	// GetBrokerType возвращает тип брокера (rabbitmq, msmq)
	GetBrokerType() string
}

// Config содержит параметры подключения к message broker
type Config struct {
	Type     string // rabbitmq, msmq
	Host     string // Хост (для RabbitMQ)
	Port     int    // Порт (для RabbitMQ)
	User     string // Пользователь (для RabbitMQ)
	Password string // Пароль (для RabbitMQ)
	Queue    string // Имя очереди
	VHost    string // Virtual host (для RabbitMQ, по умолчанию "/")

	// RabbitMQ параметры очереди (ВАЖНО: должны совпадать с существующей очередью!)
	Durable    bool // Очередь переживает перезапуск RabbitMQ
	AutoDelete bool // Очередь удаляется когда нет consumer'ов
	Exclusive  bool // Очередь доступна только одному соединению

	// MSMQ специфичные параметры (Windows only)
	QueuePath string // Путь к очереди MSMQ (например: ".\\private$\\tdtp_export")
}

// New создает новый MessageBroker на основе конфигурации
func New(cfg Config) (MessageBroker, error) {
	switch cfg.Type {
	case "rabbitmq":
		return NewRabbitMQ(cfg)
	case "msmq":
		return NewMSMQ(cfg)
	default:
		return nil, fmt.Errorf("unsupported broker type: %s (supported: rabbitmq, msmq)", cfg.Type)
	}
}
