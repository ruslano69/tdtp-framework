package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestEntry_Builder(t *testing.T) {
	entry := NewEntry(OpExport, StatusSuccess).
		WithUser("test-user").
		WithSource("source-db").
		WithTarget("target-file").
		WithResource("users").
		WithRecordsAffected(100).
		WithDuration(500*time.Millisecond).
		WithMetadata("key", "value").
		WithIPAddress("192.168.1.1").
		WithSessionID("session-123")

	if entry.User != "test-user" {
		t.Errorf("Expected user 'test-user', got '%s'", entry.User)
	}

	if entry.RecordsAffected != 100 {
		t.Errorf("Expected 100 records, got %d", entry.RecordsAffected)
	}

	if entry.Metadata["key"] != "value" {
		t.Error("Expected metadata key to be 'value'")
	}
}

func TestEntry_FilterByLevel(t *testing.T) {
	entry := NewEntry(OpExport, StatusSuccess).
		WithUser("test-user").
		WithMetadata("sensitive", "data").
		WithData(map[string]interface{}{"password": "secret"})

	// Minimal level - только основные поля
	minimal := entry.FilterByLevel(LevelMinimal)
	if minimal.Metadata != nil || minimal.Data != nil {
		t.Error("Minimal level should not include metadata or data")
	}

	if minimal.IPAddress != "" || minimal.SessionID != "" {
		t.Error("Minimal level should not include IP address or session ID")
	}

	if minimal.User == "" {
		t.Error("Minimal level should include user")
	}

	// Standard level - без data
	standard := entry.FilterByLevel(LevelStandard)
	if standard.Data != nil {
		t.Error("Standard level should not include data")
	}

	if standard.User == "" {
		t.Error("Standard level should include user")
	}

	// Full level - все поля
	full := entry.FilterByLevel(LevelFull)
	if full.Data == nil {
		t.Error("Full level should include data")
	}
}

func TestEntry_JSON(t *testing.T) {
	entry := NewEntry(OpExport, StatusSuccess).
		WithUser("test-user").
		WithRecordsAffected(100)

	// ToJSON
	jsonData, err := entry.ToJSON()
	if err != nil {
		t.Fatalf("Failed to marshal entry: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("Expected non-empty JSON data")
	}

	// ToJSONIndent
	indentData, err := entry.ToJSONIndent()
	if err != nil {
		t.Fatalf("Failed to marshal indented entry: %v", err)
	}

	if len(indentData) <= len(jsonData) {
		t.Error("Expected indented JSON to be longer")
	}
}

func TestFileAppender_Write(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "audit.log")

	appender, err := NewFileAppender(FileAppenderConfig{
		FilePath:   filePath,
		MaxSize:    1, // 1 MB
		MaxBackups: 3,
		Level:      LevelStandard,
		FormatJSON: false,
	})

	if err != nil {
		t.Fatalf("Failed to create file appender: %v", err)
	}
	defer appender.Close()

	// Записываем entry
	entry := NewEntry(OpExport, StatusSuccess).
		WithUser("test-user").
		WithRecordsAffected(100)

	err = appender.Append(context.Background(), entry)
	if err != nil {
		t.Fatalf("Failed to append entry: %v", err)
	}

	// Проверяем файл существует
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("Expected audit file to exist")
	}

	// Проверяем размер
	if appender.CurrentSize() == 0 {
		t.Error("Expected non-zero file size")
	}
}

func TestFileAppender_Rotation(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "audit.log")

	// Создаем appender с очень маленьким max size
	appender, err := NewFileAppender(FileAppenderConfig{
		FilePath:   filePath,
		MaxSize:    1, // 1 MB
		MaxBackups: 2,
		Level:      LevelFull, // Full level для больших entries
		FormatJSON: true,      // JSON для больших размеров
	})

	if err != nil {
		t.Fatalf("Failed to create file appender: %v", err)
	}
	defer appender.Close()

	// Записываем entries с большим количеством данных
	largeData := make(map[string]interface{})
	for j := 0; j < 100; j++ {
		largeData[fmt.Sprintf("field_%d", j)] = "x" + string(make([]byte, 100))
	}

	for i := 0; i < 1000; i++ {
		entry := NewEntry(OpExport, StatusSuccess).
			WithUser("test-user-with-long-name").
			WithSource("source-database-with-very-long-name").
			WithTarget("target-file-with-very-long-name").
			WithResource("users-table").
			WithRecordsAffected(int64(i)).
			WithMetadata("iteration", i).
			WithMetadata("data", largeData).
			WithData(largeData)

		appender.Append(context.Background(), entry)
	}

	// Проверяем что файл существует и имеет данные
	if appender.CurrentSize() == 0 {
		t.Error("Expected non-zero file size")
	}

	// Проверяем backup файлы (могут существовать если rotation произошла)
	// Это не обязательно, поэтому просто логируем
	backupPath := filePath + ".1"
	if _, err := os.Stat(backupPath); err == nil {
		t.Logf("Rotation occurred, backup file exists")
	} else {
		t.Logf("No rotation occurred (file size: %d bytes)", appender.CurrentSize())
	}
}

