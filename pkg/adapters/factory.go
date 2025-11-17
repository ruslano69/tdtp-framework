package adapters

import (
	"context"
	"fmt"
	"sync"
)

// AdapterConstructor - функция-конструктор адаптера
// Возвращает новый экземпляр адаптера (еще не подключенный к БД)
type AdapterConstructor func() Adapter

// Factory - фабрика для создания адаптеров
// Управляет регистрацией и созданием адаптеров различных типов
type Factory struct {
	registry map[string]AdapterConstructor
	mu       sync.RWMutex
}

// NewFactory создает новую фабрику адаптеров
func NewFactory() *Factory {
	return &Factory{
		registry: make(map[string]AdapterConstructor),
	}
}

// Register регистрирует конструктор адаптера для определенного типа БД
// dbType должен быть одним из: "sqlite", "postgres", "mssql"
// constructor - функция, которая создает новый экземпляр адаптера
//
// Пример:
//
//	factory.Register("postgres", func() adapters.Adapter {
//	    return &postgres.Adapter{}
//	})
func (f *Factory) Register(dbType string, constructor AdapterConstructor) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registry[dbType] = constructor
}

// Unregister удаляет конструктор адаптера
func (f *Factory) Unregister(dbType string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.registry, dbType)
}

// IsRegistered проверяет, зарегистрирован ли адаптер для данного типа БД
func (f *Factory) IsRegistered(dbType string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.registry[dbType]
	return ok
}

// GetRegisteredTypes возвращает список всех зарегистрированных типов БД
func (f *Factory) GetRegisteredTypes() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]string, 0, len(f.registry))
	for dbType := range f.registry {
		types = append(types, dbType)
	}
	return types
}

// Create создает и подключает адаптер по конфигурации
// Возвращает готовый к работе адаптер или ошибку
//
// Пример:
//
//	adapter, err := factory.Create(ctx, adapters.Config{
//	    Type: "postgres",
//	    DSN:  "postgresql://user:pass@localhost:5432/db",
//	})
func (f *Factory) Create(ctx context.Context, cfg Config) (Adapter, error) {
	f.mu.RLock()
	constructor, ok := f.registry[cfg.Type]
	f.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown database type: %s (available types: %v)",
			cfg.Type, f.GetRegisteredTypes())
	}

	// Создаем новый экземпляр адаптера
	adapter := constructor()

	// Подключаемся к БД
	if err := adapter.Connect(ctx, cfg); err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", cfg.Type, err)
	}

	return adapter, nil
}

// CreateWithoutConnect создает адаптер БЕЗ подключения к БД
// Полезно для тестирования или отложенного подключения
func (f *Factory) CreateWithoutConnect(dbType string) (Adapter, error) {
	f.mu.RLock()
	constructor, ok := f.registry[dbType]
	f.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown database type: %s (available types: %v)",
			dbType, f.GetRegisteredTypes())
	}

	return constructor(), nil
}

// ========== Global Factory ==========

var globalFactory = NewFactory()

// Register регистрирует адаптер в глобальной фабрике
// Эта функция обычно вызывается в init() функциях адаптеров
//
// Пример (в pkg/adapters/postgres/adapter.go):
//
//	func init() {
//	    adapters.Register("postgres", func() adapters.Adapter {
//	        return &Adapter{}
//	    })
//	}
func Register(dbType string, constructor AdapterConstructor) {
	globalFactory.Register(dbType, constructor)
}

// Unregister удаляет адаптер из глобальной фабрики
func Unregister(dbType string) {
	globalFactory.Unregister(dbType)
}

// IsRegistered проверяет регистрацию в глобальной фабрике
func IsRegistered(dbType string) bool {
	return globalFactory.IsRegistered(dbType)
}

// GetRegisteredTypes возвращает типы из глобальной фабрики
func GetRegisteredTypes() []string {
	return globalFactory.GetRegisteredTypes()
}

// New создает адаптер через глобальную фабрику
// Это основной способ создания адаптеров в приложении
//
// Пример:
//
//	adapter, err := adapters.New(ctx, adapters.Config{
//	    Type: "sqlite",
//	    DSN:  "file:app.db",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer adapter.Close(ctx)
func New(ctx context.Context, cfg Config) (Adapter, error) {
	return globalFactory.Create(ctx, cfg)
}

// NewWithoutConnect создает адаптер БЕЗ подключения через глобальную фабрику
func NewWithoutConnect(dbType string) (Adapter, error) {
	return globalFactory.CreateWithoutConnect(dbType)
}

// ========== Утилиты ==========

// MustNew создает адаптер или паникует при ошибке
// Использовать только в init() или main() где паника допустима
func MustNew(ctx context.Context, cfg Config) Adapter {
	adapter, err := New(ctx, cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to create adapter: %v", err))
	}
	return adapter
}
