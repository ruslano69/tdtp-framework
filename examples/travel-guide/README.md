# Travel Guide Database Example

Туристический справочник городов для тестирования MS SQL Server коннектора в tdtp-xray.

## Структура данных

### Таблица `cities`

| Поле | Тип | Описание |
|------|-----|----------|
| city_id | INT PRIMARY KEY | ID города (автоинкремент) |
| name | NVARCHAR(100) | Название города |
| country | NVARCHAR(100) | Страна |
| latitude | DECIMAL(9,6) | Широта |
| longitude | DECIMAL(9,6) | Долгота |
| population | INT | Население |
| timezone | VARCHAR(50) | Часовой пояс |
| attractions | NVARCHAR(MAX) | JSON с достопримечательностями |
| created_at | DATETIME2 | Дата создания записи |

### JSON структура `attractions`

```json
[
  {
    "name": "Eiffel Tower",
    "price_eur": 26.80,
    "rating": 4.6
  },
  {
    "name": "Louvre Museum",
    "price_eur": 17.00,
    "rating": 4.7
  }
]
```

## Установка

### 1. Запустить MS SQL Server в Docker

```powershell
# Используйте существующие контейнеры из tests/integration/docker-compose.yml
cd tests/integration
docker-compose up -d mssql

# Или проверьте что контейнер запущен
docker ps | findstr mssql
```

### 2. Создать БД и таблицу

```powershell
# Выполнить SQL скрипт (пароль из docker-compose.yml)
sqlcmd -S localhost,1433 -U sa -P "YourStrong!Passw0rd" -i setup_database.sql
```

### 3. Установить Python зависимости

```powershell
pip install pyodbc
```

**Важно**: Установить ODBC Driver 17 for SQL Server:
https://learn.microsoft.com/en-us/sql/connect/odbc/download-odbc-driver-for-sql-server

### 4. Загрузить данные

```powershell
# Отредактировать populate_data.py (поменять пароль если нужно)
python populate_data.py
```

## Использование в tdtp-xray

### Настройки подключения

- **Server**: `localhost,1433`
- **Database**: `TravelGuide`
- **Username**: `sa`
- **Password**: `YourStrong@Passw0rd`
- **Table**: `cities`

### Примеры запросов

```sql
-- Все города
SELECT * FROM cities

-- Города с населением > 5 млн
SELECT name, country, population
FROM cities
WHERE population > 5000000
ORDER BY population DESC

-- Города Европы
SELECT name, country, population, timezone
FROM cities
WHERE country IN ('France', 'United Kingdom', 'Spain', 'Russia')

-- Парсинг JSON достопримечательностей
SELECT
    name as city,
    country,
    JSON_VALUE(attractions, '$[0].name') as top_attraction,
    JSON_VALUE(attractions, '$[0].price_eur') as price
FROM cities

-- Бесплатные достопримечательности (требует JSON функции)
SELECT
    name,
    country,
    attractions
FROM cities
WHERE attractions LIKE '%"price_eur": 0.00%'
```

## Тестовые данные

База содержит 10 городов из разных стран:
- 🇫🇷 Paris (France)
- 🇯🇵 Tokyo (Japan)
- 🇺🇸 New York (USA)
- 🇬🇧 London (United Kingdom)
- 🇦🇪 Dubai (UAE)
- 🇦🇺 Sydney (Australia)
- 🇷🇺 Moscow (Russia)
- 🇪🇸 Barcelona (Spain)
- 🇸🇬 Singapore (Singapore)
- 🇧🇷 Rio de Janeiro (Brazil)

Каждый город содержит 5 достопримечательностей с ценами посещения и рейтингами.

## Очистка

```sql
-- Удалить все данные
USE TravelGuide;
TRUNCATE TABLE cities;

-- Удалить БД
USE master;
DROP DATABASE TravelGuide;
```

```powershell
# Остановить контейнер
docker stop tdtp-mssql

# Удалить контейнер
docker rm tdtp-mssql
```
