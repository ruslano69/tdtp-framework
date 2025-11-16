# PostgreSQL Testing Guide for TDTP Framework

## 🎯 Цель

Протестировать TDTP Framework с PostgreSQL, включая специфичные типы данных:
- UUID
- JSONB
- ARRAY (TEXT[])
- NUMERIC с precision/scale
- TIMESTAMP WITH TIME ZONE
- SERIAL/BIGSERIAL
- INET

## 📋 Предварительные требования

1. **Docker Desktop** установлен и запущен
2. **Python 3.6+** с библиотекой `psycopg2`
3. **Go 1.22+** для сборки CLI

## 🚀 Шаг 1: Запуск PostgreSQL в Docker

```bash
# Перейти в директорию проекта
cd tdtp-framework

# Запустить PostgreSQL контейнер
docker-compose up -d postgres

# Проверить статус
docker-compose ps

# Проверить логи (опционально)
docker-compose logs postgres
```

**Ожидаемый вывод:**
```
✅ Container tdtp-postgres  Running
```

**Параметры подключения:**
- Host: `localhost`
- Port: `5432`
- User: `tdtp_user`
- Password: `tdtp_dev_pass_2025`
- Database: `tdtp_test`

## 🐍 Шаг 2: Установка psycopg2

```bash
# Windows
pip install psycopg2

# Или если есть проблемы с компиляцией
pip install psycopg2-binary

# Linux/macOS
pip3 install psycopg2-binary
```

## 📊 Шаг 3: Создание тестовой базы данных

```bash
# Запустить Python скрипт
cd scripts
python create_postgres_test_db.py
```

**Ожидаемый вывод:**
```
============================================================
🐘 PostgreSQL Test Database Creator for TDTP Framework
============================================================

🔌 Connecting to PostgreSQL...
✅ Connected successfully
   Host: localhost
   Port: 5432
   Database: tdtp_test
   User: tdtp_user

📋 Creating table: users...
📋 Creating table: orders...
📋 Creating table: products...
📋 Creating table: activity_logs...
✅ Tables created successfully

📊 Generating test data...
   Users: 100
   Products: 50
   Orders: 200

👥 Inserting users...
📦 Inserting products...
🛒 Inserting orders...
📝 Inserting activity logs...
✅ Test data inserted successfully

📊 Database Statistics:
============================================================
  users                | Rows:    100 | Size: 88 kB
  products             | Rows:     50 | Size: 64 kB
  orders               | Rows:    200 | Size: 120 kB
  activity_logs        | Rows:    600 | Size: 160 kB
============================================================

🔍 Sample Data (PostgreSQL specific types):
------------------------------------------------------------

📌 Users (with UUID):
   UUID: a1b2c3d4-e5f6-7890-abcd-ef1234567890 | user_1 | user1@example.com
   ...

📌 Users (with JSONB metadata):
   user_1: {"preferences": {"theme": "dark", "language": "en"}, ...}
   ...

📌 Orders (with TEXT[] tags):
   ORD-10001: {urgent,gift}
   ...

📌 Users (with NUMERIC balance):
   user_42: $9876.54
   ...

============================================================
✅ PostgreSQL test database created successfully!
============================================================
```

## 🔧 Шаг 4: Настройка TDTP CLI

```bash
# Вернуться в корень проекта
cd ..

# Собрать CLI (если еще не собран)
go build -o tdtpcli.exe ./cmd/tdtpcli

# Создать конфиг для PostgreSQL
.\tdtpcli.exe --create-config-pg

# Переименовать
move default_config.yaml config.yaml
```

**config.yaml должен содержать:**
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  user: tdtp_user
  password: tdtp_dev_pass_2025
  dbname: tdtp_test
  schema: public
  sslmode: disable
```

## ✅ Шаг 5: Тестирование экспорта

### Тест 1: Список таблиц
```bash
.\tdtpcli.exe --list
```

**Ожидаемый вывод:**
```
📁 Using config: config.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.x)

📊 Database: postgres (PostgreSQL 15.x)
📋 Tables (4):
  • users
  • products
  • orders
  • activity_logs
```

### Тест 2: Экспорт таблицы с UUID
```bash
.\tdtpcli.exe --export users --output users.tdtp.xml
```

**Проверить:**
- ✅ Создан файл `users.tdtp.xml`
- ✅ UUID экспортированы как строки
- ✅ JSONB экспортирован как TEXT
- ✅ TIMESTAMP WITH TIME ZONE в ISO формате

**Посмотреть содержимое:**
```bash
type users.tdtp.xml | more
```

**Ожидаемая структура Schema:**
```xml
<Schema>
  <Field name="id" type="TEXT" subtype="uuid"/>
  <Field name="username" type="TEXT" length="100"/>
  <Field name="email" type="TEXT" length="255"/>
  <Field name="age" type="INTEGER" subtype="smallint"/>
  <Field name="balance" type="DECIMAL" precision="12" scale="2"/>
  <Field name="is_active" type="BOOLEAN"/>
  <Field name="metadata" type="TEXT" subtype="jsonb"/>
  <Field name="created_at" type="TIMESTAMP" timezone="true"/>
  <Field name="updated_at" type="TIMESTAMP" timezone="true"/>
