package processors

import (
	"context"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// Processor определяет интерфейс для обработки данных
type Processor interface {
	// Name возвращает имя процессора
	Name() string

	// Process обрабатывает данные
	// data - строки данных (каждая строка - массив строк)
	// schema - схема данных для понимания типов полей
	Process(ctx context.Context, data [][]string, schema packet.Schema) ([][]string, error)
}

// PreProcessor выполняется перед генерацией TDTP пакета (при экспорте)
// Используется для: маскирования, анонимизации, нормализации, валидации
type PreProcessor interface {
	Processor
}

// PostProcessor выполняется после парсинга TDTP пакета (при импорте)
// Используется для: обогащения, трансформации, валидации, присвоения значений по умолчанию
type PostProcessor interface {
	Processor
}

// Config содержит конфигурацию процессора
type Config struct {
	Type   string                 `yaml:"type"`   // Тип процессора (field_masker, normalizer, etc)
	Params map[string]interface{} `yaml:"params"` // Параметры процессора
}

// ProcessorConfig содержит конфигурацию цепочки процессоров
type ProcessorConfig struct {
	PreExport  []Config `yaml:"pre_export"`  // Процессоры перед экспортом
	PostImport []Config `yaml:"post_import"` // Процессоры после импорта
}
