# TDTP Framework - Scripts

## create_test_db.py

Python скрипт для создания тестовой SQLite базы данных.

### Зачем?

- **Нет внешних зависимостей** - Python sqlite3 встроен
- **Быстрое создание БД** - одна команда
- **Готовые тестовые данные** - 3 таблицы с 28 записями
- **Для integration тестов** - используется в Go тестах

### Использование

```bash
# Создание БД в testdata/test.db
python3 scripts/create_test_db.py

# Создание в произвольном месте
python3 scripts/create_test_db.py /path/to/mytest.db
```

### Что создается?

**Таблица Users (10 записей):**
- ID, Name, Email, Balance, IsActive, City, CreatedAt, LastLoginAt

**Таблица Orders (8 записей):**
- OrderID, UserID, ProductName, Amount, Status, CreatedAt

**Таблица Products (10 записей):**
- ProductID, Name, Category, Price, Stock, IsAvailable, UpdatedAt

### Примеры SQL запросов

Скрипт показывает готовые SQL запросы для тестирования:

```sql
-- Активные пользователи с балансом > 1000
SELECT * FROM Users WHERE IsActive = 1 AND Balance > 1000

-- Пользователи из Москвы или СПб
SELECT * FROM Users WHERE City IN ('Moscow', 'SPb')

-- Топ 3 по балансу
SELECT * FROM Users ORDER BY Balance DESC LIMIT 3

-- Ожидающие заказы
SELECT * FROM Orders WHERE Status = 'pending'

-- Обновленные продукты
SELECT * FROM Products WHERE UpdatedAt > '2025-11-01'
```

### Использование с TDTP

#### 1. Integration тесты

```bash
# Создать БД
python3 scripts/create_test_db.py

# Установить SQLite драйвер
go get modernc.org/sqlite

# Запустить тесты
cd pkg/adapters/sqlite
go test -v
```

#### 2. Live Demo

```bash
# Создать БД
python3 scripts/create_test_db.py

# Запустить demo
cd examples/live_demo
go run main.go
```

#### 3. Query Integration

```bash
cd examples/query_integration
go run main.go
```

### Структура БД

```
test.db (16 KB)
├── Users (10 records)
│   ├── Active: 7
│   ├── Inactive: 3
│   └── Cities: Moscow (5), SPb (3), Kazan (2)
├── Orders (8 records)
│   ├── Completed: 4
│   ├── Pending: 3
│   └── Cancelled: 1
└── Products (10 records)
    ├── Electronics: 4
    ├── Accessories: 3
    └── Audio: 3
```

### Требования

- Python 3.6+
- sqlite3 module (встроен в Python)
- Никаких дополнительных зависимостей!

### Пример вывода

```
╔══════════════════════════════════════════════════════════════╗
║       TDTP Framework - Test Database Generator              ║
║                   SQLite + Python                            ║
╚══════════════════════════════════════════════════════════════╝

Creating test database: testdata/test.db
Creating table: Users
Inserting test data...
Creating table: Orders
Creating table: Products

============================================================
Database created successfully!
============================================================
Users: 10 records
Orders: 8 records
Products: 10 records
============================================================

Database file: /path/to/testdata/test.db
Size: 16384 bytes

Ready for TDTP integration tests!

...
```

### Расширение

Легко добавить свои таблицы и данные:

```python
# В create_test_database():

cursor.execute("""
    CREATE TABLE MyTable (
        ID INTEGER PRIMARY KEY,
        Name TEXT,
        ...
    )
""")

my_data = [
    (1, "Value1", ...),
    (2, "Value2", ...),
]

cursor.executemany("""
    INSERT INTO MyTable (ID, Name, ...)
    VALUES (?, ?, ...)
""", my_data)
```

### Troubleshooting

**"No such file or directory: testdata/"**
```bash
mkdir -p testdata
python3 scripts/create_test_db.py
```

**"Database is locked"**
```bash
# Закройте все подключения и удалите
rm testdata/test.db
python3 scripts/create_test_db.py
```

---

*Версия: 1.0*
*Совместимость: TDTP v0.6+*
