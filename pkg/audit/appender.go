package audit

import (
	"context"
)

// Appender - интерфейс для записи audit логов
type Appender interface {
	// Append - записать audit entry
	Append(ctx context.Context, entry *Entry) error

	// Close - закрыть appender
	Close() error
}

// MultiAppender - запись в несколько appenders
type MultiAppender struct {
	appenders []Appender
}

// NewMultiAppender - создать multi appender
func NewMultiAppender(appenders ...Appender) *MultiAppender {
	return &MultiAppender{
		appenders: appenders,
	}
}

// Append - записать в все appenders
func (ma *MultiAppender) Append(ctx context.Context, entry *Entry) error {
	var firstErr error

	for _, appender := range ma.appenders {
		if err := appender.Append(ctx, entry); err != nil && firstErr == nil {
			firstErr = err
			// Продолжаем записывать в остальные appenders
		}
	}

	return firstErr
}

// Close - закрыть все appenders
func (ma *MultiAppender) Close() error {
	var firstErr error

	for _, appender := range ma.appenders {
		if err := appender.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// Add - добавить appender
func (ma *MultiAppender) Add(appender Appender) {
	ma.appenders = append(ma.appenders, appender)
}
