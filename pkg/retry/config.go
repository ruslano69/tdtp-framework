package retry

import (
	"fmt"
	"time"
)

// BackoffStrategy определяет стратегию задержки между повторами
type BackoffStrategy string

const (
	// BackoffConstant - постоянная задержка
	BackoffConstant BackoffStrategy = "constant"
	// BackoffLinear - линейное увеличение задержки
	BackoffLinear BackoffStrategy = "linear"
	// BackoffExponential - экспоненциальное увеличение задержки
	BackoffExponential BackoffStrategy = "exponential"
)

// Config содержит конфигурацию для retry механизма
type Config struct {
	// Enabled - включить retry механизм
	Enabled bool

	// MaxAttempts - максимальное количество попыток (включая первую)
	// 0 = бесконечные попытки (не рекомендуется)
	MaxAttempts int

	// InitialDelay - начальная задержка перед первым retry
	InitialDelay time.Duration

	// MaxDelay - максимальная задержка между попытками
	MaxDelay time.Duration

	// BackoffStrategy - стратегия увеличения задержки
	BackoffStrategy BackoffStrategy

	// BackoffMultiplier - множитель для exponential backoff (обычно 2.0)
	BackoffMultiplier float64

	// Jitter - добавлять случайность к задержке (0.0 - 1.0)
	// Помогает избежать "thundering herd" проблемы
	Jitter float64

	// RetryableErrors - список ошибок, для которых нужен retry
	// Пустой список = retry для всех ошибок
	RetryableErrors []string

	// OnRetry - callback функция, вызываемая перед каждым retry
	OnRetry func(attempt int, err error, delay time.Duration)

	// DeadLetterQueue - конфигурация DLQ для failed сообщений
	DLQ DLQConfig
}

// DLQConfig содержит конфигурацию Dead Letter Queue
type DLQConfig struct {
	// Enabled - включить DLQ
	Enabled bool

	// FilePath - путь к файлу для DLQ (если используется file-based DLQ)
	FilePath string

	// BrokerDLQ - отправлять в отдельную очередь брокера
	BrokerDLQ string

	// MaxSize - максимальный размер DLQ (в записях)
	// При превышении старые записи удаляются
	MaxSize int

	// RetentionPeriod - как долго хранить записи в DLQ
	RetentionPeriod time.Duration
}

// Validate проверяет корректность конфигурации
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil // Если не включено, валидация не нужна
	}

	if c.MaxAttempts < 0 {
		return fmt.Errorf("max_attempts must be >= 0, got %d", c.MaxAttempts)
	}

	if c.InitialDelay < 0 {
		return fmt.Errorf("initial_delay must be >= 0")
	}

	if c.MaxDelay < c.InitialDelay {
		return fmt.Errorf("max_delay (%v) must be >= initial_delay (%v)", c.MaxDelay, c.InitialDelay)
	}

	if c.BackoffStrategy != BackoffConstant &&
		c.BackoffStrategy != BackoffLinear &&
		c.BackoffStrategy != BackoffExponential {
		return fmt.Errorf("invalid backoff strategy: %s", c.BackoffStrategy)
	}

	if c.BackoffMultiplier <= 0 {
		c.BackoffMultiplier = 2.0 // Default
	}

	if c.Jitter < 0 || c.Jitter > 1.0 {
		return fmt.Errorf("jitter must be between 0.0 and 1.0, got %f", c.Jitter)
	}

	return nil
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig() Config {
	return Config{
		Enabled:           false,
		MaxAttempts:       3,
		InitialDelay:      1 * time.Second,
		MaxDelay:          30 * time.Second,
		BackoffStrategy:   BackoffExponential,
		BackoffMultiplier: 2.0,
		Jitter:            0.1,
		RetryableErrors:   []string{},
		OnRetry:           nil,
		DLQ: DLQConfig{
			Enabled:         false,
			FilePath:        "./dlq.json",
			MaxSize:         10000,
			RetentionPeriod: 7 * 24 * time.Hour, // 7 days
		},
	}
}

// EnableRetry создает конфигурацию с включенным retry
func EnableRetry(maxAttempts int, initialDelay time.Duration) Config {
	config := DefaultConfig()
	config.Enabled = true
	config.MaxAttempts = maxAttempts
	config.InitialDelay = initialDelay
	return config
}

// EnableRetryWithDLQ создает конфигурацию с retry и DLQ
func EnableRetryWithDLQ(maxAttempts int, initialDelay time.Duration, dlqPath string) Config {
	config := EnableRetry(maxAttempts, initialDelay)
	config.DLQ.Enabled = true
	config.DLQ.FilePath = dlqPath
	return config
}
