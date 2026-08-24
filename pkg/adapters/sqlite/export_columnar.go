package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// ReadAllColumns читает таблицу в колоночную раскладку.
//
// ПРОТОТИП, параллельный ReadAllRows: тот же SELECT, тот же перевод ячеек, но
// результат разложен по колонкам. Существует, чтобы померить колоночный путь
// на настоящем адаптере; ExportTable его не зовёт.
func (a *Adapter) ReadAllColumns(ctx context.Context, tableName string, schema packet.Schema) (*base.ColumnBlock, error) {
	tableName = tdtql.StripBrackets(tableName)
	fieldNames := make([]string, len(schema.Fields))
	for i, field := range schema.Fields {
		fieldNames[i] = selectExprForField(field)
	}

	quotedTable := fmt.Sprintf("\"%s\"", tableName) //nolint:gocritic // SQL identifier quoting, not Go string quoting
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(fieldNames, ", "), quotedTable)

	// Высота таблицы известна заранее и стоит один COUNT(*) — на сотне тысяч
	// строк это снимает около семнадцати удвоений каждого колоночного среза.
	// Ошибку намеренно глушим: точный hint это оптимизация, а не условие
	// работы, и падать из-за него незачем.
	hint := 0
	if n, err := a.GetRowCount(ctx, tableName); err == nil {
		hint = int(n)
	}

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query table: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return base.ScanSQLColumns(rows, schema, a.converter, "sqlite", hint)
}
