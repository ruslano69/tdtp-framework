package resilience

import (
	"fmt"
	"time"
)

// Config - конфигурация Circuit Breaker
type Config struct {
	// Enabled - включить Circuit Breaker
	Enabled bool

	// Name - имя Circuit Breaker для логирования
	Name string

	// MaxFailures - количество ошибок для открытия
	MaxFailures uint32

	// Timeout - время в Open состоянии перед переходом в Half-Open
	Timeout time.Duration

	// MaxConcurrentCalls - максимальное количество одновременных вызовов
	// 0 = без ограничений
	MaxConcurrentCalls uint32

	// SuccessThreshold - количество успешных вызовов в Half-Open для закрытия
	SuccessThreshold uint32

	// OnStateChange - callback при изменении состояния
	OnStateChange func(name string, from State, to State)

	// ShouldTrip - custom функция для определения открытия
	// Если nil, используется стандартная логика (MaxFailures)
	ShouldTrip func(counts Counts) bool
}

// Counts - счетчики запросов
type Counts struct {
	Requests             uint32 // Всего запросов
	TotalSuccesses       uint32 // Всего успешных
	TotalFailures        uint32 // Всего неудачных
	ConsecutiveSuccesses uint32 // Последовательных успешных
	ConsecutiveFailures  uint32 // Последовательных неудачных
}

// Validate - валидация конфигурации
func (c *Config) Validate() error {
	if c.MaxFailures == 0 {
		return fmt.Errorf("MaxFailures must be greater than 0")
	}

	if c.Timeout <= 0 {
		return fmt.Errorf("Timeout must be greater than 0")
	}

	if c.SuccessThreshold == 0 {
		c.SuccessThreshold = 1 // По умолчанию 1 успешный вызов
	}

	if c.Name == "" {
		c.Name = "circuit-breaker"
	}

	return nil
}

// DefaultConfig - конфигурация по умолчанию
func DefaultConfig(name string) Config {
	return Config{
		Enabled:              true,
		Name:                 name,
		MaxFailures:          5,
		Timeout:              60 * time.Second,
		MaxConcurrentCalls:   0, // Без ограничений
		SuccessThreshold:     2,
		OnStateChange:        nil,
		ShouldTrip:           nil, // Используем стандартную логику
	}
}

// WithCallbacks - конфигурация с callback'ами
func WithCallbacks(name string, onStateChange func(string, State, State)) Config {
	config := DefaultConfig(name)
	config.OnStateChange = onStateChange
	return config
}

// AggressiveConfig - агрессивная конфигурация (быстрое открытие)
func AggressiveConfig(name string) Config {
	return Config{
		Enabled:              true,
		Name:                 name,
		MaxFailures:          3,
		Timeout:              30 * time.Second,
		MaxConcurrentCalls:   100,
		SuccessThreshold:     3,
		OnStateChange:        nil,
		ShouldTrip:           nil,
	}
}

// ConservativeConfig - консервативная конфигурация (медленное открытие)
func ConservativeConfig(name string) Config {
	return Config{
		Enabled:              true,
		Name:                 name,
		MaxFailures:          10,
		Timeout:              120 * time.Second,
		MaxConcurrentCalls:   0,
		SuccessThreshold:     5,
		OnStateChange:        nil,
		ShouldTrip:           nil,
	}
}