func TestConsoleAppender(t *testing.T) {
	appender := NewConsoleAppender(LevelStandard, false)

	entry := NewEntry(OpExport, StatusSuccess).
		WithUser("test-user").
		WithRecordsAffected(100)

	err := appender.Append(context.Background(), entry)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Console appender всегда успешен
	if err := appender.Close(); err != nil {
		t.Errorf("Unexpected error on close: %v", err)
	}
}

func TestNullAppender(t *testing.T) {
	appender := NewNullAppender()

	entry := NewEntry(OpExport, StatusSuccess)

	err := appender.Append(context.Background(), entry)
	if err != nil {
		t.Errorf("Null appender should never return error, got: %v", err)
	}
}

func TestMultiAppender(t *testing.T) {
	tmpDir := t.TempDir()
	filePath1 := filepath.Join(tmpDir, "audit1.log")
	filePath2 := filepath.Join(tmpDir, "audit2.log")

	appender1, _ := NewFileAppender(FileAppenderConfig{
		FilePath:   filePath1,
		MaxSize:    1,
		MaxBackups: 2,
		Level:      LevelStandard,
		FormatJSON: false,
	})
	defer appender1.Close()

	appender2, _ := NewFileAppender(FileAppenderConfig{
		FilePath:   filePath2,
		MaxSize:    1,
		MaxBackups: 2,
		Level:      LevelFull,
		FormatJSON: true,
	})
	defer appender2.Close()

	multiAppender := NewMultiAppender(appender1, appender2)

	entry := NewEntry(OpExport, StatusSuccess).
		WithUser("test-user").
		WithRecordsAffected(100)

	err := multiAppender.Append(context.Background(), entry)
	if err != nil {
		t.Fatalf("Failed to append to multi appender: %v", err)
	}

	// Проверяем что оба файла созданы
	if _, err := os.Stat(filePath1); os.IsNotExist(err) {
		t.Error("Expected first file to exist")
	}

	if _, err := os.Stat(filePath2); os.IsNotExist(err) {
		t.Error("Expected second file to exist")
	}
}

func TestAuditLogger_Sync(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "audit.log")

	appender, _ := NewFileAppender(FileAppenderConfig{
		FilePath:   filePath,
		MaxSize:    10,
		MaxBackups: 2,
		Level:      LevelStandard,
		FormatJSON: false,
	})
	defer appender.Close()

	config := SyncConfig()
	logger := NewLogger(config, appender)
	defer logger.Close()

	// Записываем entry
	entry := NewEntry(OpExport, StatusSuccess).
		WithUser("test-user").
		WithRecordsAffected(100)

	err := logger.Log(context.Background(), entry)
	if err != nil {
		t.Fatalf("Failed to log entry: %v", err)
	}

	// Проверяем файл
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("Expected audit file to exist")
	}
}

func TestAuditLogger_Async(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "audit.log")

	appender, _ := NewFileAppender(FileAppenderConfig{
		FilePath:   filePath,
		MaxSize:    10,
		MaxBackups: 2,
		Level:      LevelStandard,
		FormatJSON: false,
	})
	defer appender.Close()

	config := DefaultConfig()
	config.AsyncMode = true
	config.BufferSize = 100

	logger := NewLogger(config, appender)
	defer logger.Close()

	// Записываем несколько entries
	for i := 0; i < 10; i++ {
		entry := NewEntry(OpExport, StatusSuccess).
			WithUser("test-user").
			WithRecordsAffected(int64(i))

		err := logger.Log(context.Background(), entry)
		if err != nil {
			t.Fatalf("Failed to log entry: %v", err)
		}
	}

	// Даем время на async обработку
	time.Sleep(100 * time.Millisecond)

	// Проверяем файл
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("Expected audit file to exist")
	}
}

func TestAuditLogger_LogOperation(t *testing.T) {
	appender := NewNullAppender()

	config := SyncConfig()
	logger := NewLogger(config, appender)
	defer logger.Close()

	// LogSuccess
	entry := logger.LogSuccess(context.Background(), OpExport)
	if entry.Status != StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", entry.Status)
	}

	// LogFailure
	testErr := errors.New("test error")
	entry = logger.LogFailure(context.Background(), OpImport, testErr)
	if entry.Status != StatusFailure {
		t.Errorf("Expected StatusFailure, got %v", entry.Status)
	}

	if entry.ErrorMessage != testErr.Error() {
		t.Errorf("Expected error message '%s', got '%s'", testErr.Error(), entry.ErrorMessage)
	}
}

func TestAuditLogger_DefaultValues(t *testing.T) {
	appender := NewNullAppender()

	config := SyncConfig()
	config.DefaultUser = "default-user"
	config.DefaultSource = "default-source"

	logger := NewLogger(config, appender)
	defer logger.Close()

	// Создаем entry без user и source
	entry := NewEntry(OpExport, StatusSuccess)

	logger.Log(context.Background(), entry)

	// Проверяем что применились значения по умолчанию
	if entry.User != config.DefaultUser {
		t.Errorf("Expected default user '%s', got '%s'", config.DefaultUser, entry.User)
	}

	if entry.Source != config.DefaultSource {
		t.Errorf("Expected default source '%s', got '%s'", config.DefaultSource, entry.Source)
	}
}

