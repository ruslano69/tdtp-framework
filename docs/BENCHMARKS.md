# TDTP Framework - Benchmark Tests

Документация по производительности адаптеров TDTP Framework.

## 📊 Обзор

В рамках v1.0 реализованы comprehensive benchmark тесты для измерения производительности:
- Фабрики адаптеров
- Стратегий импорта (REPLACE, IGNORE, COPY, FAIL)
- Сравнения PostgreSQL vs SQLite
- Batch vs Single операций

## 🧪 Запуск бенчмарков

### Все бенчмарки

```bash
cd /home/user/tdtp-framework
go test -bench=. -benchmem ./pkg/adapters/...
```

### Конкретные категории

**Фабрика адаптеров:**
```bash
go test -bench=BenchmarkFactory -benchmem ./pkg/adapters/
```

**Стратегии импорта:**
```bash
go test -bench=BenchmarkImportStrategy -benchmem ./pkg/adapters/
```

**Сравнение БД:**
```bash
go test -bench=BenchmarkDatabase -benchmem ./pkg/adapters/
```

### Параметры бенчмаркинга

**Длительность тестирования:**
```bash
go test -bench=. -benchtime=10s ./pkg/adapters/
```

**Количество итераций:**
```bash
go test -bench=. -benchtime=1000x ./pkg/adapters/
```

**Профилирование CPU:**
```bash
go test -bench=. -cpuprofile=cpu.prof ./pkg/adapters/
go tool pprof cpu.prof
```

**Профилирование памяти:**
```bash
go test -bench=. -memprofile=mem.prof ./pkg/adapters/
go tool pprof mem.prof
```

## 📁 Структура тестов

### 1. Factory Benchmarks (`factory_benchmark_test.go`)

**BenchmarkFactory_CreateAdapter**
- Измеряет скорость создания адаптера через фабрику
- Одиночное создание SQLite adapter

**BenchmarkFactory_CreateAdapter_Parallel**
- Параллельное создание адаптеров
- Тестирует thread-safety фабрики

**BenchmarkFactory_Overhead**
- Сравнивает overhead фабрики vs прямого подключения
- Две sub-benchmarks: ThroughFactory и DirectConnection

**BenchmarkAdapter_Operations**
- Базовые операции: Ping, GetDatabaseType, GetDatabaseVersion, GetTableNames, TableExists
- Позволяет найти узкие места в метаданных

**BenchmarkAdapter_Transactions**
- BeginCommit: Begin → Commit цикл
- BeginRollback: Begin → Rollback цикл

### 2. Strategy Benchmarks (`strategy_benchmark_test.go`)

**BenchmarkImportStrategy_REPLACE**
- Измеряет UPSERT производительность
- UPDATE существующих записей

**BenchmarkImportStrategy_IGNORE**
- INSERT OR IGNORE для SQLite
- ON CONFLICT DO NOTHING для PostgreSQL

**BenchmarkImportStrategy_Comparison**
- Сравнение всех стратегий: REPLACE, IGNORE, COPY
- Sub-benchmarks для каждой стратегии

**BenchmarkImportStrategy_DataVolume**
- Тестирует разные объемы: 100, 1000, 10000 строк
- Показывает масштабируемость

**BenchmarkImportPackets_Batch**
- Batch импорт: 10 пакетов по 100 строк
- Использует ImportPackets (транзакционный batch)

**BenchmarkImportPackets_SingleVsBatch**
- Прямое сравнение: 10x ImportPacket vs 1x ImportPackets
- Показывает выгоду от batching

**BenchmarkExportImport_RoundTrip**
- Полный цикл: Export → Import
- 1000 строк roundtrip

### 3. Database Comparison (`database_comparison_benchmark_test.go`)

**BenchmarkDatabase_Connection**
- Скорость подключения SQLite vs PostgreSQL
- Включает создание connection pool для PostgreSQL

**BenchmarkDatabase_Import**
- Import производительность: 100 и 1000 строк
- Прямое сравнение двух БД

**BenchmarkDatabase_Export**
- Export производительность: 100 и 1000 строк
- Измеряет SELECT + маппинг

**BenchmarkDatabase_Transaction**
- Commit vs Rollback для обеих БД
- Показывает overhead транзакций

**BenchmarkDatabase_Metadata**
- GetTableNames, TableExists, GetDatabaseVersion
- Сравнение metadata операций

**BenchmarkDatabase_ImportStrategies**
- Все стратегии на обеих БД
- Позволяет увидеть разницу в UPSERT реализации

**BenchmarkDatabase_BatchImport**
- Batch импорт на SQLite vs PostgreSQL
- PostgreSQL COPY vs SQLite BEGIN/COMMIT

## 📈 Интерпретация результатов

### Формат вывода

```
BenchmarkFactory_CreateAdapter-8         5000    234567 ns/op    12345 B/op    123 allocs/op
```

- `8` - количество CPU cores (GOMAXPROCS)
- `5000` - количество итераций
- `234567 ns/op` - наносекунды на операцию
- `12345 B/op` - байт выделено на операцию
- `123 allocs/op` - количество аллокаций на операцию

