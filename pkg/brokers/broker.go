// Package brokers provides functionality for the TDTP framework.
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

	// SendBatch отправляет несколько сообщений эффективнее чем N вызовов Send.
	// Kafka: один WriteMessages roundtrip; остальные брокеры — последовательные Send.
	SendBatch(ctx context.Context, messages [][]byte) error

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
	Type          string `yaml:"type"`                      // rabbitmq, msmq, kafka
	Host          string `yaml:"host,omitempty"`            // Хост (для RabbitMQ)
	Port          int    `yaml:"port,omitempty"`            // Порт (для RabbitMQ)
	User          string `yaml:"user,omitempty"`            // Пользователь (для RabbitMQ)
	Password      string `yaml:"password,omitempty"`        // Пароль (для RabbitMQ)
	Queue         string `yaml:"queue,omitempty"`           // Имя очереди (для RabbitMQ, MSMQ)
	VHost         string `yaml:"vhost,omitempty"`           // Virtual host (для RabbitMQ, по умолчанию "/")
	UseTLS        bool   `yaml:"use_tls,omitempty"`         // Использовать TLS/SSL (amqps://) для RabbitMQ
	TLSSkipVerify bool   `yaml:"tls_skip_verify,omitempty"` // Пропустить проверку TLS-сертификата
	Exchange      string `yaml:"exchange,omitempty"`        // RabbitMQ exchange (пустая строка = default exchange)
	RoutingKey    string `yaml:"routing_key,omitempty"`     // RabbitMQ routing key

	// RabbitMQ параметры очереди (ВАЖНО: должны совпадать с существующей очередью!)
	Durable        bool `yaml:"durable,omitempty"`         // Очередь переживает перезапуск RabbitMQ
	AutoDelete     bool `yaml:"auto_delete,omitempty"`     // Очередь удаляется когда нет consumer'ов
	Exclusive      bool `yaml:"exclusive,omitempty"`       // Очередь доступна только одному соединению
	PassiveDeclare bool `yaml:"passive_declare,omitempty"` // Не создавать очередь — только проверить

	// MSMQ специфичные параметры (Windows only)
	QueuePath string `yaml:"queue_path,omitempty"` // Путь к очереди MSMQ

	// Kafka специфичные параметры
	Brokers       []string `yaml:"brokers,omitempty"`        // Список Kafka brokers
	Topic         string   `yaml:"topic,omitempty"`          // Имя Kafka topic
	ConsumerGroup string   `yaml:"consumer_group,omitempty"` // Consumer group ID
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
