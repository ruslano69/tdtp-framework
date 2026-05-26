# local-pg-etl — ETL Pipeline из PostgreSQL

Пример `--pipeline` команды: два источника PostgreSQL → JOIN в SQLite in-memory → TDTP файл.  
Никаких внешних сервисов — только локальный PostgreSQL.

## Сценарий

Топ-50 клиентов по выручке: данные из таблиц `clients` и `bookings` объединяются  
через SQL JOIN в рабочем пространстве SQLite, результат сохраняется в TDTP XML.

## Запуск

```bash
# Требует PostgreSQL с тестовой БД (см. scripts/create_postgres_test_db.py)
tdtpcli --pipeline pipeline.yaml
# Результат: /tmp/client_revenue_report.tdtp.xml
```

## Структура pipeline.yaml

```
sources:            # два PostgreSQL-источника
  - clients         # SELECT client_id, full_name, phone, email, ...
  - bookings        # SELECT client_id, SUM(total_rub), COUNT(*), ...

workspace:          # SQLite :memory: для JOIN
  type: sqlite

transform:          # JOIN clients + bookings, ORDER BY revenue DESC LIMIT 50
  sql: |
    SELECT c.full_name, b.total_revenue, b.total_bookings ...
    FROM clients c JOIN bookings b ON c.client_id = b.client_id
    ORDER BY CAST(b.total_revenue AS REAL) DESC LIMIT 50

output:
  type: tdtp
  compression: true  # zstd level 3
```

## Тестовая БД

```bash
python3 scripts/create_postgres_test_db.py
# user: tdtp_user / pass: tdtp_dev_pass_2025 / db: tdtp_test
```

## См. также

- [`examples/02b-rabbitmq-mssql-etl`](../02b-rabbitmq-mssql-etl) — ETL через RabbitMQ
- [`examples/09-s3-pipeline-chain`](../09-s3-pipeline-chain) — цепочка pipeline через S3
