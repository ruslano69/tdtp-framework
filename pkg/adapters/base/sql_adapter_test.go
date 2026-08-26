package base

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

func TestMSSQLAdapter_AdaptSQL_ANSIQuotedTableName(t *testing.T) {
	// Regression test: table names with special chars ($ space) are ANSI-quoted by
	// GenerateSQL ("ZTR$Timesheet Line"). MSSQLAdapter must replace the full ANSI-quoted
	// token, not the bare substring, to avoid producing invalid SQL like
	// "[dbo].["ZTR$Timesheet Line"]" which causes a full table scan (17 GB RAM).
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "Entry No_"},
			{Name: "Calendar Date"},
		},
	}

	// GenerateSQL produces ANSI-quoted table: "ZTR$Timesheet Line"
	standardSQL := `SELECT * FROM "ZTR$Timesheet Line" WHERE "Calendar Date" >= '2024-08-12T00:00:00Z'`
	got := adapter.AdaptSQL(standardSQL, "ZTR$Timesheet Line", schema, nil)

	if strings.Contains(got, `"`) {
		t.Errorf("AdaptSQL left ANSI double-quotes in output: %s", got)
	}
	if !strings.Contains(got, "[dbo].[ZTR$Timesheet Line]") {
		t.Errorf("AdaptSQL did not produce [dbo].[ZTR$Timesheet Line]: %s", got)
	}
	if !strings.Contains(got, "[Calendar Date]") {
		t.Errorf("AdaptSQL did not bracket-quote field name: %s", got)
	}
}

func TestMSSQLAdapter_AdaptSQL_SafeTableName(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{Fields: []packet.Field{{Name: "id"}, {Name: "name"}}}

	// Safe table name is NOT ANSI-quoted by GenerateSQL
	standardSQL := `SELECT * FROM Users WHERE id = 1`
	got := adapter.AdaptSQL(standardSQL, "Users", schema, nil)

	if !strings.Contains(got, "[dbo].[Users]") {
		t.Errorf("AdaptSQL did not produce [dbo].[Users]: %s", got)
	}
}

// SQL Server datetime не принимает суффикс 'Z' (UTC marker) у ISO 8601 литералов:
// '2024-08-12T00:00:00Z' → "Conversion failed when converting date/time from character string".
// Падение SQL триггерит fallback на full table scan. Strip 'Z' страхует pushdown.
func TestMSSQLAdapter_AdaptSQL_StripsISO8601ZSuffix(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{Fields: []packet.Field{{Name: "Calendar Date"}}}

	standardSQL := `SELECT * FROM Timesheet WHERE "Calendar Date" >= '2024-08-12T00:00:00Z' AND "Calendar Date" <= '2026-05-19T23:59:59Z'`
	got := adapter.AdaptSQL(standardSQL, "Timesheet", schema, nil)

	if strings.Contains(got, "Z'") {
		t.Errorf("AdaptSQL did not strip 'Z' suffix from datetime literals: %s", got)
	}
	if !strings.Contains(got, "'2024-08-12T00:00:00'") {
		t.Errorf("AdaptSQL must keep ISO 8601 datetime body intact: %s", got)
	}
	if !strings.Contains(got, "'2026-05-19T23:59:59'") {
		t.Errorf("AdaptSQL must process every Z-suffixed literal: %s", got)
	}
}

