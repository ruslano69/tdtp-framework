package etl

import (
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework-main/pkg/brokers"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// ExportResult представляет результат экспорта
type ExportResult struct {
	OutputType   string
	Destination  string
	RowsExported int
	Error        error
}

// Exporter отвечает за экспорт результатов ETL
type Exporter struct {
	config OutputConfig
}

// NewExporter создает новый экспортер
func NewExporter(config OutputConfig) *Exporter {
	return &Exporter{
		config: config,
	}
}

// Export экспортирует DataPacket в сконфигурированный выход
func (e *Exporter) Export(ctx context.Context, dataPacket *packet.DataPacket) (*ExportResult, error) {
	if dataPacket == nil {
		return nil, fmt.Errorf("data packet is nil")
	}

	result := &ExportResult{
		OutputType:   e.config.Type,
		Destination:  e.getDestination(),
		RowsExported: len(dataPacket.Data.Rows),
	}

	switch e.config.Type {
	case "TDTP":
		err := e.exportToTDTP(ctx, dataPacket)
		result.Error = err
		return result, err

	case "RabbitMQ":
		err := e.exportToRabbitMQ(ctx, dataPacket)
		result.Error = err
		return result, err

	case "Kafka":
		err := e.exportToKafka(ctx, dataPacket)
		result.Error = err
		return result, err

	default:
		err := fmt.Errorf("unsupported output type: %s", e.config.Type)
		result.Error = err
		return result, err
	}
}

// exportToTDTP экспортирует в TDTP XML файл
func (e *Exporter) exportToTDTP(ctx context.Context, dataPacket *packet.DataPacket) error {
	if e.config.TDTPConfig == nil {
		return fmt.Errorf("TDTP config is not set")
	}

	destination := e.config.TDTPConfig.Destination
	if destination == "" {
		return fmt.Errorf("TDTP destination is not set")
	}

	// Создаем генератор пакетов
	generator := packet.NewGenerator()

	// Применяем сжатие если настроено
	if e.config.TDTPConfig.Compress {
		// TODO: Реализовать сжатие через CompressionProcessor
		// Пока оставляем без сжатия
	}

	// Генерируем XML
	xmlData, err := generator.ToXML(dataPacket, true) // pretty = true
	if err != nil {
		return fmt.Errorf("failed to generate XML: %w", err)
	}

	// Записываем в файл
	if err := os.WriteFile(destination, xmlData, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// exportToRabbitMQ экспортирует в RabbitMQ
func (e *Exporter) exportToRabbitMQ(ctx context.Context, dataPacket *packet.DataPacket) error {
	if e.config.RabbitMQConfig == nil {
		return fmt.Errorf("RabbitMQ config is not set")
	}

	cfg := e.config.RabbitMQConfig

	// Создаем broker
	broker, err := brokers.New(brokers.Config{
		Type:     "rabbitmq",
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.User,
		Password: cfg.Password,
		Queue:    cfg.Queue,
		Durable:  true, // Очередь переживает перезапуск
	})
	if err != nil {
		return fmt.Errorf("failed to create RabbitMQ broker: %w", err)
	}

	// Подключаемся
	if err := broker.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}
	defer broker.Close()

	// Генерируем XML из пакета
	generator := packet.NewGenerator()
	xmlData, err := generator.ToXML(dataPacket, false) // compact XML
	if err != nil {
		return fmt.Errorf("failed to generate XML: %w", err)
	}

	// Отправляем в RabbitMQ
	if err := broker.Send(ctx, xmlData); err != nil {
		return fmt.Errorf("failed to send to RabbitMQ: %w", err)
	}

	return nil
}

// exportToKafka экспортирует в Kafka
func (e *Exporter) exportToKafka(ctx context.Context, dataPacket *packet.DataPacket) error {
	if e.config.KafkaConfig == nil {
		return fmt.Errorf("Kafka config is not set")
	}

	cfg := e.config.KafkaConfig

	// Создаем broker
	broker, err := brokers.New(brokers.Config{
		Type:    "kafka",
		Brokers: cfg.Brokers,
		Topic:   cfg.Topic,
	})
	if err != nil {
		return fmt.Errorf("failed to create Kafka broker: %w", err)
	}

	// Подключаемся
	if err := broker.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to Kafka: %w", err)
	}
	defer broker.Close()

	// Генерируем XML из пакета
	generator := packet.NewGenerator()
	xmlData, err := generator.ToXML(dataPacket, false) // compact XML
	if err != nil {
		return fmt.Errorf("failed to generate XML: %w", err)
	}

	// Отправляем в Kafka
	if err := broker.Send(ctx, xmlData); err != nil {
		return fmt.Errorf("failed to send to Kafka: %w", err)
	}

	return nil
}

// getDestination возвращает назначение экспорта в виде строки
func (e *Exporter) getDestination() string {
	switch e.config.Type {
	case "TDTP":
		if e.config.TDTPConfig != nil {
			return e.config.TDTPConfig.Destination
		}
	case "RabbitMQ":
		if e.config.RabbitMQConfig != nil {
			return fmt.Sprintf("%s:%d/%s",
				e.config.RabbitMQConfig.Host,
				e.config.RabbitMQConfig.Port,
				e.config.RabbitMQConfig.Queue)
		}
	case "Kafka":
		if e.config.KafkaConfig != nil {
			return fmt.Sprintf("%s/%s",
				e.config.KafkaConfig.Brokers,
				e.config.KafkaConfig.Topic)
		}
	}
	return "unknown"
}

// ValidateConfig проверяет конфигурацию экспортера
func (e *Exporter) ValidateConfig() error {
	if e.config.Type == "" {
		return fmt.Errorf("output type is not set")
	}

	switch e.config.Type {
	case "TDTP":
		if e.config.TDTPConfig == nil {
			return fmt.Errorf("TDTP config is required for TDTP output")
		}
		if e.config.TDTPConfig.Destination == "" {
			return fmt.Errorf("TDTP destination is required")
		}

	case "RabbitMQ":
		if e.config.RabbitMQConfig == nil {
			return fmt.Errorf("RabbitMQ config is required for RabbitMQ output")
		}
		if e.config.RabbitMQConfig.Host == "" {
			return fmt.Errorf("RabbitMQ host is required")
		}
		if e.config.RabbitMQConfig.Queue == "" {
			return fmt.Errorf("RabbitMQ queue is required")
		}

	case "Kafka":
		if e.config.KafkaConfig == nil {
			return fmt.Errorf("Kafka config is required for Kafka output")
		}
		if e.config.KafkaConfig.Brokers == "" {
			return fmt.Errorf("Kafka brokers is required")
		}
		if e.config.KafkaConfig.Topic == "" {
			return fmt.Errorf("Kafka topic is required")
		}

	default:
		return fmt.Errorf("unsupported output type: %s", e.config.Type)
	}

	return nil
}
