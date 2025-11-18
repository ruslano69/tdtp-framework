# Example 01: Basic Export

## Описание

Этот пример демонстрирует базовый сценарий экспорта данных из PostgreSQL в TDTP XML файл.

## Что делает пример

1. Подключается к PostgreSQL в Docker контейнере
2. Проверяет существование таблицы `users`
3. Получает схему таблицы
4. Экспортирует все записи в TDTP пакеты
5. Сохраняет пакеты в XML файл

## Структура файлов

```
01-basic-export/
├── docker-compose.yml   # Docker конфигурация PostgreSQL
├── init.sql             # SQL скрипт создания таблицы и данных
├── main.go              # Основной код примера
└── README.md            # Этот файл
```

## Требования

- Docker и Docker Compose
- Go 1.22+

## Быстрый старт

```bash
# 1. Запустить PostgreSQL в Docker
docker-compose up -d

# 2. Подождать инициализацию (10 секунд)
sleep 10

# 3. Запустить пример
go run main.go

# 4. Проверить результат
cat ./output/users.tdtp.xml

# 5. Остановить контейнеры (когда закончите)
docker-compose down
```

## Конфигурация

### PostgreSQL (docker-compose.yml)
- **Host:** localhost
- **Port:** 5432
- **User:** tdtp_user
- **Password:** tdtp_pass_123
- **Database:** example01_db

### Таблица users
- **id** (INTEGER, PK) - ID пользователя
- **name** (TEXT) - Имя пользователя
- **email** (TEXT) - Email
- **created_at** (TIMESTAMP) - Дата создания
- **is_active** (BOOLEAN) - Активен ли

### Тестовые данные
В таблице 5 записей пользователей (см. init.sql).

## Результат

После запуска создается файл `./output/users.tdtp.xml` со всеми пользователями в формате TDTP.

Пример структуры пакета:

```xml
<?xml version="1.0" encoding="utf-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>reference</Type>
    <TableName>users</TableName>
    <MessageID>REF-...</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>5</RecordsInPart>
    <Timestamp>2025-11-18T20:30:00Z</Timestamp>
  </Header>
  <Schema>
    <Field name="id" type="INTEGER" key="true"/>
    <Field name="name" type="TEXT" length="100"/>
    <Field name="email" type="TEXT" length="255"/>
    <Field name="created_at" type="TIMESTAMP"/>
    <Field name="is_active" type="BOOLEAN"/>
  </Schema>
  <Data>
    <R>1|John Doe|john.doe@example.com|2024-01-15T10:30:00Z|true</R>
    <R>2|Jane Smith|jane.smith@example.com|2024-01-15T11:00:00Z|true</R>
    ...
  </Data>
</DataPacket>
```

## Проверка работы

```bash
# Проверить что PostgreSQL запустился
docker ps

# Проверить логи PostgreSQL
docker-compose logs postgres

# Подключиться к БД вручную
docker exec -it example01-postgres psql -U tdtp_user -d example01_db

# В psql:
SELECT * FROM users;
\q

# Проверить созданный XML
ls -lh ./output/
cat ./output/users.tdtp.xml
```

## Troubleshooting

### Ошибка: "connection refused"
PostgreSQL еще не запустился. Подождите 10-15 секунд после `docker-compose up`.

### Ошибка: "Table 'users' does not exist"
Проблема с инициализацией. Пересоздайте контейнеры:
```bash
docker-compose down -v
docker-compose up -d
sleep 15
go run main.go
```

### Ошибка: "port 5432 is already allocated"
На вашей системе уже запущен PostgreSQL. Варианты:
1. Остановите локальный PostgreSQL
2. Измените порт в docker-compose.yml (например, "5433:5432")

## Модификация примера

### Изменить количество записей
Отредактируйте `init.sql`, добавьте больше INSERT операций.

### Экспортировать с фильтром
```go
// Вместо ExportTable используйте ExportTableWithQuery
query, _ := tdtql.NewTranslator().Translate("SELECT * FROM users WHERE is_active = true")
packets, err := adapter.ExportTableWithQuery(ctx, "users", query, "sender", "recipient")
```

### Изменить формат вывода
```go
// Без отступов (компактный XML)
xmlData, err := gen.ToXML(pkt, false)
```

## Следующие шаги

После этого примера переходите к:
- **Example 02:** Импорт данных и работа с RabbitMQ
- **Example 03:** Инкрементальная синхронизация
