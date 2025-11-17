package retry

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// DLQEntry представляет запись в Dead Letter Queue
type DLQEntry struct {
	ID          string      `json:"id"`
	Timestamp   time.Time   `json:"timestamp"`
	Attempts    int         `json:"attempts"`
	LastError   string      `json:"last_error"`
	FailureType string      `json:"failure_type"` // max_attempts_exceeded, context_cancelled, etc.
	Data        interface{} `json:"data,omitempty"`
}

// DLQ - Dead Letter Queue для хранения проблемных сообщений
type DLQ struct {
	mu      sync.RWMutex
	config  DLQConfig
	entries []DLQEntry
	counter int
}

// NewDLQ создает новый DLQ
func NewDLQ(config DLQConfig) (*DLQ, error) {
	dlq := &DLQ{
		config:  config,
		entries: make([]DLQEntry, 0),
		counter: 0,
	}

	// Загружаем существующий DLQ если файл существует
	if _, err := os.Stat(config.FilePath); err == nil {
		if err := dlq.Load(); err != nil {
			return nil, fmt.Errorf("failed to load DLQ: %w", err)
		}
	}

	return dlq, nil
}

// Add добавляет запись в DLQ
func (d *DLQ) Add(entry DLQEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.counter++
	entry.ID = fmt.Sprintf("dlq-%d-%d", time.Now().Unix(), d.counter)

	d.entries = append(d.entries, entry)

	// Проверяем лимит размера
	if d.config.MaxSize > 0 && len(d.entries) > d.config.MaxSize {
		// Удаляем самые старые записи
		d.entries = d.entries[len(d.entries)-d.config.MaxSize:]
	}

	// Автосохранение
	d.saveUnsafe()
}

// Get возвращает все записи из DLQ
func (d *DLQ) Get() []DLQEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Возвращаем копию
	result := make([]DLQEntry, len(d.entries))
	copy(result, d.entries)
	return result
}

// GetByID возвращает запись по ID
func (d *DLQ) GetByID(id string) *DLQEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for i := range d.entries {
		if d.entries[i].ID == id {
			entry := d.entries[i]
			return &entry
		}
	}

	return nil
}

// Remove удаляет запись из DLQ
func (d *DLQ) Remove(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i, entry := range d.entries {
		if entry.ID == id {
			d.entries = append(d.entries[:i], d.entries[i+1:]...)
			d.saveUnsafe()
			return true
		}
	}

	return false
}

// Clear очищает весь DLQ
func (d *DLQ) Clear() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.entries = make([]DLQEntry, 0)
	return d.saveUnsafe()
}

// CleanupOld удаляет устаревшие записи
func (d *DLQ) CleanupOld() int {
	if d.config.RetentionPeriod == 0 {
		return 0
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	cutoffTime := time.Now().Add(-d.config.RetentionPeriod)
	newEntries := make([]DLQEntry, 0, len(d.entries))
	removed := 0

	for _, entry := range d.entries {
		if entry.Timestamp.After(cutoffTime) {
			newEntries = append(newEntries, entry)
		} else {
			removed++
		}
	}

	if removed > 0 {
		d.entries = newEntries
		d.saveUnsafe()
	}

	return removed
}

// Size возвращает количество записей в DLQ
func (d *DLQ) Size() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.entries)
}

// Save сохраняет DLQ в файл
func (d *DLQ) Save() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.saveUnsafe()
}

// saveUnsafe сохраняет без блокировки (вызывается когда lock уже взят)
func (d *DLQ) saveUnsafe() error {
	data, err := json.MarshalIndent(d.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal DLQ: %w", err)
	}

	if err := os.WriteFile(d.config.FilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write DLQ file: %w", err)
	}

	return nil
}

// Load загружает DLQ из файла
func (d *DLQ) Load() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := os.ReadFile(d.config.FilePath)
	if err != nil {
		return fmt.Errorf("failed to read DLQ file: %w", err)
	}

	var entries []DLQEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("failed to unmarshal DLQ: %w", err)
	}

	d.entries = entries
	return nil
}

// GetStats возвращает статистику DLQ
func (d *DLQ) GetStats() DLQStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := DLQStats{
		TotalEntries: len(d.entries),
		FailureTypes: make(map[string]int),
	}

	if len(d.entries) == 0 {
		return stats
	}

	stats.OldestEntry = d.entries[0].Timestamp
	stats.NewestEntry = d.entries[len(d.entries)-1].Timestamp

	for _, entry := range d.entries {
		stats.FailureTypes[entry.FailureType]++
	}

	return stats
}

// DLQStats содержит статистику DLQ
type DLQStats struct {
	TotalEntries int
	OldestEntry  time.Time
	NewestEntry  time.Time
	FailureTypes map[string]int
}