</Schema>
```

### Тест 3: Экспорт таблицы с ARRAY
```bash
.\tdtpcli.exe --export orders --output orders.tdtp.xml
```

**Проверить:**
- ✅ TEXT[] массивы экспортированы
- ✅ JSONB items экспортирован
- ✅ SERIAL id экспортирован как INTEGER

### Тест 4: Экспорт таблицы с BIGSERIAL
```bash
.\tdtpcli.exe --export activity_logs --output logs.tdtp.xml
```

**Проверить:**
- ✅ BIGSERIAL экспортирован как INTEGER
- ✅ INET адреса экспортированы как TEXT

## 🔄 Шаг 6: Тестирование импорта

### Тест 5: Импорт через временную таблицу

```bash
# Импортировать обратно
.\tdtpcli.exe --import users.tdtp.xml
```

**Ожидаемый вывод:**
```
📁 Using config: config.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.x)

📥 Importing from: users.tdtp.xml
📋 Target table: users
📊 Records in packet: 100

📋 Import to temporary table: users_tmp_20251116_150000
✅ Data loaded to temporary table
🔄 Replacing production table: users
✅ Production table replaced successfully

✅ Imported 100 rows into 'users'
```

### Тест 6: Проверка данных после импорта

**Подключиться к PostgreSQL:**
```bash
docker exec -it tdtp-postgres psql -U tdtp_user -d tdtp_test
```

**Проверить данные:**
```sql
-- Проверить количество записей
SELECT COUNT(*) FROM users;

-- Проверить UUID
SELECT id, username FROM users LIMIT 5;

-- Проверить JSONB
SELECT username, metadata->'preferences'->>'theme' as theme 
FROM users 
WHERE metadata IS NOT NULL 
LIMIT 5;

-- Проверить NUMERIC
SELECT username, balance 
FROM users 
ORDER BY balance DESC 
LIMIT 5;

-- Выход
\q
```

## 🧪 Шаг 7: Специальные тесты PostgreSQL

### Тест 7: Экспорт с фильтрацией (будущая функция)

Когда будет реализована поддержка TDTQL через CLI:
```bash
# Экспорт только активных пользователей с балансом > 1000
.\tdtpcli.exe --export users --query "SELECT * FROM users WHERE is_active = true AND balance > 1000" --output rich_users.tdtp.xml
```

### Тест 8: Проверка типов после импорта

```sql
-- Подключиться к БД
docker exec -it tdtp-postgres psql -U tdtp_user -d tdtp_test

-- Проверить типы колонок
\d users

-- Ожидаемый вывод:
-- id           | uuid
-- username     | character varying(100)
-- email        | character varying(255)
-- age          | smallint
-- balance      | numeric(12,2)
-- is_active    | boolean
-- metadata     | jsonb
-- created_at   | timestamp with time zone
-- updated_at   | timestamp with time zone
```

## 📊 Шаг 8: Производительность

### Бенчмарк экспорта больших таблиц

```bash
# Экспорт таблицы логов (600+ записей)
time .\tdtpcli.exe --export activity_logs --output logs.tdtp.xml
```

**Замерить:**
- Время выполнения
- Размер файла
- Использование памяти

## 🧹 Очистка после тестирования

```bash
# Остановить контейнер
docker-compose down

# Или остановить с удалением данных
docker-compose down -v
```

## 🐛 Troubleshooting

### Проблема 1: Не удается подключиться к PostgreSQL

**Ошибка:**
```
❌ Failed to connect: connection refused
```

**Решение:**
```bash
# Проверить запущен ли контейнер
docker-compose ps

# Перезапустить
docker-compose restart postgres

# Проверить логи
docker-compose logs postgres
```

### Проблема 2: Ошибка прав доступа

**Ошибка:**
```
FATAL: password authentication failed for user "tdtp_user"
```

**Решение:**
- Проверить пароль в `config.yaml`
- Пароль должен быть: `tdtp_dev_pass_2025`

### Проблема 3: psycopg2 не установлен

**Ошибка:**
```
ModuleNotFoundError: No module named 'psycopg2'
```

**Решение:**
```bash
pip install psycopg2-binary
```

## ✅ Контрольный список тестирования

- [ ] PostgreSQL контейнер запущен
- [ ] Тестовая БД создана Python скриптом
- [ ] 4 таблицы созданы (users, products, orders, activity_logs)
- [ ] Данные загружены (100 users, 50 products, 200 orders, 600 logs)
- [ ] CLI конфиг создан
- [ ] `--list` показывает все таблицы
- [ ] Экспорт users с UUID работает
- [ ] Экспорт orders с ARRAY работает
- [ ] Экспорт activity_logs с BIGSERIAL работает
- [ ] Импорт создает временную таблицу
- [ ] Импорт заменяет продакшен таблицу
- [ ] Данные после импорта корректны
- [ ] Типы PostgreSQL сохранены

## 🎯 Итог

После выполнения всех тестов должно быть подтверждено:

✅ **Поддержка специфичных типов PostgreSQL:**
- UUID
- JSONB
- TEXT[]
- NUMERIC(p,s)
- TIMESTAMP WITH TIME ZONE
- SERIAL/BIGSERIAL
- INET

✅ **Безопасный импорт:**
- Временные таблицы работают
- Атомарная замена работает
- Откат при ошибках работает

✅ **Производительность:**
- Экспорт быстрый
- Импорт быстрый
- Память не переполняется

---

**Готово к production использованию с PostgreSQL! 🚀**
