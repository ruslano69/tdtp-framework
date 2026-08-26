package etl

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// Workspace представляет SQLite :memory: рабочую среду для ETL операций
// Используется для загрузки данных из нескольких источников и выполнения JOIN запросов
type Workspace struct {
	adapter adapters.Adapter
	db      *sql.DB
	tables  map[string]bool // Список созданных таблиц
}

// NewWorkspace создает новый :memory: workspace
func NewWorkspace(ctx context.Context) (*Workspace, error) {
	// Создаем SQLite адаптер с :memory: базой
	adapter, err := adapters.New(ctx, adapters.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace adapter: %w", err)
	}

	// Получаем прямой доступ к *sql.DB
	// Используем type assertion для получения DB()
	sqliteAdapter, ok := adapter.(interface{ DB() *sql.DB })
	if !ok {
		_ = adapter.Close(ctx)
		return nil, fmt.Errorf("adapter does not support DB() method")
	}

	return &Workspace{
		adapter: adapter,
		db:      sqliteAdapter.DB(),
		tables:  make(map[string]bool),
	}, nil
}

// CreateTable создает таблицу в workspace на основе схемы TDTP пакета
func (w *Workspace) CreateTable(ctx context.Context, tableName string, fields []packet.Field) error {
	if tableName == "" {
		return fmt.Errorf("table name is required")
	}

	if len(fields) == 0 {
		return fmt.Errorf("at least one field is required")
	}

	// Генерируем DDL для создания таблицы
	ddl := w.generateCreateTableDDL(tableName, fields)

	// Выполняем CREATE TABLE
	if _, err := w.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("failed to create table %s: %w", tableName, err)
	}

	// Отмечаем таблицу как созданную
	w.tables[tableName] = true

	return nil
}

// LoadData загружает данные из TDTP пакета в таблицу workspace
func (w *Workspace) LoadData(ctx context.Context, tableName string, dataPacket *packet.DataPacket) error {
	if !w.tables[tableName] {
		return fmt.Errorf("table %s does not exist in workspace", tableName)
	}

	rows := dataPacket.GetRows()
	if len(rows) == 0 {
		return nil // Нет данных для загрузки
	}

	// Парсим данные и вставляем в таблицу
	fields := dataPacket.Schema.Fields
	numFields := len(fields)

	rowTuple := "(" + strings.TrimSuffix(strings.Repeat("?, ", numFields), ", ") + ")"
	batchRows := insertBatchRows(numFields)

	// Начинаем транзакцию для производительности
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback() // игнорируем ошибку, если tx.Commit() был успешным
	}()

	// Два подготовленных запроса: на полную пачку и на одну строку — хвост
	// почти никогда не кратен пачке, а готовить под каждый остаток свой
	// statement дороже, чем дописать хвост по одной.
	single, err := tx.PrepareContext(ctx,
		fmt.Sprintf("INSERT INTO %q VALUES %s", tableName, rowTuple))
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer func() { _ = single.Close() }()

	bulk := single
	if batchRows > 1 {
		bulk, err = tx.PrepareContext(ctx, fmt.Sprintf("INSERT INTO %q VALUES %s",
			tableName, strings.TrimSuffix(strings.Repeat(rowTuple+", ", batchRows), ", ")))
		if err != nil {
			return fmt.Errorf("failed to prepare batch insert statement: %w", err)
		}
		defer func() { _ = bulk.Close() }()
	}

	args := make([]any, 0, batchRows*numFields)
	batchStart := 0

	for i, values := range rows {
		if len(values) != numFields {
			return fmt.Errorf("row %d has %d values, expected %d", i, len(values), numFields)
		}
		for j, val := range values {
			args = append(args, w.convertValue(val, fields[j].Type))
		}

		if len(args) == batchRows*numFields {
			if _, err := bulk.ExecContext(ctx, args...); err != nil {
				return fmt.Errorf("failed to insert rows %d-%d: %w", batchStart, i, err)
			}
			args = args[:0]
			batchStart = i + 1
		}
	}

	// Хвост: строки, не набравшие полную пачку.
	for i := 0; i*numFields < len(args); i++ {
		if _, err := single.ExecContext(ctx, args[i*numFields:(i+1)*numFields]...); err != nil {
			return fmt.Errorf("failed to insert row %d: %w", batchStart+i, err)
		}
	}

	// Коммитим транзакцию
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// insertBatchRows — сколько строк класть в один многострочный INSERT.
//
// Считается от ЧИСЛА ПАРАМЕТРОВ, а не строк, и это измеренная величина, а не
// круглое число. Оптимум держится около 60 параметров на запрос независимо от
// ширины таблицы: на 6 колонках лучшая пачка — 10 строк, на 20 колонках — 3,
// и в обоих случаях это те же ~60 плейсхолдеров.
//
// Крупные пачки ХУЖЕ построчной вставки, и заметно: на 100k×6 пачка по 150
// строк (900 параметров) дала 406 мс против 270 мс у построчного цикла. SQLite
// тратит на такую VDBE-программу больше, чем экономится на вызовах Exec, так
// что «чем больше, тем лучше» здесь просто неверно.
//
// Выигрыш тоже неравномерный: 100k×6 — 270 → 189 мс (−30%), а на 20 колонках
// всего −8%, потому что там доля накладных расходов на вызов меньше.
//
// Верхняя граница — ограничение SQLite в 999 переменных на запрос; при очень
// широкой таблице пачка вырождается в одну строку, то есть в прежнее поведение.
func insertBatchRows(numFields int) int {
	const targetParams = 60
	if numFields <= 0 || numFields > targetParams {
		return 1
	}
	n := targetParams / numFields
	if n*numFields > 900 {
		n = 900 / numFields
	}
	if n < 1 {
		return 1
	}
	return n
}

