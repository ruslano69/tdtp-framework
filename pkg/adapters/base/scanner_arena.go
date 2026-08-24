package base

import (
	"database/sql"
	"fmt"
	"strconv"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// ColumnArena — колонка целиком в одном буфере.
//
// Buf хранит значения подряд, каждое завершается '\n'; Offsets[i] — начало
// значения i, длина Offsets равна числу строк плюс один. Границы задаются
// смещениями, а не разделителем, — '\n' лежит там для кодека, которому
// границы записей заметно помогают.
//
// ВНИМАНИЕ: значение может содержать свой '\n'. Смещения от этого не страдают,
// а вот один Buf как поток — да. Экранирование это забота формата (её делает
// writeEscaped на записи), и колоночному формату придётся либо экранировать,
// либо передавать Offsets рядом с данными. Прототип границы не размечает.
type ColumnArena struct {
	Buf     []byte
	Offsets []int32
}

// Len возвращает число значений.
func (c *ColumnArena) Len() int {
	if len(c.Offsets) == 0 {
		return 0
	}
	return len(c.Offsets) - 1
}

// At отдаёт значение без копирования — срез внутрь Buf, без завершающего '\n'.
func (c *ColumnArena) At(i int) []byte {
	return c.Buf[c.Offsets[i] : c.Offsets[i+1]-1]
}

// String отдаёт значение строкой, аллоцируя. Для сверки и для потребителей,
// которым нужна строка; в самом колоночном пути звать незачем.
func (c *ColumnArena) String(i int) string { return string(c.At(i)) }

// ArenaBlock — таблица, разложенная по аренам.
type ArenaBlock struct {
	Names   []string
	Columns []*ColumnArena
	Rows    int
}

// ScanSQLArena читает sql.Rows в поколоночные арены.
//
// Отличие от ScanSQLColumns в том, что здесь нет строки на ячейку. Колоночный
// сканер снял срез на строку, но оставил по string на значение — и именно они,
// а не срезы, составляли основную массу аллокаций. Здесь значение дописывается
// в буфер колонки прямо из того, что отдал драйвер.
//
// Быстрый путь берётся не всегда; там, где он не берётся, вызывается обычный
// cellToTDTP и его результат копируется в буфер. Так что выигрыш зависит от
// типов колонок, а корректность — нет.
func ScanSQLArena(rows *sql.Rows, sch packet.Schema, converter *UniversalTypeConverter, dbType string, rowHint int) (*ArenaBlock, error) {
	columnCount := len(sch.Fields)
	if rowHint <= 0 {
		rowHint = 1024
	}

	block := &ArenaBlock{
		Names:   make([]string, columnCount),
		Columns: make([]*ColumnArena, columnCount),
	}
	for i, field := range sch.Fields {
		block.Names[i] = field.Name
		offs := make([]int32, 1, rowHint+1)
		offs[0] = 0
		block.Columns[i] = &ColumnArena{
			// 24 байта на значение — грубая, но рабочая первая догадка:
			// ниже неё типичная ячейка не укладывается, а промах вверх стоит
			// одного удвоения, тогда как промах вниз стоит их логарифм.
			Buf:     make([]byte, 0, rowHint*24),
			Offsets: offs,
		}
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
		for i, field := range sch.Fields {
			col := block.Columns[i]
			col.Buf = appendCellTDTP(col.Buf, values[i], field, converter, dbType)
			col.Buf = append(col.Buf, '\n')
			col.Offsets = append(col.Offsets, int32(len(col.Buf)))
		}
		block.Rows++
	}
	return block, rows.Err()
}

// appendCellTDTP дописывает значение ячейки в dst, минуя промежуточную строку.
//
// Обязан выдавать ровно те байты, что cellToTDTP, — на этом стоит
// TestScanSQLArena_MatchesRowScan. Быстрые ветки взяты не наугад: каждая
// опирается на уже доказанное свойство обычного пути.
func appendCellTDTP(dst []byte, v any, field packet.Field, converter *UniversalTypeConverter, dbType string) []byte {
	if v == nil {
		return append(dst, NullSentinel...)
	}

	// Даты SQLite: appendFastSQLiteDateTime — та же функция, что зовёт
	// обычный путь, только пишущая в буфер. Если она не берётся, обычный путь
	// тоже не возьмётся и уйдёт в общую ветку ниже — ровно как здесь.
	if dbType == "sqlite" && packet.IsDateFieldType(field.Type) {
		if s, ok := v.(string); ok {
			if out, ok := appendFastSQLiteDateTime(dst, s, field.Type); ok {
				return out
			}
		} else if b, ok := v.([]byte); ok {
			if out, ok := appendFastSQLiteDateTime(dst, string(b), field.Type); ok {
				return out
			}
		}
		return append(dst, cellToTDTP(v, field, converter, dbType)...)
	}

	// Для TEXT, INTEGER и BOOLEAN ConvertValueToTDTP — тождество (см. его
	// fast path), а DBValueToString для перечисленных ниже типов сводится к
	// копированию байт или к strconv. Значит и то и другое можно дописать
	// сразу, без строки посередине.
	switch schema.NormalizeType(schema.DataType(field.Type)) {
	case schema.TypeText, schema.TypeInteger, schema.TypeBoolean:
		switch x := v.(type) {
		case string:
			return append(dst, x...)
		case []byte:
			// Ветка BLOB→base64 сюда не попадает: тип уже проверен и он не BLOB.
			return append(dst, x...)
		case int64:
			return strconv.AppendInt(dst, x, 10)
		case int32:
			return strconv.AppendInt(dst, int64(x), 10)
		case int:
			return strconv.AppendInt(dst, int64(x), 10)
		case bool:
			if x {
				return append(dst, '1')
			}
			return append(dst, '0')
		}
	}

	return append(dst, cellToTDTP(v, field, converter, dbType)...)
}
