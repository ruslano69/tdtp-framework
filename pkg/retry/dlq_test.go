package retry

import (
	"os"
	"testing"
	"time"
)

func TestDLQ_AddAndGet(t *testing.T) {
	dlqFile := "test_dlq_add.json"
	defer os.Remove(dlqFile)

	config := DLQConfig{
		FilePath: dlqFile,
		MaxSize:  100,
	}

	dlq, err := NewDLQ(config)
	if err != nil {
		t.Fatalf("Failed to create DLQ: %v", err)
	}

	// Добавляем запись
	entry := DLQEntry{
		Timestamp:   time.Now(),
		Attempts:    3,
		LastError:   "connection timeout",
		FailureType: "max_attempts_exceeded",
		Data:        map[string]string{"order_id": "12345"},
	}

	dlq.Add(entry)

	// Проверяем что запись добавлена
	entries := dlq.Get()
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].LastError != "connection timeout" {
		t.Errorf("Expected LastError 'connection timeout', got '%s'", entries[0].LastError)
	}

	// Проверяем что ID был сгенерирован
	if entries[0].ID == "" {
		t.Error("Expected non-empty ID")
	}
}

func TestDLQ_MaxSize(t *testing.T) {
	dlqFile := "test_dlq_maxsize.json"
	defer os.Remove(dlqFile)

	config := DLQConfig{
		FilePath: dlqFile,
		MaxSize:  3, // Ограничиваем размер
	}

	dlq, err := NewDLQ(config)
	if err != nil {
		t.Fatalf("Failed to create DLQ: %v", err)
	}

	// Добавляем 5 записей
	for i := 1; i <= 5; i++ {
		entry := DLQEntry{
			Timestamp:   time.Now(),
			Attempts:    i,
			LastError:   "error",
			FailureType: "test",
		}
		dlq.Add(entry)
		time.Sleep(10 * time.Millisecond) // Чтобы timestamp отличались
	}

	// Должно остаться только 3 последних записи
	entries := dlq.Get()
	if len(entries) != 3 {
		t.Errorf("Expected 3 entries (max size), got %d", len(entries))
	}

	// Проверяем что остались последние записи (с большими Attempts)
	if entries[0].Attempts < 3 {
		t.Errorf("Expected oldest remaining entry to have Attempts >= 3, got %d", entries[0].Attempts)
	}
}

func TestDLQ_SaveAndLoad(t *testing.T) {
	dlqFile := "test_dlq_saveload.json"
	defer os.Remove(dlqFile)

	config := DLQConfig{
		FilePath: dlqFile,
		MaxSize:  100,
	}

	// Создаем DLQ и добавляем записи
	dlq1, err := NewDLQ(config)
	if err != nil {
		t.Fatalf("Failed to create DLQ: %v", err)
	}

	dlq1.Add(DLQEntry{
		Timestamp:   time.Now(),
		Attempts:    3,
		LastError:   "error 1",
		FailureType: "type1",
	})

	dlq1.Add(DLQEntry{
		Timestamp:   time.Now(),
		Attempts:    5,
		LastError:   "error 2",
		FailureType: "type2",
	})

	// Создаем новый DLQ и загружаем данные
	dlq2, err := NewDLQ(config)
	if err != nil {
		t.Fatalf("Failed to create second DLQ: %v", err)
	}

	entries := dlq2.Get()
	if len(entries) != 2 {
		t.Errorf("Expected 2 entries after load, got %d", len(entries))
	}

	if entries[0].LastError != "error 1" {
		t.Errorf("Expected first entry error 'error 1', got '%s'", entries[0].LastError)
	}
}

func TestDLQ_GetByID(t *testing.T) {
	dlqFile := "test_dlq_getbyid.json"
	defer os.Remove(dlqFile)

	config := DLQConfig{
		FilePath: dlqFile,
		MaxSize:  100,
	}

	dlq, err := NewDLQ(config)
	if err != nil {
		t.Fatalf("Failed to create DLQ: %v", err)
	}

	// Добавляем запись
	dlq.Add(DLQEntry{
		Timestamp:   time.Now(),
		Attempts:    3,
		LastError:   "test error",
		FailureType: "test",
	})

	entries := dlq.Get()
	if len(entries) == 0 {
		t.Fatal("Expected at least one entry")
	}

	entryID := entries[0].ID

	// Получаем по ID
	entry := dlq.GetByID(entryID)
	if entry == nil {
		t.Fatal("Expected to find entry by ID")
	}

	if entry.ID != entryID {
		t.Errorf("Expected ID '%s', got '%s'", entryID, entry.ID)
	}

	// Пробуем несуществующий ID
	notFound := dlq.GetByID("nonexistent-id")
	if notFound != nil {
		t.Error("Expected nil for nonexistent ID")
	}
}

