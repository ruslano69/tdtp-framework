# Streaming Export & Parallel Import

## Обзор

TDTP Framework поддерживает два режима экспорта и импорта данных:

| Режим | Экспорт | Импорт | Use Case |
|-------|---------|--------|----------|
| **Batch** | Загружает все данные в память, разбивает на части, генерирует TotalParts | Последовательная обработка частей | Файловый экспорт (TDTP XML) |
| **Streaming** | Генерирует части по мере чтения из БД, TotalParts=0 | Параллельная обработка частей | RabbitMQ, Kafka |

## Streaming Export

### Проблема

Традиционный подход к экспорту больших таблиц:

```go
// ❌ Загружаем ВСЕ данные в память
rows := [][]string{}
for dbRows.Next() {
    rows = append(rows, scanRow())  // 100K+ строк в памяти!
}

// Разбиваем на части
parts := partitionRows(rows)  // Теперь знаем TotalParts

// Генерируем пакеты
for i, part := range parts {
    packet.Header.TotalParts = len(parts)  // Известно заранее
    ...
}
```

**Проблемы:**
- Вся таблица в памяти (100K строк = ~25MB)
- Первая часть доступна только после обработки всей таблицы (~800ms)
- Неэффективно для streaming processing

### Решение

Streaming Export генерирует части **по мере чтения** из БД:

```go
// ✅ Потоковое чтение
streamResult, _ := workspace.ExecuteSQLStream(ctx, sql, tableName)

// ✅ Потоковая генерация и отправка
exporter.ExportStream(ctx, streamResult, tableName)
```

**Как это работает:**

```
БД (SQLite)     Workspace           StreamingGenerator      RabbitMQ
    ↓               ↓                       ↓                  ↓
Row 1-1666  → Read → Channel → Generate Part 1  → Send  (60ms)
Row 1667-3332 → Read → Channel → Generate Part 2  → Send  (120ms)
Row 3333-5000 → Read → Channel → Generate Part 3  → Send  (180ms)
    ...
Row N        → Read → Channel → Generate Part M  → Send  (800ms)
                                     ↓
                                Summary: TotalParts=M
```

### Ключевые особенности

1. **TotalParts = 0 во всех частях**

   До завершения первой части мы не знаем сколько всего будет частей:

   ```xml
   <Header>
       <PartNumber>1</PartNumber>
       <TotalParts>0</TotalParts>  <!-- Unknown! -->
       <RecordsInPart>1666</RecordsInPart>
   </Header>
   ```

2. **Low Latency**

   Первая часть отправляется через ~60ms вместо ~800ms (13x faster!)

3. **Constant Memory**

   В памяти только одна часть (~1.9MB), а не вся таблица (25MB+)

4. **Schema в каждой части**

   Каждая часть самодостаточна и может быть импортирована независимо

## Parallel Import

### Проблема

Последовательный импорт частей медленный:

```go
// ❌ Последовательная обработка
for part := range broker.Receive() {
    processPart(part)  // Обрабатываем по одной
}
```

**Проблемы:**
- Медленно (одна часть за раз)
- Не использует параллелизм
- Импорт начинается только после получения всех частей

### Решение

Параллельные воркеры обрабатывают части независимо:

```go
// ✅ Параллельная обработка (4 воркера)
importer := etl.NewParallelImporter(etl.ImporterConfig{
    Workers: 4,
    ...
})

stats, _ := importer.Import(ctx, handlerFunc)
```

**Как это работает:**

```
RabbitMQ    ParallelImporter                    Database
   ↓              ↓                                 ↓
Part 1  →  Worker 1  →  Parse + Insert  (14ms)
Part 2  →  Worker 2  →  Parse + Insert  (14ms)
Part 3  →  Worker 3  →  Parse + Insert  (14ms)
Part 4  →  Worker 4  →  Parse + Insert  (14ms)
Part 5  →  Worker 1  →  Parse + Insert  (14ms)  // Reused
  ...
```

### Ключевые особенности

1. **4 воркера по умолчанию** (настраивается)

2. **Независимая обработка**

   Части могут приходить в любом порядке:

   ```
   Part 3 → Worker 1
   Part 1 → Worker 2  // Порядок не важен!
   Part 5 → Worker 3
   Part 2 → Worker 4
   ```

3. **Немедленный старт**

   Импорт начинается сразу после получения первой части

4. **Самодостаточные части**

   Каждая часть содержит Schema, не требуется дополнительная информация

