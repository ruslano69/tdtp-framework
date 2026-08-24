package base

import (
	"database/sql"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ColumnBlock — таблица, разложенная по колонкам: Values[c][r] это ячейка
// строки r в колонке c. Names идёт параллельно Values и повторяет порядок
// schema.Fields.
//
// ПРОТОТИП. Ничем в рабочем пути ещё не используется; смысл в том, чтобы
// померить, что даёт колоночная раскладка, не переписывая формат.
type ColumnBlock struct {
	Names  []string
	Values [][]string
	Rows   int
}

// ScanSQLColumns читает sql.Rows сразу по колонкам.
//
// Отличие от ScanSQLRows не в порядке байт, а в том, что аллоцируется. Строчный
// сканер делает по []string на строку — сто тысяч строк это сто тысяч мелких
// срезов, каждый со своим заголовком и своей долей работы для сборщика мусора.
// Здесь срезов ровно столько, сколько колонок, и каждый выделен на всю высоту
// таблицы разом.
//
// Ячейки проходят через тот же cellToTDTP, что и строчный путь, поэтому
// значения совпадают байт в байт — на это есть тест.
func ScanSQLColumns(rows *sql.Rows, schema packet.Schema, converter *UniversalTypeConverter, dbType string, hint int) (*ColumnBlock, error) {
	columnCount := len(schema.Fields)
	if hint <= 0 {
		hint = 1024
	}

	block := &ColumnBlock{
		Names:  make([]string, columnCount),
		Values: make([][]string, columnCount),
	}
	for i, field := range schema.Fields {
		block.Names[i] = field.Name
		block.Values[i] = make([]string, 0, hint)
	}

	values := make([]any, columnCount)
	valuePtrs := make([]any, columnCount)
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		for i, field := range schema.Fields {
			block.Values[i] = append(block.Values[i], cellToTDTP(values[i], field, converter, dbType))
		}
		block.Rows++
	}
	return block, rows.Err()
}

// ToRows разворачивает блок обратно в строчную матрицу.
//
// Нужен только для сверки с существующим путём и для тех потребителей, что
// ещё умеют лишь строки; в самом колоночном экспорте вызывать его незачем —
// он ровно та аллокация на строку, ради устранения которой всё и затевалось.
func (b *ColumnBlock) ToRows() [][]string {
	out := make([][]string, b.Rows)
	for r := 0; r < b.Rows; r++ {
		row := make([]string, len(b.Values))
		for c := range b.Values {
			row[c] = b.Values[c][r]
		}
		out[r] = row
	}
	return out
}
