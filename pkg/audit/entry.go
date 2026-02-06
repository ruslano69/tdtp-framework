package audit

import (
	"encoding/json"
	"fmt"
	"time"
)

// Level - уровень детализации логирования
type Level int

const (
	// LevelMinimal - только основная информация
	LevelMinimal Level = iota

	// LevelStandard - стандартная информация
	LevelStandard

	// LevelFull - полная информация включая данные
	LevelFull
)

// String - строковое представление уровня
func (l Level) String() string {
	switch l {
	case LevelMinimal:
		return "minimal"
	case LevelStandard:
		return "standard"
	case LevelFull:
		return "full"
	default:
		return fmt.Sprintf("unknown(%d)", l)
	}
}

// Operation - тип операции
type Operation string

const (
	OpExport       Operation = "export"
	OpImport       Operation = "import"
	OpQuery        Operation = "query"
	OpCreate       Operation = "create"
	OpUpdate       Operation = "update"
	OpDelete       Operation = "delete"
	OpConnect      Operation = "connect"
	OpDisconnect   Operation = "disconnect"
	OpValidate     Operation = "validate"
	OpMask         Operation = "mask"
	OpNormalize    Operation = "normalize"
	OpTransform    Operation = "transform"
	OpSync         Operation = "sync" // Синхронизация данных
	OpAuthenticate Operation = "authenticate"
)

// Status - статус выполнения операции
type Status string

const (
	StatusSuccess Status = "success"
	StatusFailure Status = "failure"
	StatusPartial Status = "partial"
)

// Entry - запись в audit логе
type Entry struct {
	// ID - уникальный идентификатор записи
	ID string `json:"id"`

	// Timestamp - время операции
	Timestamp time.Time `json:"timestamp"`

	// Operation - тип операции
	Operation Operation `json:"operation"`

	// Status - статус выполнения
	Status Status `json:"status"`

	// User - пользователь или система
	User string `json:"user,omitempty"`

	// Source - источник данных
	Source string `json:"source,omitempty"`

	// Target - целевая система
	Target string `json:"target,omitempty"`

	// Resource - ресурс (таблица, файл, и т.д.)
	Resource string `json:"resource,omitempty"`

	// RecordsAffected - количество затронутых записей
	RecordsAffected int64 `json:"records_affected,omitempty"`

	// Duration - длительность операции
	Duration time.Duration `json:"duration,omitempty"`

	// ErrorMessage - сообщение об ошибке
	ErrorMessage string `json:"error_message,omitempty"`

	// Metadata - дополнительные метаданные
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Data - данные операции (только для LevelFull)
	Data interface{} `json:"data,omitempty"`

	// IPAddress - IP адрес источника
	IPAddress string `json:"ip_address,omitempty"`

	// SessionID - идентификатор сессии
	SessionID string `json:"session_id,omitempty"`
}

// NewEntry - создать новую audit запись
func NewEntry(operation Operation, status Status) *Entry {
	return &Entry{
		ID:        generateID(),
		Timestamp: time.Now(),
		Operation: operation,
		Status:    status,
		Metadata:  make(map[string]interface{}),
	}
}

// WithUser - установить пользователя
func (e *Entry) WithUser(user string) *Entry {
	e.User = user
	return e
}

// WithSource - установить источник
func (e *Entry) WithSource(source string) *Entry {
	e.Source = source
	return e
}

// WithTarget - установить целевую систему
func (e *Entry) WithTarget(target string) *Entry {
	e.Target = target
	return e
}

// WithResource - установить ресурс
func (e *Entry) WithResource(resource string) *Entry {
	e.Resource = resource
	return e
}

// WithRecordsAffected - установить количество записей
func (e *Entry) WithRecordsAffected(count int64) *Entry {
	e.RecordsAffected = count
	return e
}

// WithDuration - установить длительность
func (e *Entry) WithDuration(duration time.Duration) *Entry {
	e.Duration = duration
	return e
}

// WithError - установить ошибку
func (e *Entry) WithError(err error) *Entry {
	if err != nil {
		e.ErrorMessage = err.Error()
		e.Status = StatusFailure
	}
	return e
}

// WithMetadata - добавить метаданные
func (e *Entry) WithMetadata(key string, value interface{}) *Entry {
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	e.Metadata[key] = value
	return e
}

// WithData - установить данные операции
func (e *Entry) WithData(data interface{}) *Entry {
	e.Data = data
	return e
}

// WithIPAddress - установить IP адрес
func (e *Entry) WithIPAddress(ip string) *Entry {
	e.IPAddress = ip
	return e
}

// WithSessionID - установить ID сессии
func (e *Entry) WithSessionID(sessionID string) *Entry {
	e.SessionID = sessionID
	return e
}

// ToJSON - преобразовать в JSON
func (e *Entry) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// ToJSONIndent - преобразовать в форматированный JSON
func (e *Entry) ToJSONIndent() ([]byte, error) {
	return json.MarshalIndent(e, "", "  ")
}

// String - строковое представление
func (e *Entry) String() string {
	return fmt.Sprintf("[%s] %s %s %s (resource=%s, records=%d, duration=%v)",
		e.Timestamp.Format(time.RFC3339),
		e.Operation,
		e.Status,
		e.User,
		e.Resource,
		e.RecordsAffected,
		e.Duration,
	)
}

// Clone - создать копию записи
func (e *Entry) Clone() *Entry {
	clone := *e

	// Копируем map
	if e.Metadata != nil {
		clone.Metadata = make(map[string]interface{}, len(e.Metadata))
		for k, v := range e.Metadata {
			clone.Metadata[k] = v
		}
	}

	return &clone
}

// FilterByLevel - фильтрация данных по уровню
func (e *Entry) FilterByLevel(level Level) *Entry {
	filtered := e.Clone()

	switch level {
	case LevelMinimal:
		// Только основная информация
		filtered.Metadata = nil
		filtered.Data = nil
		filtered.IPAddress = ""
		filtered.SessionID = ""

	case LevelStandard:
		// Без чувствительных данных
		filtered.Data = nil

	case LevelFull:
		// Вся информация
		// Ничего не фильтруем
	}

	return filtered
}

// generateID - генерация уникального ID
func generateID() string {
	return fmt.Sprintf("audit-%d-%d",
		time.Now().UnixNano(),
		time.Now().Unix()%1000,
	)
}