func TestDLQ_Remove(t *testing.T) {
	dlqFile := "test_dlq_remove.json"
	defer os.Remove(dlqFile)

	config := DLQConfig{
		FilePath: dlqFile,
		MaxSize:  100,
	}

	dlq, err := NewDLQ(config)
	if err != nil {
		t.Fatalf("Failed to create DLQ: %v", err)
	}

	// Добавляем две записи
	dlq.Add(DLQEntry{
		Timestamp:   time.Now(),
		Attempts:    3,
		LastError:   "error 1",
		FailureType: "type1",
	})

	dlq.Add(DLQEntry{
		Timestamp:   time.Now(),
		Attempts:    4,
		LastError:   "error 2",
		FailureType: "type2",
	})

	entries := dlq.Get()
	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	// Удаляем первую запись
	firstID := entries[0].ID
	removed := dlq.Remove(firstID)
	if !removed {
		t.Error("Expected successful removal")
	}

	// Проверяем что осталась только одна запись
	entriesAfter := dlq.Get()
	if len(entriesAfter) != 1 {
		t.Errorf("Expected 1 entry after removal, got %d", len(entriesAfter))
	}

	// Проверяем что осталась вторая запись
	if entriesAfter[0].LastError != "error 2" {
		t.Errorf("Expected remaining entry 'error 2', got '%s'", entriesAfter[0].LastError)
	}

	// Пробуем удалить несуществующую запись
	notRemoved := dlq.Remove("nonexistent-id")
	if notRemoved {
		t.Error("Expected false when removing nonexistent entry")
	}
}

func TestDLQ_Clear(t *testing.T) {
	dlqFile := "test_dlq_clear.json"
	defer os.Remove(dlqFile)

	config := DLQConfig{
		FilePath: dlqFile,
		MaxSize:  100,
	}

	dlq, err := NewDLQ(config)
	if err != nil {
		t.Fatalf("Failed to create DLQ: %v", err)
	}

	// Добавляем несколько записей
	for i := 0; i < 5; i++ {
		dlq.Add(DLQEntry{
			Timestamp:   time.Now(),
			Attempts:    i + 1,
			LastError:   "error",
			FailureType: "test",
		})
	}

	if dlq.Size() != 5 {
		t.Errorf("Expected 5 entries, got %d", dlq.Size())
	}

	// Очищаем DLQ
	err = dlq.Clear()
	if err != nil {
		t.Errorf("Failed to clear DLQ: %v", err)
	}

	if dlq.Size() != 0 {
		t.Errorf("Expected 0 entries after clear, got %d", dlq.Size())
	}
}

func TestDLQ_CleanupOld(t *testing.T) {
	dlqFile := "test_dlq_cleanup.json"
	defer os.Remove(dlqFile)

	config := DLQConfig{
		FilePath:        dlqFile,
		MaxSize:         100,
		RetentionPeriod: 100 * time.Millisecond, // Короткий период для теста
	}

	dlq, err := NewDLQ(config)
	if err != nil {
		t.Fatalf("Failed to create DLQ: %v", err)
	}

	// Добавляем старую запись (должна быть удалена)
	oldTime := time.Now().Add(-150 * time.Millisecond)
	dlq.Add(DLQEntry{
		Timestamp:   oldTime,
		Attempts:    3,
		LastError:   "old error",
		FailureType: "old",
	})

	// Добавляем свежую запись (должна остаться)
	newTime := time.Now().Add(-50 * time.Millisecond)
	dlq.Add(DLQEntry{
		Timestamp:   newTime,
		Attempts:    2,
		LastError:   "new error",
		FailureType: "new",
	})

	if dlq.Size() != 2 {
		t.Errorf("Expected 2 entries before cleanup, got %d", dlq.Size())
	}

	// Очищаем старые записи
	removed := dlq.CleanupOld()
	if removed != 1 {
		t.Errorf("Expected to remove 1 old entry, removed %d", removed)
	}

	// Проверяем что осталась только свежая запись
	entries := dlq.Get()
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry after cleanup, got %d", len(entries))
	}

	if len(entries) > 0 && entries[0].LastError != "new error" {
		t.Errorf("Expected remaining entry 'new error', got '%s'", entries[0].LastError)
	}
}