// ExecuteSQL выполняет SQL запрос в workspace и возвращает результат как DataPacket
func (w *Workspace) ExecuteSQL(ctx context.Context, sqlQuery, resultTableName string) (*packet.DataPacket, error) {
	// Выполняем SELECT запрос
	rows, err := w.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Получаем информацию о колонках
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	// Создаем пакет для результата
	result := packet.NewDataPacket(packet.TypeReference, resultTableName)

	// Заполняем схему
	result.Schema.Fields = make([]packet.Field, len(columns))
	for i, col := range columns {
		result.Schema.Fields[i] = packet.Field{
			Name: col,
			Type: w.mapSQLiteTypeToTDTP(columnTypes[i].DatabaseTypeName()),
		}
	}

	// Читаем данные. Всё сканируется в any — см. dateColumnKinds о том, почему
	// у дат нет отдельной привязки к *string.
	values := make([]any, len(columns))
	valuePtrs := make([]any, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}
	kinds := dateColumnKinds(columnTypes)

	var allRows [][]string
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		rowValues := make([]string, len(values))
		for i := range values {
			rowValues[i] = w.formatCell(values[i], kinds[i])
		}

		allRows = append(allRows, rowValues)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}

	result.Data = packet.RowsToData(allRows)
	result.Header.RecordsInPart = len(allRows)

	return result, nil
}

// StreamingResult содержит схему и канал с данными для потоковой обработки
type StreamingResult struct {
	Schema    packet.Schema
	RowsChan  <-chan []string
	ErrorChan <-chan error
}