// TestMSSQLAdapter_AdaptSQL_NegativeLimit_NoOrderBy проверяет bug #3 (полная семантика tail):
// --limit -N без --order-by должен генерировать подзапрос с TOP N + ORDER BY col DESC,
// чтобы вернуть именно ПОСЛЕДНИЕ N строк, а не первые.
// ORDER BY берётся из первой колонки SELECT-списка, а не из schema.Fields[0].
func TestMSSQLAdapter_AdaptSQL_NegativeLimit_NoOrderBy(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "Timetable No_"},
			{Name: "Calendar Date"},
			{Name: "Hours"},
		},
	}

	// sql_generator emits LIMIT N (absolute value) even for negative query.Limit
	standardSQL := `SELECT "Timetable No_", "Calendar Date", "Hours" FROM "ZTR$Timesheet Line" WHERE "Timetable No_" LIKE '4003%' LIMIT 10`
	query := &packet.Query{Limit: -10}

	got := adapter.AdaptSQL(standardSQL, "ZTR$Timesheet Line", schema, query)

	if strings.Contains(got, " LIMIT ") {
		t.Errorf("AdaptSQL must not emit MySQL LIMIT for MSSQL; got: %s", got)
	}
	if !strings.Contains(got, "TOP 10") {
		t.Errorf("AdaptSQL must inject TOP 10; got: %s", got)
	}
	if !strings.Contains(got, "[dbo].[ZTR$Timesheet Line]") {
		t.Errorf("AdaptSQL must qualify table name; got: %s", got)
	}
	// Must be a subquery (tail semantics): TOP goes inside, outer ORDER BY restores order.
	if !strings.Contains(got, "AS _tail") {
		t.Errorf("AdaptSQL must wrap in subquery (AS _tail) for true last-N semantics; got: %s", got)
	}
	// ORDER BY must use first projected column ([Timetable No_]) — inner DESC, outer ASC.
	if !strings.Contains(got, "ORDER BY [Timetable No_] DESC") {
		t.Errorf("inner subquery must ORDER BY first projected field DESC; got: %s", got)
	}
	if !strings.Contains(got, ") AS _tail ORDER BY [Timetable No_] ASC") {
		t.Errorf("outer query must ORDER BY first projected field ASC; got: %s", got)
	}
	// TOP must be inside the subquery, not on the outer SELECT *
	topIdx := strings.Index(got, "TOP 10")
	parenIdx := strings.Index(got, "(SELECT")
	if topIdx < parenIdx {
		t.Errorf("TOP 10 must be inside the subquery, not on outer SELECT; got: %s", got)
	}
}

// TestMSSQLAdapter_AdaptSQL_NegativeLimit_WithFields проверяет bug #3 + --fields:
// ORDER BY в подзапросе должен ссылаться только на колонки из SELECT-списка (проекции),
// а не на schema.Fields[0], которого может не быть в --fields.
// Именно это вызывало "Invalid column name 'timestamp'" на SQL Server.
func TestMSSQLAdapter_AdaptSQL_NegativeLimit_WithFields(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")

	// Полная схема таблицы — первое поле "timestamp" НЕ входит в --fields.
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "timestamp"},     // системное поле, не запрошено через --fields
			{Name: "Calendar Date"}, // запрошено
			{Name: "Hours"},         // запрошено
		},
	}

	// SQL с проекцией из --fields: только "Calendar Date" и "Hours" (без "timestamp").
	standardSQL := `SELECT "Calendar Date", "Hours" FROM "ZTR$Timesheet Line" LIMIT 5`
	query := &packet.Query{Limit: -5}

	got := adapter.AdaptSQL(standardSQL, "ZTR$Timesheet Line", schema, query)

	if strings.Contains(got, " LIMIT ") {
		t.Errorf("AdaptSQL must not emit MySQL LIMIT; got: %s", got)
	}
	// ORDER BY must NOT reference [timestamp] — it's not in the SELECT list.
	if strings.Contains(got, "[timestamp]") {
		t.Errorf("ORDER BY must not reference [timestamp] (not in --fields projection); got: %s", got)
	}
	// ORDER BY must use [Calendar Date] — the first column in the SELECT list.
	if !strings.Contains(got, "ORDER BY [Calendar Date]") {
		t.Errorf("ORDER BY must use first projected column [Calendar Date]; got: %s", got)
	}
	if !strings.Contains(got, "AS _tail") {
		t.Errorf("must be a subquery for tail semantics; got: %s", got)
	}
}

