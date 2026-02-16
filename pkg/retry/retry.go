package retry

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
)

// RetryableFunc - функция которую можно retry
type RetryableFunc func(ctx context.Context) error

// Retryer выполняет retry логику
type Retryer struct {
	config Config
	dlq    *DLQ
}

// NewRetryer создает новый Retryer
func NewRetryer(config Config) (*Retryer, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid retry config: %w", err)
	}

	var dlq *DLQ
	if config.DLQ.Enabled {
		var err error
		dlq, err = NewDLQ(config.DLQ)
		if err != nil {
			return nil, fmt.Errorf("failed to create DLQ: %w", err)
		}
	}

	return &Retryer{
		config: config,
		dlq:    dlq,
	}, nil
}

// Do выполняет функцию с retry
func (r *Retryer) Do(ctx context.Context, fn RetryableFunc) error {
	return r.doInternal(ctx, fn, nil, true)
}

// DoWithData выполняет функцию с retry и сохраняет данные в DLQ при сбое
func (r *Retryer) DoWithData(ctx context.Context, fn RetryableFunc, data any) error {
	return r.doInternal(ctx, fn, data, true)
}

// doInternal выполняет функцию с retry (внутренняя реализация)
func (r *Retryer) doInternal(ctx context.Context, fn RetryableFunc, data any, addToDLQ bool) error {
	if !r.config.Enabled {
		// Retry отключен, просто выполняем функцию
		return fn(ctx)
	}

	var lastErr error
	attempts := 0

	for {
		attempts++

		// Выполняем функцию
		err := fn(ctx)
		if err == nil {
			// Успех!
			return nil
		}

		lastErr = err

		// Проверяем нужен ли retry для этой ошибки
		if !r.isRetryableError(err) {
			return fmt.Errorf("non-retryable error: %w", err)
		}

		// Проверяем достигли ли максимального количества попыток
		if r.config.MaxAttempts > 0 && attempts >= r.config.MaxAttempts {
			// Достигли лимита, сохраняем в DLQ если включен
			if r.dlq != nil && addToDLQ {
				r.dlq.Add(DLQEntry{
					Timestamp:   time.Now(),
					Attempts:    attempts,
					LastError:   err.Error(),
					FailureType: "max_attempts_exceeded",
					Data:        data,
				})
			}
			return fmt.Errorf("max retry attempts (%d) exceeded: %w", r.config.MaxAttempts, lastErr)
		}

		// Проверяем context
		if ctx.Err() != nil {
			return fmt.Errorf("context cancelled: %w", ctx.Err())
		}

		// Вычисляем задержку
		delay := r.calculateDelay(attempts)

		// Callback перед retry
		if r.config.OnRetry != nil {
			r.config.OnRetry(attempts, err, delay)
		}

		// Ждем перед следующей попыткой
		select {
		case <-time.After(delay):
			// Продолжаем
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		}
	}
}

// calculateDelay вычисляет задержку для текущей попытки
func (r *Retryer) calculateDelay(attempt int) time.Duration {
	var delay time.Duration

	switch r.config.BackoffStrategy {
	case BackoffConstant:
		delay = r.config.InitialDelay

	case BackoffLinear:
		// Linear: delay = initial * attempt
		delay = r.config.InitialDelay * time.Duration(attempt)

	case BackoffExponential:
		// Exponential: delay = initial * multiplier^(attempt-1)
		multiplier := math.Pow(r.config.BackoffMultiplier, float64(attempt-1))
		delay = time.Duration(float64(r.config.InitialDelay) * multiplier)

	default:
		delay = r.config.InitialDelay
	}

	// Применяем max delay
	if delay > r.config.MaxDelay {
		delay = r.config.MaxDelay
	}

	// Добавляем jitter (случайность)
	if r.config.Jitter > 0 {
		jitter := time.Duration(float64(delay) * r.config.Jitter * (rand.Float64()*2 - 1))
		delay += jitter
		if delay < 0 {
			delay = r.config.InitialDelay
		}
	}

	return delay
}

// isRetryableError проверяет нужен ли retry для ошибки
func (r *Retryer) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Если список retryable errors пуст, retry все ошибки
	if len(r.config.RetryableErrors) == 0 {
		return true
	}

	// Проверяем содержит ли ошибка один из retryable patterns
	errStr := err.Error()
	for _, pattern := range r.config.RetryableErrors {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// GetDLQ возвращает DLQ если он включен
func (r *Retryer) GetDLQ() *DLQ {
	return r.dlq
}

// Close закрывает Retryer и сохраняет DLQ
func (r *Retryer) Close() error {
	if r.dlq != nil {
		return r.dlq.Save()
	}
	return nil
}
