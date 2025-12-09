package etl

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// PipelineConfig содержит полную конфигурацию ETL pipeline
type PipelineConfig struct {
	Name          string                 `yaml:"name"`
	Version       string                 `yaml:"version"`
	Description   string                 `yaml:"description"`
	Sources       []SourceConfig         `yaml:"sources"`
	Workspace     WorkspaceConfig        `yaml:"workspace"`
	Transform     TransformConfig        `yaml:"transform"`
	Output        OutputConfig           `yaml:"output"`
	Performance   PerformanceConfig      `yaml:"performance"`
	Audit         AuditConfig            `yaml:"audit"`
	ErrorHandling ErrorHandlingConfig    `yaml:"error_handling"`
}

// SourceConfig определяет источник данных (PostgreSQL, MSSQL, MySQL, SQLite)
type SourceConfig struct {
	Name    string `yaml:"name"`     // Имя источника (будет использовано как имя таблицы в workspace)
	Type    string `yaml:"type"`     // Тип: postgres, mssql, mysql, sqlite
	DSN     string `yaml:"dsn"`      // Data Source Name (строка подключения)
	Query   string `yaml:"query"`    // SQL запрос для извлечения данных
	Timeout int    `yaml:"timeout"`  // Таймаут в секундах (0 = без таймаута)
}

// WorkspaceConfig определяет временное хранилище для объединения данных
type WorkspaceConfig struct {
	Type   string                 `yaml:"type"`   // Тип: sqlite (только sqlite поддерживается)
	Mode   string                 `yaml:"mode"`   // Режим: memory (:memory:) или путь к файлу
	Config map[string]interface{} `yaml:"config"` // Дополнительные настройки SQLite
}

// TransformConfig определяет SQL трансформацию данных в workspace
type TransformConfig struct {
	SQL         string `yaml:"sql"`          // SQL запрос для трансформации
	ResultTable string `yaml:"result_table"` // Имя таблицы с результатом (опционально)
	Timeout     int    `yaml:"timeout"`      // Таймаут выполнения в секундах
}

// OutputConfig определяет назначение для результатов
type OutputConfig struct {
	Type     string                `yaml:"type"`               // Тип: tdtp, rabbitmq, kafka
	TDTP     *TDTPOutputConfig     `yaml:"tdtp,omitempty"`     // Конфигурация для TDTP
	RabbitMQ *RabbitMQOutputConfig `yaml:"rabbitmq,omitempty"` // Конфигурация для RabbitMQ
	Kafka    *KafkaOutputConfig    `yaml:"kafka,omitempty"`    // Конфигурация для Kafka
}

// TDTPOutputConfig определяет параметры экспорта в TDTP формат
type TDTPOutputConfig struct {
	Format      string `yaml:"format"`      // Формат: xml, json (в будущем)
	Compression bool   `yaml:"compression"` // Использовать zstd сжатие
	Destination string `yaml:"destination"` // Путь к файлу
}

// RabbitMQOutputConfig определяет параметры отправки в RabbitMQ
type RabbitMQOutputConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Queue    string `yaml:"queue"`
}

// KafkaOutputConfig определяет параметры отправки в Kafka
type KafkaOutputConfig struct {
	Brokers []string `yaml:"brokers"` // Список Kafka brokers
	Topic   string   `yaml:"topic"`   // Kafka topic
}

// PerformanceConfig определяет параметры производительности
type PerformanceConfig struct {
	MaxMemoryMB     int  `yaml:"max_memory_mb"`     // Максимальная память для workspace (MB)
	BatchSize       int  `yaml:"batch_size"`        // Размер batch для импорта
	ParallelSources bool `yaml:"parallel_sources"`  // Загружать источники параллельно
}

// AuditConfig определяет параметры аудита
type AuditConfig struct {
	Enabled bool   `yaml:"enabled"` // Включить аудит
	Level   string `yaml:"level"`   // Уровень: minimal, standard, detailed
	Output  string `yaml:"output"`  // Путь к файлу лога
	Format  string `yaml:"format"`  // Формат: json, text
}

// ErrorHandlingConfig определяет стратегии обработки ошибок
type ErrorHandlingConfig struct {
	OnSourceError     string `yaml:"on_source_error"`      // skip, fail, retry
	RetryAttempts     int    `yaml:"retry_attempts"`       // Количество повторов
	RetryDelaySeconds int    `yaml:"retry_delay_seconds"`  // Задержка между повторами
	OnTransformError  string `yaml:"on_transform_error"`   // skip, fail
	OnOutputError     string `yaml:"on_output_error"`      // fail, retry
}

// LoadConfig загружает конфигурацию из YAML файла
func LoadConfig(path string) (*PipelineConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config PipelineConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Валидация конфигурации
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Установка значений по умолчанию
	config.SetDefaults()

	return &config, nil
}

// Validate проверяет корректность конфигурации
func (c *PipelineConfig) Validate() error {
	// Проверка обязательных полей
	if c.Name == "" {
		return fmt.Errorf("pipeline name is required")
	}

	// Проверка sources
	if len(c.Sources) == 0 {
		return fmt.Errorf("at least one source is required")
	}

	for i, src := range c.Sources {
		if err := src.Validate(); err != nil {
			return fmt.Errorf("source[%d] (%s): %w", i, src.Name, err)
		}
	}

	// Проверка workspace
	if err := c.Workspace.Validate(); err != nil {
		return fmt.Errorf("workspace: %w", err)
	}

	// Проверка transform
	if err := c.Transform.Validate(); err != nil {
		return fmt.Errorf("transform: %w", err)
	}

	// Проверка output
	if err := c.Output.Validate(); err != nil {
		return fmt.Errorf("output: %w", err)
	}

	return nil
}

