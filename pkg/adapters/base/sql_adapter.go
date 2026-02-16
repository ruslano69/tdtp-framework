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
// 3. Добавляет ORDER BY если нужен для OFFSET/FETCH
// 4. Квалифицирует имена таблиц: [schema].[table]
// 5. Квалифицирует имена полей: [field]
func (a *MSSQLAdapter) AdaptSQL(standardSQL, tableName string, schema packet.Schema, query *packet.Query) string {
	// Квалифицируем имя таблицы: [schema].[table]
	fullTableName := fmt.Sprintf("[%s].[%s]", a.schemaName, tableName)
	sql := strings.Replace(standardSQL, tableName, fullTableName, 1)

	// Квалифицируем имена полей квадратными скобками
	for _, field := range schema.Fields {
		// Заменяем field на [field] в WHERE и ORDER BY
		sql = strings.ReplaceAll(sql, " "+field.Name+" ", " ["+field.Name+"] ")
		sql = strings.ReplaceAll(sql, "("+field.Name+")", "(["+field.Name+"])")
		sql = strings.ReplaceAll(sql, ","+field.Name+" ", ",["+field.Name+"] ")
	}

	// Заменяем LIMIT/OFFSET на SQL Server синтаксис
	if query != nil && (query.Limit > 0 || query.Offset > 0) {
		// Убираем LIMIT N и OFFSET N из стандартного SQL
		limitPattern := fmt.Sprintf(" LIMIT %d", query.Limit)
		offsetPattern := fmt.Sprintf(" OFFSET %d", query.Offset)
		sql = strings.Replace(sql, limitPattern, "", 1)
		sql = strings.Replace(sql, offsetPattern, "", 1)

		// SQL Server требует ORDER BY для OFFSET/FETCH
		// Проверяем есть ли ORDER BY в запросе
		hasOrderBy := strings.Contains(sql, "ORDER BY")
		if !hasOrderBy && len(schema.Fields) > 0 {
			// Добавляем ORDER BY по первому полю (обязательно для OFFSET/FETCH)
			sql += fmt.Sprintf(" ORDER BY [%s]", schema.Fields[0].Name)
		}

		// Добавляем OFFSET ... ROWS
		if query.Offset > 0 {
			sql += fmt.Sprintf(" OFFSET %d ROWS", query.Offset)
		} else {
			// OFFSET 0 ROWS обязателен если есть FETCH
			sql += " OFFSET 0 ROWS"
		}

		// Добавляем FETCH NEXT ... ROWS ONLY
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
