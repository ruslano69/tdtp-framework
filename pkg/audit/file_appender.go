package audit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileAppender - запись в файл
type FileAppender struct {
	mu          sync.Mutex
	file        *os.File
	filePath    string
	maxSize     int64 // Максимальный размер файла в байтах
	maxBackups  int   // Количество backup файлов
	currentSize int64
	level       Level
	formatJSON  bool
}

// FileAppenderConfig - конфигурация file appender
type FileAppenderConfig struct {
	FilePath   string
	MaxSize    int64 // В мегабайтах
	MaxBackups int
	Level      Level
	FormatJSON bool
}

// NewFileAppender - создать file appender
func NewFileAppender(config FileAppenderConfig) (*FileAppender, error) {
	// Создаем директорию если не существует
	dir := filepath.Dir(config.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Открываем файл
	file, err := os.OpenFile(config.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit file: %w", err)
	}

	// Получаем размер файла
	fileInfo, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	maxSize := config.MaxSize
	if maxSize == 0 {
		maxSize = 100 // По умолчанию 100 MB
	}

	maxBackups := config.MaxBackups
	if maxBackups == 0 {
		maxBackups = 5 // По умолчанию 5 backup файлов
	}

	return &FileAppender{
		file:        file,
		filePath:    config.FilePath,
		maxSize:     maxSize * 1024 * 1024, // Конвертируем MB в bytes
		maxBackups:  maxBackups,
		currentSize: fileInfo.Size(),
		level:       config.Level,
		formatJSON:  config.FormatJSON,
	}, nil
}

// Append - записать entry в файл
func (fa *FileAppender) Append(ctx context.Context, entry *Entry) error {
	fa.mu.Lock()
	defer fa.mu.Unlock()

	// Фильтруем по уровню
	filtered := entry.FilterByLevel(fa.level)

	var data []byte
	var err error

	if fa.formatJSON {
		data, err = filtered.ToJSON()
		if err != nil {
			return fmt.Errorf("failed to marshal entry: %w", err)
		}
		data = append(data, '\n')
	} else {
		data = []byte(filtered.String() + "\n")
	}

	// Проверяем нужна ли ротация
	if fa.currentSize+int64(len(data)) > fa.maxSize {
		if err := fa.rotate(); err != nil {
			return fmt.Errorf("failed to rotate file: %w", err)
		}
	}

	// Записываем
	n, err := fa.file.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write entry: %w", err)
	}

	fa.currentSize += int64(n)
	return nil
}

// rotate - ротация файлов
func (fa *FileAppender) rotate() error {
	// Закрываем текущий файл
	if err := fa.file.Close(); err != nil {
		return err
	}

	// Переименовываем существующие backup файлы
	for i := fa.maxBackups - 1; i > 0; i-- {
		oldPath := fmt.Sprintf("%s.%d", fa.filePath, i)
		newPath := fmt.Sprintf("%s.%d", fa.filePath, i+1)

		if _, err := os.Stat(oldPath); err == nil {
			if i+1 > fa.maxBackups {
				// Удаляем самый старый файл
				os.Remove(newPath)
			}
			os.Rename(oldPath, newPath)
		}
	}

	// Переименовываем текущий файл
	backupPath := fmt.Sprintf("%s.1", fa.filePath)
	if err := os.Rename(fa.filePath, backupPath); err != nil {
		return err
	}

	// Создаем новый файл
	file, err := os.OpenFile(fa.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	fa.file = file
	fa.currentSize = 0

	return nil
}

// Close - закрыть файл
func (fa *FileAppender) Close() error {
	fa.mu.Lock()
	defer fa.mu.Unlock()

	if fa.file != nil {
		return fa.file.Close()
	}

	return nil
}

// Flush - сбросить буфер
func (fa *FileAppender) Flush() error {
	fa.mu.Lock()
	defer fa.mu.Unlock()

	if fa.file != nil {
		return fa.file.Sync()
	}

	return nil
}

// CurrentSize - текущий размер файла
func (fa *FileAppender) CurrentSize() int64 {
	fa.mu.Lock()
	defer fa.mu.Unlock()
	return fa.currentSize
}

// FilePath - путь к файлу
func (fa *FileAppender) FilePath() string {
	return fa.filePath
}

// ConsoleAppender - запись в console (stdout/stderr)
type ConsoleAppender struct {
	level      Level
	formatJSON bool
	useStderr  bool
}

// NewConsoleAppender - создать console appender
func NewConsoleAppender(level Level, formatJSON bool) *ConsoleAppender {
	return &ConsoleAppender{
		level:      level,
		formatJSON: formatJSON,
		useStderr:  false,
	}
}

// Append - записать в console
func (ca *ConsoleAppender) Append(ctx context.Context, entry *Entry) error {
	filtered := entry.FilterByLevel(ca.level)

	var output string
	if ca.formatJSON {
		data, err := filtered.ToJSONIndent()
		if err != nil {
			return err
		}
		output = string(data)
	} else {
		output = filtered.String()
	}

	if ca.useStderr && entry.Status == StatusFailure {
		fmt.Fprintln(os.Stderr, output)
	} else {
		fmt.Println(output)
	}

	return nil
}

// Close - закрыть console appender (noop)
func (ca *ConsoleAppender) Close() error {
	return nil
}

// NullAppender - пустой appender (для тестов)
type NullAppender struct{}

// NewNullAppender - создать null appender
func NewNullAppender() *NullAppender {
	return &NullAppender{}
}

// Append - ничего не делает
func (na *NullAppender) Append(ctx context.Context, entry *Entry) error {
	return nil
}

// Close - ничего не делает
func (na *NullAppender) Close() error {
	return nil
}
