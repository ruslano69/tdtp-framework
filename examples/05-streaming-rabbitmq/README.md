# Streaming Export & Parallel Import Example

Этот пример демонстрирует продвинутые возможности TDTP Framework:

## Streaming Export

**Проблема:** При экспорте больших таблиц (100K+ строк) традиционный подход требует:
1. Загрузки всех данных в память
2. Разбиения на части
3. Только потом отправки в брокер

**Решение:** Streaming Export генерирует и отправляет части **по мере чтения** из БД:

```
БД → Read Row → Generate Part → Send to Broker
     ↓ Stream    ↓ ~1.9MB        ↓ Immediate
```

### Ключевые особенности

1. **TotalParts = 0** в каждой части
   До завершения первой части мы не знаем сколько всего будет частей

2. **Low Latency**
   Первая часть отправляется через ~60ms, а не после обработки всей таблицы

3. **Constant Memory**
   В памяти только одна часть (~1.9MB), а не вся таблица (25MB+)

4. **Schema в каждой части**
   Каждая часть самодостаточна и может быть импортирована независимо

## Parallel Import

**Проблема:** Последовательный импорт частей медленный и неэффективный

**Решение:** Параллельные воркеры обрабатывают части независимо:

```
RabbitMQ → Worker 1 → Part 1 → Insert to DB
         → Worker 2 → Part 2 → Insert to DB
         → Worker 3 → Part 3 → Insert to DB
         → Worker 4 → Part 4 → Insert to DB
```

### Ключевые особенности

1. **4 воркера** по умолчанию (настраивается)

2. **Независимая обработка**
   Части могут приходить в любом порядке

3. **Немедленный старт**
   Импорт начинается сразу после получения первой части

4. **Самодостаточные части**
   Каждая часть содержит Schema, не требуется дополнительная информация

## Использование

### Предварительные требования

```bash
# Запустить RabbitMQ
docker run -d --name rabbitmq \
  -p 5672:5672 \
  -p 15672:15672 \
  rabbitmq:3-management
```

### Запуск примера

```bash
cd examples/05-streaming-rabbitmq
go run main.go
```

### Ожидаемый результат

```
=== TDTP Streaming Export/Import Demo ===

--- Streaming Export Demo ---
✓ Workspace created with test data
✓ SQL streaming started
✓ Exporter configured for RabbitMQ

Starting streaming export...

--- Export Results ---
Output Type:     RabbitMQ
Destination:     localhost:5672/tdtp_streaming_demo
Total Parts:     6
Parts Sent:      6
Total Rows:      10000
Duration:        127ms
Errors:          0
Avg Rows/Part:   1666

✓ Streaming export completed successfully!

--- Parallel Import Demo ---
✓ Import workspace created
✓ Parallel importer configured (4 workers)

Starting parallel import...

--- Import Results ---
Parts Imported:      6
Total Rows:          10000
Duration:            89ms
Avg Part Duration:   14ms
Errors:              0
Throughput:          112359 rows/sec

Verifying imported data...
✓ Imported rows verified: 10000

✓ Parallel import completed successfully!
```

## Производительность

### Сравнение с традиционным подходом

**Традиционный (Batch) подход:**
- Экспорт 100K строк: ~800ms (загрузка всех данных, разбиение, отправка)
- Память: ~25MB (все данные в памяти)
- Первая часть доступна: через 800ms

**Streaming подход:**
- Экспорт 100K строк: ~850ms (небольшой overhead на координацию)
- Память: ~1.9MB (только одна часть)
- Первая часть доступна: через ~60ms (**13x faster to first part!**)

### Pipeline Processing

При использовании message broker получается pipeline:

```
Time:  0ms      60ms     120ms    180ms    800ms
       ↓        ↓        ↓        ↓        ↓
Export: Part1 → Part2 → Part3 → ... → Complete
                ↓        ↓
Import:         Part1 → Part2 → ...
```

**Результат:** Импорт завершается практически одновременно с экспортом!

## Конфигурация

### Размер части

```go
streamGen := packet.NewStreamingGenerator()
streamGen.SetPartSize(5000000) // 5MB части
```

### Количество воркеров

```go
importer := etl.NewParallelImporter(etl.ImporterConfig{
    Workers: 8, // 8 параллельных воркеров
    ...
})
```

## Рекомендации

1. **Для больших таблиц (100K+ строк):**
   Используйте Streaming Export в RabbitMQ/Kafka

2. **Для файлового экспорта:**
   Используйте обычный Export (нужен TotalParts в каждой части)

3. **Для максимальной производительности:**
   - Количество воркеров = количество CPU cores
   - Размер части = 1.9-5MB (оптимально для RabbitMQ)
   - Используйте compression для экономии трафика

4. **Мониторинг:**
   ```go
   stats, err := importer.Import(ctx, handler)
   fmt.Printf("Throughput: %.0f rows/sec\n",
       float64(stats.TotalRows) / stats.Duration.Seconds())
   ```

## Интеграция с ETL Pipeline

Streaming Export автоматически используется при экспорте в RabbitMQ/Kafka:

```yaml
# etl_pipeline.yaml
output:
  type: RabbitMQ
  rabbitmq:
    host: localhost
    port: 5672
    queue: tdtp_data
```

ETL Processor автоматически выберет Streaming режим!
