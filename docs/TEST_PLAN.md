# План тестирования TDTP Framework - PostgreSQL + Message Brokers

## Цель
Протестировать полный цикл работы с PostgreSQL, экспорт/импорт через файлы, RabbitMQ и MSMQ очереди.

---

## Этап 1: Подготовка окружения

### 1.1 Проверка Docker
```bash
docker --version
docker-compose --version
```

### 1.2 Создание docker-compose.yml
Создать файл с сервисами:
- PostgreSQL (порт 5432)
- RabbitMQ (порты 5672, 15672 - management UI)
- (опционально) ActiveMQ для MSMQ-like очередей

### 1.3 Запуск контейнеров
```bash
docker-compose up -d
docker-compose ps
```

### 1.4 Проверка доступности
```bash
# PostgreSQL
docker exec -it <postgres-container> psql -U postgres -c "SELECT version();"

# RabbitMQ Management UI
# http://localhost:15672 (guest/guest)
```

---

## Этап 2: Настройка PostgreSQL

### 2.1 Создание БД и пользователя
```sql
CREATE DATABASE tdtp_test;
CREATE USER tdtp_user WITH PASSWORD 'tdtp_pass';
GRANT ALL PRIVILEGES ON DATABASE tdtp_test TO tdtp_user;
```

### 2.2 Создание схемы таблицы Users
```sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    gender CHAR(1),
    birth_date DATE,
    email VARCHAR(255),
    phone VARCHAR(20),
    inn VARCHAR(12),
    insurance_policy VARCHAR(20),
    city VARCHAR(100),
    marital_status VARCHAR(20),
    status VARCHAR(20),
    balance DECIMAL(18,2),
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    description TEXT
);
```

### 2.3 Создание config.postgres.yaml
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  database: tdtp_test
  user: tdtp_user
  password: tdtp_pass
  sslmode: disable
export:
  compress: false
  compress_level: 3
resilience:
  circuit_breaker:
    enabled: true
    threshold: 5
    timeout: 60
    max_concurrent: 100
    success_threshold: 2
  retry:
    enabled: true
    max_attempts: 3
    strategy: exponential
    initial_wait_ms: 1000
    max_wait_ms: 30000
    jitter: true
audit:
  enabled: true
  level: standard
  file: audit_postgres.log
  max_size_mb: 100
```

---

## Этап 3: Импорт тестовых данных в PostgreSQL

### 3.1 Проверка наличия TDTP файла с данными
```bash
ls -lh test_data.db
ls -lh Users*.xml 2>/dev/null || echo "Нужен экспорт из SQLite"
```

### 3.2 Экспорт из SQLite (если нет файлов)
```bash
.\tdtpcli --config config.yaml --export Users --output users_full.xml
```

### 3.3 Импорт в PostgreSQL
```bash
.\tdtpcli --config config.postgres.yaml --import users_full.xml --strategy replace
```

### 3.4 Проверка данных
```sql
SELECT COUNT(*) FROM users;
SELECT status, COUNT(*) FROM users GROUP BY status;
SELECT * FROM users LIMIT 5;
```

---

## Этап 4: Тестирование экспорта/импорта файлов

### 4.1 Экспорт всей таблицы
```bash
.\tdtpcli --config config.postgres.yaml --export users --output pg_full.xml
```

**Ожидаемый результат:**
- Файл создан
- Количество записей соответствует COUNT(*) из БД

### 4.2 Экспорт с фильтром
```bash
# Только активные
.\tdtpcli --config config.postgres.yaml --export users --where "status = active" --output pg_active.xml

# По городу
.\tdtpcli --config config.postgres.yaml --export users --where "city = Москва" --output pg_moscow.xml

# С балансом > 300k
.\tdtpcli --config config.postgres.yaml --export users --where "balance > 300000" --output pg_rich.xml
```

**Проверка:**
```bash
# Подсчитать записи в XML
grep -c "<R>" pg_active.xml
```

### 4.3 Экспорт с сжатием
```bash
.\tdtpcli --config config.postgres.yaml --export users --compress --compress-level 5 --output pg_compressed.xml
```

### 4.4 Импорт обратно с заменой
```bash
# Создать копию таблицы
.\tdtpcli --config config.postgres.yaml --import pg_active.xml --table users_active --strategy copy

# Проверить
docker exec -it <postgres-container> psql -U tdtp_user -d tdtp_test -c "SELECT COUNT(*) FROM users_active;"
```

---

## Этап 5: Тестирование RabbitMQ

### 5.1 Создание config.rabbitmq.yaml
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  database: tdtp_test
  user: tdtp_user
  password: tdtp_pass
  sslmode: disable
broker:
  type: rabbitmq
  host: localhost
  port: 5672
  user: guest
  password: guest
  queue: tdtp_test_queue
export:
  compress: true
  compress_level: 3
resilience:
  circuit_breaker:
    enabled: true
    threshold: 5
    timeout: 60
  retry:
    enabled: true
    max_attempts: 3
audit:
  enabled: true
  level: standard
  file: audit_rabbitmq.log
```

### 5.2 Экспорт в RabbitMQ
```bash
.\tdtpcli --config config.rabbitmq.yaml --export-broker users --where "status = active"
```

**Проверка через Management UI:**
- http://localhost:15672
- Проверить очередь `tdtp_test_queue`
- Посмотреть количество сообщений

