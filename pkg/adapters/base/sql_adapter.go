package base

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// isoDatetimeZ matches ISO 8601 datetime literals with UTC 'Z' suffix in SQL strings.
// SQL Server datetime type rejects 'Z'; datetime2/datetimeoffset accept it.
// We strip 'Z' unconditionally so pushdown works for all datetime column types.
var isoDatetimeZ = regexp.MustCompile(`'(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})Z'`)

// StandardSQLAdapter реализует SQLAdapter для стандартного SQL (SQLite, PostgreSQL, MySQL)
// Использует синтаксис LIMIT/OFFSET
type StandardSQLAdapter struct {
	dbType        string // "sqlite", "postgres", "mysql"
	schemaPrefix  string // "" для SQLite/MySQL, "schema_name." для PostgreSQL
	quoteChar     string // '`' для MySQL, '"' для PostgreSQL/SQLite
	useSchemaName bool
}

// NewStandardSQLAdapter создает StandardSQLAdapter
func NewStandardSQLAdapter(dbType, schemaPrefix, quoteChar string) *StandardSQLAdapter {
	return &StandardSQLAdapter{
		dbType:        dbType,
		schemaPrefix:  schemaPrefix,
		quoteChar:     quoteChar,
		useSchemaName: schemaPrefix != "",
	}
}

// AdaptSQL адаптирует стандартный SQL (не требуется для SQLite/PostgreSQL/MySQL с LIMIT/OFFSET)
func (a *StandardSQLAdapter) AdaptSQL(standardSQL, tableName string, schema packet.Schema, query *packet.Query) string {
	// Для стандартного SQL адаптация минимальна
	sql := standardSQL

	// Добавляем schema prefix если нужно (для PostgreSQL)
	if a.useSchemaName && !strings.Contains(sql, a.schemaPrefix) {
		sql = strings.Replace(sql, " FROM "+tableName+" ", " FROM "+a.schemaPrefix+tableName+" ", 1)
	}

	// Квотируем идентификаторы если нужно
	if a.quoteChar != "" && a.quoteChar != "\"" {
		// Для MySQL заменяем " на `
		sql = strings.ReplaceAll(sql, "\"", a.quoteChar)
	}

	return sql
}

// PostgreSQLSchemaAdapter реализует SQLAdapter для PostgreSQL с non-public схемой.
// Квалифицирует имя таблицы: "schema"."table".
// Используется когда schema != "public" (т.к. SQLGenerator генерирует только имя таблицы).
type PostgreSQLSchemaAdapter struct {
	schema string
}

// NewPostgreSQLSchemaAdapter создает PostgreSQLSchemaAdapter для указанной схемы.
func NewPostgreSQLSchemaAdapter(schema string) *PostgreSQLSchemaAdapter {
	return &PostgreSQLSchemaAdapter{schema: schema}
}

// AdaptSQL квалифицирует имя таблицы в FROM clause добавляя schema prefix с quoted identifiers.
func (a *PostgreSQLSchemaAdapter) AdaptSQL(standardSQL, tableName string, schema packet.Schema, query *packet.Query) string {
	quotedTable := fmt.Sprintf(`"%s"."%s"`, a.schema, tableName) //nolint:gocritic // SQL identifier quoting, not Go string quoting
	return strings.Replace(standardSQL, " FROM "+tableName+" ", " FROM "+quotedTable+" ", 1)
}

// MSSQLAdapter реализует SQLAdapter для MS SQL Server
// Использует синтаксис OFFSET/FETCH вместо LIMIT
type MSSQLAdapter struct {
	schemaName string // "dbo" или custom schema
}

// NewMSSQLAdapter создает MSSQLAdapter
func NewMSSQLAdapter(schemaName string) *MSSQLAdapter {
	if schemaName == "" {
		schemaName = "dbo"
	}
	return &MSSQLAdapter{
		schemaName: schemaName,
	}
}