// TestMSSQLAdapter_AdaptSQL_NegativeLimit_WithOrderBy проверяет bug #3 для подзапроса tail-mode.
// sql_generator оборачивает запрос в подзапрос с ORDER BY reversed + LIMIT N внутри,
// а MSSQL-адаптер должен конвертировать этот LIMIT в TOP N внутри подзапроса.
func TestMSSQLAdapter_AdaptSQL_NegativeLimit_WithOrderBy(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "Entry No_"},
			{Name: "Posting Date"},
		},
	}

	// sql_generator produces subquery pattern for tail+order-by
	standardSQL := `SELECT * FROM (SELECT "Entry No_", "Posting Date" FROM Timesheets ORDER BY "Posting Date" DESC LIMIT 5) AS _tail ORDER BY "Posting Date" ASC`
	query := &packet.Query{Limit: -5}

	got := adapter.AdaptSQL(standardSQL, "Timesheets", schema, query)

	if strings.Contains(got, " LIMIT ") {
		t.Errorf("AdaptSQL must not emit MySQL LIMIT for MSSQL; got: %s", got)
	}
	if !strings.Contains(got, "TOP 5") {
		t.Errorf("AdaptSQL must inject TOP 5 inside the subquery; got: %s", got)
	}
	// TOP must be inside the subquery, not on the outer SELECT
	topIdx := strings.Index(got, "TOP 5")
	parenIdx := strings.Index(got, "(SELECT")
	if topIdx < parenIdx {
		t.Errorf("TOP 5 must be inside the subquery (after '(SELECT'), not on outer SELECT; got: %s", got)
	}
	if !strings.Contains(got, "AS _tail") {
		t.Errorf("AdaptSQL must preserve the _tail subquery alias; got: %s", got)
	}
}

// TestMSSQLAdapter_AdaptSQL_NegativeLimit_SelectStar проверяет, что при SELECT *
// (без --fields) ORDER BY fallback пропускает read-only поля (timestamp/rowversion)
// и берёт первый writable столбец схемы.
func TestMSSQLAdapter_AdaptSQL_NegativeLimit_SelectStar(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")

	// timestamp — первое поле, ReadOnly=true (как в реальных NAV/BC таблицах).
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "timestamp", ReadOnly: true},  // rowversion — отсекается PostProcessRows
			{Name: "Entry No_", ReadOnly: false}, // первый writable — должен стать ORDER BY
			{Name: "Posting Date", ReadOnly: false},
		},
	}

	// SELECT * — нет --fields, firstProjectedColumn вернёт ""
	standardSQL := `SELECT * FROM Timesheets LIMIT 5`
	query := &packet.Query{Limit: -5}

	got := adapter.AdaptSQL(standardSQL, "Timesheets", schema, query)

	if strings.Contains(got, " LIMIT ") {
		t.Errorf("must not emit MySQL LIMIT; got: %s", got)
	}
	// ORDER BY must NOT use [timestamp] (ReadOnly, cut by PostProcessRows)
	if strings.Contains(got, "[timestamp]") {
		t.Errorf("ORDER BY must skip read-only [timestamp]; got: %s", got)
	}
	// ORDER BY must use first writable field [Entry No_]
	if !strings.Contains(got, "[Entry No_]") {
		t.Errorf("ORDER BY must use first writable field [Entry No_]; got: %s", got)
	}
	if !strings.Contains(got, "AS _tail") {
		t.Errorf("must wrap in subquery; got: %s", got)
	}
}

// Без 'Z' literal остаётся неизменным — regex не должен трогать обычные строки.
func TestMSSQLAdapter_AdaptSQL_LeavesNonDatetimeStringsAlone(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{Fields: []packet.Field{{Name: "Code"}}}

	// Строка с 'Z' но не ISO 8601 — должна остаться нетронутой
	standardSQL := `SELECT * FROM Users WHERE "Code" = 'XYZ' AND "Code" = '24626-1'`
	got := adapter.AdaptSQL(standardSQL, "Users", schema, nil)

	if !strings.Contains(got, "'XYZ'") {
		t.Errorf("regex stripped non-datetime 'XYZ' value: %s", got)
	}
	if !strings.Contains(got, "'24626-1'") {
		t.Errorf("regex damaged code-style value '24626-1': %s", got)
	}
}

