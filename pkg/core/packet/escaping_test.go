package packet

import (
	"encoding/xml"
	"reflect"
	"testing"
)

func TestEscapeValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no special chars",
			input:    "simple value",
			expected: "simple value",
		},
		{
			name:     "pipe only",
			input:    "path|to|file",
			expected: `path\|to\|file`,
		},
		{
			name:     "backslash only",
			input:    `C:\Windows\System32`,
			expected: `C:\\Windows\\System32`,
		},
		{
			name:     "pipe and backslash",
			input:    `C:\path|to|file`,
			expected: `C:\\path\|to\|file`,
		},
		{
			name:     "already escaped pipe",
			input:    `value\|with\|escaped`,
			expected: `value\\\|with\\\|escaped`, // \ → \\ затем | → \|
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeValue(tt.input)
			if result != tt.expected {
				t.Errorf("escapeValue(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetRowValues_Escaping(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name     string
		rowValue string
		expected []string
	}{
		{
			name:     "simple values",
			rowValue: "value1|value2|value3",
			expected: []string{"value1", "value2", "value3"},
		},
		{
			name:     "escaped pipe",
			rowValue: `path\|to\|file|value2|value3`,
			expected: []string{"path|to|file", "value2", "value3"},
		},
		{
			name:     "escaped backslash",
			rowValue: `C:\\Windows\\System32|value2`,
			expected: []string{`C:\Windows\System32`, "value2"},
		},
		{
			name:     "escaped pipe and backslash",
			rowValue: `C:\\path\|to\|file|value2`,
			expected: []string{`C:\path|to|file`, "value2"},
		},
		{
			name:     "multiple escapes in one field",
			rowValue: `value\\with\\backslashes\|and\|pipes|value2`,
			expected: []string{`value\with\backslashes|and|pipes`, "value2"},
		},
		{
			name:     "empty fields",
			rowValue: "|value2||value4",
			expected: []string{"", "value2", "", "value4"},
		},
		{
			name:     "single value no separator",
			rowValue: "single",
			expected: []string{"single"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := Row{Value: tt.rowValue}
			result := parser.GetRowValues(row)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("GetRowValues(%q) = %v, want %v", tt.rowValue, result, tt.expected)
			}
		})
	}
}

func TestRoundTrip_EscapeUnescape(t *testing.T) {
	// Тест полного цикла: данные → escape → XML → parse → unescape → данные
	generator := NewGenerator()
	parser := NewParser()

	testCases := []struct {
		name string
		rows [][]string
	}{
		{
			name: "simple values",
			rows: [][]string{
				{"value1", "value2", "value3"},
				{"a", "b", "c"},
			},
		},
		{
			name: "values with pipes",
			rows: [][]string{
				{"path|to|file", "normal", "value3"},
				{"a|b|c", "d|e|f", "g"},
			},
		},
		{
			name: "values with backslashes",
			rows: [][]string{
				{`C:\Windows\System32`, "normal", "value3"},
				{`D:\path\to\file`, "value2", "value3"},
			},
		},
		{
			name: "values with pipes and backslashes",
			rows: [][]string{
				{`C:\path|to|file`, "normal", "value3"},
				{`path\|with\|escapes`, `C:\\Windows`, "value3"},
			},
		},
		{
			name: "empty values",
			rows: [][]string{
				{"", "value2", ""},
				{"value1", "", "value3"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 1. Создаем схему
			schema := Schema{
				Fields: []Field{
					{Name: "field1", Type: "TEXT"},
					{Name: "field2", Type: "TEXT"},
					{Name: "field3", Type: "TEXT"},
				},
			}

			// 2. Генерируем пакет
			packets, err := generator.GenerateReference("test_table", schema, tc.rows)
			if err != nil {
				t.Fatalf("GenerateReference failed: %v", err)
			}

			// 3. Сериализуем в XML
			xmlData, err := xml.Marshal(packets[0])
			if err != nil {
				t.Fatalf("XML marshal failed: %v", err)
			}

			// 4. Парсим обратно
			parsedPacket, err := parser.ParseBytes(xmlData)
			if err != nil {
				t.Fatalf("ParseBytes failed: %v", err)
			}

			// 5. Извлекаем значения
			for i, row := range parsedPacket.Data.Rows {
				values := parser.GetRowValues(row)

				// 6. Сравниваем с оригиналом
				if !reflect.DeepEqual(values, tc.rows[i]) {
					t.Errorf("Row %d: got %v, want %v", i, values, tc.rows[i])
					t.Logf("  Original:    %v", tc.rows[i])
					t.Logf("  Row.Value:   %q", row.Value)
					t.Logf("  Parsed back: %v", values)
				}
			}
		})
	}
}

func TestEscaping_EdgeCases(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name     string
		rowValue string
		expected []string
		desc     string
	}{
		{
			name:     "trailing backslash",
			rowValue: `value1\`,
			expected: []string{`value1\`},
			desc:     "backslash at end should remain",
		},
		{
			name:     "double backslash at end",
			rowValue: `value1\\`,
			expected: []string{`value1\`},
			desc:     "escaped backslash at end",
		},
		{
			name:     "backslash before pipe",
			rowValue: `value1\|value2`,
			expected: []string{"value1|value2"},
			desc:     "escaped pipe should become regular pipe",
		},
		{
			name:     "three backslashes",
			rowValue: `value1\\\|value2`,
			expected: []string{`value1\|value2`},
			desc:     `\\\ should become \| (first two → \, third escapes pipe)`,
		},
		{
			name:     "multiple pipes",
			rowValue: "|||",
			expected: []string{"", "", "", ""},
			desc:     "three pipes create four empty fields",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := Row{Value: tt.rowValue}
			result := parser.GetRowValues(row)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("%s\n  GetRowValues(%q) = %v\n  want %v",
					tt.desc, tt.rowValue, result, tt.expected)
			}
		})
	}
}
