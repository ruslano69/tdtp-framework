package packet

import (
	"strings"
	"testing"
)

// BenchmarkGetRowValues тестирует производительность парсинга строк
func BenchmarkGetRowValues(b *testing.B) {
	parser := NewParser()

	testCases := []struct {
		name string
		row  Row
	}{
		{
			name: "simple_10_fields",
			row:  Row{Value: "value1|value2|value3|value4|value5|value6|value7|value8|value9|value10"},
		},
		{
			name: "with_escaping",
			row:  Row{Value: "path\\|to\\|file|another\\\\value|normal|test\\|data|more|fields|here|last"},
		},
		{
			name: "long_fields_50_chars",
			row:  Row{Value: strings.Repeat("a", 50) + "|" + strings.Repeat("b", 50) + "|" + strings.Repeat("c", 50)},
		},
		{
			name: "many_fields_100",
			row:  Row{Value: strings.Repeat("field|", 99) + "field"},
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = parser.GetRowValues(tc.row)
			}
		})
	}
}

// BenchmarkGetRowValues_Comparison сравнивает разные размеры данных
func BenchmarkGetRowValues_Comparison(b *testing.B) {
	parser := NewParser()

	sizes := []int{10, 50, 100, 500}

	for _, size := range sizes {
		fields := make([]string, size)
		for i := 0; i < size; i++ {
			fields[i] = "field_value"
		}
		row := Row{Value: strings.Join(fields, "|")}

		b.Run("fields_"+string(rune('0'+size/10)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = parser.GetRowValues(row)
			}
		})
	}
}
