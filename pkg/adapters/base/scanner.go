package base

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// ScanSQLRows scans database/sql rows into [][]string using the provided converter.
// dbType must match the converter's dbType parameter (e.g. "mssql", "sqlite", "mysql").
// This eliminates the duplicated scanRows pattern across sql-based adapters.
//
// Every column is scanned into `any`, including SQLite DATE/DATETIME/TIMESTAMP.
// There used to be a special case that bound those to *string, on the theory
// that it skipped modernc.parseTime. It did not: modernc keys its time parsing
// off the column's declared type — exactly the types the special case matched —
// so the driver still produced a time.Time and database/sql then formatted it
// back into the string with RFC3339Nano. The path cost an extra format and
// allocation per cell, left normalizeSQLiteDateTime dead (its input never had
// the space separator it looked for), carried the driver's own zone into a
// packet whose schema declares UTC, and — because a string cannot hold NULL —
// aborted the whole export with "converting NULL to string is unsupported" on
// the first NULL date.
func ScanSQLRows(rows *sql.Rows, schema packet.Schema, converter *UniversalTypeConverter, dbType string) ([][]string, error) {
	columnCount := len(schema.Fields)
	values := make([]any, columnCount)
	valuePtrs := make([]any, columnCount)
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	var result [][]string
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		row := make([]string, columnCount)
		for i, field := range schema.Fields {
			raw := converter.DBValueToString(values[i], field, dbType)
			if dbType == "sqlite" {
				if norm, ok := fastSQLiteDateTime(raw, field.Type); ok {
					row[i] = norm
					continue
				}
			}
			if _, isTime := values[i].(time.Time); isTime {
				// Драйвер отдал time.Time — DBValueToString уже собрал
				// канонический вид (RFC3339Nano для DATETIME/TIMESTAMP,
				// YYYY-MM-DD для DATE, маркер для no-date). Round-trip
				// ParseValue→FormatValue вернул бы ту же строку, а для
				// маркера ещё и записал бы ошибку разбора в лог.
				row[i] = raw
				continue
			}
			row[i] = converter.ConvertValueToTDTP(field, raw)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// fastSQLiteDateTime переводит сырую дату из SQLite в канонический вид TDTP
// одной склейкой строки, без разбора во время и обратной сборки.
//
// ReadAllRows берёт колонки дат через CAST(... AS TEXT), так что значение
// приезжает ровно тем текстом, каким записано: "YYYY-MM-DD" или
// "YYYY-MM-DD HH:MM:SS[.fff]". Round-trip ParseValue→FormatValue вернул бы для
// него ту же строку, потратив около 390 нс и три аллокации на ячейку; здесь
// выходит около 100 нс и одна.
//
// Именно под эту форму и писался прежний normalizeSQLiteDateTime — тот, что
// годами лежал мёртвым, потому что до него доходил уже RFC3339 от драйвера.
//
// ok=false означает "не берусь" и отправляет значение по обычному пути. Так
// закрыты все случаи, где простая склейка дала бы НЕ те байты, что даёт
// FormatTimestamp сегодня:
//
//   - своё смещение ("...+03:00") — его нужно привести к UTC, а не оставить;
//   - хвостовые нули в дробной части (".500") — RFC3339Nano их срезает;
//   - дробь длиннее девяти знаков — за пределами точности time.Time;
//   - любая форма, которую time.Parse отверг бы: не та раскладка, не те
//     разделители, несуществующие месяц, день, час, минута или секунда.
//
// Проверка календаря здесь полная, включая длину месяца и високосный год:
// без неё "2026-02-31 00:00:00" прошло бы склейкой, тогда как time.Parse его
// отвергает, и битое значение поехало бы в пакет как валидная метка времени.
func fastSQLiteDateTime(s, fieldType string) (string, bool) {
	if !packet.IsDateFieldType(fieldType) {
		return "", false
	}

	// Форма обязана соответствовать объявленному типу, иначе обычный путь
	// пришёл бы к другому ответу: дату без времени в колонке TIMESTAMP он
	// разворачивает в полночь ("2026-08-21" → "2026-08-21T00:00:00Z"), а
	// полную метку в колонке DATE не разбирает вовсе и отдаёт сырой.
	isDateOnly := schema.NormalizeType(schema.DataType(fieldType)) == schema.TypeDate

	// "YYYY-MM-DD" — уже канонический вид для DATE.
	if len(s) == 10 {
		if !isDateOnly || !validCivilDate(s) {
			return "", false
		}
		return s, true
	}

	// "YYYY-MM-DD HH:MM:SS" плюс необязательная дробная часть.
	if isDateOnly || len(s) < 19 || s[10] != ' ' ||
		!validCivilDate(s[:10]) || !validClockTime(s[11:19]) {
		return "", false
	}

	frac := s[19:]
	if len(frac) > 0 {
		// Только точка и от одной до девяти цифр, и последняя — не ноль:
		// RFC3339Nano хвостовые нули срезает, так что ".500" пришлось бы
		// укорачивать, а это уже не склейка.
		if len(frac) < 2 || len(frac) > 10 || frac[0] != '.' || frac[len(frac)-1] == '0' {
			return "", false
		}
		for i := 1; i < len(frac); i++ {
			if frac[i] < '0' || frac[i] > '9' {
				return "", false
			}
		}
	}

	out := make([]byte, 0, len(s)+1)
	out = append(out, s[:10]...)
	out = append(out, 'T')
	out = append(out, s[11:]...)
	out = append(out, 'Z')
	return string(out), true
}

// validCivilDate проверяет "YYYY-MM-DD" по символам и по календарю.
func validCivilDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	year, ok := atoiN(s[0:4])
	if !ok {
		return false
	}
	month, ok := atoiN(s[5:7])
	if !ok || month < 1 || month > 12 {
		return false
	}
	day, ok := atoiN(s[8:10])
	if !ok || day < 1 || day > daysInMonth(year, month) {
		return false
	}
	return true
}

// validClockTime проверяет "HH:MM:SS".
func validClockTime(s string) bool {
	if len(s) != 8 || s[2] != ':' || s[5] != ':' {
		return false
	}
	h, ok := atoiN(s[0:2])
	if !ok || h > 23 {
		return false
	}
	m, ok := atoiN(s[3:5])
	if !ok || m > 59 {
		return false
	}
	sec, ok := atoiN(s[6:8])
	// 60 не принимаем: time.Parse тоже не принимает — високосной секунды в
	// Go нет.
	return ok && sec <= 59
}

// atoiN разбирает короткую строку из одних цифр.
func atoiN(s string) (int, bool) {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

var monthDays = [13]int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

func daysInMonth(year, month int) int {
	if month == 2 && (year%4 == 0 && (year%100 != 0 || year%400 == 0)) {
		return 29
	}
	return monthDays[month]
}