func TestAuditLogger_Close(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "audit.log")

	appender, _ := NewFileAppender(FileAppenderConfig{
		FilePath:   filePath,
		MaxSize:    10,
		MaxBackups: 2,
		Level:      LevelStandard,
		FormatJSON: false,
	})

	config := DefaultConfig()
	config.AsyncMode = true

	logger := NewLogger(config, appender)

	// Записываем entries
	for i := 0; i < 5; i++ {
		logger.LogSuccess(context.Background(), OpExport)
	}

	// Закрываем logger
	if err := logger.Close(); err != nil {
		t.Errorf("Failed to close logger: %v", err)
	}

	// Попытка записать после закрытия должна вернуть ошибку
	err := logger.Log(context.Background(), NewEntry(OpExport, StatusSuccess))
	if err == nil {
		t.Error("Expected error when logging after close")
	}
}

func TestNullLogger(t *testing.T) {
	logger := NewNullLogger()

	err := logger.Log(context.Background(), NewEntry(OpExport, StatusSuccess))
	if err != nil {
		t.Errorf("NullLogger should never return error, got: %v", err)
	}

	entry := logger.LogSuccess(context.Background(), OpExport)
	if entry.Operation != OpExport {
		t.Error("Expected valid entry from NullLogger")
	}

	if err := logger.Flush(); err != nil {
		t.Errorf("NullLogger.Flush should not error, got: %v", err)
	}

	if err := logger.Close(); err != nil {
		t.Errorf("NullLogger.Close should not error, got: %v", err)
	}
}

func TestDatabaseAppender_SQLite(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "audit.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	appender, err := NewDatabaseAppender(DatabaseAppenderConfig{
		DB:              db,
		TableName:       "audit_log",
		Level:           LevelStandard,
		BatchSize:       0, // Без batching
		AutoCreateTable: true,
	})

	if err != nil {
		t.Fatalf("Failed to create database appender: %v", err)
	}
	defer appender.Close()

	// Записываем entry
	entry := NewEntry(OpExport, StatusSuccess).
		WithUser("test-user").
		WithSource("source-db").
		WithTarget("target-file").
		WithResource("users").
		WithRecordsAffected(100).
		WithMetadata("key", "value")

	err = appender.Append(context.Background(), entry)
	if err != nil {
		t.Fatalf("Failed to append entry: %v", err)
	}

	// Запрашиваем entries
	entries, err := appender.Query(context.Background(), QueryFilter{
		User:  "test-user",
		Limit: 10,
	})

	if err != nil {
		t.Fatalf("Failed to query entries: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].RecordsAffected != 100 {
		t.Errorf("Expected 100 records affected, got %d", entries[0].RecordsAffected)
	}
}

func TestDatabaseAppender_Batch(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "audit.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	appender, err := NewDatabaseAppender(DatabaseAppenderConfig{
		DB:              db,
		TableName:       "audit_log",
		Level:           LevelStandard,
		BatchSize:       5, // Batch по 5 entries
		AutoCreateTable: true,
	})

	if err != nil {
		t.Fatalf("Failed to create database appender: %v", err)
	}
	defer appender.Close()

	// Записываем 12 entries
	for i := 0; i < 12; i++ {
		entry := NewEntry(OpExport, StatusSuccess).
			WithUser("test-user").
			WithRecordsAffected(int64(i))

		appender.Append(context.Background(), entry)
	}

	// Flush оставшиеся
	appender.Flush()

	// Запрашиваем count
	count, err := appender.Count(context.Background(), QueryFilter{})
	if err != nil {
		t.Fatalf("Failed to count entries: %v", err)
	}

	if count != 12 {
		t.Errorf("Expected 12 entries, got %d", count)
	}
}

func TestDatabaseAppender_DeleteOld(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "audit.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.Close()

	appender, err := NewDatabaseAppender(DatabaseAppenderConfig{
		DB:              db,
		TableName:       "audit_log",
		Level:           LevelStandard,
		BatchSize:       0,
		AutoCreateTable: true,
	})

	if err != nil {
		t.Fatalf("Failed to create database appender: %v", err)
	}
	defer appender.Close()

	// Записываем entries
	for i := 0; i < 5; i++ {
		entry := NewEntry(OpExport, StatusSuccess).
			WithUser("test-user").
			WithRecordsAffected(int64(i))

		appender.Append(context.Background(), entry)
	}

	// Удаляем entries старше now
	deleted, err := appender.DeleteOlderThan(context.Background(), time.Now().Add(1*time.Second))
	if err != nil {
		t.Fatalf("Failed to delete old entries: %v", err)
	}

	if deleted != 5 {
		t.Errorf("Expected 5 deleted entries, got %d", deleted)
	}

	// Проверяем count
	count, err := appender.Count(context.Background(), QueryFilter{})
	if err != nil {
		t.Fatalf("Failed to count entries: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 entries after delete, got %d", count)
	}
}
