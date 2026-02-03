package etl

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ruslano69/tdtp-framework-main/pkg/brokers"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// ImporterConfig содержит конфигурацию импортера
type ImporterConfig struct {
	Type      string              // "RabbitMQ" или "Kafka"
	RabbitMQ  *RabbitMQInputConfig
	Kafka     *KafkaInputConfig
	Workers   int                 // Количество параллельных воркеров для обработки частей
}

// RabbitMQInputConfig конфигурация для чтения из RabbitMQ
type RabbitMQInputConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Queue    string
}

// KafkaInputConfig конфигурация для чтения из Kafka
type KafkaInputConfig struct {
	Brokers []string
	Topic   string
	GroupID string
}

// ImportResult представляет результат импорта одной части
type ImportResult struct {
	PartNumber int
	TotalParts int           // Из Header.TotalParts
	RowsCount  int
	Error      error
	Duration   time.Duration
}

// ParallelImporter выполняет параллельный импорт TDTP пакетов из брокеров
type ParallelImporter struct {
	config ImporterConfig
}

// NewParallelImporter создает новый параллельный импортер
func NewParallelImporter(config ImporterConfig) *ParallelImporter {
	// По умолчанию используем 4 воркера
	if config.Workers <= 0 {
		config.Workers = 4
	}
	return &ParallelImporter{
		config: config,
	}
}

// ImportStats содержит статистику импорта
type ImportStats struct {
	TotalParts      int
	PartsImported   int
	TotalRows       int
	Errors          []error
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	AvgPartDuration time.Duration
}

// Import выполняет параллельный импорт из брокера
// Принимает части из очереди/топика и обрабатывает их параллельно
// handler - функция обработки каждой части (например, вставка в БД)
func (pi *ParallelImporter) Import(
	ctx context.Context,
	handler func(ctx context.Context, dataPacket *packet.DataPacket) error,
) (*ImportStats, error) {
	stats := &ImportStats{
		StartTime: time.Now(),
	}

	// Создаем broker в зависимости от типа
	var broker brokers.MessageBroker
	var err error

	switch pi.config.Type {
	case "RabbitMQ":
		broker, err = pi.createRabbitMQBroker()
	case "Kafka":
		broker, err = pi.createKafkaBroker()
	default:
		return nil, fmt.Errorf("unsupported broker type: %s", pi.config.Type)
	}

	if err != nil {
		return stats, fmt.Errorf("failed to create broker: %w", err)
	}

	// Подключаемся к брокеру
	if err := broker.Connect(ctx); err != nil {
		return stats, fmt.Errorf("failed to connect to broker: %w", err)
	}
	defer broker.Close()

	// Создаем каналы для координации воркеров
	partsChan := make(chan []byte, pi.config.Workers*2)
	resultsChan := make(chan *ImportResult, pi.config.Workers*2)
	errorsChan := make(chan error, 1)

	// WaitGroup для отслеживания воркеров
	var wg sync.WaitGroup

	// Запускаем воркеры для параллельной обработки
	for i := 0; i < pi.config.Workers; i++ {
		wg.Add(1)
		go pi.worker(ctx, i, partsChan, resultsChan, handler, &wg)
	}

	// Горутина для получения сообщений из брокера
	go func() {
		defer close(partsChan)

		for {
			select {
			case <-ctx.Done():
				errorsChan <- ctx.Err()
				return
			default:
				// Получаем сообщение из брокера
				msg, err := broker.Receive(ctx)
				if err != nil {
					// Если контекст отменен, выходим нормально
					if ctx.Err() != nil {
						return
					}
					errorsChan <- fmt.Errorf("failed to receive message: %w", err)
					return
				}

				if msg == nil {
					// Нет больше сообщений
					return
				}

				// Отправляем в канал для обработки воркерами
				select {
				case partsChan <- msg:
				case <-ctx.Done():
					errorsChan <- ctx.Err()
					return
				}
			}
		}
	}()

	// Собираем результаты
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Обрабатываем результаты
	for result := range resultsChan {
		stats.PartsImported++
		stats.TotalRows += result.RowsCount

		// Обновляем TotalParts если есть значение (берем максимальное)
		if result.TotalParts > stats.TotalParts {
			stats.TotalParts = result.TotalParts
		}

		if result.Error != nil {
			stats.Errors = append(stats.Errors, fmt.Errorf("part %d: %w", result.PartNumber, result.Error))
		}
	}

	// Проверяем ошибки
	select {
	case err := <-errorsChan:
		if err != nil && err != context.Canceled {
			stats.Errors = append(stats.Errors, err)
		}
	default:
	}

	stats.EndTime = time.Now()
	stats.Duration = stats.EndTime.Sub(stats.StartTime)

	if stats.PartsImported > 0 {
		stats.AvgPartDuration = stats.Duration / time.Duration(stats.PartsImported)
	}

	if len(stats.Errors) > 0 {
		return stats, fmt.Errorf("import completed with %d errors", len(stats.Errors))
	}

	return stats, nil
}