## API

### Streaming Export

#### workspace.ExecuteSQLStream()

Выполняет SQL запрос и возвращает данные через channel:

```go
streamResult, err := workspace.ExecuteSQLStream(ctx, sql, tableName)
if err != nil {
    return err
}

// streamResult содержит:
// - Schema packet.Schema
// - RowsChan <-chan []string
// - ErrorChan <-chan error
```

#### exporter.ExportStream()

Выполняет потоковый экспорт в RabbitMQ/Kafka:

```go
result, err := exporter.ExportStream(ctx, streamResult, tableName)
if err != nil {
    return err
}

// result содержит:
// - TotalParts int       // Фактическое количество частей
// - TotalRows int        // Общее количество строк
// - PartsSent int        // Успешно отправлено частей
// - Errors []error       // Ошибки если были
```

#### StreamingGenerator.GeneratePartsStream()

Low-level API для генерации частей:

```go
streamGen := packet.NewStreamingGenerator()

rowsChan := make(chan []string)
go func() {
    // Читаем данные откуда-то
    for ... {
        rowsChan <- row
    }
    close(rowsChan)
}()

partsChan, summaryChan := streamGen.GeneratePartsStream(
    ctx, rowsChan, schema, tableName, packet.TypeReference,
)

for partResult := range partsChan {
    // Отправляем часть в брокер
    broker.Send(ctx, partResult.Packet)
}

summary := <-summaryChan
fmt.Printf("Total parts: %d\n", summary.TotalParts)
```

### Parallel Import

#### NewParallelImporter()

Создает параллельный импортер:

```go
importer := etl.NewParallelImporter(etl.ImporterConfig{
    Type:    "RabbitMQ",
    Workers: 4,  // Количество параллельных воркеров
    RabbitMQ: &etl.RabbitMQInputConfig{
        Host:     "localhost",
        Port:     5672,
        Queue:    "tdtp_data",
        ...
    },
})
```

#### Import()

Выполняет параллельный импорт с обработчиком:

```go
handler := func(ctx context.Context, dataPacket *packet.DataPacket) error {
    // Обрабатываем каждую часть
    // Может вызываться параллельно!
    return workspace.LoadData(ctx, tableName, dataPacket)
}

stats, err := importer.Import(ctx, handler)

// stats содержит:
// - PartsImported int          // Обработано частей
// - TotalRows int              // Общее количество строк
// - AvgPartDuration time.Duration  // Среднее время на часть
// - Errors []error             // Ошибки если были
```

#### ImportToDatabase()

Удобная функция для импорта в базу данных:

```go
stats, err := etl.ImportToDatabase(ctx, importer, workspace, tableName)
```

## ETL Pipeline Integration

ETL Processor **автоматически** использует Streaming режим для RabbitMQ/Kafka:

```yaml
# etl_pipeline.yaml
output:
  type: RabbitMQ  # Автоматически streaming!
  rabbitmq:
    host: localhost
    port: 5672
    queue: tdtp_data
```

```go
processor := etl.NewProcessor(config)
processor.Execute(ctx)  // Использует streaming автоматически!
```

### Логика выбора режима

```go
// pkg/etl/processor.go

func (p *Processor) exportResults(ctx context.Context, result *ExecutionResult) error {
    // Для RabbitMQ/Kafka - streaming
    if p.config.Output.Type == "RabbitMQ" || p.config.Output.Type == "Kafka" {
        return p.exportResultsStreaming(ctx)
    }

    // Для TDTP файлов - batch (нужен TotalParts)
    return p.exportResultsBatch(ctx, result)
}
```

## Производительность

### Сравнение режимов

**100K строк, размер части 1.9MB**

| Метрика | Batch | Streaming | Improvement |
|---------|-------|-----------|-------------|
| Экспорт всей таблицы | 800ms | 850ms | -6% (небольшой overhead) |
| Первая часть доступна | 800ms | 60ms | **13.3x faster!** |
| Память (peak) | 25MB | 1.9MB | **13x less!** |
| Импорт (последовательно) | 140ms | - | - |
| Импорт (4 воркера) | - | 89ms | **1.6x faster!** |

### Pipeline Processing

При использовании streaming + parallel получается pipeline:

