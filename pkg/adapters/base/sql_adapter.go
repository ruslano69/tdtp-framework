package base

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

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
	schemaName := a.schemaName
	table := tableName
	if parts := strings.SplitN(tableName, ".", 2); len(parts) == 2 {
		schemaName = parts[0]
		table = parts[1]
	}

	// Квалифицируем имя таблицы: [schema].[table]
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, table)
	sql := strings.Replace(standardSQL, tableName, fullTableName, 1)

	// Квалифицируем имена полей квадратными скобками
	for _, field := range schema.Fields {
		// Заменяем field на [field] в WHERE и ORDER BY
		sql = strings.ReplaceAll(sql, " "+field.Name+" ", " ["+field.Name+"] ")
		sql = strings.ReplaceAll(sql, "("+field.Name+")", "(["+field.Name+"])")
		sql = strings.ReplaceAll(sql, ","+field.Name+" ", ",["+field.Name+"] ")
	}

	// Заменяем LIMIT/OFFSET на SQL Server синтаксис (SQL Server 2012+)
	if query != nil && (query.Limit > 0 || query.Offset > 0) {
		// Убираем LIMIT N и OFFSET N из стандартного SQL
		limitPattern := fmt.Sprintf(" LIMIT %d", query.Limit)
		offsetPattern := fmt.Sprintf(" OFFSET %d", query.Offset)
		sql = strings.Replace(sql, limitPattern, "", 1)
		sql = strings.Replace(sql, offsetPattern, "", 1)

		// SQL Server требует ORDER BY для OFFSET/FETCH
		hasOrderBy := strings.Contains(sql, "ORDER BY")
		if !hasOrderBy && len(schema.Fields) > 0 {
			// Добавляем ORDER BY по первому полю (обязательно для OFFSET/FETCH)
			sql += fmt.Sprintf(" ORDER BY [%s]", schema.Fields[0].Name)
		}

		// OFFSET ... ROWS
		if query.Offset > 0 {
			sql += fmt.Sprintf(" OFFSET %d ROWS", query.Offset)
		} else {
			// OFFSET 0 ROWS обязателен если есть FETCH
			sql += " OFFSET 0 ROWS"
		}

		// FETCH NEXT ... ROWS ONLY
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