// Validate проверяет корректность SourceConfig
func (s *SourceConfig) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	if s.Type == "" {
		return fmt.Errorf("type is required")
	}
	if s.DSN == "" {
		return fmt.Errorf("dsn is required")
	}
	if s.Query == "" {
		return fmt.Errorf("query is required")
	}

	// Проверка поддерживаемых типов
	validTypes := map[string]bool{
		"postgres": true,
		"mssql":    true,
		"mysql":    true,
		"sqlite":   true,
	}
	if !validTypes[s.Type] {
		return fmt.Errorf("unsupported type '%s', must be one of: postgres, mssql, mysql, sqlite", s.Type)
	}

	return nil
}

// Validate проверяет корректность WorkspaceConfig
func (w *WorkspaceConfig) Validate() error {
	if w.Type == "" {
		return fmt.Errorf("type is required")
	}
	if w.Type != "sqlite" {
		return fmt.Errorf("only 'sqlite' workspace type is supported")
	}
	if w.Mode == "" {
		return fmt.Errorf("mode is required (use 'memory' for in-memory database)")
	}
	return nil
}

// Validate проверяет корректность TransformConfig
func (t *TransformConfig) Validate() error {
	if t.SQL == "" {
		return fmt.Errorf("sql is required")
	}
	return nil
}

// Validate проверяет корректность OutputConfig
func (o *OutputConfig) Validate() error {
	if o.Type == "" {
		return fmt.Errorf("type is required")
	}

	switch o.Type {
	case "tdtp":
		if o.TDTP == nil {
			return fmt.Errorf("tdtp configuration is required when type is 'tdtp'")
		}
		if o.TDTP.Destination == "" {
			return fmt.Errorf("tdtp.destination is required")
		}
		if o.TDTP.Format == "" {
			return fmt.Errorf("tdtp.format is required")
		}
		if o.TDTP.Format != "xml" && o.TDTP.Format != "json" {
			return fmt.Errorf("tdtp.format must be 'xml' or 'json'")
		}

	case "rabbitmq":
		if o.RabbitMQ == nil {
			return fmt.Errorf("rabbitmq configuration is required when type is 'rabbitmq'")
		}
		if o.RabbitMQ.Host == "" {
			return fmt.Errorf("rabbitmq.host is required")
		}
		if o.RabbitMQ.Queue == "" {
			return fmt.Errorf("rabbitmq.queue is required")
		}

	case "kafka":
		if o.Kafka == nil {
			return fmt.Errorf("kafka configuration is required when type is 'kafka'")
		}
		if len(o.Kafka.Brokers) == 0 {
			return fmt.Errorf("kafka.brokers is required")
		}
		if o.Kafka.Topic == "" {
			return fmt.Errorf("kafka.topic is required")
		}

	default:
		return fmt.Errorf("unsupported output type '%s', must be one of: tdtp, rabbitmq, kafka", o.Type)
	}

	return nil
}

// SetDefaults устанавливает значения по умолчанию для необязательных полей
func (c *PipelineConfig) SetDefaults() {
	// Defaults для version
	if c.Version == "" {
		c.Version = "1.0"
	}

	// Defaults для sources
	for i := range c.Sources {
		if c.Sources[i].Timeout == 0 {
			c.Sources[i].Timeout = 60 // 60 секунд по умолчанию
		}
	}

	// Defaults для workspace mode
	if c.Workspace.Mode == "memory" {
		c.Workspace.Mode = ":memory:"
	}

	// Defaults для transform
	if c.Transform.ResultTable == "" {
		c.Transform.ResultTable = "result"
	}
	if c.Transform.Timeout == 0 {
		c.Transform.Timeout = 300 // 5 минут по умолчанию
	}

	// Defaults для TDTP output
	if c.Output.Type == "tdtp" && c.Output.TDTP != nil {
		if c.Output.TDTP.Format == "" {
			c.Output.TDTP.Format = "xml"
		}
	}

	// Defaults для RabbitMQ
	if c.Output.Type == "rabbitmq" && c.Output.RabbitMQ != nil {
		if c.Output.RabbitMQ.Port == 0 {
			c.Output.RabbitMQ.Port = 5672
		}
		if c.Output.RabbitMQ.User == "" {
			c.Output.RabbitMQ.User = "guest"
		}
		if c.Output.RabbitMQ.Password == "" {
			c.Output.RabbitMQ.Password = "guest"
		}
	}

	// Defaults для performance
	if c.Performance.MaxMemoryMB == 0 {
		c.Performance.MaxMemoryMB = 2048 // 2GB по умолчанию
	}
	if c.Performance.BatchSize == 0 {
		c.Performance.BatchSize = 10000
	}

	// Defaults для audit
	if c.Audit.Level == "" {
		c.Audit.Level = "standard"
	}
	if c.Audit.Format == "" {
		c.Audit.Format = "json"
	}

	// Defaults для error handling
	if c.ErrorHandling.OnSourceError == "" {
		c.ErrorHandling.OnSourceError = "fail"
	}
	if c.ErrorHandling.OnTransformError == "" {
		c.ErrorHandling.OnTransformError = "fail"
	}
	if c.ErrorHandling.OnOutputError == "" {
		c.ErrorHandling.OnOutputError = "fail"
	}
	if c.ErrorHandling.RetryAttempts == 0 {
		c.ErrorHandling.RetryAttempts = 3
	}
	if c.ErrorHandling.RetryDelaySeconds == 0 {
		c.ErrorHandling.RetryDelaySeconds = 5
	}
}
