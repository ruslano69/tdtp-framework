package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// pgTypedScanSupported сообщает, можно ли прочитать эту схему через typed
// Scan вместо rows.Values().
//
// rows.Values() платит дважды за каждую ячейку: DecodeValue декодирует в
// «правильный» Go-тип и тут же коробит результат в any для среза, который сам
// пересоздаётся на каждой строке (make([]any, 0, n) + append). Профиль на
// 100k строк × 9 колонок (id, три text, numeric, bool, три date/time) отдал
// этому 76% всех аллокаций чтения. rows.Scan в переиспользуемые типизированные
// приёмники — тот же приём, что дал SQLite CAST(...AS TEXT): декодер знает
// конечный тип заранее и пишет прямо в него, без общего прохода через
// TypeForOID.
//
// Список типов НАМЕРЕННО уже, чем TypeInteger/TypeReal в схеме:
//
//   - REAL/FLOAT/DOUBLE исключены целиком. TDTP не различает float4 и float8
//     в объявленном типе поля, а виджение float4→float64 меняет
//     форматирование: 1.1 как float4 печатается strconv.FormatFloat('f',-1,32)
//     как "1.1", а тот же бит-паттерн, поднятый до float64 и напечатанный
//     'f',-1,64, даёт "1.100000023841858" — другая строка на диске.
//     INTEGER такой ловушки не имеет: FormatInt не зависит от разрядности, и
//     int16/int32, поднятые до int64, дают тот же десятичный текст.
//   - TypeText принимается только с пустым Subtype. uuid/json/jsonb/inet/
//     cidr/macaddr/xml/array тоже мапятся в TypeText, но некоторые из них
//     PostgreSQL может отдать в бинарном формате колонки, который
//     pgtype.Text не обязан понимать, и pgValueToString для части из них
//     ждёт ровно тот []byte/map[string]any/[]any, что производит DecodeValue
//     — переиспользовать эти ветки без переисследования каждой мы не стали.
func pgTypedScanSupported(pkgSchema packet.Schema) bool {
	for _, f := range pkgSchema.Fields {
		switch schema.DataType(f.Type) {
		case schema.TypeInteger, schema.TypeInt, schema.TypeDecimal,
			schema.TypeBoolean, schema.TypeBool, schema.TypeDate:
			// без ограничений на Subtype
		case schema.TypeTimestamp:
			switch f.Subtype {
			case "", "time", "timestamptz":
			default:
				return false
			}
		case schema.TypeText:
			if f.Subtype != "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// readAllRowsTyped — то же самое, что readRowsWithSQL, для схем, прошедших
// pgTypedScanSupported. Декодирует каждую строку через rows.Scan в
// переиспользуемые типизированные приёмники, затем собирает те же формы any
// (time.Time / pgtype.InfinityModifier / pgtype.Numeric / pgtype.Time / ...),
// которые сегодня строит rows.Values() — так что дальше идёт НЕИЗМЕНЁННЫЙ
// pgCellToTDTP, и результат обязан быть побайтово тем же самым.
//
// Полей ровно len(pkgSchema.Fields) — иначе типизированный путь не
// применяется вовсе (см. вызывающий код в ReadAllRows).
func (a *Adapter) readAllRowsTyped(ctx context.Context, sqlQuery string, pkgSchema packet.Schema) ([][]string, error) {
	rows, err := a.pool.Query(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}
	defer rows.Close()

	n := len(pkgSchema.Fields)
	dests := make([]any, n)
	scanArgs := make([]any, n)
	for i, f := range pkgSchema.Fields {
		switch schema.DataType(f.Type) {
		case schema.TypeInteger, schema.TypeInt:
			dests[i] = new(pgtype.Int8)
		case schema.TypeDecimal:
			dests[i] = new(pgtype.Numeric)
		case schema.TypeBoolean, schema.TypeBool:
			dests[i] = new(pgtype.Bool)
		case schema.TypeDate:
			dests[i] = new(pgtype.Date)
		case schema.TypeTimestamp:
			switch f.Subtype {
			case "time":
				dests[i] = new(pgtype.Time)
			case "timestamptz":
				dests[i] = new(pgtype.Timestamptz)
			default:
				dests[i] = new(pgtype.Timestamp)
			}
		default: // schema.TypeText, Subtype == ""
			dests[i] = new(pgtype.Text)
		}
		scanArgs[i] = dests[i]
	}

	var dataRows [][]string
	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		rowData := make([]string, n)
		for i, f := range pkgSchema.Fields {
			rowData[i] = a.pgCellToTDTP(f, typedCellToAny(dests[i]))
		}
		dataRows = append(dataRows, rowData)
	}

	return dataRows, rows.Err()
}

// typedCellToAny приводит переиспользуемый типизированный приёмник к тому же
// any, что вернула бы rows.Values() для той же ячейки — см. комментарий
// readAllRowsTyped о том, зачем это важно.
func typedCellToAny(dest any) any {
	switch v := dest.(type) {
	case *pgtype.Int8:
		if !v.Valid {
			return nil
		}
		return v.Int64
	case *pgtype.Bool:
		if !v.Valid {
			return nil
		}
		return v.Bool
	case *pgtype.Text:
		if !v.Valid {
			return nil
		}
		return v.String
	case *pgtype.Numeric:
		if !v.Valid {
			return nil
		}
		return *v
	case *pgtype.Time:
		if !v.Valid {
			return nil
		}
		return *v
	case *pgtype.Date:
		return dateOrInfinity(v.Valid, v.InfinityModifier, v.Time)
	case *pgtype.Timestamp:
		return dateOrInfinity(v.Valid, v.InfinityModifier, v.Time)
	case *pgtype.Timestamptz:
		return dateOrInfinity(v.Valid, v.InfinityModifier, v.Time)
	default:
		return nil
	}
}

func dateOrInfinity(valid bool, inf pgtype.InfinityModifier, t time.Time) any {
	if !valid {
		return nil
	}
	if inf != pgtype.Finite {
		return inf
	}
	return t
}
