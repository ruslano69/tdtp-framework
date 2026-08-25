package mysql

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// ReadAllRowsStream читает таблицу потоком, реализуя base.StreamingDataReader.
//
// Значения те же, что у ReadAllRows: обе дороги идут через base.cellToTDTP.
// Схему возвращает без изменений — MySQL, в отличие от MSSQL, ничего из неё не
// выкидывает (read-only колонки там режет PostProcessRows, которого здесь нет).
//
// go-sql-driver действительно отдаёт строки по мере поступления, а не читает
// результат целиком. Это проверено, а не взято из документации: пик RSS
// экспорта одинаков на 524 288 и на 4 194 304 строках — 62 МБ в обоих случаях,
// при том что обычный путь на тех же данных вырос с 388 МБ до 2 823 МБ.
// Контейнер MySQL при этом держал те же ~617 МБ, что и до выборки, то есть
// проблема не переехала на сервер.
//
// Признак настоящего потока — именно плоский пик при росте данных. Если он
// растёт линейно, драйвер материализует результат и поток декоративен.
func (a *Adapter) ReadAllRowsStream(ctx context.Context, tableName string, pkgSchema packet.Schema) (packet.Schema, <-chan []string, <-chan error, error) {
	tableName = tdtql.StripBrackets(tableName)

	columns := make([]string, 0, len(pkgSchema.Fields))
	for _, field := range pkgSchema.Fields {
		columns = append(columns, fmt.Sprintf("`%s`", field.Name))
	}
	query := fmt.Sprintf("SELECT %s FROM `%s`", strings.Join(columns, ", "), tableName)

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return pkgSchema, nil, nil, fmt.Errorf("failed to query table: %w", err)
	}

	out := make(chan []string, 1024)
	errc := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errc)
		defer func() { _ = rows.Close() }()

		if err := base.StreamSQLRows(ctx, rows, pkgSchema, a.converter, "mysql", out); err != nil {
			errc <- err
		}
	}()

	return pkgSchema, out, errc, nil
}