// OFFSET без LIMIT: генератор подставляет LIMIT maxint, чтобы SQL был валиден
// в SQLite и MySQL. MSSQL выражает то же самое через OFFSET ... ROWS, поэтому
// обязан этот LIMIT убрать — иначе в запросе останется синтаксис, которого
// SQL Server не знает.
func TestMSSQLAdapter_AdaptSQL_StripsOffsetOnlyLimit(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{Fields: []packet.Field{{Name: "ID"}, {Name: "Name"}}}

	standardSQL := fmt.Sprintf(`SELECT * FROM "Users" LIMIT %d OFFSET 5`, tdtql.OffsetOnlyLimit)
	query := &packet.Query{Offset: 5}

	got := adapter.AdaptSQL(standardSQL, "Users", schema, query)

	if strings.Contains(got, "LIMIT") {
		t.Errorf("the sentinel LIMIT must be stripped for MSSQL; got: %s", got)
	}
	// Пагинация идёт через ROW_NUMBER(), а не OFFSET/FETCH: последний не
	// разбирается при уровне совместимости 80.
	if strings.Contains(got, "OFFSET") || strings.Contains(got, "FETCH") {
		t.Errorf("OFFSET/FETCH is not available at compatibility level 80; got: %s", got)
	}
	if !strings.Contains(got, "ROW_NUMBER() OVER (ORDER BY") {
		t.Errorf("expected a ROW_NUMBER() window; got: %s", got)
	}
	if !strings.Contains(got, "_tdtp_rn > 5") {
		t.Errorf("expected the offset as a row-number predicate; got: %s", got)
	}
}

// Внешний SELECT обязан перечислять колонки явно: со звёздочкой наружу вышел
// бы служебный _tdtp_rn, строка стала бы на колонку шире схемы, и пакет
// разъехался бы молча — схема сопоставляется с колонками ПО ПОЗИЦИИ.
func TestMSSQLAdapter_PagingDoesNotLeakRowNumber(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{Fields: []packet.Field{
		{Name: "ID"}, {Name: "Calendar Date"}, {Name: "Hours"},
	}}

	got := adapter.AdaptSQL(`SELECT * FROM "Sheet" LIMIT 10 OFFSET 5`, "Sheet", schema,
		&packet.Query{Limit: 10, Offset: 5})

	outer := got[:strings.Index(got, " FROM (")]
	if strings.Contains(outer, "*") {
		t.Errorf("the outer SELECT must name its columns, not use *; got: %s", got)
	}
	if strings.Contains(outer, "_tdtp_rn") {
		t.Errorf("_tdtp_rn must not reach the caller; got: %s", got)
	}
	for _, f := range schema.Fields {
		if !strings.Contains(outer, "["+f.Name+"]") {
			t.Errorf("column %q missing from the projection; got: %s", f.Name, got)
		}
	}
	if !strings.Contains(got, "_tdtp_rn > 5 AND _tdtp_rn <= 15") {
		t.Errorf("expected the window to be rows 6..15; got: %s", got)
	}
}

// Запрошенный порядок сохраняется: ROW_NUMBER нумерует по нему, а не по
// первой попавшейся колонке.
func TestMSSQLAdapter_PagingKeepsRequestedOrder(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{Fields: []packet.Field{{Name: "ID"}, {Name: "Name"}}}

	got := adapter.AdaptSQL(`SELECT * FROM "Users" ORDER BY [Name] DESC LIMIT 5 OFFSET 10`,
		"Users", schema, &packet.Query{Limit: 5, Offset: 10})

	if !strings.Contains(got, "ROW_NUMBER() OVER (ORDER BY [Name] DESC)") {
		t.Errorf("the requested ORDER BY must drive the numbering; got: %s", got)
	}
}

// --fields: проекция из самого SQL важнее схемы, иначе наружу вышли бы
// колонки, которых пользователь не просил.
func TestMSSQLAdapter_PagingRespectsExplicitProjection(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{Fields: []packet.Field{
		{Name: "ID"}, {Name: "Name"}, {Name: "Secret"},
	}}

	got := adapter.AdaptSQL(`SELECT [ID], [Name] FROM "Users" LIMIT 2 OFFSET 1`,
		"Users", schema, &packet.Query{Limit: 2, Offset: 1})

	outer := got[:strings.Index(got, " FROM (")]
	if strings.Contains(outer, "Secret") {
		t.Errorf("a column outside the projection leaked into the result; got: %s", got)
	}
}
