# Example 02 - v1.0.0

## Интеграционный тест MSSQL + RabbitMQ

Тестирует все типы данных MSSQL, фильтры TDTQL, маскирование, RabbitMQ.

## Быстрый старт

```bash
# 1. Запустить контейнеры
docker-compose up -d

# 2. Подождать запуска (важно!)
sleep 40

# 3. Инициализировать БД вручную
docker exec -it example02-mssql /opt/mssql-tools18/bin/sqlcmd -S localhost -U sa -P "Tdtp_Pass_123!" -i /init.sql -C

# 4. Проверить что БД создана
docker exec -it example02-mssql /opt/mssql-tools18/bin/sqlcmd -S localhost -U sa -P "Tdtp_Pass_123!" -Q "USE example02_db; SELECT COUNT(*) FROM orders" -C

# 5. Запустить тесты
go run main.go

# 6. Остановить
docker-compose down
```

## Что тестируется

- ✅ Все типы данных MSSQL (INT, BIGINT, DECIMAL, FLOAT, VARCHAR, NVARCHAR, TEXT, DATE, DATETIME, DATETIME2, DATETIMEOFFSET, BIT, VARBINARY)
- ✅ NULL значения
- ✅ Unicode данные (кириллица, китайский)
- ✅ Специальные символы
- ✅ Экстремальные значения
- ✅ Фильтрация через TDTQL
- ✅ SQL -> TDTQL -> SQL трансляция
- ✅ Маскирование PII (email, phone, card, ssn)
- ✅ Отправка в RabbitMQ
- ✅ Audit logging

## Конфигурация

**MSSQL:**
- Host: localhost:1433
- User: sa
- Password: Tdtp_Pass_123!
- Database: example02_db

**RabbitMQ:**
- AMQP: localhost:5672
- Management UI: http://localhost:15672
- User: tdtp
- Password: tdtp_pass_123
- Queue: tdtp-orders

## Результаты

- `./output/test1-all-data.xml` - все записи
- `./output/test2-paid-orders.xml` - оплаченные заказы
- `./output/test3-complex-filter.xml` - сложный фильтр
- `./output/test4-masked-data.xml` - маскированные данные
- `./logs/example02.log` - audit trail
