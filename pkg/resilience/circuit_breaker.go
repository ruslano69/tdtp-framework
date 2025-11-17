package resilience

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrCircuitOpen - circuit breaker открыт
	ErrCircuitOpen = errors.New("circuit breaker is open")

	// ErrTooManyCalls - слишком много одновременных вызовов
	ErrTooManyCalls = errors.New("too many concurrent calls")

	// ErrCircuitDisabled - circuit breaker отключен
	ErrCircuitDisabled = errors.New("circuit breaker is disabled")
)

// ExecuteFunc - функция для выполнения с circuit breaker
type ExecuteFunc func(ctx context.Context) error

// CircuitBreaker - защита от каскадных сбоев
type CircuitBreaker struct {
	config       Config
	stateManager *stateManager
}

// New - создать новый Circuit Breaker
func New(config Config) (*CircuitBreaker, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid circuit breaker config: %w", err)
	}

	cb := &CircuitBreaker{
		config:       config,
		stateManager: newStateManager(config),
	}

	return cb, nil
}

// Execute - выполнить функцию с защитой circuit breaker
func (cb *CircuitBreaker) Execute(ctx context.Context, fn ExecuteFunc) error {
	// Если circuit breaker отключен, просто выполняем функцию
	if !cb.config.Enabled {
		return fn(ctx)
	}

	// Проверяем можем ли выполнить запрос
	generation, err := cb.stateManager.beforeRequest()
	if err != nil {
		return err
	}

	// Выполняем функцию и отслеживаем результат
	defer func() {
		if r := recover(); r != nil {
			// При panic считаем как ошибку
			cb.stateManager.afterRequest(generation, false)
			panic(r)
		}
	}()

	// Выполняем функцию
	err = fn(ctx)

	// Записываем результат
	success := err == nil
	cb.stateManager.afterRequest(generation, success)

	return err
}

// ExecuteWithFallback - выполнить с fallback функцией
func (cb *CircuitBreaker) ExecuteWithFallback(
	ctx context.Context,
	fn ExecuteFunc,
	fallback ExecuteFunc,
) error {
	err := cb.Execute(ctx, fn)

	// Если circuit открыт или ошибка, используем fallback
	if errors.Is(err, ErrCircuitOpen) || errors.Is(err, ErrTooManyCalls) {
		if fallback != nil {
			return fallback(ctx)
		}
	}

	return err
}

// Call - альтернативное имя для Execute (более короткое)
func (cb *CircuitBreaker) Call(ctx context.Context, fn ExecuteFunc) error {
	return cb.Execute(ctx, fn)
}

// State - получить текущее состояние
func (cb *CircuitBreaker) State() State {
	return cb.stateManager.getState()
}

// Counts - получить счетчики
func (cb *CircuitBreaker) Counts() Counts {
	return cb.stateManager.getCounts()
}

// Stats - получить полную статистику
func (cb *CircuitBreaker) Stats() Stats {
	return cb.stateManager.getStats()
}

// Reset - сбросить состояние в Closed
func (cb *CircuitBreaker) Reset() {
	cb.stateManager.reset()
}

// IsOpen - проверка открыт ли circuit
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.stateManager.getState() == StateOpen
}

// IsClosed - проверка закрыт ли circuit
func (cb *CircuitBreaker) IsClosed() bool {
	return cb.stateManager.getState() == StateClosed
}

// IsHalfOpen - проверка в Half-Open состоянии
func (cb *CircuitBreaker) IsHalfOpen() bool {
	return cb.stateManager.getState() == StateHalfOpen
}

// Name - имя Circuit Breaker
func (cb *CircuitBreaker) Name() string {
	return cb.config.Name
}

// WaitUntilReady - ожидание до готовности (Half-Open или Closed)
func (cb *CircuitBreaker) WaitUntilReady(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		state := cb.State()
		if state == StateClosed || state == StateHalfOpen {
			return nil
		}

		select {
		case <-ticker.C:
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// String - строковое представление
func (cb *CircuitBreaker) String() string {
	stats := cb.Stats()
	return fmt.Sprintf("CircuitBreaker(%s state=%s failures=%d/%d)",
		cb.config.Name,
		stats.State,
		stats.Counts.ConsecutiveFailures,
		cb.config.MaxFailures,
	)
}

// CircuitBreakerGroup - группа circuit breakers
type CircuitBreakerGroup struct {
	breakers map[string]*CircuitBreaker
}

// NewGroup - создать группу circuit breakers
func NewGroup() *CircuitBreakerGroup {
	return &CircuitBreakerGroup{
		breakers: make(map[string]*CircuitBreaker),
	}
}

// Add - добавить circuit breaker в группу
func (g *CircuitBreakerGroup) Add(name string, cb *CircuitBreaker) {
	g.breakers[name] = cb
}

// Get - получить circuit breaker по имени
func (g *CircuitBreakerGroup) Get(name string) (*CircuitBreaker, bool) {
	cb, ok := g.breakers[name]
	return cb, ok
}

// GetOrCreate - получить или создать circuit breaker
func (g *CircuitBreakerGroup) GetOrCreate(name string, config Config) (*CircuitBreaker, error) {
	if cb, ok := g.breakers[name]; ok {
		return cb, nil
	}

	config.Name = name
	cb, err := New(config)
	if err != nil {
		return nil, err
	}

	g.breakers[name] = cb
	return cb, nil
}

// Execute - выполнить функцию с circuit breaker по имени
func (g *CircuitBreakerGroup) Execute(ctx context.Context, name string, fn ExecuteFunc) error {
	cb, ok := g.breakers[name]
	if !ok {
		return fmt.Errorf("circuit breaker '%s' not found", name)
	}

	return cb.Execute(ctx, fn)
}

// ResetAll - сбросить все circuit breakers
func (g *CircuitBreakerGroup) ResetAll() {
	for _, cb := range g.breakers {
		cb.Reset()
	}
}

// StatsAll - получить статистику всех circuit breakers
func (g *CircuitBreakerGroup) StatsAll() map[string]Stats {
	result := make(map[string]Stats, len(g.breakers))
	for name, cb := range g.breakers {
		result[name] = cb.Stats()
	}
	return result
}