func TestDLQ_GetStats(t *testing.T) {
	dlqFile := "test_dlq_stats.json"
	defer os.Remove(dlqFile)

	config := DLQConfig{
		FilePath: dlqFile,
		MaxSize:  100,
	}

	dlq, err := NewDLQ(config)
	if err != nil {
		t.Fatalf("Failed to create DLQ: %v", err)
	}

	// Добавляем записи с разными типами ошибок
	dlq.Add(DLQEntry{
		Timestamp:   time.Now(),
		Attempts:    3,
		LastError:   "error 1",
		FailureType: "timeout",
	})

	time.Sleep(10 * time.Millisecond)

	dlq.Add(DLQEntry{
		Timestamp:   time.Now(),
		Attempts:    5,
		LastError:   "error 2",
		FailureType: "timeout",
	})

	time.Sleep(10 * time.Millisecond)

	dlq.Add(DLQEntry{
		Timestamp:   time.Now(),
		Attempts:    2,
		LastError:   "error 3",
		FailureType: "connection_refused",
	})

	// Получаем статистику
	stats := dlq.GetStats()

	if stats.TotalEntries != 3 {
		t.Errorf("Expected TotalEntries 3, got %d", stats.TotalEntries)
	}

	if stats.FailureTypes["timeout"] != 2 {
		t.Errorf("Expected 2 timeout failures, got %d", stats.FailureTypes["timeout"])
	}

	if stats.FailureTypes["connection_refused"] != 1 {
		t.Errorf("Expected 1 connection_refused failure, got %d", stats.FailureTypes["connection_refused"])
	}

	if stats.OldestEntry.IsZero() {
		t.Error("Expected non-zero OldestEntry")
	}

	if stats.NewestEntry.IsZero() {
		t.Error("Expected non-zero NewestEntry")
	}

	// NewestEntry должен быть позже OldestEntry
	if !stats.NewestEntry.After(stats.OldestEntry) {
		t.Error("Expected NewestEntry to be after OldestEntry")
	}
}

func TestDLQ_EmptyStats(t *testing.T) {
	dlqFile := "test_dlq_emptystats.json"
	defer os.Remove(dlqFile)

	config := DLQConfig{
		FilePath: dlqFile,
		MaxSize:  100,
	}

	dlq, err := NewDLQ(config)
	if err != nil {
		t.Fatalf("Failed to create DLQ: %v", err)
	}

	// Получаем статистику для пустого DLQ
	stats := dlq.GetStats()

	if stats.TotalEntries != 0 {
		t.Errorf("Expected TotalEntries 0, got %d", stats.TotalEntries)
	}

	if stats.OldestEntry != (time.Time{}) {
		t.Error("Expected zero OldestEntry for empty DLQ")
	}

	if stats.NewestEntry != (time.Time{}) {
		t.Error("Expected zero NewestEntry for empty DLQ")
	}

	if len(stats.FailureTypes) != 0 {
		t.Errorf("Expected empty FailureTypes map, got %d entries", len(stats.FailureTypes))
	}
}

func TestDLQ_Size(t *testing.T) {
	dlqFile := "test_dlq_size.json"
	defer os.Remove(dlqFile)

	config := DLQConfig{
		FilePath: dlqFile,
		MaxSize:  100,
	}

	dlq, err := NewDLQ(config)
	if err != nil {
		t.Fatalf("Failed to create DLQ: %v", err)
	}

	// Проверяем размер пустого DLQ
	if dlq.Size() != 0 {
		t.Errorf("Expected size 0, got %d", dlq.Size())
	}

	// Добавляем записи
	for i := 0; i < 10; i++ {
		dlq.Add(DLQEntry{
			Timestamp:   time.Now(),
			Attempts:    i + 1,
			LastError:   "error",
			FailureType: "test",
		})
	}

	if dlq.Size() != 10 {
		t.Errorf("Expected size 10, got %d", dlq.Size())
	}
}