// worker обрабатывает части из канала параллельно
func (pi *ParallelImporter) worker(
	ctx context.Context,
	workerID int,
	partsChan <-chan []byte,
	resultsChan chan<- *ImportResult,
	handler func(ctx context.Context, dataPacket *packet.DataPacket) error,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	parser := packet.NewParser()

	for {
		select {
		case <-ctx.Done():
			return

		case xmlData, ok := <-partsChan:
			if !ok {
				// Канал закрыт, завершаем воркер
				return
			}

			startTime := time.Now()

			// Парсим TDTP пакет
			dataPacket, err := parser.Parse(bytes.NewReader(xmlData))
			if err != nil {
				resultsChan <- &ImportResult{
					Error:    fmt.Errorf("worker %d: failed to parse packet: %w", workerID, err),
					Duration: time.Since(startTime),
				}
				continue
			}

			// Обрабатываем пакет через handler
			err = handler(ctx, dataPacket)

			resultsChan <- &ImportResult{
				PartNumber: dataPacket.Header.PartNumber,
				TotalParts: dataPacket.Header.TotalParts,
				RowsCount:  len(dataPacket.Data.Rows),
				Error:      err,
				Duration:   time.Since(startTime),
			}
		}
	}
}

// createRabbitMQBroker создает RabbitMQ брокер для чтения
func (pi *ParallelImporter) createRabbitMQBroker() (brokers.MessageBroker, error) {
	if pi.config.RabbitMQ == nil {
		return nil, fmt.Errorf("RabbitMQ config is not set")
	}

	cfg := pi.config.RabbitMQ

	return brokers.New(brokers.Config{
		Type:     "rabbitmq",
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.User,
		Password: cfg.Password,
		Queue:    cfg.Queue,
		Durable:  true,
	})
}

// createKafkaBroker создает Kafka брокер для чтения
func (pi *ParallelImporter) createKafkaBroker() (brokers.MessageBroker, error) {
	if pi.config.Kafka == nil {
		return nil, fmt.Errorf("Kafka config is not set")
	}

	cfg := pi.config.Kafka

	return brokers.New(brokers.Config{
		Type:    "kafka",
		Brokers: cfg.Brokers,
		Topic:   cfg.Topic,
	})
}

// ImportToDatabase импортирует данные из брокера в базу данных
// Автоматически создает таблицу если её нет и загружает данные
func ImportToDatabase(
	ctx context.Context,
	importer *ParallelImporter,
	workspace *Workspace,
	tableName string,
) (*ImportStats, error) {
	var mu sync.Mutex
	tableCreated := false
	var expectedBatchID string    // MessageID base первого пакета
	var expectedSchema []packet.Field // Schema первого пакета

	// Handler который вставляет данные в workspace
	handler := func(ctx context.Context, dataPacket *packet.DataPacket) error {
		// Извлекаем batch ID из MessageID (часть до "-P")
		batchID := extractBatchID(dataPacket.Header.MessageID)

		// Создаем таблицу при обработке первой части (thread-safe)
		mu.Lock()
		if !tableCreated {
			// Сохраняем batch ID и schema первого пакета
			expectedBatchID = batchID
			expectedSchema = dataPacket.Schema.Fields

			if err := workspace.CreateTable(ctx, tableName, dataPacket.Schema.Fields); err != nil {
				mu.Unlock()
				return fmt.Errorf("failed to create table: %w", err)
			}
			tableCreated = true
		} else {
			// Валидация: проверяем что пакет из того же batch
			if batchID != expectedBatchID {
				mu.Unlock()
				return fmt.Errorf("batch mismatch: expected %s, got %s (mixed batches in queue)", expectedBatchID, batchID)
			}

			// Валидация: проверяем что schema совпадает
			if !schemaEquals(dataPacket.Schema.Fields, expectedSchema) {
				mu.Unlock()
				return fmt.Errorf("schema mismatch: packet from batch %s has different schema", batchID)
			}
		}
		mu.Unlock()

		// Загружаем данные
		if err := workspace.LoadData(ctx, tableName, dataPacket); err != nil {
			return fmt.Errorf("failed to load data: %w", err)
		}

		return nil
	}

	return importer.Import(ctx, handler)
}

// extractBatchID извлекает batch ID из MessageID (часть до "-P")
// Например: "MSG-2024-123-P1" -> "MSG-2024-123"
func extractBatchID(messageID string) string {
	// Ищем последний "-P" в строке
	lastPIndex := -1
	for i := len(messageID) - 2; i >= 0; i-- {
		if messageID[i:i+2] == "-P" {
			lastPIndex = i
			break
		}
	}

	if lastPIndex > 0 {
		return messageID[:lastPIndex]
	}

	// Если не нашли "-P", возвращаем весь MessageID
	return messageID
}

// schemaEquals проверяет что две schema идентичны
func schemaEquals(a, b []packet.Field) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].Name != b[i].Name || a[i].Type != b[i].Type {
			return false
		}
	}

	return true
}
