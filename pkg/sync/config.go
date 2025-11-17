package sync

import "fmt"

// SyncMode определяет режим синхронизации
type SyncMode string

const (
	// SyncModeFull - полная синхронизация (все записи)
	SyncModeFull SyncMode = "full"
	// SyncModeIncremental - инкрементальная синхронизация (только изменения)
	SyncModeIncremental SyncMode = "incremental"
)

// TrackingStrategy определяет стратегию отслеживания изменений
type TrackingStrategy string

const (
	// TrackingTimestamp - отслеживание по timestamp полю (updated_at, modified_at, etc.)
	TrackingTimestamp TrackingStrategy = "timestamp"
	// TrackingSequence - отслеживание по auto-increment ID или sequence
	TrackingSequence TrackingStrategy = "sequence"
	// TrackingVersion - отслеживание по version field
	TrackingVersion TrackingStrategy = "version"
)

// IncrementalConfig содержит конфигурацию для инкрементальной синхронизации
type IncrementalConfig struct {
	// Enabled - включить инкрементальную синхронизацию
	Enabled bool

	// Mode - режим синхронизации
	Mode SyncMode

	// Strategy - стратегия отслеживания изменений
	Strategy TrackingStrategy

	// TrackingField - имя поля для отслеживания изменений
	// Примеры: "updated_at", "modified_at", "id", "version"
	TrackingField string

	// StateFile - путь к файлу с состоянием синхронизации
	// Если не указан, используется "./sync_state.json"
	StateFile string

	// BatchSize - размер batch для загрузки (0 = без ограничений)
	BatchSize int

	// InitialValue - начальное значение для первой синхронизации
	// Если не указано, загружаются все записи
	InitialValue string

	// OrderBy - направление сортировки (ASC или DESC)
	// По умолчанию ASC для правильной последовательности
	OrderBy string
}

// Validate проверяет корректность конфигурации
func (c *IncrementalConfig) Validate() error {
	if !c.Enabled {
		return nil // Если не включено, валидация не нужна
	}

	if c.Mode != SyncModeFull && c.Mode != SyncModeIncremental {
		return fmt.Errorf("invalid sync mode: %s (supported: full, incremental)", c.Mode)
	}

	if c.TrackingField == "" {
		return fmt.Errorf("tracking_field is required for incremental sync")
	}

	if c.Strategy == "" {
		c.Strategy = TrackingTimestamp // По умолчанию timestamp
	}

	if c.Strategy != TrackingTimestamp && c.Strategy != TrackingSequence && c.Strategy != TrackingVersion {
		return fmt.Errorf("invalid tracking strategy: %s (supported: timestamp, sequence, version)", c.Strategy)
	}

	if c.StateFile == "" {
		c.StateFile = "./sync_state.json"
	}

	if c.OrderBy == "" {
		c.OrderBy = "ASC"
	}

	if c.OrderBy != "ASC" && c.OrderBy != "DESC" {
		return fmt.Errorf("invalid order_by: %s (supported: ASC, DESC)", c.OrderBy)
	}

	return nil
}

// DefaultIncrementalConfig возвращает конфигурацию по умолчанию
func DefaultIncrementalConfig() IncrementalConfig {
	return IncrementalConfig{
		Enabled:       false,
		Mode:          SyncModeFull,
		Strategy:      TrackingTimestamp,
		TrackingField: "updated_at",
		StateFile:     "./sync_state.json",
		BatchSize:     10000,
		InitialValue:  "",
		OrderBy:       "ASC",
	}
}

// EnableIncrementalSync создает конфигурацию для инкрементальной синхронизации
func EnableIncrementalSync(trackingField string) IncrementalConfig {
	config := DefaultIncrementalConfig()
	config.Enabled = true
	config.Mode = SyncModeIncremental
	config.TrackingField = trackingField
	return config
}
