package etl

import (
	"context"
	"fmt"
	"time"
)

// ProcessorStats представляет статистику выполнения ETL
type ProcessorStats struct {
	StartTime        time.Time
	EndTime          time.Time
	Duration         time.Duration
	SourcesLoaded    int
	TotalRowsLoaded  int
	TotalRowsExported int
	Errors           []error
}

// Processor представляет главный ETL процессор
type Processor struct {
	config    *PipelineConfig
	workspace *Workspace
	loader    *Loader
	executor  *Executor
	exporter  *Exporter
	stats     ProcessorStats
}

// NewProcessor создает новый ETL процессор
func NewProcessor(config *PipelineConfig) *Processor {
	return &Processor{
		config: config,
		loader: NewLoader(config.Sources),
		stats:  ProcessorStats{},
	}
}

// Execute выполняет весь ETL процесс
func (p *Processor) Execute(ctx context.Context) error {
	p.stats.StartTime = time.Now()
	defer func() {
		p.stats.EndTime = time.Now()
		p.stats.Duration = p.stats.EndTime.Sub(p.stats.StartTime)
	}()

	// 1. Создаем workspace
	if err := p.initWorkspace(ctx); err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}
	defer p.closeWorkspace(ctx)

	// 2. Загружаем данные из всех источников
	sourcesData, err := p.loadSources(ctx)
	if err != nil {
		return fmt.Errorf("failed to load sources: %w", err)
	}

	// 3. Создаем таблицы в workspace и загружаем данные
	if err := p.populateWorkspace(ctx, sourcesData); err != nil {
		return fmt.Errorf("failed to populate workspace: %w", err)
	}

	// 4. Выполняем SQL трансформацию
	result, err := p.executeTransformation(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute transformation: %w", err)
	}

	// 5. Экспортируем результаты
	if err := p.exportResults(ctx, result); err != nil {
		return fmt.Errorf("failed to export results: %w", err)
	}

	return nil
}

// initWorkspace инициализирует workspace
func (p *Processor) initWorkspace(ctx context.Context) error {
	workspace, err := NewWorkspace(ctx)
	if err != nil {
		return err
	}

	p.workspace = workspace
	p.executor = NewExecutor(workspace)
	p.exporter = NewExporter(p.config.Output)

	return nil
}

// loadSources загружает данные из всех источников
func (p *Processor) loadSources(ctx context.Context) ([]SourceData, error) {
	// Загружаем данные параллельно
	sourcesData, err := p.loader.LoadAll(ctx)
	if err != nil {
		return nil, err
	}

	// Подсчитываем статистику
	p.stats.SourcesLoaded = len(sourcesData)
	for _, data := range sourcesData {
		if data.Packet != nil {
			p.stats.TotalRowsLoaded += len(data.Packet.Data.Rows)
		}
	}

	return sourcesData, nil
}

// populateWorkspace создает таблицы и загружает данные в workspace
func (p *Processor) populateWorkspace(ctx context.Context, sourcesData []SourceData) error {
	for _, source := range sourcesData {
		if source.Error != nil {
			return fmt.Errorf("source '%s' has error: %w", source.SourceName, source.Error)
		}

		if source.Packet == nil {
			return fmt.Errorf("source '%s' has no data", source.SourceName)
		}

		// Создаем таблицу в workspace
		if err := p.workspace.CreateTable(ctx, source.TableName, source.Packet.Schema.Fields); err != nil {
			return fmt.Errorf("failed to create table '%s': %w", source.TableName, err)
		}

		// Загружаем данные в таблицу
		if err := p.workspace.LoadData(ctx, source.TableName, source.Packet); err != nil {
			return fmt.Errorf("failed to load data into '%s': %w", source.TableName, err)
		}
	}

	return nil
}

// executeTransformation выполняет SQL трансформацию
func (p *Processor) executeTransformation(ctx context.Context) (*ExecutionResult, error) {
	// Выполняем SQL из конфигурации
	result, err := p.executor.Execute(ctx, p.config.Transform.SQL, p.config.Transform.ResultTable)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// exportResults экспортирует результаты
// Автоматически выбирает streaming режим для RabbitMQ/Kafka и batch для TDTP файлов
func (p *Processor) exportResults(ctx context.Context, result *ExecutionResult) error {
	// Для RabbitMQ/Kafka используем streaming экспорт (не требует всех данных в памяти)
	if p.config.Output.Type == "rabbitmq" || p.config.Output.Type == "kafka" {
		return p.exportResultsStreaming(ctx)
	}

	// Для TDTP файлов используем batch экспорт (нужны все данные для TotalParts)
	if result.Packet == nil {
		return fmt.Errorf("no data to export")
	}

	exportResult, err := p.exporter.Export(ctx, result.Packet)
	if err != nil {
		return err
	}

	p.stats.TotalRowsExported = exportResult.RowsExported

	return nil
}

// exportResultsStreaming выполняет потоковый экспорт результатов в RabbitMQ/Kafka
func (p *Processor) exportResultsStreaming(ctx context.Context) error {
	// Выполняем SQL с потоковым чтением
	streamResult, err := p.workspace.ExecuteSQLStream(ctx, p.config.Transform.SQL, p.config.Transform.ResultTable)
	if err != nil {
		return fmt.Errorf("failed to execute SQL stream: %w", err)
	}

	// Экспортируем в потоковом режиме
	exportResult, err := p.exporter.ExportStream(ctx, streamResult, p.config.Transform.ResultTable)
	if err != nil {
		return fmt.Errorf("failed to export stream: %w", err)
	}

	// Обновляем статистику
	p.stats.TotalRowsExported = exportResult.TotalRows

	return nil
}

// closeWorkspace закрывает workspace
func (p *Processor) closeWorkspace(ctx context.Context) {
	if p.workspace != nil {
		if err := p.workspace.Close(ctx); err != nil {
			p.stats.Errors = append(p.stats.Errors, fmt.Errorf("failed to close workspace: %w", err))
		}
	}
}

// GetStats возвращает статистику выполнения
func (p *Processor) GetStats() ProcessorStats {
	return p.stats
}

// Validate проверяет конфигурацию процессора перед выполнением
func (p *Processor) Validate() error {
	if p.config == nil {
		return fmt.Errorf("config is nil")
	}

	// Проверяем конфигурацию через config.Validate()
	if err := p.config.Validate(); err != nil {
		return err
	}

	// Проверяем конфигурацию экспортера
	tempExporter := NewExporter(p.config.Output)
	if err := tempExporter.ValidateConfig(); err != nil {
		return fmt.Errorf("output validation failed: %w", err)
	}

	return nil
}

// GetConfig возвращает конфигурацию процессора
func (p *Processor) GetConfig() *PipelineConfig {
	return p.config
}
