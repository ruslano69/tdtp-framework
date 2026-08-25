package mssql

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ReadAllRowsStream читает таблицу потоком, реализуя base.StreamingDataReader.
//
// Значения те же, что у ReadAllRows: обе дороги идут через base.cellToTDTP.
// Разница в том, что таблица никогда не существует в памяти целиком.
//
// Read-only колонки отсекаются ЗДЕСЬ, на уровне SELECT, а не хуком после
// чтения. Обычный путь зовёт PostProcessRows, когда все строки уже собраны;
// потоку фильтровать постфактум нечего — строка ушла потребителю и упакована.
// Поэтому фильтр применяется к схеме заранее, отфильтрованная схема
// возвращается вызывающему, и сервер эти колонки просто не присылает.
func (a *Adapter) ReadAllRowsStream(ctx context.Context, tableName string, pkgSchema packet.Schema) (packet.Schema, <-chan []string, <-chan error, error) {
	// filterReadOnlyFields умеет работать без строк: он индексный, и на nil
	// возвращает только отфильтрованную схему.
	effective, _ := filterReadOnlyFields(pkgSchema, nil, getIncludeReadOnlyFromContext(ctx))

	if len(effective.Fields) == 0 {
		return effective, nil, nil, fmt.Errorf("no exportable columns in %q: every field is read-only (use --readonly-fields)", tableName)
	}

	schemaName, table := a.parseTableName(tableName)
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, table)

	columns := make([]string, 0, len(effective.Fields))
	for _, field := range effective.Fields {
		columns = append(columns, fmt.Sprintf("[%s]", field.Name))
	}
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), fullTableName)

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return effective, nil, nil, fmt.Errorf("failed to query table: %w", err)
	}

	out := make(chan []string, 1024)
	errc := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errc)
		defer func() { _ = rows.Close() }()

		if err := base.StreamSQLRows(ctx, rows, effective, a.converter, "mssql", out); err != nil {
			errc <- err
		}
	}()

	return effective, out, errc, nil
}
