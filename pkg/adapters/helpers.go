package adapters

import (
	"database/sql"
	"fmt"

	"github.com/queuebridge/tdtp/pkg/core/packet"
)

// ============================================================================
// Error Handling Helpers
// ============================================================================
// Найдено funcstat: Errorf вызывается 7-13 раз в каждом адаптере
// Экономия: ~40 строк дублированного кода

// WrapDBError оборачивает ошибку БД с контекстом операции
// Вызывается только когда err != nil, проверка снаружи
func WrapDBError(operation string, err error) error {
	return fmt.Errorf("%s failed: %w", operation, err)
}

// WrapTableError оборачивает ошибку с информацией о таблице
// Вызывается только когда err != nil, проверка снаружи
func WrapTableError(operation, tableName string, err error) error {
	return fmt.Errorf("%s for table %s failed: %w", operation, tableName, err)
}

// WrapQueryError оборачивает ошибку с информацией о запросе
// Вызывается только когда err != nil, проверка снаружи
func WrapQueryError(operation, query string, err error) error {
	// Обрезаем длинные запросы для читаемости
	maxLen := 100
	if len(query) > maxLen {
		query = query[:maxLen] + "..."
	}
	return fmt.Errorf("%s failed (query: %s): %w", operation, query, err)
}

// ============================================================================
// Row Scanning Helpers
// ============================================================================
// Найдено funcstat: Scan вызывается 3-4 раза в каждом адаптере
// Экономия: ~30 строк дублированного кода

// ScanRow сканирует строку БД в массив string согласно схеме
// Универсальная функция для всех адаптеров
func ScanRow(rows *sql.Rows, schema packet.Schema) ([]string, error) {
	// Создаём слайс для сканирования
	values := make([]interface{}, len(schema.Fields))
	scanDest := make([]interface{}, len(schema.Fields))

	for i := range values {
		scanDest[i] = &values[i]
	}

	// Сканируем строку
	if err := rows.Scan(scanDest...); err != nil {
		return nil, WrapDBError("scan row", err)
	}

	// Конвертируем в строки
	result := make([]string, len(values))
	for i, val := range values {
		result[i] = ValueToString(val)
	}

	return result, nil
}

// ValueToString конвертирует database value в string
// Обрабатывает nil, []byte, и стандартные типы
func ValueToString(val interface{}) string {
	if val == nil {
		return ""
	}

	switch v := val.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%g", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ============================================================================
// Data Collection Helpers
// ============================================================================

// CollectRows собирает все строки из sql.Rows в [][]string
// Оптимизирован с pre-allocation для производительности
func CollectRows(rows *sql.Rows, schema packet.Schema, estimatedRows int) ([][]string, error) {
	// Pre-allocate с оценочной capacity (оптимизация из funcstat анализа)
	var result [][]string
	if estimatedRows > 0 {
		result = make([][]string, 0, estimatedRows)
	} else {
		result = make([][]string, 0, 100) // Разумное значение по умолчанию
	}

	for rows.Next() {
		row, err := ScanRow(rows, schema)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, WrapDBError("iterate rows", err)
	}

	return result, nil
}

// ============================================================================
// Validation Helpers
// ============================================================================

// ValidateTableName проверяет имя таблицы на безопасность
// Предотвращает SQL injection через имя таблицы
func ValidateTableName(tableName string) error {
	if tableName == "" {
		return fmt.Errorf("table name cannot be empty")
	}

	// Базовая проверка на допустимые символы
	// Разрешены: буквы, цифры, подчёркивание, точка (для schema.table)
	for _, r := range tableName {
		if !((r >= 'a' && r <= 'z') ||
		     (r >= 'A' && r <= 'Z') ||
		     (r >= '0' && r <= '9') ||
		     r == '_' || r == '.') {
			return fmt.Errorf("invalid character in table name: %c", r)
		}
	}

	return nil
}
