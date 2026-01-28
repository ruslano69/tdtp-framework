package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// ========== Делегирование в ExportHelper ==========

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
	query := fmt.Sprintf("PRAGMA table_info(%s)", tableName)

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return packet.Schema{}, fmt.Errorf("failed to get table info: %w", err)
	}
	defer rows.Close()

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

// ReadAllRows читает все строки из таблицы
// Реализует base.DataReader интерфейс
func (a *Adapter) ReadAllRows(ctx context.Context, tableName string, schema packet.Schema) ([][]string, error) {
	// Формируем список полей для SELECT
	fieldNames := make([]string, len(schema.Fields))
	for i, field := range schema.Fields {
		fieldNames[i] = field.Name
	}

	query := fmt.Sprintf("SELECT %s FROM %s",
		strings.Join(fieldNames, ", "),
		tableName)

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query table: %w", err)
	}
	defer rows.Close()

	return a.scanRows(rows, schema)
}

// ReadRowsWithSQL читает строки используя произвольный SQL запрос
// Реализует base.DataReader интерфейс
func (a *Adapter) ReadRowsWithSQL(ctx context.Context, sqlQuery string, schema packet.Schema) ([][]string, error) {
	rows, err := a.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query: %w", err)
	}
	defer rows.Close()

	return a.scanRows(rows, schema)
}

// GetRowCount возвращает количество строк в таблице
// Реализует base.DataReader интерфейс
func (a *Adapter) GetRowCount(ctx context.Context, tableName string) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)

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
	var result [][]string

	// Подготавливаем scanner для всех колонок
	scanArgs := make([]interface{}, len(schema.Fields))
	for i := range scanArgs {
		var v sql.NullString
		scanArgs[i] = &v
	}

	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Конвертируем в строки согласно TDTP формату
		row := make([]string, len(schema.Fields))
		for i, arg := range scanArgs {
			v := arg.(*sql.NullString)
			if v.Valid {
				// Используем универсальный конвертер из base
				row[i] = a.converter.ConvertValueToTDTP(schema.Fields[i], v.String)
			} else {
				row[i] = "" // NULL представляется пустой строкой
			}
		}

		result = append(result, row)
	}

	return result, rows.Err()
}
