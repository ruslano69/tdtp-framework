// +build !windows

package brokers

import (
	"context"
	"fmt"
)

// MSMQ заглушка для не-Windows платформ
type MSMQ struct {
	config Config
}

// NewMSMQ создает новый MSMQ брокер (заглушка для не-Windows)
func NewMSMQ(cfg Config) (*MSMQ, error) {
	return nil, fmt.Errorf("MSMQ is only supported on Windows platforms")
}

// Connect заглушка
func (m *MSMQ) Connect(ctx context.Context) error {
	return fmt.Errorf("MSMQ is only supported on Windows platforms")
}

// Close заглушка
func (m *MSMQ) Close() error {
	return fmt.Errorf("MSMQ is only supported on Windows platforms")
}

// Send заглушка
func (m *MSMQ) Send(ctx context.Context, message []byte) error {
	return fmt.Errorf("MSMQ is only supported on Windows platforms")
}

// Receive заглушка
func (m *MSMQ) Receive(ctx context.Context) ([]byte, error) {
	return nil, fmt.Errorf("MSMQ is only supported on Windows platforms")
}

// Ping заглушка
func (m *MSMQ) Ping(ctx context.Context) error {
	return fmt.Errorf("MSMQ is only supported on Windows platforms")
}

// GetBrokerType возвращает тип брокера
func (m *MSMQ) GetBrokerType() string {
	return "msmq"
}
