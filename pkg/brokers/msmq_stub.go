//go:build !windows
// +build !windows

package brokers

import (
	"context"
	"fmt"
	"runtime"
)

// MSMQ заглушка для не-Windows платформ
type MSMQ struct {
	config Config
}

// NewMSMQ создает новый MSMQ брокер (заглушка для не-Windows)
// Валидирует конфигурацию даже на не-Windows платформах
func NewMSMQ(cfg Config) (*MSMQ, error) {
	// Валидация конфига работает на всех платформах!
	if cfg.QueuePath == "" {
		return nil, fmt.Errorf("queue_path is required for MSMQ (example: \".\\private$\\tdtp_export\")")
	}

	// Проверяем, что пытаются запустить на правильной ОС
	return nil, fmt.Errorf("MSMQ is only supported on Windows (current OS: %s). Please use RabbitMQ or Kafka on Unix systems", runtime.GOOS)
}

// Connect заглушка
func (m *MSMQ) Connect(ctx context.Context) error {
	return fmt.Errorf("MSMQ is only supported on Windows (current OS: %s)", runtime.GOOS)
}

// Close заглушка
func (m *MSMQ) Close() error {
	return nil // Ничего не делаем, но не ошибка
}

// Send заглушка
func (m *MSMQ) Send(ctx context.Context, message []byte) error {
	return fmt.Errorf("MSMQ is only supported on Windows (current OS: %s)", runtime.GOOS)
}

// Receive заглушка
func (m *MSMQ) Receive(ctx context.Context) ([]byte, error) {
	return nil, fmt.Errorf("MSMQ is only supported on Windows (current OS: %s)", runtime.GOOS)
}

// Ping заглушка
func (m *MSMQ) Ping(ctx context.Context) error {
	return fmt.Errorf("MSMQ is only supported on Windows (current OS: %s)", runtime.GOOS)
}

// GetBrokerType возвращает тип брокера
func (m *MSMQ) GetBrokerType() string {
	return "msmq"
}