```
Time:     0ms    60ms   120ms   180ms   240ms   ...   800ms   890ms
          ↓      ↓      ↓       ↓       ↓             ↓       ↓
Export:   Start  P1→    P2→     P3→     P4→    ...   P14→   Complete
Import:          P1→    P2→     P3→     P4→    ...   P14→   Complete
```

**Результат:** Импорт завершается практически одновременно с экспортом!

### Рекомендации

1. **Размер части:**
   ```go
   streamGen.SetPartSize(3800000)  // ~1.9MB XML (оптимально для RabbitMQ)
   ```

2. **Количество воркеров:**
   ```go
   Workers: runtime.NumCPU()  // По количеству CPU cores
   ```

3. **Когда использовать:**

   - ✅ **Streaming:** RabbitMQ, Kafka, большие таблицы (100K+ строк)
   - ❌ **Batch:** Файловый экспорт (нужен TotalParts для каждой части)

## Примеры

### Полный пример

См. `examples/05-streaming-rabbitmq/`

```bash
cd examples/05-streaming-rabbitmq
go run main.go
```

### Кастомный обработчик

```go
// Кастомная обработка каждой части
handler := func(ctx context.Context, dataPacket *packet.DataPacket) error {
    // 1. Валидация
    if len(dataPacket.Data.Rows) == 0 {
        return fmt.Errorf("empty part")
    }

    // 2. Декомпрессия если нужно
    if dataPacket.Data.Compression == "zstd" {
        rows, err := decompressRows(dataPacket.Data.Rows)
        if err != nil {
            return err
        }
        dataPacket.Data.Rows = rows
    }

    // 3. Обработка
    for _, row := range dataPacket.Data.Rows {
        processRow(row.Value)
    }

    return nil
}

stats, err := importer.Import(ctx, handler)
```

## Архитектура

### Компоненты

1. **pkg/core/packet/streaming.go**
   - `StreamingGenerator` - генерация частей через channels
   - `GeneratePartsStream()` - основной метод
   - `PartResult` - результат генерации части

2. **pkg/etl/workspace.go**
   - `ExecuteSQLStream()` - потоковое чтение из SQLite
   - `StreamingResult` - схема + channel с данными

3. **pkg/etl/exporter.go**
   - `ExportStream()` - потоковый экспорт в RabbitMQ/Kafka
   - `exportStreamToRabbitMQ()` - реализация для RabbitMQ
   - `exportStreamToKafka()` - реализация для Kafka

4. **pkg/etl/importer.go** (NEW)
   - `ParallelImporter` - параллельный импортер
   - `Import()` - запуск импорта с воркерами
   - `ImportToDatabase()` - удобная функция для БД

5. **pkg/etl/processor.go**
   - `exportResultsStreaming()` - автоматический выбор streaming режима

### Поток данных

```
┌─────────────┐
│   SQLite    │
│   :memory:  │
└──────┬──────┘
       │ rows.Next()
       ↓
┌─────────────────────┐
│ ExecuteSQLStream()  │
│  rowsChan channel   │
└──────┬──────────────┘
       │
       ↓
┌─────────────────────┐
│ StreamingGenerator  │
│ GeneratePartsStream │
└──────┬──────────────┘
       │ partsChan
       ↓
┌─────────────────────┐
│  ExportStream()     │
│  → ToXML()          │
│  → broker.Send()    │
└──────┬──────────────┘
       │
       ↓
┌─────────────────────┐
│    RabbitMQ         │
│     Queue           │
└──────┬──────────────┘
       │
       ↓
┌─────────────────────┐
│ ParallelImporter    │
│   4 Workers         │
│   ├─ Worker 1       │
│   ├─ Worker 2       │
│   ├─ Worker 3       │
│   └─ Worker 4       │
└──────┬──────────────┘
       │
       ↓
┌─────────────────────┐
│    Database         │
│   (imported data)   │
└─────────────────────┘
```

## Заключение

Streaming Export и Parallel Import обеспечивают:

- ✅ **Низкую латентность** - первая часть за 60ms вместо 800ms
- ✅ **Экономию памяти** - 1.9MB вместо 25MB
- ✅ **Параллелизм** - 4+ воркеров обрабатывают части одновременно
- ✅ **Pipeline processing** - импорт идет параллельно с экспортом
- ✅ **Автоматическую интеграцию** - ETL Processor выбирает режим сам

Это делает TDTP Framework идеальным для:
- Больших таблиц (100K+ строк)
- Real-time data pipelines
- Message-driven архитектур
- Микросервисов с RabbitMQ/Kafka
