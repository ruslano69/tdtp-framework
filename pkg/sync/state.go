package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// SyncState представляет состояние синхронизации для конкретной таблицы
type SyncState struct {
	TableName      string    `json:"table_name"`
	LastSyncValue  string    `json:"last_sync_value"`  // Последнее значение tracking поля (timestamp, id, etc.)
	LastSyncTime   time.Time `json:"last_sync_time"`   // Время последней синхронизации
	RecordsExported int64     `json:"records_exported"` // Количество экспортированных записей
	LastError      string    `json:"last_error,omitempty"`
}

// StateManager управляет состоянием синхронизации для нескольких таблиц
type StateManager struct {
	mu         sync.RWMutex
	states     map[string]*SyncState // table_name -> state
	stateFile  string                // Путь к файлу состояния
	autoSave   bool                  // Автоматически сохранять при изменениях
}

// NewStateManager создает новый менеджер состояния
func NewStateManager(stateFile string, autoSave bool) (*StateManager, error) {
	sm := &StateManager{
		states:    make(map[string]*SyncState),
		stateFile: stateFile,
		autoSave:  autoSave,
	}

	// Загружаем существующее состояние если файл существует
	if _, err := os.Stat(stateFile); err == nil {
		if err := sm.Load(); err != nil {
			return nil, fmt.Errorf("failed to load state: %w", err)
		}
	}

	return sm, nil
}

// GetState возвращает состояние для таблицы
func (sm *StateManager) GetState(tableName string) *SyncState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	state, exists := sm.states[tableName]
	if !exists {
		// Возвращаем новое состояние если еще не было синхронизации
		return &SyncState{
			TableName:      tableName,
			LastSyncValue:  "",
			LastSyncTime:   time.Time{},
			RecordsExported: 0,
		}
	}

	// Возвращаем копию чтобы избежать race conditions
	stateCopy := *state
	return &stateCopy
}

// UpdateState обновляет состояние синхронизации
func (sm *StateManager) UpdateState(tableName string, lastSyncValue string, recordsExported int64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state := &SyncState{
		TableName:      tableName,
		LastSyncValue:  lastSyncValue,
		LastSyncTime:   time.Now(),
		RecordsExported: recordsExported,
	}

	sm.states[tableName] = state

	if sm.autoSave {
		return sm.saveUnsafe()
	}

	return nil
}

// UpdateStateWithError обновляет состояние с информацией об ошибке
func (sm *StateManager) UpdateStateWithError(tableName string, err error) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, exists := sm.states[tableName]
	if !exists {
		state = &SyncState{
			TableName: tableName,
		}
		sm.states[tableName] = state
	}

	state.LastSyncTime = time.Now()
	state.LastError = err.Error()

	if sm.autoSave {
		return sm.saveUnsafe()
	}

	return nil
}

// Reset сбрасывает состояние для таблицы (для полной ре-синхронизации)
func (sm *StateManager) Reset(tableName string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.states, tableName)

	if sm.autoSave {
		return sm.saveUnsafe()
	}

	return nil
}

// ResetAll сбрасывает все состояния
func (sm *StateManager) ResetAll() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.states = make(map[string]*SyncState)

	if sm.autoSave {
		return sm.saveUnsafe()
	}

	return nil
}

// Save сохраняет состояние в файл
func (sm *StateManager) Save() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	return sm.saveUnsafe()
}

// saveUnsafe сохраняет без блокировки (вызывается когда lock уже взят)
func (sm *StateManager) saveUnsafe() error {
	data, err := json.MarshalIndent(sm.states, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(sm.stateFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// Load загружает состояние из файла
func (sm *StateManager) Load() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	data, err := os.ReadFile(sm.stateFile)
	if err != nil {
		return fmt.Errorf("failed to read state file: %w", err)
	}

	states := make(map[string]*SyncState)
	if err := json.Unmarshal(data, &states); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	sm.states = states
	return nil
}

// GetAllStates возвращает все состояния
func (sm *StateManager) GetAllStates() map[string]*SyncState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Возвращаем копию
	result := make(map[string]*SyncState)
	for k, v := range sm.states {
		stateCopy := *v
		result[k] = &stateCopy
	}

	return result
}

// GetStatePath возвращает путь к файлу состояния
func (sm *StateManager) GetStatePath() string {
	return sm.stateFile
}
