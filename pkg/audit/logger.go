package audit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Logger - основной интерфейс для аудита
type Logger interface {
	Log(ctx context.Context, entry *Entry) error
	LogOperation(ctx context.Context, operation Operation, status Status) *Entry
	LogSuccess(ctx context.Context, operation Operation) *Entry
	LogFailure(ctx context.Context, operation Operation, err error) *Entry
	Flush() error
	Close() error
}

// AuditLogger - основной логгер аудита
type AuditLogger struct {
	appenders    []Appender
	asyncMode    bool
	bufferSize   int
	entryChannel chan *Entry
	wg           sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.RWMutex
	config       LoggerConfig
}

// LoggerConfig - конфигурация логгера
type LoggerConfig struct {
	// AsyncMode - асинхронная запись в appenders
	AsyncMode bool

	// BufferSize - размер буфера для асинхронного режима
	BufferSize int

	// DefaultLevel - уровень логирования по умолчанию
	DefaultLevel Level

	// DefaultUser - пользователь по умолчанию (если не указан в entry)
	DefaultUser string

	// DefaultSource - источник по умолчанию
	DefaultSource string

	// FlushInterval - интервал автоматического flush (0 = отключен)
	FlushInterval time.Duration

	// OnError - callback при ошибке записи
	OnError func(error)
}

// NewLogger - создать новый audit logger
func NewLogger(config LoggerConfig, appenders ...Appender) *AuditLogger {
	ctx, cancel := context.WithCancel(context.Background())

	logger := &AuditLogger{
		appenders:  appenders,
		asyncMode:  config.AsyncMode,
		bufferSize: config.BufferSize,
		ctx:        ctx,
		cancel:     cancel,
		config:     config,
	}

	// Устанавливаем значения по умолчанию
	if logger.bufferSize <= 0 {
		logger.bufferSize = 1000
	}

	if config.DefaultLevel == 0 {
		logger.config.DefaultLevel = LevelStandard
	}

	// Запускаем асинхронный режим если включен
	if logger.asyncMode {
		logger.entryChannel = make(chan *Entry, logger.bufferSize)
		logger.wg.Add(1)
		go logger.processEntries()
	}

	// Запускаем автоматический flush если настроен
	if config.FlushInterval > 0 {
		logger.wg.Add(1)
		go logger.autoFlush()
	}

	return logger
}

// Log - записать audit entry
func (l *AuditLogger) Log(ctx context.Context, entry *Entry) error {
	if entry == nil {
		return fmt.Errorf("entry is nil")
	}

	// Устанавливаем timestamp если не установлен
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	// Устанавливаем ID если не установлен
	if entry.ID == "" {
		entry.ID = generateID()
	}

	// Применяем значения по умолчанию
	if entry.User == "" && l.config.DefaultUser != "" {
		entry.User = l.config.DefaultUser
	}
	if entry.Source == "" && l.config.DefaultSource != "" {
		entry.Source = l.config.DefaultSource
	}

	// Асинхронный режим
	if l.asyncMode {
		select {
		case l.entryChannel <- entry:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-l.ctx.Done():
			return fmt.Errorf("logger is closed")
		default:
			// Буфер переполнен, записываем синхронно
			return l.writeEntry(ctx, entry)
		}
	}

	// Синхронный режим
	return l.writeEntry(ctx, entry)
}

// LogOperation - создать и записать entry для операции
func (l *AuditLogger) LogOperation(ctx context.Context, operation Operation, status Status) *Entry {
	entry := NewEntry(operation, status)

	if err := l.Log(ctx, entry); err != nil {
		l.handleError(err)
	}

	return entry
}

// LogSuccess - записать успешную операцию
func (l *AuditLogger) LogSuccess(ctx context.Context, operation Operation) *Entry {
	return l.LogOperation(ctx, operation, StatusSuccess)
}

// LogFailure - записать неудачную операцию
func (l *AuditLogger) LogFailure(ctx context.Context, operation Operation, err error) *Entry {
	entry := l.LogOperation(ctx, operation, StatusFailure)
	if err != nil {
		entry.ErrorMessage = err.Error()
	}
	return entry
}

// writeEntry - записать entry во все appenders
func (l *AuditLogger) writeEntry(ctx context.Context, entry *Entry) error {
	l.mu.RLock()
	appenders := l.appenders
	l.mu.RUnlock()

	var firstError error

	for _, appender := range appenders {
		if err := appender.Append(ctx, entry); err != nil {
			if firstError == nil {
				firstError = err
			}
			l.handleError(fmt.Errorf("appender failed: %w", err))
		}
	}

	return firstError
}