// ExecuteSQLStream выполняет SQL запрос и возвращает данные через channel (streaming)
// Используется для экспорта больших объемов данных в RabbitMQ/Kafka без загрузки всего в память
func (w *Workspace) ExecuteSQLStream(ctx context.Context, sqlQuery, resultTableName string) (*StreamingResult, error) {
	// Выполняем SELECT запрос
	rows, err := w.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}

	// Получаем информацию о колонках
	columns, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	// Создаем схему
	pktSchema := packet.Schema{
		Fields: make([]packet.Field, len(columns)),
	}
	for i, col := range columns {
		pktSchema.Fields[i] = packet.Field{
			Name: col,
			Type: w.mapSQLiteTypeToTDTP(columnTypes[i].DatabaseTypeName()),
		}
	}

	// Создаем каналы для передачи данных
	rowsChan := make(chan []string, 100) // Буферизованный канал для производительности
	errorChan := make(chan error, 1)

	// Запускаем горутину для чтения данных
	go func() {
		defer close(rowsChan)
		defer close(errorChan)
		defer func() { _ = rows.Close() }()

		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		kinds := dateColumnKinds(columnTypes)

		for rows.Next() {
			// Проверяем контекст
			select {
			case <-ctx.Done():
				errorChan <- ctx.Err()
				return
			default:
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				errorChan <- fmt.Errorf("failed to scan row: %w", err)
				return
			}

			rowValues := make([]string, len(values))
			for i := range values {
				rowValues[i] = w.formatCell(values[i], kinds[i])
			}

			select {
			case rowsChan <- rowValues:
			case <-ctx.Done():
				errorChan <- ctx.Err()
				return
			}
		}

		if err := rows.Err(); err != nil {
			errorChan <- fmt.Errorf("error reading rows: %w", err)
			return
		}
	}()

	return &StreamingResult{
		Schema:    pktSchema,
		RowsChan:  rowsChan,
		ErrorChan: errorChan,
	}, nil
}

// Close закрывает workspace
func (w *Workspace) Close(ctx context.Context) error {
	if w.adapter != nil {
		return w.adapter.Close(ctx)
	}
	return nil
}

// generateCreateTableDDL генерирует DDL для создания таблицы
func (w *Workspace) generateCreateTableDDL(tableName string, fields []packet.Field) string {
	columns := make([]string, 0, len(fields))

	for _, field := range fields {
		sqliteType := w.mapTDTPTypeToSQLite(schema.DataType(field.Type))
		column := fmt.Sprintf("%q %s", field.Name, sqliteType)
		columns = append(columns, column)
	}

	return fmt.Sprintf("CREATE TABLE %q (%s)", tableName, strings.Join(columns, ", "))
}

// mapTDTPTypeToSQLite конвертирует TDTP тип в SQLite тип
func (w *Workspace) mapTDTPTypeToSQLite(tdtpType schema.DataType) string {
	switch tdtpType {
	case schema.TypeInteger, schema.TypeInt:
		return "INTEGER"
	case schema.TypeReal, schema.TypeFloat, schema.TypeDouble, schema.TypeDecimal:
		return "REAL"
	case schema.TypeBoolean, schema.TypeBool:
		return "INTEGER" // SQLite хранит boolean как 0/1
	case schema.TypeDate:
		return "DATE" // Сохраняем имя типа — DatabaseTypeName() вернёт "DATE"
	case schema.TypeDatetime, schema.TypeTimestamp:
		return "DATETIME" // Аналогично для DATETIME
	case schema.TypeBlob:
		return "BLOB"
	default:
		return "TEXT"
	}
}

// mapSQLiteTypeToTDTP конвертирует SQLite тип в TDTP тип.
// Для DATE/DATETIME колонок SQLite сохраняет объявленное имя типа —
// DatabaseTypeName() возвращает "DATE"/"DATETIME", а не "TEXT".
func (w *Workspace) mapSQLiteTypeToTDTP(sqliteType string) string {
	sqliteType = strings.ToUpper(sqliteType)
	switch {
	case strings.Contains(sqliteType, "INT"):
		return "INTEGER"
	case strings.Contains(sqliteType, "REAL"), strings.Contains(sqliteType, "FLOAT"), strings.Contains(sqliteType, "DOUBLE"):
		return "REAL"
	case strings.Contains(sqliteType, "BLOB"):
		return "BLOB"
	case sqliteType == "DATE":
		return "DATE"
	case strings.Contains(sqliteType, "DATETIME"), strings.Contains(sqliteType, "TIMESTAMP"):
		return "DATETIME"
	default:
		return "TEXT"
	}
}

