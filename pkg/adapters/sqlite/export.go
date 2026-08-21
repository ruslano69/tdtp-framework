package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// ========== Делегирование в ExportHelper ==========

// SetSkipSpecialValues включает режим --fast: DetectAndApply пропускается.
func (a *Adapter) SetSkipSpecialValues(skip bool) {
	a.exportHelper.SetSkipSpecialValues(skip)
}

// SetMaxFallbackRows задаёт лимит строк для in-memory fallback при провале SQL pushdown.
func (a *Adapter) SetMaxFallbackRows(n int64) {
	a.exportHelper.SetMaxFallbackRows(n)
}

// ExportTable экспортирует всю таблицу в TDTP reference пакеты
// Делегирует выполнение в base.ExportHelper
func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
	return a.exportHelper.ExportTable(ctx, tableName)
}

// ExportTableWithQuery экспортирует таблицу с применением TDTQL фильтрации
// Делегирует выполнение в base.ExportHelper с автоматической SQL оптимизацией
func (a *Adapter) ExportTableWithQuery(ctx context.Context, tableName string, query *packet.Query, sender, recipient string) ([]*packet.DataPacket, error) {
	return a.exportHelper.ExportTableWithQuery(ctx, tableName, query, sender, recipient)
}

// ExportTableIncremental экспортирует только измененные записи с момента последней синхронизации
// Пока не реализовано для SQLite адаптера
func (a *Adapter) ExportTableIncremental(ctx context.Context, tableName string, incrementalConfig adapters.IncrementalConfig) ([]*packet.DataPacket, string, error) {
	return nil, "", fmt.Errorf("incremental export not yet implemented for SQLite adapter")
}

// ========== Реализация интерфейсов для ExportHelper ==========

// GetTableSchema читает схему таблицы из SQLite
// Реализует base.SchemaReader интерфейс
func (a *Adapter) GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error) {
	tableName = tdtql.StripBrackets(tableName)
	query := fmt.Sprintf("PRAGMA table_info(\"%s\")", tableName) //nolint:gocritic // SQL identifier quoting, not Go string quoting

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return packet.Schema{}, fmt.Errorf("failed to get table info: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var fields []packet.Field

	for rows.Next() {
		var (
			cid       int
			name      string
			dataType  string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)

		if err := rows.Scan(&cid, &name, &dataType, &notNull, &dfltValue, &pk); err != nil {
			return packet.Schema{}, fmt.Errorf("failed to scan column info: %w", err)
		}

		field, err := BuildFieldFromColumn(name, dataType, pk == 1)
		if err != nil {
			return packet.Schema{}, fmt.Errorf("failed to build field: %w", err)
		}

		// SQLite не хранит ограничения длины для TEXT полей
		// Оставляем Length = 0, что означает "неограниченная длина"

		fields = append(fields, field)
	}

	if err := rows.Err(); err != nil {
		return packet.Schema{}, fmt.Errorf("error iterating columns: %w", err)
	}

	if len(fields) == 0 {
		return packet.Schema{}, fmt.Errorf("table %s not found or has no columns", tableName)
	}

	return packet.Schema{Fields: fields}, nil
}

// selectExprForField возвращает выражение для колонки в SELECT.
//
// Колонки дат берутся через CAST(... AS TEXT), и это самая крупная экономия на
// чтении из всех, что здесь есть. modernc.org/sqlite решает, разбирать ли
// ячейку в time.Time, по ОБЪЯВЛЕННОМУ типу колонки — DATE, DATETIME,
// TIMESTAMP. У выражения объявленного типа нет, поэтому разбор не запускается
// вовсе. Измерено на 50k строк с тремя колонками дат: 351 мс → 103 мс, 45
// аллокаций на строку → 11. Разбор внутри драйвера стоит около 1.65 мкс и 11
// аллокаций на ячейку — на порядок больше всего, что делает конверсионный слой
// на нашей стороне.
//
// Значение при этом не портится. Текстовое хранение возвращается байт в байт
// как записано (для этой формы и писался normalizeSQLiteDateTime), INTEGER
// даёт те же цифры, что и strconv сегодня, а REAL — своё точное хранимое
// представление вместо сегодняшнего "2.46090911e+06".
//
// Приём применим только там, где SELECT строим мы сами. Для произвольного
// запроса (ReadRowsWithSQL, TDTQL, вьюхи) переписывать проекцию нельзя, и там
// всё остаётся по-старому.
func selectExprForField(field packet.Field) string {
	quoted := fmt.Sprintf("\"%s\"", field.Name) //nolint:gocritic // SQL identifier quoting, not Go string quoting
	if !packet.IsDateFieldType(field.Type) {
		return quoted
	}
	// Псевдоним сохраняет имя колонки в результате — на случай, если до него
	// кто-то доберётся через rows.Columns().
	return fmt.Sprintf("CAST(%s AS TEXT) AS %s", quoted, quoted)
}

// ReadAllRows читает все строки из таблицы
// Реализует base.DataReader интерфейс
func (a *Adapter) ReadAllRows(ctx context.Context, tableName string, schema packet.Schema) ([][]string, error) {
	tableName = tdtql.StripBrackets(tableName)
	// Формируем список полей для SELECT — квотируем каждое имя на случай пробелов
	fieldNames := make([]string, len(schema.Fields))
	for i, field := range schema.Fields {
		fieldNames[i] = selectExprForField(field)
	}

	quotedTable := fmt.Sprintf("\"%s\"", tableName) //nolint:gocritic // SQL identifier quoting, not Go string quoting
	query := fmt.Sprintf("SELECT %s FROM %s",
		strings.Join(fieldNames, ", "),
		quotedTable)

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query table: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return a.scanRows(rows, schema)
}

// ReadRowsWithSQL читает строки используя произвольный SQL запрос
// Реализует base.DataReader интерфейс
func (a *Adapter) ReadRowsWithSQL(ctx context.Context, sqlQuery string, schema packet.Schema) ([][]string, error) {
	rows, err := a.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return a.scanRows(rows, schema)
}

// GetRowCount возвращает количество строк в таблице
// Реализует base.DataReader интерфейс
func (a *Adapter) GetRowCount(ctx context.Context, tableName string) (int64, error) {
	tableName = tdtql.StripBrackets(tableName)
	quotedTable := fmt.Sprintf("\"%s\"", tableName) //nolint:gocritic // SQL identifier quoting, not Go string quoting
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", quotedTable)

	var count int64
	err := a.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count rows: %w", err)
	}

	return count, nil
}

// ========== Вспомогательные функции (SQLite-специфичные) ==========

// scanRows сканирует sql.Rows в [][]string
// Используется ReadAllRows и ReadRowsWithSQL
func (a *Adapter) scanRows(rows *sql.Rows, schema packet.Schema) ([][]string, error) {
	return base.ScanSQLRows(rows, schema, a.converter, "sqlite")
}
