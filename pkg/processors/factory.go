package processors

import (
	"fmt"
)

// Factory создает процессоры по их типу и конфигурации
type Factory struct {
	creators map[string]CreatorFunc
}

// CreatorFunc функция для создания процессора из конфигурации
type CreatorFunc func(params map[string]any) (Processor, error)

// NewFactory создает новую фабрику процессоров
func NewFactory() *Factory {
	f := &Factory{
		creators: make(map[string]CreatorFunc),
	}

	// Регистрируем встроенные процессоры
	f.Register("field_masker", func(params map[string]any) (Processor, error) {
		return NewFieldMaskerFromConfig(params)
	})

	f.Register("field_normalizer", func(params map[string]any) (Processor, error) {
		return NewFieldNormalizerFromConfig(params)
	})

	f.Register("field_validator", func(params map[string]any) (Processor, error) {
		return NewFieldValidatorFromConfig(params)
	})

	return f
}

// Register регистрирует новый тип процессора
func (f *Factory) Register(processorType string, creator CreatorFunc) {
	f.creators[processorType] = creator
}

// Create создает процессор по конфигурации
func (f *Factory) Create(config Config) (Processor, error) {
	creator, ok := f.creators[config.Type]
	if !ok {
		return nil, fmt.Errorf("unknown processor type: %s", config.Type)
	}

	processor, err := creator(config.Params)
	if err != nil {
		return nil, fmt.Errorf("failed to create processor '%s': %w", config.Type, err)
	}

	return processor, nil
}

// CreateChain создает цепочку процессоров из массива конфигураций
func (f *Factory) CreateChain(configs []Config) (*Chain, error) {
	chain := NewChain()

	for i, config := range configs {
		processor, err := f.Create(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create processor %d: %w", i, err)
		}
		chain.Add(processor)
	}

	return chain, nil
}

// DefaultFactory возвращает фабрику со всеми встроенными процессорами
var DefaultFactory = NewFactory()

// CreateProcessor создает процессор используя дефолтную фабрику
func CreateProcessor(config Config) (Processor, error) {
	return DefaultFactory.Create(config)
}

// CreateChainFromConfigs создает цепочку процессоров используя дефолтную фабрику
func CreateChainFromConfigs(configs []Config) (*Chain, error) {
	return DefaultFactory.CreateChain(configs)
}