### Что искать

**Производительность:**
- Меньше ns/op = быстрее
- Меньше B/op = меньше памяти
- Меньше allocs/op = меньше GC pressure

**Сравнение:**
```
BenchmarkStrategy_REPLACE-8     1000    1234567 ns/op
BenchmarkStrategy_COPY-8        2000     654321 ns/op
```
COPY в ~2x быстрее REPLACE

## 🎯 Ожидаемые результаты

### SQLite

**Сильные стороны:**
- Быстрое подключение (нет сети)
- Легковесные транзакции
- Низкий overhead для малых данных

**Слабые стороны:**
- Медленнее на больших объемах (>10K строк)
- Один writer одновременно
- COPY = INSERT (нет native bulk)

### PostgreSQL

**Сильные стороны:**
- COPY - очень быстрый bulk import
- Параллельные transactions
- Отличная производительность на >1K строках

**Слабые стороны:**
- Connection overhead (сеть + auth)
- Тяжелее для малых операций

### Рекомендации по выбору

**Используйте SQLite:**
- Embedded приложения
- Малые объемы (<1K строк)
- Простота деплоя
- Тестирование

**Используйте PostgreSQL:**
- Production workloads
- Большие объемы (>10K строк)
- Параллельный доступ
- Сложные типы (UUID, JSONB)

## 🔍 Пример анализа

### Сценарий: Import 10,000 строк

**Результаты (примерные):**
```
BenchmarkDatabase_Import/10000rows/SQLite-8      100    12000000 ns/op
BenchmarkDatabase_Import/10000rows/PostgreSQL-8  500     2500000 ns/op
```

**Анализ:**
- PostgreSQL в ~5x быстрее на больших данных
- PostgreSQL COPY эффективнее обычных INSERT
- SQLite ограничен одним writer-ом

### Сценарий: Batch Import (10 пакетов)

**Результаты (примерные):**
```
BenchmarkImportPackets_SingleVsBatch/Single-8    100    15000000 ns/op
BenchmarkImportPackets_SingleVsBatch/Batch-8     300     5000000 ns/op
```

**Анализ:**
- Batch в ~3x быстрее
- Причина: одна транзакция вместо 10
- Меньше fsync/flush операций

## 🛠️ Оптимизация

### Советы для SQLite

**1. Используйте batch импорт:**
```go
adapter.ImportPackets(ctx, packets, adapters.StrategyReplace)
```

**2. Настройте PRAGMA:**
```sql
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
```

**3. Используйте транзакции:**
```go
tx, _ := adapter.BeginTx(ctx)
// ... multiple operations ...
tx.Commit(ctx)
```

### Советы для PostgreSQL

**1. Используйте COPY стратегию:**
```go
adapter.ImportPacket(ctx, pkt, adapters.StrategyCopy)
```

**2. Настройте connection pool:**
```go
cfg := adapters.Config{
    Type:     "postgres",
    DSN:      "...",
    MaxConns: 20,
    MinConns: 5,
}
```

**3. Используйте batch operations:**
```go
adapter.ImportPackets(ctx, packets, adapters.StrategyCopy)
```

## 📊 Сравнительная таблица

| Операция | SQLite | PostgreSQL | Победитель |
|----------|--------|------------|------------|
| Connection | ~1ms | ~10ms | SQLite |
| Import 100 rows | ~5ms | ~15ms | SQLite |
| Import 1K rows | ~50ms | ~30ms | PostgreSQL |
| Import 10K rows | ~500ms | ~100ms | PostgreSQL |
| Export 1K rows | ~20ms | ~25ms | SQLite |
| Transaction overhead | ~0.1ms | ~1ms | SQLite |
| Batch import (10 пакетов) | ~100ms | ~50ms | PostgreSQL |
| COPY strategy | N/A | ~20ms (10K) | PostgreSQL |

*Примечание: значения примерные, зависят от hardware*

## 🧩 Добавление собственных бенчмарков

### Шаблон

```go
func BenchmarkMyOperation(b *testing.B) {
    ctx := context.Background()

    cfg := adapters.Config{
        Type: "sqlite",
        DSN:  ":memory:",
    }

    adapter, err := adapters.New(ctx, cfg)
    if err != nil {
        b.Fatalf("Setup failed: %v", err)
    }
    defer adapter.Close(ctx)

    // Setup code here (не измеряется)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // Код для измерения
    }
}
```

### Best practices

1. **Используйте b.ResetTimer()** после setup
2. **Отключайте таймер для cleanup:** b.StopTimer() / b.StartTimer()
3. **Проверяйте ошибки, но не в timing path**
4. **Используйте defer для cleanup**
5. **Параметризуйте через sub-benchmarks**

## 📖 См. также

- [Adapter Interface](ADAPTER_INTERFACE.md)
- [SQLite Adapter](SQLITE_ADAPTER.md)
- [PostgreSQL Adapter](POSTGRES_ADAPTER.md)
- [Go Benchmarking Best Practices](https://dave.cheney.net/2013/06/30/how-to-write-benchmarks-in-go)

---

**Статус:** v1.0
**Последнее обновление:** 15.11.2025
