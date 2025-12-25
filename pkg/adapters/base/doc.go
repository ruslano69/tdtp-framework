// Package base предоставляет общие хелперы и утилиты для всех адаптеров БД
//
// Этот пакет устраняет дублирование кода между адаптерами (SQLite, PostgreSQL, MS SQL Server, MySQL)
// путем вынесения общей логики экспорта, импорта и конвертации типов в переиспользуемые компоненты.
//
// # Основные компоненты
//
// ExportHelper - общая логика экспорта данных в TDTP пакеты:
//   - ExportTable() - экспорт всей таблицы
//   - ExportTableWithQuery() - экспорт с TDTQL фильтрацией и SQL оптимизацией
//   - ExportTableIncremental() - инкрементальная синхронизация
//
// ImportHelper - общая логика импорта TDTP пакетов в БД:
//   - ImportPacket() - импорт одного пакета
//   - ImportPackets() - импорт нескольких пакетов атомарно
//   - Поддержка временных таблиц для атомарной замены
//
// UniversalTypeConverter - универсальная конвертация типов данных:
//   - ConvertValueToTDTP() - БД → TDTP формат
//   - DBValueToString() - значение БД → строка (с учетом специфики СУБД)
//   - TypedValueToSQL() - TDTP → SQL значение для PreparedStatement
//   - Поддержка PostgreSQL-специфичных типов (UUID, JSONB, NUMERIC)
//   - Поддержка MS SQL-специфичных типов (UNIQUEIDENTIFIER, TIMESTAMP/ROWVERSION)
//
// # Использование
//
// Для создания адаптера необходимо:
//
// 1. Реализовать интерфейсы для ExportHelper:
//   - SchemaReader (GetTableSchema)
//   - DataReader (ReadAllRows, ReadRowsWithSQL, GetRowCount)
//   - SQLAdapter (AdaptSQL) - опционально, для адаптации SQL под СУБД
//
// 2. Реализовать интерфейсы для ImportHelper:
//   - TableManager (TableExists, CreateTable, DropTable, RenameTable)
//   - DataInserter (InsertRows)
//   - TransactionManager (BeginTx)
//
// 3. Создать хелперы в адаптере:
//
//	type Adapter struct {
//	    db           *sql.DB
//	    exportHelper *base.ExportHelper
//	    importHelper *base.ImportHelper
//	    converter    *base.UniversalTypeConverter
//	}
//
//	func (a *Adapter) initHelpers() {
//	    a.converter = base.NewUniversalTypeConverter()
//	    a.exportHelper = base.NewExportHelper(a, a, a.converter, nil)
//	    a.importHelper = base.NewImportHelper(a, a, a, true) // useTemporaryTables = true
//	}
//
// 4. Делегировать методы интерфейса Adapter хелперам:
//
//	func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
//	    return a.exportHelper.ExportTable(ctx, tableName)
//	}
//
//	func (a *Adapter) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
//	    return a.importHelper.ImportPacket(ctx, pkt, strategy)
//	}
//
// # Эффект от использования
//
// Использование base пакета позволяет:
//   - Сократить код адаптеров на ~60-70% (с ~1000 строк до ~300 строк)
//   - Устранить дублирование логики экспорта (~800 строк дублированного кода)
//   - Устранить дублирование логики импорта (~600 строк дублированного кода)
//   - Устранить дублирование конвертации типов (~300 строк дублированного кода)
//   - Упростить поддержку и добавление новых адаптеров
//   - Обеспечить консистентность поведения между адаптерами
//
// # Совместимость
//
// Пакет совместим с:
//   - pkg/core/packet - генерация и парсинг TDTP пакетов
//   - pkg/core/schema - система типов данных
//   - pkg/core/tdtql - язык запросов и SQL оптимизация
//   - pkg/etl - ETL конвейеры (использует packet.Generator)
//   - Все существующие адаптеры (SQLite, PostgreSQL, MS SQL Server, MySQL)
//
// # Архитектурные принципы
//
// - Dependency Inversion: хелперы зависят от интерфейсов, не от конкретных реализаций
// - Single Responsibility: каждый хелпер отвечает за свою область (export/import/conversion)
// - Open/Closed: легко расширяется новыми адаптерами без изменения базового кода
// - DRY (Don't Repeat Yourself): устранение дублирования кода
//
package base