// processEntries - обработка entries в асинхронном режиме
func (l *AuditLogger) processEntries() {
	defer l.wg.Done()

	for {
		select {
		case entry := <-l.entryChannel:
			if err := l.writeEntry(context.Background(), entry); err != nil {
				l.handleError(err)
			}

		case <-l.ctx.Done():
			// Обрабатываем оставшиеся entries
			l.drainChannel()
			return
		}
	}
}

// drainChannel - обработать оставшиеся entries в канале
func (l *AuditLogger) drainChannel() {
	for {
		select {
		case entry := <-l.entryChannel:
			l.writeEntry(context.Background(), entry)
		default:
			return
		}
	}
}

// autoFlush - автоматический flush appenders
func (l *AuditLogger) autoFlush() {
	defer l.wg.Done()

	ticker := time.NewTicker(l.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.Flush()

		case <-l.ctx.Done():
			return
		}
	}
}

// Flush - сбросить буферы всех appenders
func (l *AuditLogger) Flush() error {
	l.mu.RLock()
	appenders := l.appenders
	l.mu.RUnlock()

	var firstError error

	for _, appender := range appenders {
		// Проверяем поддерживает ли appender flush
		if flusher, ok := appender.(interface{ Flush() error }); ok {
			if err := flusher.Flush(); err != nil {
				if firstError == nil {
					firstError = err
				}
				l.handleError(fmt.Errorf("flush failed: %w", err))
			}
		}
	}

	return firstError
}

// Close - закрыть logger и все appenders
func (l *AuditLogger) Close() error {
	// Останавливаем прием новых entries
	l.cancel()

	// Ждем обработки всех entries
	l.wg.Wait()

	// Flush перед закрытием
	l.Flush()

	// Закрываем appenders
	l.mu.RLock()
	appenders := l.appenders
	l.mu.RUnlock()

	var firstError error

	for _, appender := range appenders {
		if err := appender.Close(); err != nil {
			if firstError == nil {
				firstError = err
			}
			l.handleError(fmt.Errorf("close failed: %w", err))
		}
	}

	return firstError
}

// AddAppender - добавить appender
func (l *AuditLogger) AddAppender(appender Appender) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.appenders = append(l.appenders, appender)
}

// RemoveAppender - удалить appender
func (l *AuditLogger) RemoveAppender(appender Appender) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for i, a := range l.appenders {
		if a == appender {
			l.appenders = append(l.appenders[:i], l.appenders[i+1:]...)
			break
		}
	}
}

// handleError - обработка ошибки
func (l *AuditLogger) handleError(err error) {
	if l.config.OnError != nil {
		l.config.OnError(err)
	}
}

// DefaultConfig - конфигурация по умолчанию
func DefaultConfig() LoggerConfig {
	return LoggerConfig{
		AsyncMode:     true,
		BufferSize:    1000,
		DefaultLevel:  LevelStandard,
		FlushInterval: 0, // Отключен
		OnError:       nil,
	}
}

// SyncConfig - конфигурация для синхронного режима
func SyncConfig() LoggerConfig {
	return LoggerConfig{
		AsyncMode:     false,
		BufferSize:    0,
		DefaultLevel:  LevelStandard,
		FlushInterval: 0,
		OnError:       nil,
	}
}

// NullLogger - пустой logger (для тестов)
type NullLogger struct{}

// NewNullLogger - создать null logger
func NewNullLogger() *NullLogger {
	return &NullLogger{}
}

// Log - ничего не делает
func (nl *NullLogger) Log(ctx context.Context, entry *Entry) error {
	return nil
}

// LogOperation - ничего не делает
func (nl *NullLogger) LogOperation(ctx context.Context, operation Operation, status Status) *Entry {
	return NewEntry(operation, status)
}

// LogSuccess - ничего не делает
func (nl *NullLogger) LogSuccess(ctx context.Context, operation Operation) *Entry {
	return NewEntry(operation, StatusSuccess)
}

// LogFailure - ничего не делает
func (nl *NullLogger) LogFailure(ctx context.Context, operation Operation, err error) *Entry {
	return NewEntry(operation, StatusFailure)
}

// Flush - ничего не делает
func (nl *NullLogger) Flush() error {
	return nil
}

// Close - ничего не делает
func (nl *NullLogger) Close() error {
	return nil
}