// AdaptSQL адаптирует стандартный SQL под MS SQL Server синтаксис
// Основные изменения:
// 1. LIMIT N → FETCH NEXT N ROWS ONLY
// 2. OFFSET N → OFFSET N ROWS
// 3. Добавляет ORDER BY если нужен для OFFSET/FETCH (обязательно с 2012+)
// 4. Квалифицирует имена таблиц: [schema].[table]  (поддержка "schema.table" формата)
// 5. Квалифицирует имена полей: [field]
func (a *MSSQLAdapter) AdaptSQL(standardSQL, tableName string, schema packet.Schema, query *packet.Query) string {
	// Поддержка формата "schema.table" в tableName (например, "dbo.Users")
	tableName = tdtql.StripBrackets(tableName)
	schemaName := a.schemaName
	table := tableName
	if parts := strings.SplitN(tableName, ".", 2); len(parts) == 2 {
		schemaName = parts[0]
		table = parts[1]
	}

	// Квалифицируем имя таблицы: [schema].[table]
	// GenerateSQL wraps names with special chars in ANSI double-quotes (e.g. "ZTR$Timesheet Line").
	// Replace the ANSI-quoted form first to avoid partial substring corruption.
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, table)
	ansiTable := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
	sql := strings.Replace(standardSQL, ansiTable, fullTableName, 1)
	if sql == standardSQL {
		sql = strings.Replace(standardSQL, tableName, fullTableName, 1)
	}

	// Квалифицируем имена полей квадратными скобками.
	// Обрабатываем два варианта: ANSI "field" (из quoteFieldName для имён со спецсимволами)
	// и голое имя (для безопасных идентификаторов).
	for _, field := range schema.Fields {
		bracket := "[" + strings.ReplaceAll(field.Name, "]", "]]") + "]"
		ansi := `"` + strings.ReplaceAll(field.Name, `"`, `""`) + `"`
		// Сначала ANSI-форма (чтобы не дублировать для безопасных имён)
		sql = strings.ReplaceAll(sql, ansi, bracket)
		// Затем голое имя (безопасные идентификаторы, не обёрнутые quoteFieldName)
		sql = strings.ReplaceAll(sql, " "+field.Name+" ", " "+bracket+" ")
		sql = strings.ReplaceAll(sql, "("+field.Name+")", "("+bracket+")")
		sql = strings.ReplaceAll(sql, ","+field.Name+" ", ","+bracket+" ")
	}

	// SQL Server datetime does not accept ISO 8601 'Z' suffix; strip it.
	sql = isoDatetimeZ.ReplaceAllString(sql, "'$1'")

	// Apply LIMIT/OFFSET for SQL Server.
	//
	// Three strategies:
	//   TOP N        — limit-only, no offset. Works on all SQL Server versions (2000+).
	//   OFFSET/FETCH — limit+offset, requires SQL Server 2012+ (compat level 110+).
	//   Tail mode    — negative limit (--limit -N), "last N rows". Uses TOP N inside
	//                  a subquery (when ORDER BY is present) or just TOP N (no ORDER BY).
	//                  The outer ORDER BY in the subquery pattern preserves original order.
	//
	// Using TOP when possible avoids failures on older compat levels (80, 90, 100).
	switch {
	case query != nil && query.Limit > 0 && query.Offset == 0:
		// TOP N: inject after SELECT (handles SELECT DISTINCT too)
		limitPattern := fmt.Sprintf(" LIMIT %d", query.Limit)
		sql = strings.Replace(sql, limitPattern, "", 1)
		sql = strings.Replace(sql, "SELECT DISTINCT ", fmt.Sprintf("SELECT DISTINCT TOP %d ", query.Limit), 1)
		if !strings.Contains(sql, fmt.Sprintf("TOP %d", query.Limit)) {
			sql = strings.Replace(sql, "SELECT ", fmt.Sprintf("SELECT TOP %d ", query.Limit), 1)
		}
	case query != nil && (query.Limit > 0 || query.Offset > 0):
		// OFFSET/FETCH: SQL Server 2012+ only (compat level 110+)
		limitPattern := fmt.Sprintf(" LIMIT %d", query.Limit)
		offsetPattern := fmt.Sprintf(" OFFSET %d", query.Offset)
		sql = strings.Replace(sql, limitPattern, "", 1)
		sql = strings.Replace(sql, offsetPattern, "", 1)

		// ORDER BY is mandatory for OFFSET/FETCH.
		// Use the first *projected* column (from the SQL itself) so that the ORDER BY
		// key is always in the SELECT list — schema.Fields[0] may not be projected
		// when --fields restricts the column list.
		// For "SELECT *" (no --fields), fall back to the first non-read-only schema field
		// so we never ORDER BY timestamp/rowversion (which is cut by PostProcessRows).
		if !strings.Contains(sql, "ORDER BY") {
			if col := firstProjectedColumn(sql); col != "" {
				sql += " ORDER BY " + col
			} else if col := firstWritableColumn(schema); col != "" {
				sql += " ORDER BY " + col
			}
		}

		if query.Offset > 0 {
			sql += fmt.Sprintf(" OFFSET %d ROWS", query.Offset)
		} else {
			sql += " OFFSET 0 ROWS"
		}
		if query.Limit > 0 {
			sql += fmt.Sprintf(" FETCH NEXT %d ROWS ONLY", query.Limit)
		}
	case query != nil && query.Limit < 0:
		// Tail mode: --limit -N means "last N rows" (like tail -n).
		// sql_generator emits LIMIT N (where N = abs(query.Limit)).
		// Two sub-cases depending on whether ORDER BY was requested:
		//
		//   With ORDER BY: sql_generator wraps the query in a subquery:
		//     SELECT * FROM (SELECT ... ORDER BY col DESC LIMIT N) AS _tail ORDER BY col ASC
		//   → SQL Server:
		//     SELECT * FROM (SELECT TOP N ... ORDER BY col DESC) AS _tail ORDER BY col ASC
		//
		//   Without ORDER BY: build a subquery using the first schema field as fallback key.
		//     SELECT ... FROM [table] WHERE ... LIMIT N
		//   → SQL Server:
		//     SELECT * FROM (SELECT TOP N ... FROM [table] WHERE ... ORDER BY [col] DESC) AS _tail ORDER BY [col] ASC
		//
		//   The fallback key is the first schema field — same heuristic used by OFFSET/FETCH.
		//   For correct tail semantics, callers should specify --order-by explicitly.
		n := -query.Limit
		limitPattern := fmt.Sprintf(" LIMIT %d", n)
		if strings.Contains(sql, "AS _tail") {
			// Subquery tail pattern (ORDER BY was specified): inject TOP N into the inner SELECT.
			sql = strings.Replace(sql, limitPattern, "", 1)
			sql = strings.Replace(sql, "(SELECT DISTINCT ", fmt.Sprintf("(SELECT DISTINCT TOP %d ", n), 1)
			if !strings.Contains(sql, fmt.Sprintf("TOP %d", n)) {
				sql = strings.Replace(sql, "(SELECT ", fmt.Sprintf("(SELECT TOP %d ", n), 1)
			}
		} else {
			// No ORDER BY and no subquery pattern yet.
			// Build a subquery using the first *projected* column (from the SQL itself) as
			// the sort key — schema.Fields[0] must NOT be used here because it refers to
			// the full table schema, which may differ from the --fields projection.
			// Using firstProjectedColumn guarantees the ORDER BY key is in the SELECT list.
			sql = strings.Replace(sql, limitPattern, "", 1)
			orderCol := firstProjectedColumn(sql)
			if orderCol == "" {
				// "SELECT *" or unparseable projection: use first non-read-only schema field.
				// This skips timestamp/rowversion which is cut by PostProcessRows anyway.
				orderCol = firstWritableColumn(schema)
			}
			if orderCol != "" {
				// Subquery: SELECT TOP N ... ORDER BY col DESC → wrap → ORDER BY col ASC
				inner := strings.TrimRight(sql, " ")
				inner = strings.Replace(inner, "SELECT DISTINCT ", fmt.Sprintf("SELECT DISTINCT TOP %d ", n), 1)
				if !strings.Contains(inner, fmt.Sprintf("TOP %d", n)) {
					inner = strings.Replace(inner, "SELECT ", fmt.Sprintf("SELECT TOP %d ", n), 1)
				}
				inner += fmt.Sprintf(" ORDER BY %s DESC", orderCol)
				sql = fmt.Sprintf("SELECT * FROM (%s) AS _tail ORDER BY %s ASC", inner, orderCol)
			} else {
				// Degenerate (no schema, SELECT *): TOP N only, order undefined.
				sql = strings.Replace(sql, "SELECT DISTINCT ", fmt.Sprintf("SELECT DISTINCT TOP %d ", n), 1)
				if !strings.Contains(sql, fmt.Sprintf("TOP %d", n)) {
					sql = strings.Replace(sql, "SELECT ", fmt.Sprintf("SELECT TOP %d ", n), 1)
				}
			}
		}
	}

	return sql
}

