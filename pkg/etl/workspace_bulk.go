package etl

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// errBulkUnsupported — драйвер не даёт нужных интерфейсов, нужен общий путь.
//
// Возвращается ТОЛЬКО до первой записи. После неё откат на общий путь дописал
// бы строки поверх уже вставленных, поэтому дальше ошибки идут как есть.
var errBulkUnsupported = errors.New("bulk load not available for this driver")

// bulkLoadViaDriver вставляет строки минуя обвязку database/sql.
//
// Профиль загрузки показывает, что там тратится: grabConn,
// driverArgsConnLocked, resultFromStatement, newResult — около 20% аллокаций на
// бухгалтерию, которая при массовой вставке не нужна. Statement берётся у
// драйвера напрямую, аргументы передаются как []driver.NamedValue без
// convertAssign.
//
// Измерено:
//
//	1 млн строк   2.35 → 2.00 с (−15%), пик кучи 353 → 289 МБ (−18%)
//	100 тыс.      245 → 210 мс,          пик 36.3 → 33.4 МБ
//
// Экономия растёт с объёмом, так что мерить её на маленькой таблице — значит её
// не увидеть.
//
// Сквозь пайплайн это 2.5–3.2% в зависимости от источника: вставка занимает 27%
// шага загрузки при SQLite-источнике и 40% при PostgreSQL (тот читается
// быстрее — 4.5 против 8.1 с на 2 млн строк). Немного, и брать это стоит именно
// потому, что такие проценты складываются: три по три дают десять.
//
// ВАЖНО про замер: транзакция здесь заводится вручную, и без неё сравнение
// бессмысленно — каждая вставка стала бы своей неявной транзакцией. Первая
// версия этого пути дала 306 мс против 174 ровно потому, что транзакции не
// было.
func (w *Workspace) bulkLoadViaDriver(ctx context.Context, tableName string,
	fields []packet.Field, rows [][]string, batchRows int) error {

	n := len(fields)
	rowTuple := "(" + strings.TrimSuffix(strings.Repeat("?, ", n), ", ") + ")"
	oneSQL := fmt.Sprintf("INSERT INTO %q VALUES %s", tableName, rowTuple)
	bulkSQL := fmt.Sprintf("INSERT INTO %q VALUES %s", tableName,
		strings.TrimSuffix(strings.Repeat(rowTuple+", ", batchRows), ", "))

	conn, err := w.db.Conn(ctx)
	if err != nil {
		return errBulkUnsupported
	}
	defer func() { _ = conn.Close() }()

	var wrote error // ошибка ПОСЛЕ первой записи: откатываться уже нельзя
	rawErr := conn.Raw(func(dc any) error {
		prep, ok := dc.(driver.ConnPrepareContext)
		if !ok {
			return errBulkUnsupported
		}
		beginner, ok := dc.(driver.ConnBeginTx)
		if !ok {
			return errBulkUnsupported
		}

		tx, err := beginner.BeginTx(ctx, driver.TxOptions{})
		if err != nil {
			return errBulkUnsupported
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		single, err := prep.PrepareContext(ctx, oneSQL)
		if err != nil {
			return errBulkUnsupported
		}
		defer func() { _ = single.Close() }()
		singleExec, ok := single.(driver.StmtExecContext)
		if !ok {
			return errBulkUnsupported
		}

		bulkExec := singleExec
		if batchRows > 1 {
			bulk, err := prep.PrepareContext(ctx, bulkSQL)
			if err != nil {
				return errBulkUnsupported
			}
			defer func() { _ = bulk.Close() }()
			if bulkExec, ok = bulk.(driver.StmtExecContext); !ok {
				return errBulkUnsupported
			}
		}

		// args живёт всю загрузку и НЕ пересоздаётся между пачками: слот k
		// всегда держит одну и ту же колонку (k mod n), и если пришло то же
		// значение, что уже лежит в слоте, работу можно не повторять.
		//
		// Экономится не сравнение, а АЛЛОКАЦИЯ. convertValue возвращает any, и
		// на этом возврате строка упаковывается в интерфейс — 16 байт на
		// ЯЧЕЙКУ, 53% всех аллокаций загрузки. Измерено отдельно: 1.2 млн
		// упаковок стоят 32 мс и 19 МБ, те же присваивания уже упакованных
		// значений — 1.8 мс и ноль.
		//
		// Поэтому сравнивается ИСХОДНАЯ строка, до convertValue. Сравнивать
		// результат бесполезно — упаковка к тому моменту уже произошла, и
		// первая версия этой правки ровно поэтому не дала ничего: аллокации
		// не сдвинулись ни на одну.
		//
		// Сравнение исходной строки законно, потому что слот k — это всегда
		// колонка k mod n, а значит и тип один и тот же: одинаковый вход при
		// одинаковом типе даёт одинаковый выход.
		//
		// Слот при этом жёстче, чем колонка: это ПАРА (строка внутри пачки,
		// колонка), потому что пачка идёт одним INSERT на batchRows строк.
		// Поэтому переиспользование срабатывает только на значениях, которые
		// повторяются с периодом, кратным batchRows, — а не на любом повторе в
		// колонке. Отсюда же берётся ограничение на тест: значение,
		// приходящее всегда в один и тот же слот, устаревшего соседа там не
		// застанет и поломку учёта не покажет.
		//
		// Выигрыш зависит от данных и на уникальных значениях равен нулю (плюс
		// одно сравнение, которое почти всегда расходится на первом байте).
		// Зато на колонках-справочниках — статусах, валютах, названиях
		// отделов, повторяющихся датах — он настоящий.
		args := make([]driver.NamedValue, batchRows*n)
		raw := make([]string, batchRows*n)
		filled := make([]bool, batchRows*n)
		for k := range args {
			args[k].Ordinal = k + 1
		}
		pos := 0
		batchStart := 0
		for i, values := range rows {
			for j, val := range values {
				if filled[pos] && raw[pos] == val {
					pos++
					continue // то же значение в той же колонке — упаковка цела
				}
				args[pos].Value = w.convertValue(val, fields[j].Type)
				raw[pos] = val
				filled[pos] = true
				pos++
			}
			if pos == batchRows*n {
				if _, err := bulkExec.ExecContext(ctx, args); err != nil {
					wrote = fmt.Errorf("failed to insert rows %d-%d: %w", batchStart, i, err)
					return wrote
				}
				pos = 0
				batchStart = i + 1
			}
		}
		for i := 0; i*n < pos; i++ {
			chunk := args[i*n : (i+1)*n]
			for k := range chunk {
				chunk[k].Ordinal = k + 1
			}
			if _, err := singleExec.ExecContext(ctx, chunk); err != nil {
				wrote = fmt.Errorf("failed to insert row %d: %w", batchStart+i, err)
				return wrote
			}
		}

		if err := tx.Commit(); err != nil {
			wrote = fmt.Errorf("failed to commit transaction: %w", err)
			return wrote
		}
		committed = true
		return nil
	})

	if wrote != nil {
		return wrote
	}
	return rawErr
}

// bulkReadViaDriver читает результат запроса минуя обвязку database/sql.
//
// Зеркало bulkLoadViaDriver, и на чтении выигрыш БОЛЬШЕ, чем на записи: та же
// бухгалтерия плюс convertAssign на каждую ячейку, который здесь превращает
// значение драйвера в приёмник и тут же отдаётся обратно строкой.
//
// Важно, где это заметно. В отчёте с агрегацией обратно едут 600 строк, и
// чтение не стоит ничего. Но большинство трансформаций в этом репозитории —
// `SELECT * FROM таблица`, и тогда обратно едет весь набор, а чтение стоит
// столько же, сколько вставка: на 100k строк 196 мс против 193 мс.
//
// Схема и форматирование ячеек — те же, что на общем пути: dateColumnKinds и
// formatCell вызываются здесь же, иначе datetime-колонки поехали бы в другом
// виде и разница между путями стала бы видна в данных.
//
// Драйвер без нужных интерфейсов отправляется на общий путь через
// errBulkUnsupported — до того, как прочитана первая строка.
func (w *Workspace) bulkReadViaDriver(ctx context.Context, sqlQuery, resultTableName string) (*packet.DataPacket, error) {
	conn, err := w.db.Conn(ctx)
	if err != nil {
		return nil, errBulkUnsupported
	}
	defer func() { _ = conn.Close() }()

	var result *packet.DataPacket
	var readErr error // ошибка после начала чтения: на общий путь не откатываемся

	rawErr := conn.Raw(func(dc any) error {
		qc, ok := dc.(driver.QueryerContext)
		if !ok {
			return errBulkUnsupported
		}
		dr, err := qc.QueryContext(ctx, sqlQuery, nil)
		if err != nil {
			// Запрос не выполнился — данных ещё нет, общий путь вернёт
			// пользователю ту же ошибку с привычным текстом.
			return errBulkUnsupported
		}
		defer func() { _ = dr.Close() }()

		namer, ok := dr.(driver.RowsColumnTypeDatabaseTypeName)
		if !ok {
			return errBulkUnsupported
		}

		columns := dr.Columns()
		pkt := packet.NewDataPacket(packet.TypeReference, resultTableName)
		pkt.Schema.Fields = make([]packet.Field, len(columns))
		dbTypes := make([]string, len(columns))
		for i, col := range columns {
			dbTypes[i] = namer.ColumnTypeDatabaseTypeName(i)
			pkt.Schema.Fields[i] = packet.Field{
				Name: col,
				Type: w.mapSQLiteTypeToTDTP(dbTypes[i]),
			}
		}
		kinds := dateColumnKindsFromNames(dbTypes)

		dest := make([]driver.Value, len(columns))
		var allRows [][]string
		for {
			if err := dr.Next(dest); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				readErr = fmt.Errorf("failed to scan row: %w", err)
				return readErr
			}
			rec := make([]string, len(dest))
			for i, v := range dest {
				rec[i] = w.formatCell(driverValueToAny(v), kinds[i])
			}
			allRows = append(allRows, rec)
		}

		pkt.Data = packet.RowsToData(allRows)
		pkt.Header.RecordsInPart = len(allRows)
		result = pkt
		return nil
	})

	if readErr != nil {
		return nil, readErr
	}
	if rawErr != nil {
		return nil, rawErr
	}
	return result, nil
}

// driverValueToAny приводит значение драйвера к тому, что ожидает formatCell.
//
// []byte копируется в строку: буфер драйвера переиспользуется между строками,
// и без копии все ячейки колонки оказались бы одним и тем же значением —
// последним прочитанным.
func driverValueToAny(v driver.Value) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

// dateColumnKindsFromNames — та же разметка, что dateColumnKinds, но по готовым
// именам типов: у драйвера нет []*sql.ColumnType.
func dateColumnKindsFromNames(dbTypes []string) []string {
	kinds := make([]string, len(dbTypes))
	for i, t := range dbTypes {
		t = strings.ToUpper(t)
		switch {
		case t == "DATE":
			kinds[i] = "DATE"
		case strings.Contains(t, "DATETIME"), strings.Contains(t, "TIMESTAMP"):
			kinds[i] = "DATETIME"
		}
	}
	return kinds
}