// convertValue конвертирует строковое значение в правильный тип для SQLite
func (w *Workspace) convertValue(value, fieldType string) any {
	// NULL значения
	if value == "" || value == "NULL" {
		return nil
	}

	// Для SQLite все значения могут храниться как есть (динамическая типизация)
	// Но для корректности попробуем конвертировать
	tdtpType := schema.DataType(fieldType)
	switch tdtpType {
	case schema.TypeInteger, schema.TypeInt:
		// SQLite сам конвертирует строку в INTEGER если возможно
		return value
	case schema.TypeReal, schema.TypeFloat, schema.TypeDouble, schema.TypeDecimal:
		return value
	case schema.TypeBoolean, schema.TypeBool:
		// Конвертируем boolean в 0/1
		if value == "true" || value == "1" || value == "TRUE" {
			return 1
		}
		return 0
	default:
		return value
	}
}

// dateColumnKinds размечает колонки: "" — обычная, "DATE" — только дата,
// "DATETIME" — дата со временем.
//
// Раньше на этом же признаке DATE/DATETIME/TIMESTAMP колонки привязывались к
// *string, «иначе modernc парсит в time.Time». Привязка ничего не отменяла:
// драйвер решает, разбирать ли ячейку, по ОБЪЯВЛЕННОМУ типу колонки, а не по
// типу приёмника, — а database/sql потом форматировал time.Time обратно в
// строку. Лишний проход, и один отказ: NULL в такой колонке ронял весь запрос
// с "converting NULL to string is unsupported", то есть пайплайн с пустой
// датой в результате трансформации падал целиком.
//
// Та же ловушка описана в CLAUDE.md для адаптера SQLite, где её уже чинили;
// здесь она осталась.
func dateColumnKinds(columnTypes []*sql.ColumnType) []string {
	kinds := make([]string, len(columnTypes))
	for i, ct := range columnTypes {
		dbType := strings.ToUpper(ct.DatabaseTypeName())
		switch {
		case dbType == "DATE":
			kinds[i] = "DATE"
		case strings.Contains(dbType, "DATETIME"), strings.Contains(dbType, "TIMESTAMP"):
			kinds[i] = "DATETIME"
		}
	}
	return kinds
}

// formatCell приводит значение к строке пакета с учётом того, датовая колонка
// или нет.
//
// Байты для непустых дат те же, что давал normalizeDateString: "2006-01-02"
// для DATE и "2006-01-02 15:04:05" для остальных. NULL становится пустой
// строкой — тем же представлением, которое convertValue понимает как NULL при
// загрузке обратно.
func (w *Workspace) formatCell(val any, kind string) string {
	if kind == "" {
		return w.formatValue(val)
	}
	switch v := val.(type) {
	case nil:
		return ""
	case time.Time:
		if kind == "DATE" {
			return v.Format("2006-01-02")
		}
		return v.Format("2006-01-02 15:04:05")
	case string:
		return normalizeDateString(v, kind)
	case []byte:
		return normalizeDateString(string(v), kind)
	default:
		return w.formatValue(val)
	}
}

// normalizeDateString нормализует строку даты/времени после round-trip через modernc.
// modernc при *string scan для DATE-колонок возвращает RFC3339 ("1990-05-15T00:00:00Z"),
// а не исходную строку. Парсим обратно и форматируем в стандартный TDTP формат.
func normalizeDateString(s, dbType string) string {
	if s == "" {
		return s
	}
	// Пробуем распарсить распространённые форматы
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			if dbType == "DATE" {
				return t.Format("2006-01-02")
			}
			return t.Format("2006-01-02 15:04:05")
		}
	}
	return s // если не распознали — вернуть как есть
}

// formatValue конвертирует значение из SQL в строку для TDTP
func (w *Workspace) formatValue(val any) string {
	if val == nil {
		return ""
	}

	switch v := val.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		// 'f', а не %g: %g уходит в экспоненту на больших значениях, а проверка
		// scale в parseDecimal режет строку по точке и принимает мантиссу за
		// дробную часть — DECIMAL(12,2) со значением 9999999999.99 доезжал до
		// пакета как "9.99999999999e+09".
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}
