# TDTP CLI v1.2 - Новые возможности

**Версия:** 1.2
**Дата:** 17.11.2024

Этот документ описывает новые возможности TDTP CLI версии 1.2, добавленные в рамках модернизации CLI.

---

## Содержание

1. [XLSX Converter](#xlsx-converter)
2. [Import Strategies](#import-strategies)
3. [Production Features](#production-features)
4. [Data Processors](#data-processors)
5. [Incremental Sync](#incremental-sync)
6. [Примеры использования](#примеры-использования)

---

## XLSX Converter

Поддержка прямого экспорта/импорта в формат Excel (XLSX).

### Команды

#### Конвертация TDTP → XLSX
```bash
tdtpcli --to-xlsx input.xml --output output.xlsx --sheet "Data"
```

#### Конвертация XLSX → TDTP
```bash
tdtpcli --from-xlsx input.xlsx --output output.xml --sheet "Sheet1"
```

#### Прямой экспорт в XLSX
```bash
tdtpcli --export-xlsx customers --output customers.xlsx
```

#### Прямой импорт из XLSX
```bash
tdtpcli --import-xlsx data.xlsx --strategy replace
```

### Параметры

- `--sheet <name>` - Имя листа Excel (по умолчанию: "Sheet1")
- `--output <file>` - Выходной файл

### Примеры

```bash
# Экспорт таблицы напрямую в Excel
tdtpcli --export-xlsx orders --output orders.xlsx --sheet "Orders"

# Экспорт с фильтрацией
tdtpcli --export-xlsx customers \
  --where "status = active" \
  --output active_customers.xlsx

# Импорт из Excel в БД
tdtpcli --import-xlsx products.xlsx \
  --sheet "Products" \
  --strategy replace

# Конвертация TDTP в Excel для анализа
tdtpcli --to-xlsx export.xml --output analysis.xlsx
```

---

## Import Strategies

Различные стратегии обработки конфликтов при импорте данных.

### Доступные стратегии

#### 1. **Replace** (по умолчанию)
Обновляет существующие записи, вставляет новые.

```bash
tdtpcli --import data.xml --strategy replace
```

**Поведение:**
- Существующие записи → UPDATE
- Новые записи → INSERT
- Использует MERGE/UPSERT SQL операции

#### 2. **Ignore**
Игнорирует существующие записи, вставляет только новые.

```bash
tdtpcli --import data.xml --strategy ignore
```

**Поведение:**
- Существующие записи → SKIP
- Новые записи → INSERT
- Использует INSERT IGNORE или ON CONFLICT DO NOTHING

#### 3. **Fail**
Прерывает импорт при обнаружении дубликатов.

```bash
tdtpcli --import data.xml --strategy fail
```

**Поведение:**
- Существующие записи → ERROR (rollback транзакции)
- Новые записи → INSERT
- Строгая проверка уникальности

#### 4. **Copy**
Копирует данные без учета первичных ключей.

```bash
tdtpcli --import data.xml --strategy copy
```

**Поведение:**
- Все записи → INSERT (новые PK генерируются автоматически)
- Игнорирует PRIMARY KEY из источника
- Используется для миграции данных

### Примеры

```bash
# Обновление существующих записей
tdtpcli --import customers.xml --strategy replace

# Импорт только новых записей
tdtpcli --import new_products.xml --strategy ignore

# Строгий импорт с проверкой дубликатов
tdtpcli --import critical_data.xml --strategy fail

# Копирование данных для тестирования
tdtpcli --import test_data.xml --strategy copy
```

---

## Production Features

Промышленные возможности для надежности и аудита в production среде.

### Circuit Breaker

Защита от каскадных сбоев при работе с БД и брокерами сообщений.

#### Конфигурация

```yaml
resilience:
  circuit_breaker:
    enabled: true
    threshold: 5              # Количество ошибок для открытия
    timeout: 60               # Секунд в Open состоянии
    max_concurrent: 100       # Максимум одновременных вызовов
    success_threshold: 2      # Успешных вызовов для закрытия
```

#### Состояния

- **Closed** - Нормальная работа
- **Open** - Блокировка вызовов после threshold ошибок
- **Half-Open** - Тестирование после timeout

#### Поведение

```bash
# Circuit Breaker автоматически применяется ко всем операциям
tdtpcli --export orders  # Защищен Circuit Breaker

# При достижении threshold ошибок:
# ⚠ Circuit Breaker [tdtpcli]: Closed → Open
# Error: circuit breaker is open
```

### Retry Mechanism

Автоматические повторы при временных сбоях.

#### Конфигурация

```yaml
resilience:
  retry:
    enabled: true
    max_attempts: 3
    strategy: exponential     # constant, linear, exponential
    initial_wait_ms: 1000     # Начальная задержка
    max_wait_ms: 30000        # Максимальная задержка
    jitter: true              # Случайность для избежания thundering herd
```

#### Стратегии backoff

- **constant**: Постоянная задержка (1s, 1s, 1s)
- **linear**: Линейное увеличение (1s, 2s, 3s)
- **exponential**: Экспоненциальное увеличение (1s, 2s, 4s, 8s)

#### Примеры

```bash
# Retry автоматически применяется к операциям БД
tdtpcli --export large_table  # Retry при network timeout

# С Circuit Breaker:
# Attempt 1: Retry after 1s
# Attempt 2: Retry after 2s
# Attempt 3: Retry after 4s
# Circuit opens if all fail
```

### Audit Logging

Логирование всех операций для соответствия GDPR/HIPAA.

#### Конфигурация

```yaml
audit:
  enabled: true
  level: standard             # minimal, standard, full
  file: audit.log
  max_size_mb: 100
  console: true               # Дублировать в консоль
```

#### Уровни логирования

- **minimal**: Только критичные операции (import, delete)
- **standard**: Все операции с метаданными
- **full**: Полная детализация с данными

#### Формат логов

```log
[2024-11-17 16:00:00] [SUCCESS] EXPORT table=customers user=tdtpcli duration_ms=1234
[2024-11-17 16:00:05] [SUCCESS] IMPORT table=orders user=tdtpcli duration_ms=567 strategy=replace
[2024-11-17 16:00:10] [FAILURE] EXPORT table=products user=tdtpcli error="connection timeout"
```

#### Примеры

```bash
# Audit логирование включено автоматически
tdtpcli --export customers  # Logged to audit.log

# Просмотр audit лога
tail -f audit.log

# Фильтрация по операциям
grep "EXPORT" audit.log
grep "FAILURE" audit.log
```

---

## Data Processors

Обработка данных "на лету" для защиты PII и контроля качества.

### Field Masking (--mask)

Маскирование чувствительных данных при экспорте.

#### Использование

```bash
tdtpcli --export customers --mask email,phone,card_number
```

#### Паттерны маскирования

Автоматическое определение по имени поля:

| Поле | Паттерн | Результат |
|------|---------|-----------|
| email | Partial | `j***@example.com` |
| phone, mobile | Middle | `+1 (555) XXX-X567` |
| card, credit | First2Last2 | `12** **78` |
| passport, ssn | Stars | `*****` |
| другие | Partial | `ab***fg` |

#### Примеры

```bash
# Маскирование одного поля
tdtpcli --export users --mask email

# Маскирование нескольких полей
tdtpcli --export customers --mask email,phone,credit_card

# Экспорт в XLSX с маскированием
tdtpcli --export-xlsx patients --mask ssn,medical_record

# Incremental sync с маскированием
tdtpcli --sync-incremental users \
  --tracking-field updated_at \
  --mask email,phone
```

### Field Validation (--validate)

Валидация данных перед импортом.

#### Использование

```bash
tdtpcli --import data.xml --validate rules.yaml
```

#### Встроенные валидаторы

- **email**: RFC 5322 email validation
- **phone**: Международный формат телефона (7-15 цифр)
- **url**: HTTP/HTTPS URL validation
- **date**: YYYY-MM-DD формат

#### Конфигурация правил

```yaml
# rules.yaml
email:
  - type: email
phone:
  - type: phone
age:
  - type: range:18-100
status:
  - type: enum:active,inactive,pending
```

#### Примеры

```bash
# Валидация при импорте
tdtpcli --import customers.xml --validate customer_rules.yaml

# Валидация с остановкой на первой ошибке
tdtpcli --import data.xml --validate strict_rules.yaml

# Импорт из XLSX с валидацией
tdtpcli --import-xlsx data.xlsx --validate rules.yaml
```

### Field Normalization (--normalize)

Нормализация данных для консистентности.

#### Использование

```bash
tdtpcli --import data.xml --normalize rules.yaml
```

#### Встроенные нормализаторы

- **email**: Lowercase, trim
- **phone**: Удаление форматирования → `79991234567`
- **whitespace**: Trim, множественные пробелы → один
- **uppercase/lowercase**: Регистр
- **date**: DD.MM.YYYY → YYYY-MM-DD

#### Конфигурация правил

```yaml
# rules.yaml
email:
  - type: email          # john.DOE@Example.COM → john.doe@example.com
phone:
  - type: phone          # +1 (555) 123-4567 → 15551234567
name:
  - type: whitespace     # "John   Doe" → "John Doe"
```

#### Примеры

```bash
# Нормализация при импорте
tdtpcli --import messy_data.xml --normalize cleanup_rules.yaml

# Нормализация перед валидацией
tdtpcli --import data.xml \
  --normalize normalize.yaml \
  --validate validate.yaml

# Нормализация с маскированием
tdtpcli --export users \
  --normalize normalize.yaml \
  --mask ssn,card
```

### Комбинирование процессоров

Процессоры применяются последовательно:

```bash
# Normalize → Validate → Mask
tdtpcli --export customers \
  --normalize cleanup.yaml \
  --validate rules.yaml \
  --mask email,phone
```

---

## Incremental Sync

Инкрементальная синхронизация только измененных данных.

### Основная команда

```bash
tdtpcli --sync-incremental <table> --tracking-field <field>
```

### Параметры

- `--tracking-field` - Поле для отслеживания изменений (default: `updated_at`)
- `--checkpoint-file` - Файл состояния (default: `checkpoint.yaml`)
- `--batch-size` - Размер batch (default: 1000)
- `--output` - Выходной файл

### Стратегии отслеживания

#### 1. Timestamp tracking
```bash
tdtpcli --sync-incremental orders --tracking-field updated_at
```

Для полей: `updated_at`, `modified_at`, `changed_at`

#### 2. Sequence tracking
```bash
tdtpcli --sync-incremental products --tracking-field id
```

Для автоинкрементных ID или sequence

#### 3. Version tracking
```bash
tdtpcli --sync-incremental documents --tracking-field version
```

Для версионированных данных

### Checkpoint файл

Автоматически сохраняется состояние синхронизации:

```json
{
  "orders": {
    "table_name": "orders",
    "last_sync_value": "2024-11-17 16:00:00",
    "last_sync_time": "2024-11-17T16:05:23Z",
    "records_exported": 1234
  }
}
```

### Примеры

#### Первая синхронизация
```bash
# Экспортирует все записи
tdtpcli --sync-incremental orders --tracking-field updated_at
# Output: orders_sync_20241117_160000.xml
# Checkpoint: last_sync_value = "2024-11-17 16:00:00"
```

#### Повторная синхронизация
```bash
# Экспортирует только изменения с last_sync_value
tdtpcli --sync-incremental orders --tracking-field updated_at
# Query: WHERE updated_at > '2024-11-17 16:00:00' ORDER BY updated_at ASC
# Output: orders_sync_20241117_170000.xml
# Checkpoint: last_sync_value = "2024-11-17 17:00:00"
```

#### Batch синхронизация
```bash
# Ограничить размер batch
tdtpcli --sync-incremental large_table \
  --tracking-field id \
  --batch-size 500
```

#### Кастомный checkpoint
```bash
# Использовать свой checkpoint файл
tdtpcli --sync-incremental users \
  --tracking-field last_modified \
  --checkpoint-file /data/checkpoints/users.json
```

#### С процессорами
```bash
# Sync с маскированием PII
tdtpcli --sync-incremental customers \
  --tracking-field updated_at \
  --mask email,phone,ssn \
  --output daily_sync.xml
```

### Workflow использования

```bash
# Инициализация (первый запуск)
tdtpcli --sync-incremental orders --tracking-field updated_at

# Ежедневная синхронизация (cron job)
0 2 * * * tdtpcli --sync-incremental orders --tracking-field updated_at --output /backup/orders_$(date +\%Y\%m\%d).xml

# Сброс checkpoint для полной ре-синхронизации
rm checkpoint.yaml
tdtpcli --sync-incremental orders --tracking-field updated_at
```

---

## Примеры использования

### Пример 1: Production-ready экспорт

```bash
# Экспорт с защитой PII и аудитом
tdtpcli --config production.yaml \
  --export customers \
  --mask email,phone,card_number \
  --validate customer_rules.yaml \
  --output customers_$(date +%Y%m%d).xml
```

**Результат:**
- Circuit Breaker защищает от сбоев
- Retry при временных ошибках
- Audit log: все операции записаны
- PII данные замаскированы
- Валидация перед экспортом

### Пример 2: Ежедневная инкрементальная синхронизация

```bash
#!/bin/bash
# daily_sync.sh

# Sync orders
tdtpcli --sync-incremental orders \
  --tracking-field updated_at \
  --mask customer_email,customer_phone \
  --output /backup/orders_$(date +%Y%m%d).xml

# Sync users
tdtpcli --sync-incremental users \
  --tracking-field last_modified \
  --mask email,phone,ssn \
  --output /backup/users_$(date +%Y%m%d).xml

# Sync products (no PII)
tdtpcli --sync-incremental products \
  --tracking-field updated_at \
  --output /backup/products_$(date +%Y%m%d).xml
```

**Cron:**
```cron
0 2 * * * /scripts/daily_sync.sh
```

### Пример 3: ETL pipeline

```bash
# 1. Экспорт из production БД
tdtpcli --config prod.yaml \
  --export transactions \
  --where "created_at >= '2024-11-01'" \
  --mask customer_email,card_number \
  --output raw_transactions.xml

# 2. Конвертация в XLSX для анализа
tdtpcli --to-xlsx raw_transactions.xml \
  --output transactions_analysis.xlsx \
  --sheet "Transactions"

# 3. Нормализация и импорт в DWH
tdtpcli --config dwh.yaml \
  --import raw_transactions.xml \
  --normalize cleanup_rules.yaml \
  --validate dwh_rules.yaml \
  --strategy replace
```

### Пример 4: Миграция между БД

```bash
# Source: PostgreSQL
tdtpcli --config source_pg.yaml \
  --export products \
  --output products_migration.xml

# Target: MySQL (с нормализацией)
tdtpcli --config target_mysql.yaml \
  --import products_migration.xml \
  --normalize mysql_compatibility.yaml \
  --strategy replace
```

### Пример 5: Batch импорт с Excel

```bash
# Импорт из множества Excel файлов
for file in data/*.xlsx; do
  echo "Importing $file..."
  tdtpcli --import-xlsx "$file" \
    --sheet "Data" \
    --normalize cleanup.yaml \
    --validate rules.yaml \
    --strategy ignore
done
```

### Пример 6: Backup и restore

```bash
# Backup всех таблиц
tables=$(tdtpcli --config prod.yaml --list | tail -n +2)
for table in $tables; do
  tdtpcli --export "$table" \
    --output "backup/${table}_$(date +%Y%m%d).xml"
done

# Restore в другую БД
for file in backup/*.xml; do
  tdtpcli --config restore.yaml \
    --import "$file" \
    --strategy replace
done
```

---

## Производительность и оптимизация

### Circuit Breaker настройки

**Для медленных операций:**
```yaml
resilience:
  circuit_breaker:
    threshold: 10          # Больше попыток
    timeout: 120           # Дольше ждать восстановления
```

**Для критичных операций:**
```yaml
resilience:
  circuit_breaker:
    threshold: 3           # Меньше попыток
    timeout: 30            # Быстрее fallback
```

### Retry стратегии

**Для network issues:**
```yaml
resilience:
  retry:
    strategy: exponential
    max_attempts: 5
    initial_wait_ms: 500
    max_wait_ms: 60000
    jitter: true
```

**Для быстрых операций:**
```yaml
resilience:
  retry:
    strategy: constant
    max_attempts: 3
    initial_wait_ms: 100
```

### Batch размеры

```bash
# Малые таблицы (< 10K rows)
--batch-size 1000

# Средние таблицы (10K-100K rows)
--batch-size 5000

# Большие таблицы (> 100K rows)
--batch-size 10000
```

---

## Устранение неполадок

### Circuit Breaker открыт

```
Error: circuit breaker is open
```

**Решение:**
1. Проверить доступность БД/брокера
2. Дождаться timeout (60s по умолчанию)
3. Увеличить threshold в config.yaml

### Processor валидация не прошла

```
Error: validation failed with 5 errors:
- row 1, field 'email': invalid email format
```

**Решение:**
1. Применить нормализацию перед валидацией
2. Исправить данные в источнике
3. Обновить правила валидации

### Checkpoint файл поврежден

```
Error: failed to load state: invalid JSON
```

**Решение:**
1. Удалить checkpoint файл
2. Выполнить полную синхронизацию
3. Backup checkpoint файла после каждого sync

---

## Заключение

TDTP CLI v1.2 предоставляет production-ready решение для миграции данных с:
- **Надежностью**: Circuit Breaker, Retry, Audit
- **Безопасностью**: PII masking, валидация
- **Эффективностью**: Incremental sync, batch processing
- **Гибкостью**: XLSX, стратегии импорта, процессоры

Для получения дополнительной информации см. [USER_GUIDE.md](USER_GUIDE.md).
