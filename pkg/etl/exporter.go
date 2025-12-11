package etl

import (
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework-main/pkg/brokers"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/processors"
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
	if e.config.TDTP == nil {
		return fmt.Errorf("TDTP config is not set")
	}

	destination := e.config.TDTP.Destination
	if destination == "" {
		return fmt.Errorf("TDTP destination is not set")
	}

	// Применяем сжатие если настроено
	if e.config.TDTP.Compression {
		if err := e.compressDataPacket(dataPacket); err != nil {
			return fmt.Errorf("failed to compress data: %w", err)
		}
	}

	// Создаем генератор пакетов
	generator := packet.NewGenerator()

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
	if e.config.RabbitMQ == nil {
		return fmt.Errorf("RabbitMQ config is not set")
	}

	cfg := e.config.RabbitMQ

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
	if e.config.Kafka == nil {
		return fmt.Errorf("Kafka config is not set")
	}

	cfg := e.config.Kafka

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
		if e.config.TDTP != nil {
			return e.config.TDTP.Destination
		}
	case "RabbitMQ":
		if e.config.RabbitMQ != nil {
			return fmt.Sprintf("%s:%d/%s",
				e.config.RabbitMQ.Host,
				e.config.RabbitMQ.Port,
				e.config.RabbitMQ.Queue)
		}
	case "Kafka":
		if e.config.Kafka != nil {
			return fmt.Sprintf("%s/%s",
				e.config.Kafka.Brokers,
				e.config.Kafka.Topic)
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
		if e.config.TDTP == nil {
			return fmt.Errorf("TDTP config is required for TDTP output")
		}
		if e.config.TDTP.Destination == "" {
			return fmt.Errorf("TDTP destination is required")
		}

	case "RabbitMQ":
		if e.config.RabbitMQ == nil {
			return fmt.Errorf("RabbitMQ config is required for RabbitMQ output")
		}
		if e.config.RabbitMQ.Host == "" {
			return fmt.Errorf("RabbitMQ host is required")
		}
		if e.config.RabbitMQ.Queue == "" {
			return fmt.Errorf("RabbitMQ queue is required")
		}

	case "Kafka":
		if e.config.Kafka == nil {
			return fmt.Errorf("Kafka config is required for Kafka output")
		}
		if len(e.config.Kafka.Brokers) == 0 {
			return fmt.Errorf("Kafka brokers is required")
		}
		if e.config.Kafka.Topic == "" {
			return fmt.Errorf("Kafka topic is required")
		}

	default:
		return fmt.Errorf("unsupported output type: %s", e.config.Type)
	}

	return nil
}

// compressDataPacket сжимает данные в DataPacket используя zstd
func (e *Exporter) compressDataPacket(dataPacket *packet.DataPacket) error {
	if len(dataPacket.Data.Rows) == 0 {
		return nil // Нечего сжимать
	}

	// Проверяем минимальный размер для сжатия (1KB)
	totalSize := 0
	rowStrings := make([]string, len(dataPacket.Data.Rows))
	for i, row := range dataPacket.Data.Rows {
		rowStrings[i] = row.Value
		totalSize += len(row.Value)
	}

	// Если данные слишком маленькие, сжатие не выгодно
	if totalSize < 1024 {
		return nil
	}

	// Сжимаем данные используя CompressDataForTdtp
	compressedData, stats, err := processors.CompressDataForTdtp(rowStrings, 3) // Level 3 - balanced
	if err != nil {
		return fmt.Errorf("compression failed: %w", err)
	}

	// Проверяем выгоду от сжатия (должно уменьшиться хотя бы на 10%)
	if stats.CompressedSize >= int(float64(stats.OriginalSize)*0.9) {
		// Сжатие не дало значительной выгоды, оставляем без сжатия
		return nil
	}

	// Обновляем DataPacket сжатыми данными
	dataPacket.Data = packet.Data{
		Compression: "zstd",
		Rows: []packet.Row{
			{Value: compressedData},
		},
	}

	return nil
}
