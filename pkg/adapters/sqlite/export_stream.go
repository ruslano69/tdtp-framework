package sqlite

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
// Отличие от ReadAllRows не в значениях — они те же, через тот же
// cellToTDTP, — а в том, что таблица никогда не существует в памяти целиком.
// На 24 млн строк ReadAllRows держит около 23 ГБ ([][]string, ~965 байт на
// строку по замеру на 100k×10); здесь живёт одна строка на канале плюс то, что
// накопил потребитель.
//
// Курсор закрывается горутиной, которая его и открыла: вернуть его наружу
// значило бы обязать вызывающего закрыть, а он видит только каналы.
func (a *Adapter) ReadAllRowsStream(ctx context.Context, tableName string, schema packet.Schema) (<-chan []string, <-chan error, error) {
	tableName = tdtql.StripBrackets(tableName)
	fieldNames := make([]string, len(schema.Fields))
	for i, field := range schema.Fields {
		fieldNames[i] = selectExprForField(field)
	}
	quotedTable := fmt.Sprintf("\"%s\"", tableName) //nolint:gocritic // SQL identifier quoting, not Go string quoting
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(fieldNames, ", "), quotedTable)

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query table: %w", err)
	}

	// Буфер на канале сглаживает разницу темпов чтения и упаковки. Он же —
	// потолок памяти этого пути: сколько строк может ждать потребителя.
	out := make(chan []string, 1024)
	errc := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errc)
		defer func() { _ = rows.Close() }()

		if err := base.StreamSQLRows(ctx, rows, schema, a.converter, "sqlite", out); err != nil {
			errc <- err
		}
	}()

	return out, errc, nil
}
