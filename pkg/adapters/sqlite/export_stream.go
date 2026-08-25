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
func (a *Adapter) ReadAllRowsStream(ctx context.Context, tableName string, schema packet.Schema) (packet.Schema, <-chan []string, <-chan error, error) {
	tableName = tdtql.StripBrackets(tableName)
	fieldNames := make([]string, len(schema.Fields))
	for i, field := range schema.Fields {
		fieldNames[i] = selectExprForField(field)
	}
	quotedTable := fmt.Sprintf("\"%s\"", tableName) //nolint:gocritic // SQL identifier quoting, not Go string quoting
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(fieldNames, ", "), quotedTable)

	// Выделенное соединение — ради страничного кеша.
	//
	// Адаптер ставит PRAGMA cache_size = -64000, то есть 64 МБ на соединение,
	// и это оправдано на импорте: случайная запись переиспользует страницы.
	// Последовательный скан не переиспользует ничего, и кеш просто занимает
	// память. Замерено на 1 млн строк: 179 МБ пикового RSS против 62 МБ при
	// кеше в 2 МБ — то есть 117 МБ из 179 держал именно он, при живой куче Go
	// в 17 МБ.
	//
	// PRAGMA действует на соединение, а database/sql раздаёт их из пула, так
	// что менять её на a.db значило бы менять на случайном соединении и,
	// возможно, испортить кеш импорту. Conn берёт одно и держит до закрытия.
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return schema, nil, nil, fmt.Errorf("failed to take a connection: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA cache_size = -2000"); err != nil {
		_ = conn.Close()
		return schema, nil, nil, fmt.Errorf("failed to size the page cache: %w", err)
	}

	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		_ = conn.Close()
		return schema, nil, nil, fmt.Errorf("failed to query table: %w", err)
	}

	// Буфер на канале сглаживает разницу темпов чтения и упаковки. Он же —
	// потолок памяти этого пути: сколько строк может ждать потребителя.
	out := make(chan []string, 1024)
	errc := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errc)
		defer func() { _ = rows.Close() }()
		// Соединение закрывается здесь же: наружу оно не видно, вернуть его в
		// пул некому.
		defer func() { _ = conn.Close() }()

		if err := base.StreamSQLRows(ctx, rows, schema, a.converter, "sqlite", out); err != nil {
			errc <- err
		}
	}()

	// SQLite ничего из схемы не выкидывает — отдаём как получили.
	return schema, out, errc, nil
}
