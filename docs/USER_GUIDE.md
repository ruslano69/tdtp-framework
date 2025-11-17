# TDTP CLI - Руководство пользователя

**tdtpcli** - утилита командной строки для работы с TDTP (Table Data Transfer Protocol).

**Версия:** 1.2
**Дата:** 16.11.2025

---

## Содержание

1. [Установка](#установка)
2. [Быстрый старт](#быстрый-старт)
3. [Конфигурация](#конфигурация)
4. [Команды](#команды)
5. [Фильтрация данных (TDTQL)](#фильтрация-данных-tdtql)
6. [Работа с Message Brokers](#работа-с-message-brokers)
7. [Примеры использования](#примеры-использования)
8. [Устранение неполадок](#устранение-неполадок)

---

## Установка

### Требования

- **Go** 1.21 или выше (для сборки из исходников)
- **Доступ к БД:** SQLite, PostgreSQL, или MS SQL Server
- **Message Broker** (опционально): RabbitMQ или MSMQ

### Сборка из исходников

```bash
git clone https://github.com/ruslano69/tdtp-framework
cd tdtp-framework
go mod tidy
go build -o tdtpcli ./cmd/tdtpcli
```

### Проверка установки

```bash
./tdtpcli --help
```

---

## Быстрый старт

### 1. Создание конфигурации

Выберите тип базы данных:

**SQLite:**
```bash
./tdtpcli --create-config-sqlite
```

**PostgreSQL:**
```bash
./tdtpcli --create-config-pg
```

**MS SQL Server:**
```bash
./tdtpcli --create-config-mssql
```

Будет создан файл `config.{dbtype}.yaml` с шаблоном настроек.

### 2. Редактирование конфигурации

Откройте созданный файл и укажите параметры подключения:

**config.postgres.yaml:**
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  user: myuser
  password: mypassword
  dbname: mydb
  schema: public
  sslmode: disable
```

### 3. Проверка подключения

Получите список таблиц:

```bash
./tdtpcli -config config.postgres.yaml --list
```

### 4. Экспорт данных

Экспортируйте таблицу в файл:

```bash
./tdtpcli -config config.postgres.yaml --export users --output users.tdtp.xml
```

### 5. Импорт данных

Импортируйте данные из файла:

```bash
./tdtpcli -config config.postgres.yaml --import users.tdtp.xml
```

---

## Конфигурация

### Структура конфигурационного файла

```yaml
# Настройки базы данных
database:
  type: postgres         # sqlite | postgres | mssql

  # SQLite
  path: database.db     # Путь к файлу БД (только для SQLite)

  # PostgreSQL / MS SQL
  host: localhost
  port: 5432            # 5432 для PostgreSQL, 1433 для MS SQL
  user: username
  password: password
  dbname: database_name

  # PostgreSQL specific
  schema: public        # Схема БД (default: public)
  sslmode: disable      # disable | require | verify-ca | verify-full

  # MS SQL specific
  instance: SQLEXPRESS  # Имя инстанса (опционально)
  encrypt: false        # Шифрование соединения
  trustServerCertificate: true

# Настройки message broker (опционально)
broker:
  type: rabbitmq        # rabbitmq | msmq
  host: localhost
  port: 5672            # 5672 для RabbitMQ
  user: guest
  password: guest
  queue: tdtp_queue     # Имя очереди
  vhost: /              # Virtual host (RabbitMQ)
  durable: true         # Устойчивость очереди
  auto_delete: false    # Автоудаление очереди
  exclusive: false      # Эксклюзивность очереди
```

### Примеры конфигураций

**SQLite:**
```yaml
database:
  type: sqlite
  path: ./database.db
```

**PostgreSQL с RabbitMQ:**
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  user: tdtp_user
  password: secure_password
  dbname: production_db
  schema: public
  sslmode: require

broker:
  type: rabbitmq
  host: rabbitmq.example.com
  port: 5672
  user: tdtp
  password: broker_password
  queue: tdtp_production_queue
  vhost: /
  durable: true
  auto_delete: false
  exclusive: false
```

**MS SQL Server:**
```yaml
database:
  type: mssql
  host: sql-server.example.com
  port: 1433
  user: sa
  password: MyStr0ngP@ssw0rd
  dbname: MyDatabase
  instance: SQLEXPRESS
  encrypt: true
  trustServerCertificate: false
```

---

## Команды

### --list

Показать список таблиц в базе данных.

**Синтаксис:**
```bash
tdtpcli -config <config.yaml> --list
```

**Пример:**
```bash
./tdtpcli -config config.postgres.yaml --list
```

**Вывод:**
```
📁 Using config: config.postgres.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.15)

📋 Tables in database (4):
  1. users
  2. products
  3. orders
  4. activity_logs
```

---

### --export

Экспортировать таблицу в файл или stdout.

**Синтаксис:**
```bash
tdtpcli -config <config.yaml> --export <table> [--output <file>]
```

**Параметры:**
- `<table>` - имя таблицы (обязательно)
- `--output <file>` - выходной файл (опционально, по умолчанию stdout)

**Примеры:**

Экспорт в stdout:
```bash
./tdtpcli -config config.postgres.yaml --export users
```

Экспорт в файл:
```bash
./tdtpcli -config config.postgres.yaml --export users --output users.tdtp.xml
```

Автоматическое добавление расширения:
```bash
./tdtpcli -config config.postgres.yaml --export users --output users
# Создаст файл: users.tdtp.xml
```

---

### --import

Импортировать данные из TDTP файла.

**Синтаксис:**
```bash
tdtpcli -config <config.yaml> --import <file>
```

**Параметры:**
- `<file>` - путь к TDTP файлу (обязательно)

**Пример:**
```bash
./tdtpcli -config config.postgres.yaml --import users.tdtp.xml
```

**Вывод:**
```
📁 Using config: config.postgres.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.15)

📥 Importing from file: users.tdtp.xml
✅ Imported 100 rows into table 'users'
```

**Стратегии импорта:**

По умолчанию используется стратегия на основе типа пакета:
- **reference** → REPLACE (полная замена через temp table)
- **delta** → COPY (вставка новых записей)
- **response** → REPLACE

Поведение можно изменить в коде, модифицировав `cmd/tdtpcli/main.go`.

---

### --export-broker

Экспортировать таблицу в message broker queue.

**Синтаксис:**
```bash
tdtpcli -config <config.yaml> --export-broker <table>
```

**Параметры:**
- `<table>` - имя таблицы (обязательно)

**Пример:**
```bash
./tdtpcli -config config.postgres.yaml --export-broker users
```

**Вывод:**
```
📁 Using config: config.postgres.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.15)

📡 Connecting to rabbitmq broker...
✅ Connected to broker

📤 Exporting table: users
✅ Successfully published 1 packets to queue 'tdtp_queue'
   Total rows: 100
```

---

### --import-broker

Импортировать данные из message broker queue.

**Синтаксис:**
```bash
tdtpcli -config <config.yaml> --import-broker
```

**Работа:**
- Подключается к очереди
- Ожидает сообщения (blocking mode)
- Импортирует данные в БД
- Подтверждает получение (manual ACK)
- Продолжает ожидать следующих сообщений

**Пример:**
```bash
./tdtpcli -config config.postgres.yaml --import-broker
```

**Вывод:**
```
📁 Using config: config.postgres.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.15)

📡 Connecting to rabbitmq broker...
✅ Connected to broker

🎧 Listening for messages on queue 'tdtp_queue'...
   Press Ctrl+C to stop

📦 Received reference packet for table 'users' (100 rows)
   Type: REFERENCE - full sync via temp table
📋 Import to temporary table: users_tmp_20251116_204210
✅ Data loaded to temporary table
🔄 Replacing production table: users
✅ Production table replaced successfully
   ✓ Message acknowledged and removed from queue
✅ Imported 100 rows into table 'users' (total: 1 packets, 100 rows)

🎧 Waiting for next message...
```

**Остановка:**
- Нажмите `Ctrl+C` для корректного завершения

---

## Фильтрация данных (TDTQL)

### Параметры фильтрации

| Параметр | Описание | Пример |
|----------|----------|--------|
| `--where` | Условие фильтрации | `--where "age > 25"` |
| `--order-by` | Сортировка | `--order-by "balance DESC"` |
| `--limit` | Лимит записей | `--limit 100` |
| `--offset` | Пропустить записей | `--offset 50` |

### Операторы WHERE

**Числовые сравнения:**
```bash
--where "age > 25"
--where "balance >= 1000.50"
--where "quantity < 10"
--where "price <= 99.99"
```

**Текстовые совпадения:**
```bash
--where "username = 'admin'"
--where "status != 'deleted'"
```

**Boolean:**
```bash
--where "is_active = 1"
--where "is_verified = 0"
```

### Сортировка

**Одиночная:**
```bash
--order-by "created_at DESC"
--order-by "username ASC"
```

**Множественная:**
```bash
--order-by "balance DESC, age ASC"
--order-by "city ASC, created_at DESC"
```

### Пагинация

**Первые 100 записей:**
```bash
--limit 100
```

**Записи 51-100 (пропустить первые 50):**
```bash
--limit 50 --offset 50
```

### Комбинированные запросы

**Фильтр + Сортировка + Лимит:**
```bash
./tdtpcli -config config.postgres.yaml --export users \
  --where "balance >= 5000" \
  --order-by "balance DESC" \
  --limit 20
```

**Пагинация + Фильтр:**
```bash
./tdtpcli -config config.postgres.yaml --export orders \
  --where "status = 'completed'" \
  --order-by "order_date DESC" \
  --limit 50 --offset 100
```

### Фильтрация при экспорте в broker

```bash
./tdtpcli -config config.postgres.yaml --export-broker users \
  --where "is_active = 1" \
  --limit 1000
```

---

## Работа с Message Brokers

### RabbitMQ

**Настройка конфигурации:**
```yaml
broker:
  type: rabbitmq
  host: localhost
  port: 5672
  user: guest
  password: guest
  queue: tdtp_queue
  vhost: /
  durable: true
  auto_delete: false
  exclusive: false
```

**Параметры очереди:**
- `durable: true` - очередь сохраняется при перезапуске RabbitMQ
- `auto_delete: false` - очередь не удаляется автоматически
- `exclusive: false` - очередь доступна для нескольких подключений

**Типичный workflow:**

1. **Система A** - экспорт данных:
```bash
./tdtpcli -config config.postgres.yaml --export-broker users --where "updated_at >= '2025-11-16'"
```

2. **Система B** - импорт данных:
```bash
./tdtpcli -config config.sqlite.yaml --import-broker
```

### MSMQ (Windows)

**Настройка конфигурации:**
```yaml
broker:
  type: msmq
  queue: .\\private$\\tdtp_queue
```

**Особенности:**
- Работает только на Windows
- Использует локальные или сетевые очереди MSMQ
- Поддерживает транзакционные очереди

**Пример:**
```bash
tdtpcli.exe -config config.mssql.yaml --export-broker users
```

---

## Примеры использования

### Пример 1: Синхронизация справочников между PostgreSQL и SQLite

**Задача:** Синхронизировать справочник пользователей из PostgreSQL в SQLite.

**Шаг 1:** Экспорт из PostgreSQL в файл
```bash
./tdtpcli -config config.postgres.yaml --export users --output users.tdtp.xml
```

**Шаг 2:** Импорт в SQLite
```bash
./tdtpcli -config config.sqlite.yaml --import users.tdtp.xml
```

### Пример 2: Выборочный экспорт активных пользователей

**Задача:** Экспортировать только активных пользователей с балансом > 1000.

```bash
./tdtpcli -config config.postgres.yaml --export users \
  --where "is_active = 1" \
  --where "balance > 1000" \
  --order-by "balance DESC" \
  --output active_users.tdtp.xml
```

**Примечание:** Текущая версия CLI поддерживает один `--where` параметр. Для сложных запросов используйте SQL-like синтаксис или модифицируйте код CLI.

### Пример 3: Репликация через RabbitMQ

**Задача:** Непрерывная репликация заказов из MS SQL в PostgreSQL через RabbitMQ.

**Терминал 1 (MS SQL - Publisher):**
```bash
# Экспорт новых заказов каждые 5 минут (через cron/scheduled task)
./tdtpcli -config config.mssql.yaml --export-broker orders \
  --where "created_at >= '2025-11-16 12:00:00'"
```

**Терминал 2 (PostgreSQL - Subscriber):**
```bash
# Непрерывное ожидание сообщений
./tdtpcli -config config.postgres.yaml --import-broker
```

### Пример 4: Топ-20 клиентов по балансу

**Задача:** Получить топ-20 клиентов с максимальным балансом.

```bash
./tdtpcli -config config.postgres.yaml --export customers \
  --order-by "balance DESC" \
  --limit 20 \
  --output top_customers.tdtp.xml
```

### Пример 5: Пагинация больших таблиц

**Задача:** Экспортировать таблицу с миллионом записей порциями по 10000.

```bash
# Первая порция (0-9999)
./tdtpcli -config config.postgres.yaml --export large_table \
  --limit 10000 --offset 0 --output part_01.tdtp.xml

# Вторая порция (10000-19999)
./tdtpcli -config config.postgres.yaml --export large_table \
  --limit 10000 --offset 10000 --output part_02.tdtp.xml

# И так далее...
```

### Пример 6: Экспорт в stdout и обработка

**Задача:** Экспортировать данные и сразу обработать через pipe.

```bash
./tdtpcli -config config.postgres.yaml --export users | \
  grep "balance" | \
  wc -l
```

---

## Устранение неполадок

### Проблема: "Database connection failed"

**Симптомы:**
```
❌ Error connecting to database: connection refused
```

**Решение:**
1. Проверьте, что БД запущена:
   ```bash
   # PostgreSQL
   sudo systemctl status postgresql

   # MS SQL (Docker)
   docker ps | grep mssql
   ```

2. Проверьте параметры подключения в config.yaml
3. Проверьте firewall и доступность порта:
   ```bash
   telnet localhost 5432
   ```

### Проблема: "Table not found"

**Симптомы:**
```
❌ Table 'users' does not exist
```

**Решение:**
1. Проверьте список таблиц:
   ```bash
   ./tdtpcli -config config.yaml --list
   ```

2. Для PostgreSQL проверьте схему:
   ```yaml
   database:
     schema: public  # или другая схема
   ```

### Проблема: "Permission denied"

**Симптомы:**
```
❌ Error: permission denied for table users
```

**Решение:**
1. Проверьте права пользователя БД
2. Для PostgreSQL:
   ```sql
   GRANT SELECT, INSERT, UPDATE ON TABLE users TO tdtp_user;
   ```

### Проблема: "Broker connection failed"

**Симптомы:**
```
❌ Failed to connect to broker: dial tcp: connection refused
```

**Решение:**
1. Проверьте, что RabbitMQ запущен:
   ```bash
   sudo systemctl status rabbitmq-server
   ```

2. Проверьте параметры подключения:
   ```yaml
   broker:
     host: localhost  # правильный хост?
     port: 5672       # правильный порт?
   ```

3. Проверьте учетные данные:
   ```bash
   # RabbitMQ default: guest/guest (только для localhost)
   ```

### Проблема: "Packet too large"

**Симптомы:**
```
⚠️ Warning: Packet size exceeds recommended limit
```

**Решение:**
1. Используйте фильтрацию для уменьшения размера:
   ```bash
   --limit 1000
   ```

2. Модифицируйте `MaxMessageSize` в коде:
   ```go
   generator.SetMaxMessageSize(5000000) // 5MB
   ```

### Проблема: "Invalid TDTP format"

**Симптомы:**
```
❌ Failed to parse TDTP file: invalid XML
```

**Решение:**
1. Проверьте, что файл является валидным XML:
   ```bash
   xmllint --noout users.tdtp.xml
   ```

2. Убедитесь, что файл не поврежден
3. Проверьте, что файл создан tdtpcli, а не вручную

---

## Дополнительные ресурсы

- **[SPECIFICATION.md](SPECIFICATION.md)** - полная спецификация TDTP v1.0
- **[MODULES.md](MODULES.md)** - описание модулей фреймворка
- **[PACKET_MODULE.md](PACKET_MODULE.md)** - API для работы с пакетами
- **[SCHEMA_MODULE.md](SCHEMA_MODULE.md)** - валидация и типы данных
- **[TDTQL_TRANSLATOR.md](TDTQL_TRANSLATOR.md)** - язык запросов TDTQL

---

## Обратная связь

Нашли баг или хотите предложить улучшение?

- **GitHub Issues:** https://github.com/ruslano69/tdtp-framework/issues
- **Email:** ruslano69@gmail.com

---

*Последнее обновление: 16.11.2025*
