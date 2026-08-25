package postgres

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// ReadAllRowsStream читает таблицу потоком, реализуя base.StreamingDataReader.
//
// Отдельная реализация, а не base.StreamSQLRows: тот работает с *sql.Rows, а
// этот адаптер живёт на pgx/pgxpool мимо database/sql. Общее у них — ячейка:
// значения идут через тот же pgCellToTDTP, что и readRowsWithSQL, поэтому
// потоковое и построчное чтение дают одни байты.
//
// Запрос — тот же SELECT *, что у ReadAllRows, и по той же причине: схема
// сопоставляется с колонками ПО ПОЗИЦИИ (schema.Fields[i]), так что перечислять
// колонки в другом порядке нельзя.
//
// Соединение берётся из пула на всю выгрузку. На большой таблице это минуты, и
// для боевой базы это заметнее, чем для файла: одно соединение из пула занято.
func (a *Adapter) ReadAllRowsStream(ctx context.Context, tableName string, pkgSchema packet.Schema) (packet.Schema, <-chan []string, <-chan error, error) {
	tableName = tdtql.StripBrackets(tableName)
	quotedTable := QuoteIdentifier(tableName)
	if a.schema != "public" {
		quotedTable = QuoteIdentifier(a.schema) + "." + quotedTable
	}
	query := fmt.Sprintf("SELECT * FROM %s", quotedTable)

	rows, err := a.pool.Query(ctx, query)
	if err != nil {
		return pkgSchema, nil, nil, fmt.Errorf("failed to query table: %w", err)
	}

	out := make(chan []string, 1024)
	errc := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errc)
		defer rows.Close()

		for rows.Next() {
			if err := ctx.Err(); err != nil {
				errc <- err
				return
			}
			values, err := rows.Values()
			if err != nil {
				errc <- fmt.Errorf("failed to scan row: %w", err)
				return
			}
			// Срез на строку выделяется заново: он уходит потребителю и живёт
			// столько, сколько нужно ему. Общий буфер сделал бы все строки
			// части алиасами последней прочитанной.
			row := make([]string, len(values))
			for i, val := range values {
				if i < len(pkgSchema.Fields) {
					row[i] = a.pgCellToTDTP(pkgSchema.Fields[i], val)
				}
			}
			select {
			case out <- row:
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			}
		}
		if err := rows.Err(); err != nil {
			errc <- err
		}
	}()

	return pkgSchema, out, errc, nil
}