### 5.3 Создание приемной БД
```sql
CREATE DATABASE tdtp_target;
CREATE USER tdtp_user WITH PASSWORD 'tdtp_pass';
GRANT ALL PRIVILEGES ON DATABASE tdtp_target TO tdtp_user;

\c tdtp_target

CREATE TABLE users (
    -- такая же схема
);
```

### 5.4 Создание config.rabbitmq.target.yaml
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  database: tdtp_target
  user: tdtp_user
  password: tdtp_pass
  sslmode: disable
broker:
  type: rabbitmq
  host: localhost
  port: 5672
  user: guest
  password: guest
  queue: tdtp_test_queue
```

### 5.5 Импорт из RabbitMQ
```bash
.\tdtpcli --config config.rabbitmq.target.yaml --import-broker --strategy replace
```

### 5.6 Проверка данных
```sql
\c tdtp_target
SELECT COUNT(*) FROM users;
SELECT status, COUNT(*) FROM users GROUP BY status;
```

---

## Этап 6: Тестирование MSMQ (опционально)

### 6.1 Варианты реализации
- Kafka (вместо MSMQ)
- ActiveMQ (поддержка STOMP)
- Azure Service Bus (если есть доступ)

### 6.2 Создание docker-compose с Kafka
```yaml
kafka:
  image: confluentinc/cp-kafka:latest
  ports:
    - "9092:9092"
```

### 6.3 Создание config.kafka.yaml
```yaml
broker:
  type: kafka
  bootstrap_servers: localhost:9092
  topic: tdtp_test_topic
```

### 6.4 Тестирование экспорт/импорт
Аналогично RabbitMQ (шаги 5.2-5.6)

---

## Этап 7: Тестирование производительности

### 7.1 Импорт больших объемов
```bash
# Замерить время импорта 100k записей
time .\tdtpcli --config config.postgres.yaml --import users_full.xml --strategy replace
```

### 7.2 Экспорт с фильтрами
```bash
# Сложные фильтры
time .\tdtpcli --config config.postgres.yaml --export users --where "balance > 100000 AND status = active" --output perf_test.xml
```

### 7.3 Batch-размеры
Проверить влияние `--batch` на производительность:
```bash
.\tdtpcli --config config.postgres.yaml --import users_full.xml --batch 500
.\tdtpcli --config config.postgres.yaml --import users_full.xml --batch 1000
.\tdtpcli --config config.postgres.yaml --import users_full.xml --batch 5000
```

---

## Этап 8: Проверка edge cases

### 8.1 Пустые таблицы
```bash
# Экспорт пустой таблицы
.\tdtpcli --config config.postgres.yaml --export users --where "status = nonexistent" --output empty.xml
```

### 8.2 NULL значения
```sql
INSERT INTO users (id, first_name, last_name) VALUES (999999, 'Test', NULL);
```
Экспортировать и импортировать обратно, проверить NULL

### 8.3 Спецсимволы
```sql
INSERT INTO users (id, first_name, description) VALUES (999998, 'Test<>"&', 'Text with <xml> & special chars');
```

### 8.4 Большие TEXT поля
```sql
UPDATE users SET description = REPEAT('A', 100000) WHERE id = 1;
```

---

## Чек-лист перед началом тестирования

- [ ] Docker установлен и работает
- [ ] docker-compose.yml создан
- [ ] Контейнеры запущены (postgres, rabbitmq)
- [ ] config.postgres.yaml создан
- [ ] config.rabbitmq.yaml создан
- [ ] Таблица users создана в PostgreSQL
- [ ] Есть тестовые данные (SQLite экспорт)
- [ ] tdtpcli собран и работает

---

## Ожидаемые результаты

### Успешный сценарий:
1. ✅ Данные импортированы в PostgreSQL
2. ✅ Экспорт с фильтрами работает корректно
3. ✅ Multi-part файлы создаются для больших объемов
4. ✅ Сжатие работает, размер файлов уменьшается
5. ✅ RabbitMQ принимает и отдает сообщения
6. ✅ Импорт из очереди восстанавливает данные
7. ✅ Производительность: >10k записей/сек

### Возможные проблемы:
- Проблемы с кодировкой (UTF-8 для кириллицы)
- Тайм-ауты при больших объемах
- Проблемы с NULL значениями
- Ошибки сериализации спецсимволов в XML

---

## Команды для быстрого дебага

```bash
# Логи Docker
docker-compose logs -f postgres
docker-compose logs -f rabbitmq

# Подключение к PostgreSQL
docker exec -it <container> psql -U tdtp_user -d tdtp_test

# Проверка очередей RabbitMQ
docker exec -it <rabbitmq-container> rabbitmqctl list_queues

# Очистка очереди
docker exec -it <rabbitmq-container> rabbitmqctl purge_queue tdtp_test_queue

# Audit логи
tail -f audit_postgres.log
tail -f audit_rabbitmq.log
```

---

## Документация результатов

Для каждого теста записывать:
- Команду
- Время выполнения
- Количество записей
- Размер файлов/сообщений
- Возникшие ошибки
- Скриншоты (RabbitMQ UI)

---

## Примечания

- Все пароли в примерах - тестовые, для prod использовать безопасные
- PostgreSQL настроить для оптимальной производительности (shared_buffers, work_mem)
- RabbitMQ - проверить политики очередей (TTL, max length)
- Backup данных перед деструктивными операциями
