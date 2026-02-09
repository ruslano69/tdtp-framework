# Database Views Setup Scripts

Эти скрипты создают тестовые database views для демонстрации команды `tdtpcli --list-views`.

## 📁 Файлы

- **`setup-views-sqlite.sql`** - SQL скрипт для SQLite
- **`setup-views-postgres.sql`** - SQL скрипт для PostgreSQL
- **`setup-views-mysql.sql`** - SQL скрипт для MySQL
- **`setup-views-mssql.sql`** - SQL скрипт для MS SQL Server
- **`setup-views.sh`** - Bash helper script для всех БД

## 🎯 Типы Views

Каждый скрипт создает два типа views:

### ✅ Updatable Views (U* prefix)
Views, в которые можно делать `INSERT/UPDATE/DELETE`:
- `users_editable` - простой SELECT
- `users_active` - с WHERE фильтром и CHECK OPTION
- `users_recent` - последние 30 дней

### ❌ Read-Only Views (R* prefix)
Views только для чтения (с aggregates, DISTINCT, window functions):
- `users_summary` - статистика с COUNT/AVG/MIN/MAX
- `users_readonly` - с DISTINCT
- `users_with_stats` - с window functions

## 🚀 Использование

### SQLite

```bash
# Применить к вашим базам данных
./scripts/setup-views.sh sqlite test_data.db
./scripts/setup-views.sh sqlite benchmark_100k.db

# Или напрямую через sqlite3
sqlite3 test_data.db < scripts/setup-views-sqlite.sql
```

### PostgreSQL

```bash
# С помощью helper script
./scripts/setup-views.sh postgres localhost 5432 postgres testdb

# Или напрямую через psql
psql -h localhost -p 5432 -U postgres -d testdb -f scripts/setup-views-postgres.sql

# Через Docker
docker exec -i postgres_container psql -U postgres -d testdb < scripts/setup-views-postgres.sql
```

### MySQL

```bash
# С помощью helper script
./scripts/setup-views.sh mysql localhost 3306 root testdb

# Или напрямую через mysql
mysql -h localhost -P 3306 -u root -p testdb < scripts/setup-views-mysql.sql

# Через Docker
docker exec -i mysql_container mysql -u root -ptestpass testdb < scripts/setup-views-mysql.sql
```

### MS SQL Server

```bash
# С помощью helper script
./scripts/setup-views.sh mssql localhost 1433 sa testdb

# Или напрямую через sqlcmd
sqlcmd -S localhost,1433 -U sa -P YourPassword -d testdb -i scripts/setup-views-mssql.sql

# Через Docker
docker exec -i mssql_container /opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P YourPassword -d testdb -i /scripts/setup-views-mssql.sql
```

## ✅ Проверка результата

После применения скриптов, проверьте результат:

```bash
# Список всех views
tdtpcli --config sqlite_config.yaml --list-views
```

Ожидаемый вывод:
```
Found 6 view(s):
  1. U*users_active (updatable)
  2. U*users_editable (updatable)
  3. U*users_recent (updatable)
  4. R*users_readonly (read-only)
  5. R*users_summary (read-only)
  6. R*users_with_stats (read-only)

Legend:
  U* = Updatable view (can import)
  R* = Read-only view (export only)
```

## 🔍 Как работает определение updatable views

### SQLite
- Views по умолчанию read-only
- Для updatable нужны `INSTEAD OF` триггеры (INSERT/UPDATE/DELETE)
- Скрипт создает триггеры для `users_editable` и `users_copy_editable`

### PostgreSQL
- Автоматически определяет updatable views
- Критерии: простой SELECT из одной таблицы, без aggregates/DISTINCT/window functions
- `WITH CHECK OPTION` гарантирует что INSERT/UPDATE соответствуют WHERE условию

### MySQL
- Автоматически определяет updatable views
- Критерии: простой SELECT, без GROUP BY/HAVING/DISTINCT/aggregates/UNION
- Проверяется через `information_schema.views.is_updatable = 'YES'`

### MS SQL Server
- Автоматически определяет updatable views
- Критерии: простой SELECT, без TOP/GROUP BY/DISTINCT/aggregates
- Можно сделать сложный view updatable через `INSTEAD OF` триггеры

## 📝 Примеры использования views

### Экспорт из view
```bash
# Экспорт из read-only view (всегда работает)
tdtpcli --export users_summary --output summary.xml

# Экспорт из updatable view (всегда работает)
tdtpcli --export users_editable --output users.xml

# Экспорт с фильтром
tdtpcli --export users_active --where "created_at > '2024-01-01'" --output recent.xml
```

### Импорт в view
```bash
# Импорт в updatable view (работает только для U* views)
tdtpcli --import users.xml --table users_editable --strategy replace

# Импорт в read-only view - ОШИБКА!
tdtpcli --import data.xml --table users_summary --strategy replace
# Error: Cannot import into read-only view
```

## 🧪 Тестирование

После применения скриптов можно протестировать:

1. **Проверить список views:**
   ```bash
   tdtpcli --list-views
   ```

2. **Экспорт из view:**
   ```bash
   tdtpcli --export users_editable --output test.xml
   ```

3. **Проверить содержимое:**
   ```bash
   cat test.xml
   ```

4. **Импорт обратно (только для updatable views):**
   ```bash
   tdtpcli --import test.xml --table users_editable --strategy replace
   ```

## 🔧 Troubleshooting

### SQLite: View показывается как R* хотя должна быть U*
- Проверьте наличие INSTEAD OF триггеров:
  ```sql
  SELECT name FROM sqlite_master
  WHERE type='trigger' AND tbl_name='your_view_name';
  ```
- Убедитесь что есть все 3 триггера: INSERT, UPDATE, DELETE

### PostgreSQL: View показывается как R* хотя должна быть U*
- Проверьте что view содержит простой SELECT без DISTINCT/aggregates
- Убедитесь что все изменяемые колонки присутствуют в view

### MySQL/MS SQL: View показывается как R*
- Проверьте через `information_schema.views`:
  ```sql
  SELECT table_name, is_updatable
  FROM information_schema.views
  WHERE table_name = 'your_view_name';
  ```

## 📚 Дополнительная информация

Для более подробной информации о работе с views см.:
- [PostgreSQL Views Documentation](https://www.postgresql.org/docs/current/sql-createview.html)
- [MySQL Views Documentation](https://dev.mysql.com/doc/refman/8.0/en/create-view.html)
- [SQLite Views Documentation](https://www.sqlite.org/lang_createview.html)
- [MS SQL Views Documentation](https://learn.microsoft.com/en-us/sql/t-sql/statements/create-view-transact-sql)
