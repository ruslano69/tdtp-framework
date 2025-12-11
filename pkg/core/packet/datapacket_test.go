package packet

import (
	"reflect"
	"testing"
)

// TestDataPacket_GetRows тестирует метод GetRows
func TestDataPacket_GetRows(t *testing.T) {
	tests := []struct {
		name     string
		rows     [][]string
		expected [][]string
	}{
		{
			name: "simple values",
			rows: [][]string{
				{"1", "John", "john@example.com"},
				{"2", "Jane", "jane@example.com"},
			},
			expected: [][]string{
				{"1", "John", "john@example.com"},
				{"2", "Jane", "jane@example.com"},
			},
		},
		{
			name: "values with pipes",
			rows: [][]string{
				{"1", "path|to|file", "value3"},
				{"2", "a|b|c", "value6"},
			},
			expected: [][]string{
				{"1", "path|to|file", "value3"},
				{"2", "a|b|c", "value6"},
			},
		},
		{
			name: "values with backslashes",
			rows: [][]string{
				{"1", `C:\Windows\System32`, "normal"},
				{"2", `D:\path\to\file`, "value2"},
			},
			expected: [][]string{
				{"1", `C:\Windows\System32`, "normal"},
				{"2", `D:\path\to\file`, "value2"},
			},
		},
		{
			name: "values with pipes and backslashes",
			rows: [][]string{
				{"1", `C:\path|to|file`, "normal"},
				{"2", `path\|with\|escapes`, `C:\\Windows`},
			},
			expected: [][]string{
				{"1", `C:\path|to|file`, "normal"},
				{"2", `path\|with\|escapes`, `C:\\Windows`},
			},
		},
		{
			name: "empty values",
			rows: [][]string{
				{"", "value2", ""},
				{"value1", "", "value3"},
			},
			expected: [][]string{
				{"", "value2", ""},
				{"value1", "", "value3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Создаем пакет
			pkt := NewDataPacket(TypeReference, "test_table")

			// Устанавливаем данные
			pkt.SetRows(tt.rows)

			// Проверяем что RecordsInPart установлен правильно
			if pkt.Header.RecordsInPart != len(tt.rows) {
				t.Errorf("RecordsInPart = %d, want %d", pkt.Header.RecordsInPart, len(tt.rows))
			}

			// Извлекаем данные обратно
			result := pkt.GetRows()

			// Проверяем что данные совпадают
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("GetRows() returned incorrect data")
				for i := range result {
					if i < len(tt.expected) {
						if !reflect.DeepEqual(result[i], tt.expected[i]) {
							t.Errorf("  Row %d: got %v, want %v", i, result[i], tt.expected[i])
						}
					} else {
						t.Errorf("  Extra row %d: %v", i, result[i])
					}
				}
			}
		})
	}
}

// TestDataPacket_RoundTrip тестирует полный цикл: SetRows → GetRows
func TestDataPacket_RoundTrip(t *testing.T) {
	originalData := [][]string{
		{"1", "Test|Value", `C:\Path`, "normal"},
		{"2", "Another|One", `D:\Windows\System32`, "test"},
		{"3", `Complex\|Escape`, "simple", ""},
	}

	// Создаем пакет и устанавливаем данные
	pkt := NewDataPacket(TypeReference, "test_table")
	pkt.SetRows(originalData)

	// Извлекаем данные обратно
	result := pkt.GetRows()

	// Проверяем что данные совпадают
	if !reflect.DeepEqual(result, originalData) {
		t.Errorf("Round-trip failed")
		t.Logf("Original: %v", originalData)
		t.Logf("Result:   %v", result)

		for i := range result {
			if i < len(originalData) {
				if !reflect.DeepEqual(result[i], originalData[i]) {
					t.Errorf("  Row %d differs:", i)
					t.Errorf("    Original: %v", originalData[i])
					t.Errorf("    Result:   %v", result[i])
				}
			}
		}
	}
}

// TestRowsToData тестирует публичную функцию RowsToData
func TestRowsToData(t *testing.T) {
	rows := [][]string{
		{"1", "value1"},
		{"2", "value|with|pipes"},
		{"3", `C:\Windows`},
	}

	data := RowsToData(rows)

	if len(data.Rows) != len(rows) {
		t.Errorf("RowsToData created %d rows, want %d", len(data.Rows), len(rows))
	}

	// Проверяем что значения правильно экранированы
	parser := NewParser()
	for i, row := range data.Rows {
		fields := parser.GetRowValues(row)
		if !reflect.DeepEqual(fields, rows[i]) {
			t.Errorf("Row %d: got %v, want %v", i, fields, rows[i])
		}
	}
}
