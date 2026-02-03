# ETL Pipeline Processor - Руководство пользователя

## Содержание

1. [Введение](#введение)
2. [Архитектура](#архитектура)
3. [Безопасность](#безопасность)
4. [Конфигурация YAML](#конфигурация-yaml)
5. [Использование CLI](#использование-cli)
6. [Примеры](#примеры)
7. [Производительность](#производительность)
8. [Устранение неполадок](#устранение-неполадок)

---

## Введение

ETL Pipeline Processor - это мощный инструмент для сбора данных из множественных источников, их объединения через SQL трансформации и экспорта в различные форматы.

### Ключевые возможности

- ✅ **Множественные источники**: PostgreSQL, MS SQL Server, MySQL, SQLite
- ✅ **Параллельная загрузка**: все источники загружаются одновременно
- ✅ **SQLite :memory: workspace**: быстрые JOIN операции без дисковых операций
- ✅ **SQL трансформации**: полная мощь SQL для обработки данных
- ✅ **Множественные выходы**: TDTP XML, RabbitMQ, Kafka
- ✅ **4-уровневая безопасность**: защита от случайного повреждения данных
- ✅ **Детальная статистика**: время выполнения, количество строк, ошибки

### Когда использовать ETL Pipeline

- Объединение данных из разных баз данных
- Миграция данных между системами
- Создание отчетов из множественных источников
- Синхронизация справочников
- Агрегация данных для аналитики

---

## Архитектура

### Компоненты ETL Pipeline

```
┌─────────────────────────────────────────────────────────────┐
│                    ETL Pipeline Processor                    │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  1. Loader (параллельная загрузка из источников)            │
│     ├── PostgreSQL источник 1                                │
│     ├── MS SQL Server источник 2                             │
│     ├── MySQL источник 3                                     │
│     └── SQLite источник 4                                    │
│                                                               │
│  2. Workspace (SQLite :memory: для JOIN операций)           │
│     ├── CREATE TABLE для каждого источника                  │
│     ├── INSERT данных из источников                          │
│     └── Готово для SQL трансформаций                        │
│                                                               │
│  3. Executor (выполнение SQL трансформаций)                 │
│     └── SELECT ... FROM source1 JOIN source2 ...            │
│                                                               │
│  4. Exporter (экспорт результатов)                          │
│     ├── TDTP XML файл                                        │
│     ├── RabbitMQ очередь                                     │
│     └── Kafka топик                                          │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

### Поток данных

```
Sources → Loader → Workspace → Executor → Exporter → Output
   ║         ║         ║           ║          ║         ║
   ║         ║         ║           ║          ║         ║
PostgreSQL   ║    :memory:     SELECT ...   ║      result.xml
MS SQL    Parallel   Tables       JOIN     Export    queue://
MySQL     Loading    + Data      Filter    TDTP      topic://
SQLite      ║         ║           ║          ║         ║
```

---

## Безопасность

### 4-уровневая система безопасности

ETL Pipeline реализует многоуровневую защиту для предотвращения случайного повреждения данных:

#### Уровень 1: Code Level (SQLValidator)
```go
// Запрещенные операции в safe mode:
- INSERT, UPDATE, DELETE
- DROP, TRUNCATE, ALTER
- CREATE, GRANT, REVOKE
- PRAGMA (SQLite)
- Множественные команды через ;
- Комментарии -- и /* */
```

#### Уровень 2: OS Level (IsAdmin)
```bash
# Unsafe mode требует прав администратора
$ tdtpcli --pipeline pipeline.yaml --unsafe
Error: unsafe mode requires administrator privileges (current user: user)

# Запуск под root/Administrator
$ sudo tdtpcli --pipeline pipeline.yaml --unsafe  # Unix
$ runas /user:Administrator "tdtpcli --pipeline pipeline.yaml --unsafe"  # Windows
```

#### Уровень 3: CLI Level (флаг --unsafe)
```bash
# По умолчанию: READ-ONLY режим (safe mode)
$ tdtpcli --pipeline pipeline.yaml
Mode: 🔒 SAFE (READ-ONLY: SELECT/WITH only)

# Явное указание unsafe mode
$ tdtpcli --pipeline pipeline.yaml --unsafe
Mode: 🔓 UNSAFE (All SQL operations allowed - ADMIN MODE)
```

#### Уровень 4: SQL Level (валидация запросов)
```yaml
# Все SQL запросы валидируются перед выполнением
sources:
  - name: users
    query: "SELECT * FROM users"  # ✅ Разрешено
    # query: "DELETE FROM users"  # ❌ Заблокировано в safe mode
```

### Safe vs Unsafe режимы

| Режим | Разрешенные операции | Admin права | Использование |
|-------|---------------------|-------------|---------------|
| **Safe** (по умолчанию) | SELECT, WITH | Не требуются | Чтение данных, отчеты, экспорт |
| **Unsafe** (--unsafe) | Все SQL операции | **Обязательны** | Миграции с INSERT/UPDATE, изменение структуры |

---

## Конфигурация YAML

### Полный пример конфигурации

```yaml
# Основная информация о pipeline
name: "User Orders Report"
version: "1.0"
description: "Объединение пользователей и заказов из разных источников"

# Источники данных
sources:
  # PostgreSQL источник
  - name: users  # Имя источника = имя таблицы в workspace
    type: postgres
    dsn: "postgres://user:password@localhost:5432/production?sslmode=disable"
    query: |
      SELECT
        id,
        username,
        email,
        created_at
      FROM users
      WHERE active = true

  # MS SQL Server источник
  - name: orders
    type: mssql
    dsn: "server=localhost;user id=sa;password=Password123;database=OrdersDB"
    query: |
      SELECT
        order_id,
        user_id,
        total_amount,
        order_date
      FROM orders
      WHERE order_date >= '2024-01-01'

  # MySQL источник
  - name: products
    type: mysql
    dsn: "user:password@tcp(localhost:3306)/products_db"
    query: |
      SELECT
        product_id,
        product_name,
        price
      FROM products
      WHERE in_stock = 1

# Workspace конфигурация
workspace:
  type: sqlite
  mode: ":memory:"  # Использовать память (быстро)
  # mode: "workspace.db"  # Или файл на диске (для отладки)

# SQL трансформация
transform:
  result_table: "user_orders_report"
  sql: |
    SELECT
      u.id as user_id,
      u.username,
      u.email,
      COUNT(o.order_id) as total_orders,
      SUM(o.total_amount) as total_spent,
      GROUP_CONCAT(p.product_name) as products_purchased
    FROM users u
    LEFT JOIN orders o ON u.id = o.user_id
    LEFT JOIN products p ON o.order_id = p.product_id
    GROUP BY u.id, u.username, u.email
    HAVING total_orders > 0
    ORDER BY total_spent DESC

# Выходной формат
output:
  type: tdtp
  tdtp:
    destination: "reports/user_orders_report.xml"
    format: "xml"
    compression: true  # Использовать zstd сжатие (уровень 3)

# Настройки производительности
performance:
  timeout: 300  # 5 минут
  batch_size: 10000
  parallel_sources: true
  max_memory_mb: 2048

# Аудит выполнения
audit:
  enabled: true
  log_file: "logs/etl_pipeline.log"
  log_queries: true
  log_errors: true

# Обработка ошибок
error_handling:
  on_source_error: "continue"  # continue | fail
  on_transform_error: "fail"   # continue | fail
  on_export_error: "fail"      # continue | fail
  retry_count: 3
  retry_delay_sec: 5
```

### Минимальная конфигурация

```yaml
name: "Simple ETL"
version: "1.0"

sources:
  - name: source1
    type: postgres
    dsn: "postgres://localhost/db1"
    query: "SELECT * FROM table1"

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "result"
  sql: "SELECT * FROM data1"

output:
  type: tdtp
  tdtp:
    destination: "output.xml"
    format: "xml"
```

> **Примечание**: Тип output (`type`) не чувствителен к регистру. Можно использовать `tdtp`, `TDTP` или `Tdtp` - все варианты будут работать одинаково. Рекомендуется использовать lowercase для единообразия.

### Конфигурация для RabbitMQ

```yaml
output:
  type: rabbitmq  # Также можно: RabbitMQ, RABBITMQ (case-insensitive)
  rabbitmq:
    host: localhost
    port: 5672
    user: guest
    password: guest
    queue: etl_results
    vhost: "/"
    exchange: ""
    routing_key: etl_results
```

### Конфигурация для Kafka

```yaml
output:
  type: kafka  # Также можно: Kafka, KAFKA (case-insensitive)
  kafka:
    brokers: "localhost:9092,localhost:9093"
    topic: etl_results
    partition: 0
    compression: gzip
```

---

## Использование CLI

### Базовое использование

```bash
# Safe mode (по умолчанию)
tdtpcli --pipeline pipeline.yaml

# Unsafe mode (требует admin)
sudo tdtpcli --pipeline pipeline.yaml --unsafe
```

### Примеры вывода

#### Успешное выполнение (Safe mode)

```
📋 Pipeline: User Orders Report
   Объединение пользователей и заказов из разных источников
   Version: 1.0
   Mode: 🔒 SAFE (READ-ONLY: SELECT/WITH only)
   Sources: 3
   Workspace: sqlite (:memory:)
   Output: TDTP

🚀 Starting ETL pipeline execution...

✅ ETL Pipeline completed successfully!
   Duration: 2.45s
   Sources loaded: 3
   Rows loaded: 15,234
   Rows exported: 8,967
```

#### Ошибка валидации (попытка DELETE в safe mode)

```
📋 Pipeline: Dangerous Pipeline
   Version: 1.0
   Mode: 🔒 SAFE (READ-ONLY: SELECT/WITH only)
   Sources: 1
   Workspace: sqlite (:memory:)
   Output: TDTP

Error: SQL validation failed: source[0] 'users' query validation failed:
forbidden keyword detected: DELETE
```

#### Ошибка прав доступа (unsafe без admin)

```
Error: unsafe mode requires administrator privileges (current user: john)
```

---

## Примеры

### Пример 1: Объединение справочников из разных БД

**Задача**: Объединить справочники клиентов из PostgreSQL и MS SQL Server.

**pipeline.yaml**:
```yaml
name: "Unified Customers Directory"
version: "1.0"

sources:
  - name: pg_customers
    type: postgres
    dsn: "postgres://user:pass@pg-server:5432/crm"
    query: |
      SELECT
        customer_id,
        'PG' as source,
        customer_name,
        email,
        phone
      FROM customers

  - name: mssql_customers
    type: mssql
    dsn: "server=mssql-server;database=Sales;user id=sa;password=Pass"
    query: |
      SELECT
        CustomerID as customer_id,
        'MSSQL' as source,
        CustomerName as customer_name,
        Email as email,
        Phone as phone
      FROM Customers

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "unified_customers"
  sql: |
    SELECT
      customer_id,
      source,
      customer_name,
      email,
      phone
    FROM (
      SELECT * FROM pg_customers
      UNION ALL
      SELECT * FROM mssql_customers
    )
    ORDER BY customer_name

output:
  type: tdtp
  tdtp:
    destination: "unified_customers.xml"
    format: "xml"
    compression: true
```

**Запуск**:
```bash
tdtpcli --pipeline pipeline.yaml
```

### Пример 2: Отчет о продажах с JOIN

**Задача**: Создать отчет о продажах, объединив данные о заказах, продуктах и клиентах.

**pipeline.yaml**:
```yaml
name: "Sales Report"
version: "1.0"

sources:
  - name: orders
    type: postgres
    dsn: "postgres://localhost/orders_db"
    query: |
      SELECT
        order_id,
        customer_id,
        product_id,
        quantity,
        order_date
      FROM orders
      WHERE order_date BETWEEN '2024-01-01' AND '2024-12-31'

  - name: products
    type: mysql
    dsn: "user:pass@tcp(localhost:3306)/products_db"
    query: |
      SELECT
        product_id,
        product_name,
        price,
        category
      FROM products

  - name: customers
    type: mssql
    dsn: "server=localhost;database=CRM;user id=sa;password=Pass"
    query: |
      SELECT
        customer_id,
        customer_name,
        region
      FROM customers

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "sales_report"
  sql: |
    SELECT
      c.customer_name,
      c.region,
      p.product_name,
      p.category,
      SUM(o.quantity) as total_quantity,
      SUM(o.quantity * p.price) as total_revenue,
      COUNT(DISTINCT o.order_id) as order_count
    FROM orders o
    INNER JOIN products p ON o.product_id = p.product_id
    INNER JOIN customers c ON o.customer_id = c.customer_id
    GROUP BY c.customer_name, c.region, p.product_name, p.category
    HAVING total_revenue > 1000
    ORDER BY total_revenue DESC

output:
  type: tdtp
  tdtp:
    destination: "sales_report_2024.xml"
    format: "xml"
    compression: true

audit:
  enabled: true
  log_file: "logs/sales_report.log"
```

**Запуск**:
```bash
tdtpcli --pipeline pipeline.yaml
```

### Пример 3: Миграция данных с трансформацией (Unsafe mode)

**Задача**: Скопировать пользователей из старой БД в новую с нормализацией данных.

**migration.yaml**:
```yaml
name: "User Migration"
version: "1.0"
description: "Миграция пользователей из legacy системы"

sources:
  - name: legacy_users
    type: mysql
    dsn: "user:pass@tcp(old-server:3306)/legacy_db"
    query: |
      SELECT
        user_id,
        TRIM(username) as username,
        LOWER(TRIM(email)) as email,
        created_date
      FROM users
      WHERE status = 'active'

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "migrated_users"
  sql: |
    SELECT
      user_id,
      username,
      email,
      created_date,
      CURRENT_TIMESTAMP as migrated_at
    FROM old_users
    WHERE email LIKE '%@%'  -- Только валидные email

output:
  type: tdtp
  tdtp:
    destination: "migrated_users.xml"
    format: "xml"

performance:
  batch_size: 5000
  timeout: 600
```

**Запуск** (требует admin для unsafe):
```bash
# Первый этап: экспорт в TDTP (safe mode)
tdtpcli --pipeline migration.yaml

# Второй этап: импорт в новую БД (отдельной командой)
tdtpcli --import migrated_users.xml --config new-db-config.yaml
```

---

## Производительность

### Оптимизация производительности

#### 1. Используйте :memory: режим

```yaml
workspace:
  type: sqlite
  mode: ":memory:"  # Быстрее чем disk
```

**Производительность**:
- `:memory:` - до 10x быстрее для JOIN операций
- Disk mode - для отладки или очень больших объемов

#### 2. Настройте batch_size

```yaml
performance:
  batch_size: 10000  # Оптимально для большинства случаев
```

**Рекомендации**:
- 1,000 - малые объемы (< 10k строк)
- 10,000 - средние объемы (10k - 1M строк)
- 50,000 - большие объемы (> 1M строк)

#### 3. Параллельная загрузка

```yaml
performance:
  parallel_sources: true  # Все источники загружаются одновременно
```

#### 4. Ограничьте данные на источнике

```yaml
sources:
  - name: large_table
    query: |
      SELECT * FROM large_table
      WHERE created_at >= CURRENT_DATE - INTERVAL '7 days'  -- Только последние 7 дней
      LIMIT 100000  -- Ограничение на источнике, не в workspace
```

#### 5. Используйте индексы в источниках

```sql
-- Создайте индексы в источниках перед запуском ETL
CREATE INDEX idx_users_created ON users(created_at);
CREATE INDEX idx_orders_user_id ON orders(user_id);
```

### Benchmarks

| Операция | Объем данных | Время (safe mode) | Память |
|----------|-------------|-------------------|--------|
| 1 источник, простой SELECT | 10,000 строк | ~0.5s | ~50MB |
| 1 источник, простой SELECT | 100,000 строк | ~3.2s | ~200MB |
| 1 источник, простой SELECT | 1,000,000 строк | ~28s | ~1.5GB |
| 3 источника, JOIN | 10,000 строк каждый | ~1.8s | ~100MB |
| 3 источника, JOIN | 100,000 строк каждый | ~15s | ~800MB |
| 3 источника, сложный JOIN + GROUP BY | 100,000 строк каждый | ~22s | ~1GB |

**Тестовая среда**: Intel i7-9700K, 32GB RAM, SSD, PostgreSQL/MySQL/MSSQL на localhost

---

## Устранение неполадок

### Частые ошибки

#### 1. "SQL validation failed: forbidden keyword"

**Причина**: Попытка использовать запрещенные операции в safe mode.

**Решение**:
```bash
# Вариант 1: Используйте только SELECT/WITH в safe mode
# Вариант 2: Запустите в unsafe mode с admin правами
sudo tdtpcli --pipeline pipeline.yaml --unsafe
```

#### 2. "unsafe mode requires administrator privileges"

**Причина**: Unsafe mode без прав администратора.

**Решение**:
```bash
# Unix/Linux
sudo tdtpcli --pipeline pipeline.yaml --unsafe

# Windows (запустите cmd как Администратор)
tdtpcli --pipeline pipeline.yaml --unsafe
```

#### 3. "failed to load pipeline config"

**Причина**: Ошибка в YAML синтаксисе или отсутствие обязательных полей.

**Решение**:
```bash
# Проверьте YAML синтаксис
yamllint pipeline.yaml

# Проверьте обязательные поля:
# - name, version
# - sources (минимум 1)
# - sources[].name, type, dsn, table_alias, query
# - workspace.type, mode
# - transform.sql, result_table
# - output.type
```

#### 4. "adapter not connected" / "failed to ping database"

**Причина**: Неверный DSN или недоступна БД.

**Решение**:
```yaml
# Проверьте DSN строки:
# PostgreSQL: postgres://user:password@host:5432/database
# MySQL: user:password@tcp(host:3306)/database
# MSSQL: server=host;user id=sa;password=pass;database=db
# SQLite: /path/to/file.db или :memory:

# Проверьте доступность:
ping database-host
telnet database-host 5432
```

#### 5. "query returned no columns"

**Причина**: SQL запрос источника не возвращает данных.

**Решение**:
```sql
-- Проверьте запрос напрямую в БД:
-- Убедитесь что запрос возвращает строки
SELECT COUNT(*) FROM users WHERE active = true;

-- Добавьте отладку в pipeline.yaml:
audit:
  enabled: true
  log_queries: true  # Логировать все SQL запросы
```

#### 6. "out of memory" при больших объемах

**Причина**: Слишком большой объем данных для :memory: режима.

**Решение**:
```yaml
# Вариант 1: Ограничьте данные на источнике
sources:
  - query: "SELECT * FROM large_table LIMIT 1000000"

# Вариант 2: Используйте disk workspace
workspace:
  mode: "/tmp/etl_workspace.db"

# Вариант 3: Увеличьте лимит памяти
performance:
  max_memory_mb: 4096  # 4GB
```

### Отладка

#### Включите подробное логирование

```yaml
audit:
  enabled: true
  log_file: "debug.log"
  log_queries: true
  log_errors: true

error_handling:
  on_source_error: "continue"  # Продолжить при ошибках источника
```

#### Используйте disk workspace для отладки

```yaml
workspace:
  type: sqlite
  mode: "debug_workspace.db"  # Файл останется после выполнения
```

Затем проверьте данные:
```bash
sqlite3 debug_workspace.db
sqlite> .tables
sqlite> SELECT * FROM source1 LIMIT 10;
sqlite> SELECT COUNT(*) FROM source2;
```

#### Тестируйте SQL запросы отдельно

```bash
# Скопируйте SQL из transform.sql и выполните в SQLite
sqlite3 :memory: < transform.sql
```

---

## Дополнительные ресурсы

- [TDTP Specification](SPECIFICATION.md) - Спецификация TDTP протокола
- [Modules Documentation](MODULES.md) - Документация всех модулей
- [CLI Guide](USER_GUIDE.md) - Полное руководство по CLI
- [Developer Guide](DEVELOPER_GUIDE.md) - Руководство разработчика

---

## Обратная связь

Нашли ошибку или есть предложения? Создайте issue на GitHub:
https://github.com/ruslano69/tdtp-framework-main/issues
