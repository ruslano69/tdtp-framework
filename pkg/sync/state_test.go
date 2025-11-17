package sync

import (
	"os"
	"testing"
	"time"
)

func TestStateManager_CreateAndLoad(t *testing.T) {
	stateFile := "test_state.json"
	defer os.Remove(stateFile)

	// Создаем менеджер
	sm, err := NewStateManager(stateFile, false)
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	// Обновляем состояние
	err = sm.UpdateState("users", "2024-01-15T10:30:00Z", 1000)
	if err != nil {
		t.Fatalf("Failed to update state: %v", err)
	}

	// Сохраняем
	err = sm.Save()
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Создаем новый менеджер и загружаем состояние
	sm2, err := NewStateManager(stateFile, false)
	if err != nil {
		t.Fatalf("Failed to create second state manager: %v", err)
	}

	// Проверяем что состояние загрузилось
	state := sm2.GetState("users")
	if state.LastSyncValue != "2024-01-15T10:30:00Z" {
		t.Errorf("Expected LastSyncValue '2024-01-15T10:30:00Z', got '%s'", state.LastSyncValue)
	}

	if state.RecordsExported != 1000 {
		t.Errorf("Expected RecordsExported 1000, got %d", state.RecordsExported)
	}
}

func TestStateManager_AutoSave(t *testing.T) {
	stateFile := "test_autosave_state.json"
	defer os.Remove(stateFile)

	// Создаем менеджер с autosave
	sm, err := NewStateManager(stateFile, true)
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	// Обновляем состояние (должно автоматически сохраниться)
	err = sm.UpdateState("orders", "2024-01-15T11:00:00Z", 500)
	if err != nil {
		t.Fatalf("Failed to update state: %v", err)
	}

	// Создаем новый менеджер и проверяем что файл существует и загружается
	sm2, err := NewStateManager(stateFile, false)
	if err != nil {
		t.Fatalf("Failed to create second state manager: %v", err)
	}

	state := sm2.GetState("orders")
	if state.LastSyncValue != "2024-01-15T11:00:00Z" {
		t.Errorf("Auto-save failed: expected LastSyncValue '2024-01-15T11:00:00Z', got '%s'", state.LastSyncValue)
	}
}

func TestStateManager_GetStateForNonExistentTable(t *testing.T) {
	stateFile := "test_nonexistent_state.json"
	defer os.Remove(stateFile)

	sm, err := NewStateManager(stateFile, false)
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	// Получаем состояние для несуществующей таблицы
	state := sm.GetState("nonexistent_table")

	// Должно вернуть новое пустое состояние
	if state.TableName != "nonexistent_table" {
		t.Errorf("Expected TableName 'nonexistent_table', got '%s'", state.TableName)
	}

	if state.LastSyncValue != "" {
		t.Errorf("Expected empty LastSyncValue, got '%s'", state.LastSyncValue)
	}

	if !state.LastSyncTime.IsZero() {
		t.Errorf("Expected zero LastSyncTime, got %v", state.LastSyncTime)
	}
}

func TestStateManager_Reset(t *testing.T) {
	stateFile := "test_reset_state.json"
	defer os.Remove(stateFile)

	sm, err := NewStateManager(stateFile, false)
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	// Добавляем состояния
	sm.UpdateState("table1", "value1", 100)
	sm.UpdateState("table2", "value2", 200)

	// Сбрасываем одну таблицу
	err = sm.Reset("table1")
	if err != nil {
		t.Fatalf("Failed to reset table1: %v", err)
	}

	// Проверяем что table1 сброшена
	state1 := sm.GetState("table1")
	if state1.LastSyncValue != "" {
		t.Errorf("Expected empty LastSyncValue after reset, got '%s'", state1.LastSyncValue)
	}

	// Проверяем что table2 осталась
	state2 := sm.GetState("table2")
	if state2.LastSyncValue != "value2" {
		t.Errorf("Expected LastSyncValue 'value2', got '%s'", state2.LastSyncValue)
	}
}

func TestStateManager_ResetAll(t *testing.T) {
	stateFile := "test_resetall_state.json"
	defer os.Remove(stateFile)

	sm, err := NewStateManager(stateFile, false)
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	// Добавляем несколько состояний
	sm.UpdateState("table1", "value1", 100)
	sm.UpdateState("table2", "value2", 200)
	sm.UpdateState("table3", "value3", 300)

	// Сбрасываем все
	err = sm.ResetAll()
	if err != nil {
		t.Fatalf("Failed to reset all: %v", err)
	}

	// Проверяем что все сброшено
	allStates := sm.GetAllStates()
	if len(allStates) != 0 {
		t.Errorf("Expected 0 states after ResetAll, got %d", len(allStates))
	}
}

func TestStateManager_UpdateStateWithError(t *testing.T) {
	stateFile := "test_error_state.json"
	defer os.Remove(stateFile)

	sm, err := NewStateManager(stateFile, false)
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	// Симулируем ошибку
	testErr := "connection timeout"
	err = sm.UpdateStateWithError("failed_table", &testError{testErr})
	if err != nil {
		t.Fatalf("Failed to update state with error: %v", err)
	}

	// Проверяем что ошибка сохранена
	state := sm.GetState("failed_table")
	if state.LastError != testErr {
		t.Errorf("Expected LastError '%s', got '%s'", testErr, state.LastError)
	}

	// Проверяем что LastSyncTime обновлено
	if state.LastSyncTime.IsZero() {
		t.Error("Expected non-zero LastSyncTime after error")
	}

	// Проверяем что время недавнее (в пределах 1 секунды)
	if time.Since(state.LastSyncTime) > time.Second {
		t.Errorf("LastSyncTime too old: %v", state.LastSyncTime)
	}
}

// testError - простая реализация error для тестов
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
