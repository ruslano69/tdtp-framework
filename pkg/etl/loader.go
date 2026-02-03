package etl

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// SourceData представляет загруженные данные из одного источника
type SourceData struct {
	SourceName string
	TableName  string
	Packet     *packet.DataPacket
	Error      error
}

// Loader отвечает за загрузку данных из источников
type Loader struct {
	sources       []SourceConfig
	errorHandling ErrorHandlingConfig
}

// NewLoader создает новый загрузчик данных
func NewLoader(sources []SourceConfig, errorHandling ErrorHandlingConfig) *Loader {
	return &Loader{
		sources:       sources,
		errorHandling: errorHandling,
	}
}

// LoadAll загружает данные из всех источников параллельно
func (l *Loader) LoadAll(ctx context.Context) ([]SourceData, error) {
	if len(l.sources) == 0 {
		return nil, fmt.Errorf("no sources configured")
	}

	// Канал для результатов
	results := make(chan SourceData, len(l.sources))

	// WaitGroup для ожидания завершения всех горутин
	var wg sync.WaitGroup

	// Запускаем загрузку из каждого источника параллельно
	for _, source := range l.sources {
		wg.Add(1)
		go func(src SourceConfig) {
			defer wg.Done()

			result := SourceData{
				SourceName: src.Name,
				TableName:  src.Name,
			}

			// Загружаем данные из источника
			packet, err := l.loadFromSource(ctx, src)
			if err != nil {
				result.Error = err
			} else {
				result.Packet = packet
			}

			results <- result
		}(source)
	}

	// Ждем завершения всех горутин и закрываем канал
	go func() {
		wg.Wait()
		close(results)
	}()

	// Собираем результаты
	var allResults []SourceData
	var sourceErrors []error

	for result := range results {
		allResults = append(allResults, result)
		if result.Error != nil {
			sourceErrors = append(sourceErrors, fmt.Errorf("source '%s': %w", result.SourceName, result.Error))
		}
	}

	// Обработка ошибок согласно on_source_error стратегии
	if len(sourceErrors) > 0 {
		switch l.errorHandling.OnSourceError {
		case "continue":
			// Continue: возвращаем все результаты (включая ошибочные) и все ошибки
			// Processor решит что делать с источниками где Error != nil
			return allResults, errors.Join(sourceErrors...)

		case "fail":
			// Fail: останавливаемся на первой ошибке
			return allResults, sourceErrors[0]

		default:
			// По умолчанию fail
			return allResults, sourceErrors[0]
		}
	}

	return allResults, nil
}

// LoadOne загружает данные из одного источника
func (l *Loader) LoadOne(ctx context.Context, sourceName string) (*SourceData, error) {
	// Ищем источник по имени
	var source *SourceConfig
	for _, src := range l.sources {
		if src.Name == sourceName {
			source = &src
			break
		}
	}

	if source == nil {
		return nil, fmt.Errorf("source '%s' not found", sourceName)
	}

	// Загружаем данные
	packet, err := l.loadFromSource(ctx, *source)
	if err != nil {
		return &SourceData{
			SourceName: source.Name,
			TableName:  source.Name,
			Error:      err,
		}, err
	}

	return &SourceData{
		SourceName: source.Name,
		TableName:  source.Name,
		Packet:     packet,
	}, nil
}

// loadFromSource загружает данные из конкретного источника
func (l *Loader) loadFromSource(ctx context.Context, source SourceConfig) (*packet.DataPacket, error) {
	// Создаем адаптер для источника
	adapter, err := adapters.New(ctx, adapters.Config{
		Type: source.Type,
		DSN:  source.DSN,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create adapter: %w", err)
	}
	defer adapter.Close(ctx)

	// Проверяем соединение
	if err := adapter.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Выполняем SQL запрос источника
	// Используем ExecuteRawSQL для выполнения произвольного SELECT
	packet, err := l.executeSourceQuery(ctx, adapter, source)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Обновляем имя таблицы в пакете на alias
	packet.Header.TableName = source.Name

	return packet, nil
}

// executeSourceQuery выполняет SQL запрос источника и возвращает DataPacket
func (l *Loader) executeSourceQuery(ctx context.Context, adapter adapters.Adapter, source SourceConfig) (*packet.DataPacket, error) {
	// Для выполнения произвольного SQL нам нужно получить прямой доступ к *sql.DB
	// Используем интерфейс RawQueryExecutor если адаптер его поддерживает

	type RawQueryExecutor interface {
		ExecuteRawQuery(ctx context.Context, query string) (*packet.DataPacket, error)
	}

	executor, ok := adapter.(RawQueryExecutor)
	if !ok {
		// Если адаптер не поддерживает ExecuteRawQuery, используем обходной путь
		// Это временное решение - в Phase 2 мы добавим ExecuteRawQuery во все адаптеры
		return nil, fmt.Errorf("adapter does not support ExecuteRawQuery (will be implemented in next step)")
	}

	return executor.ExecuteRawQuery(ctx, source.Query)
}

// GetSourceCount возвращает количество сконфигурированных источников
func (l *Loader) GetSourceCount() int {
	return len(l.sources)
}

// GetSourceNames возвращает имена всех источников
func (l *Loader) GetSourceNames() []string {
	names := make([]string, len(l.sources))
	for i, src := range l.sources {
		names[i] = src.Name
	}
	return names
}
