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
	// Two strategies:
	//   TOP N        — limit-only, no offset. Works on all SQL Server versions (2000+).
	//   OFFSET/FETCH — limit+offset, requires SQL Server 2012+ (compat level 110+).
	//
	// Using TOP when possible avoids failures on older compat levels (80, 90, 100).
	if query != nil && query.Limit > 0 && query.Offset == 0 {
		// TOP N: inject after SELECT (handles SELECT DISTINCT too)
		limitPattern := fmt.Sprintf(" LIMIT %d", query.Limit)
		sql = strings.Replace(sql, limitPattern, "", 1)
		sql = strings.Replace(sql, "SELECT DISTINCT ", fmt.Sprintf("SELECT DISTINCT TOP %d ", query.Limit), 1)
		if !strings.Contains(sql, fmt.Sprintf("TOP %d", query.Limit)) {
			sql = strings.Replace(sql, "SELECT ", fmt.Sprintf("SELECT TOP %d ", query.Limit), 1)
		}
	} else if query != nil && (query.Limit > 0 || query.Offset > 0) {
		// OFFSET/FETCH: SQL Server 2012+ only (compat level 110+)
		limitPattern := fmt.Sprintf(" LIMIT %d", query.Limit)
		offsetPattern := fmt.Sprintf(" OFFSET %d", query.Offset)
		sql = strings.Replace(sql, limitPattern, "", 1)
		sql = strings.Replace(sql, offsetPattern, "", 1)

		// ORDER BY is mandatory for OFFSET/FETCH
		if !strings.Contains(sql, "ORDER BY") && len(schema.Fields) > 0 {
			sql += fmt.Sprintf(" ORDER BY [%s]", schema.Fields[0].Name)
		}

		if query.Offset > 0 {
			sql += fmt.Sprintf(" OFFSET %d ROWS", query.Offset)
		} else {
			sql += " OFFSET 0 ROWS"
		}
		if query.Limit > 0 {
			sql += fmt.Sprintf(" FETCH NEXT %d ROWS ONLY", query.Limit)
		}
	}

	return sql
}

// QuoteIdentifier квотирует идентификатор для SQL Server
func (a *MSSQLAdapter) QuoteIdentifier(identifier string) string {
	return fmt.Sprintf("[%s]", identifier)
}
