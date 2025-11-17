# TDTP CLI - Примеры использования

Практические примеры использования TDTP CLI v1.2 для различных сценариев.

## Содержание

1. [Базовые операции](#базовые-операции)
2. [XLSX операции](#xlsx-операции)
3. [Incremental Sync](#incremental-sync)
4. [Data Processors](#data-processors)
5. [Production сценарии](#production-сценарии)
6. [ETL pipelines](#etl-pipelines)

---

## Базовые операции

### Экспорт таблицы

```bash
# Простой экспорт
tdtpcli --export customers --output customers.xml

# Экспорт с фильтром
tdtpcli --export orders --where "status = pending" --output pending_orders.xml

# Экспорт с лимитом
tdtpcli --export products --limit 1000 --output sample_products.xml
```

### Импорт таблицы

```bash
# Простой импорт (стратегия replace)
tdtpcli --import customers.xml

# Импорт с игнорированием дубликатов
tdtpcli --import new_products.xml --strategy ignore

# Импорт с проверкой уникальности
tdtpcli --import critical_data.xml --strategy fail
```

---

## XLSX операции

### Экспорт таблицы в Excel

```bash
# Прямой экспорт в XLSX
tdtpcli --export-xlsx customers --output customers.xlsx

# Экспорт с кастомным листом
tdtpcli --export-xlsx orders \
  --output monthly_report.xlsx \
  --sheet "November Orders"

# Экспорт с фильтром
tdtpcli --export-xlsx sales \
  --where "date >= '2024-11-01'" \
  --output november_sales.xlsx
```

### Импорт из Excel

```bash
# Импорт из XLSX
tdtpcli --import-xlsx products.xlsx --sheet "Products"

# Импорт с заменой существующих данных
tdtpcli --import-xlsx prices.xlsx \
  --sheet "Prices" \
  --strategy replace
```

### Конвертация форматов

```bash
# TDTP → XLSX для анализа
tdtpcli --to-xlsx export.xml \
  --output analysis.xlsx \
  --sheet "Data"

# XLSX → TDTP для передачи
tdtpcli --from-xlsx report.xlsx \
  --output report.xml \
  --sheet "Report"
```

---

## Incremental Sync

### Настройка инкрементальной синхронизации

```bash
# Первая синхронизация (все данные)
tdtpcli --sync-incremental orders --tracking-field updated_at
# Создан checkpoint.yaml с last_sync_value

# Повторная синхронизация (только изменения)
tdtpcli --sync-incremental orders --tracking-field updated_at
# Экспортирует WHERE updated_at > last_sync_value
```

### Ежедневная синхронизация

**sync_daily.sh:**
```bash
#!/bin/bash
DATE=$(date +%Y%m%d)

# Orders sync
tdtpcli --sync-incremental orders \
  --tracking-field updated_at \
  --output "/backup/orders_${DATE}.xml"

# Users sync
tdtpcli --sync-incremental users \
  --tracking-field last_modified \
  --output "/backup/users_${DATE}.xml"

# Products sync (по ID)
tdtpcli --sync-incremental products \
  --tracking-field id \
  --batch-size 5000 \
  --output "/backup/products_${DATE}.xml"
```

**Cron настройка:**
```bash
# Запуск каждый день в 2:00 AM
0 2 * * * /scripts/sync_daily.sh >> /var/log/sync.log 2>&1
```

### Использование кастомного checkpoint

```bash
# Отдельный checkpoint для каждой таблицы
tdtpcli --sync-incremental orders \
  --tracking-field updated_at \
  --checkpoint-file /data/checkpoints/orders.json

tdtpcli --sync-incremental users \
  --tracking-field updated_at \
  --checkpoint-file /data/checkpoints/users.json
```

### Batch обработка больших таблиц

```bash
# Sync большой таблицы по частям
tdtpcli --sync-incremental events \
  --tracking-field timestamp \
  --batch-size 10000 \
  --output events_batch.xml
```

---

## Data Processors

### PII маскирование

```bash
# Маскирование email
tdtpcli --export customers \
  --mask email \
  --output customers_masked.xml

# Множественное маскирование
tdtpcli --export users \
  --mask email,phone,ssn \
  --output users_anonymous.xml

# Маскирование при XLSX экспорте
tdtpcli --export-xlsx patients \
  --mask ssn,medical_record,dob \
  --output patients_report.xlsx
```

### Валидация данных

**validation_rules.yaml:**
```yaml
email:
  - type: email
phone:
  - type: phone
age:
  - type: range:0-120
status:
  - type: enum:active,inactive,pending
```

**Использование:**
```bash
# Валидация при импорте
tdtpcli --import customers.xml \
  --validate validation_rules.yaml

# Валидация с нормализацией
tdtpcli --import messy_data.xml \
  --normalize cleanup.yaml \
  --validate validation_rules.yaml
```

### Нормализация данных

**normalization_rules.yaml:**
```yaml
email:
  - type: email          # lowercase + trim
phone:
  - type: phone          # удалить форматирование
name:
  - type: whitespace     # trim + единичные пробелы
```

**Использование:**
```bash
# Нормализация при импорте
tdtpcli --import users.xml \
  --normalize normalization_rules.yaml

# Нормализация + валидация
tdtpcli --import data.xml \
  --normalize cleanup.yaml \
  --validate rules.yaml
```

### Комбинирование процессоров

```bash
# Полный pipeline: normalize → validate → mask
tdtpcli --export sensitive_data \
  --normalize cleanup.yaml \
  --validate strict_rules.yaml \
  --mask ssn,card_number,email \
  --output secure_export.xml
```

---

## Production сценарии

### Сценарий 1: Безопасный экспорт для аналитики

```bash
#!/bin/bash
# export_for_analytics.sh

DATE=$(date +%Y%m%d)

# Экспорт customer data с маскированием PII
tdtpcli --config production.yaml \
  --export customers \
  --where "created_at >= '2024-11-01'" \
  --mask email,phone,ssn,card_number \
  --output "/analytics/customers_${DATE}.xml"

# Конвертация в Excel для аналитиков
tdtpcli --to-xlsx "/analytics/customers_${DATE}.xml" \
  --output "/analytics/customers_${DATE}.xlsx" \
  --sheet "Customer Data"

# Audit check
grep "EXPORT.*customers" /var/log/audit.log | tail -1
```

### Сценарий 2: Инкрементальный backup

```bash
#!/bin/bash
# incremental_backup.sh

BACKUP_DIR="/backup/$(date +%Y/%m/%d)"
mkdir -p "$BACKUP_DIR"

# Список критичных таблиц
TABLES="orders customers transactions products"

for TABLE in $TABLES; do
  echo "Backing up $TABLE..."

  tdtpcli --sync-incremental "$TABLE" \
    --tracking-field updated_at \
    --checkpoint-file "/backup/checkpoints/${TABLE}.json" \
    --output "${BACKUP_DIR}/${TABLE}.xml"

  if [ $? -eq 0 ]; then
    echo "✓ $TABLE backup complete"
  else
    echo "✗ $TABLE backup failed"
    exit 1
  fi
done

# Архивация
tar -czf "${BACKUP_DIR}.tar.gz" "$BACKUP_DIR"
echo "Backup archive: ${BACKUP_DIR}.tar.gz"
```

### Сценарий 3: ETL pipeline

```bash
#!/bin/bash
# etl_pipeline.sh

set -e  # Exit on error

echo "=== ETL Pipeline Start ==="

# 1. Extract from source DB
echo "Step 1: Extract"
tdtpcli --config source.yaml \
  --export transactions \
  --where "date = CURRENT_DATE - 1" \
  --output /tmp/raw_transactions.xml

# 2. Transform: normalize + validate
echo "Step 2: Transform"
tdtpcli --import /tmp/raw_transactions.xml \
  --config staging.yaml \
  --normalize transform_rules.yaml \
  --validate validation_rules.yaml \
  --strategy replace

# 3. Export transformed data
echo "Step 3: Export transformed"
tdtpcli --config staging.yaml \
  --export transactions \
  --output /tmp/clean_transactions.xml

# 4. Load into DWH
echo "Step 4: Load"
tdtpcli --config dwh.yaml \
  --import /tmp/clean_transactions.xml \
  --strategy replace

# 5. Cleanup
rm /tmp/raw_transactions.xml /tmp/clean_transactions.xml

echo "=== ETL Pipeline Complete ==="
```

### Сценарий 4: Database migration

```bash
#!/bin/bash
# migrate_postgres_to_mysql.sh

SOURCE_CONFIG="source_pg.yaml"
TARGET_CONFIG="target_mysql.yaml"
MIGRATION_DIR="/tmp/migration"

mkdir -p "$MIGRATION_DIR"

# Получить список таблиц из source
TABLES=$(tdtpcli --config "$SOURCE_CONFIG" --list | tail -n +2)

for TABLE in $TABLES; do
  echo "Migrating $TABLE..."

  # Export from PostgreSQL
  tdtpcli --config "$SOURCE_CONFIG" \
    --export "$TABLE" \
    --output "${MIGRATION_DIR}/${TABLE}.xml"

  # Import to MySQL with normalization
  tdtpcli --config "$TARGET_CONFIG" \
    --import "${MIGRATION_DIR}/${TABLE}.xml" \
    --normalize mysql_compat.yaml \
    --strategy replace

  echo "✓ $TABLE migrated"
done

echo "Migration complete. Check ${MIGRATION_DIR} for exports."
```

### Сценарий 5: Batch import from Excel files

```bash
#!/bin/bash
# batch_import_excel.sh

EXCEL_DIR="/data/excel_imports"
LOG_FILE="/var/log/excel_imports.log"

echo "=== Batch Excel Import Started ===" | tee -a "$LOG_FILE"

# Импорт всех XLSX файлов
for FILE in "$EXCEL_DIR"/*.xlsx; do
  if [ -f "$FILE" ]; then
    BASENAME=$(basename "$FILE")
    echo "Processing $BASENAME..." | tee -a "$LOG_FILE"

    tdtpcli --import-xlsx "$FILE" \
      --sheet "Data" \
      --normalize cleanup.yaml \
      --validate import_rules.yaml \
      --strategy ignore

    if [ $? -eq 0 ]; then
      echo "✓ $BASENAME imported successfully" | tee -a "$LOG_FILE"
      # Переместить в processed
      mv "$FILE" "${EXCEL_DIR}/processed/"
    else
      echo "✗ $BASENAME import failed" | tee -a "$LOG_FILE"
      # Переместить в failed
      mv "$FILE" "${EXCEL_DIR}/failed/"
    fi
  fi
done

echo "=== Batch Excel Import Completed ===" | tee -a "$LOG_FILE"
```

---

## ETL Pipelines

### Pipeline 1: Daily aggregation

```bash
#!/bin/bash
# daily_aggregation.sh

DATE=$(date -d "yesterday" +%Y-%m-%d)

echo "Aggregating data for $DATE"

# 1. Export raw sales
tdtpcli --export sales \
  --where "sale_date = '$DATE'" \
  --output /tmp/sales_raw.xml

# 2. Convert to XLSX for processing
tdtpcli --to-xlsx /tmp/sales_raw.xml \
  --output /tmp/sales.xlsx

# 3. Process with external tool (e.g., Python)
python3 /scripts/aggregate_sales.py /tmp/sales.xlsx /tmp/aggregated.xlsx

# 4. Import aggregated data
tdtpcli --import-xlsx /tmp/aggregated.xlsx \
  --sheet "Aggregated" \
  --strategy replace

echo "Aggregation complete"
```

### Pipeline 2: Multi-source consolidation

```bash
#!/bin/bash
# consolidate_sources.sh

OUTPUT_DIR="/consolidated/$(date +%Y%m%d)"
mkdir -p "$OUTPUT_DIR"

# Source 1: Main database
tdtpcli --config db1.yaml \
  --export products \
  --output "${OUTPUT_DIR}/products_db1.xml"

# Source 2: Regional database
tdtpcli --config db2.yaml \
  --export products \
  --output "${OUTPUT_DIR}/products_db2.xml"

# Source 3: Excel import
tdtpcli --from-xlsx /data/products_manual.xlsx \
  --output "${OUTPUT_DIR}/products_manual.xml"

# Merge and import to consolidated DB
# (Custom merge script)
python3 /scripts/merge_products.py \
  "${OUTPUT_DIR}/products_*.xml" \
  "${OUTPUT_DIR}/products_consolidated.xml"

# Import consolidated
tdtpcli --config consolidated.yaml \
  --import "${OUTPUT_DIR}/products_consolidated.xml" \
  --validate product_rules.yaml \
  --strategy replace
```

---

## Мониторинг и алерты

### Проверка успешности backup

```bash
#!/bin/bash
# check_backup_status.sh

CHECKPOINT="/backup/checkpoints/orders.json"
ALERT_EMAIL="admin@company.com"

# Проверить последнюю синхронизацию
if [ -f "$CHECKPOINT" ]; then
  LAST_SYNC=$(jq -r '.orders.last_sync_time' "$CHECKPOINT")
  RECORDS=$(jq -r '.orders.records_exported' "$CHECKPOINT")

  echo "Last sync: $LAST_SYNC"
  echo "Records: $RECORDS"

  # Проверить что sync был недавно (< 25 часов)
  LAST_SYNC_TS=$(date -d "$LAST_SYNC" +%s)
  NOW_TS=$(date +%s)
  DIFF=$(( (NOW_TS - LAST_SYNC_TS) / 3600 ))

  if [ $DIFF -gt 25 ]; then
    echo "WARNING: Last sync was $DIFF hours ago!"
    echo "Backup may be stale" | mail -s "Backup Alert" "$ALERT_EMAIL"
  fi
else
  echo "ERROR: Checkpoint file not found!"
  echo "Backup checkpoint missing" | mail -s "Backup Critical" "$ALERT_EMAIL"
fi
```

### Audit log monitoring

```bash
#!/bin/bash
# monitor_audit.sh

AUDIT_LOG="/var/log/audit.log"
ALERT_EMAIL="security@company.com"

# Проверить ошибки за последний час
FAILURES=$(grep -c "FAILURE" "$AUDIT_LOG" | tail -100)

if [ "$FAILURES" -gt 10 ]; then
  echo "WARNING: $FAILURES failures in last 100 operations"
  tail -100 "$AUDIT_LOG" | grep "FAILURE" | \
    mail -s "Audit Alert: High failure rate" "$ALERT_EMAIL"
fi

# Проверить Circuit Breaker события
CB_EVENTS=$(grep "Circuit Breaker.*Open" "$AUDIT_LOG" | tail -10)
if [ -n "$CB_EVENTS" ]; then
  echo "Circuit Breaker opened!"
  echo "$CB_EVENTS" | mail -s "Circuit Breaker Alert" "$ALERT_EMAIL"
fi
```

---

## Тестирование

### Тест импорта с различными стратегиями

```bash
#!/bin/bash
# test_import_strategies.sh

TEST_DATA="test_data.xml"
TEST_DB="test.db"

# Cleanup
rm -f "$TEST_DB"

echo "Testing import strategies..."

# Test 1: Replace
echo "Test 1: Replace strategy"
tdtpcli --config test.yaml --import "$TEST_DATA" --strategy replace
echo "Records after replace: $(sqlite3 $TEST_DB 'SELECT COUNT(*) FROM test_table')"

# Test 2: Ignore (повторный импорт тех же данных)
echo "Test 2: Ignore strategy"
tdtpcli --config test.yaml --import "$TEST_DATA" --strategy ignore
echo "Records after ignore: $(sqlite3 $TEST_DB 'SELECT COUNT(*) FROM test_table')"

# Test 3: Fail (должен быть error)
echo "Test 3: Fail strategy"
tdtpcli --config test.yaml --import "$TEST_DATA" --strategy fail || echo "Expected error"

# Test 4: Copy
echo "Test 4: Copy strategy"
tdtpcli --config test.yaml --import "$TEST_DATA" --strategy copy
echo "Records after copy: $(sqlite3 $TEST_DB 'SELECT COUNT(*) FROM test_table')"
```

---

## Заключение

Эти примеры демонстрируют различные сценарии использования TDTP CLI v1.2:

- **Базовые операции**: export/import с фильтрами
- **XLSX**: прямая работа с Excel
- **Incremental Sync**: эффективная синхронизация
- **Data Processors**: защита PII и контроль качества
- **Production**: готовые production-ready сценарии
- **ETL**: комплексные data pipelines

Для получения дополнительной информации см. [CLI_v1.2_FEATURES.md](../../docs/CLI_v1.2_FEATURES.md).