// QuoteIdentifier квотирует идентификатор для SQL Server
func (a *MSSQLAdapter) QuoteIdentifier(identifier string) string {
	return fmt.Sprintf("[%s]", identifier)
}

// firstWritableColumn returns the first non-read-only field from schema, bracket-quoted.
// Used as ORDER BY fallback for "SELECT *" queries (no --fields projection) so that
// we never ORDER BY timestamp/rowversion or computed columns — those are cut by
// PostProcessRows and cannot be reliably ordered in a subquery context.
// Returns "" when schema has no writable fields.
func firstWritableColumn(schema packet.Schema) string {
	for _, f := range schema.Fields {
		if !f.ReadOnly {
			return fmt.Sprintf("[%s]", f.Name)
		}
	}
	return ""
}

// firstProjectedColumn extracts the first column name from an already-adapted SQL SELECT
// statement. Field names are expected to be bracket-quoted ([name]) at this point.
//
// This is used as a fallback ORDER BY key for tail mode and OFFSET/FETCH when no
// ORDER BY was specified. Reading from the SQL itself (rather than schema.Fields[0])
// ensures the chosen column is always part of the projection, even when --fields
// restricts the SELECT list.
//
// Returns "" if the projection is "*" or cannot be determined.
func firstProjectedColumn(sql string) string {
	upper := strings.ToUpper(sql)
	selIdx := strings.Index(upper, "SELECT ")
	fromIdx := strings.Index(upper, " FROM ")
	if selIdx < 0 || fromIdx <= selIdx+7 {
		return ""
	}
	projection := strings.TrimSpace(sql[selIdx+7 : fromIdx])

	// Skip "TOP N " injected earlier (e.g. "TOP 10 [Field1], ...").
	if strings.HasPrefix(strings.ToUpper(projection), "TOP ") {
		parts := strings.SplitN(projection, " ", 3)
		if len(parts) == 3 {
			projection = parts[2]
		}
	}

	// Wildcard: no safe column to pick.
	if projection == "*" || projection == "" {
		return ""
	}

	// First column: everything before the first top-level comma.
	// For bracket-quoted names like [Calendar Date] commas are only separators.
	first := projection
	if commaIdx := strings.Index(first, ","); commaIdx >= 0 {
		first = strings.TrimSpace(first[:commaIdx])
	}
	return first
}
