# Скрипты для настройки тестового окружения

Автоматизированная настройка PostgreSQL, RabbitMQ и Kafka для тестирования TDTP Framework.

## Требования

- Python 3.7+
- Docker и docker-compose
- pip для установки зависимостей

## Установка зависимостей

```bash
pip install -r requirements.txt
```

## Быстрый старт

**Полная автоматическая настройка (один скрипт делает все):**

```bash
python setup_all.py
```

Этот скрипт выполнит:
1. ✅ Проверку Docker
2. ✅ Генерацию docker-compose.yml
3. ✅ Запуск контейнеров (PostgreSQL, RabbitMQ, Kafka)
4. ✅ Инициализацию PostgreSQL (создание БД и таблиц)
5. ✅ Генерацию конфигурационных файлов
6. ✅ Генерацию 10 000 тестовых записей

**Опции:**

```bash
# Пропустить настройку Docker (если уже запущен)
python setup_all.py --skip-docker

# Пропустить генерацию данных
python setup_all.py --skip-data

# Сгенерировать 100 000 записей
python setup_all.py --count 100000
```

---

## Ручной запуск (пошагово)

Если нужен контроль над каждым шагом:

### Шаг 1: Генерация docker-compose.yml

```bash
python generate_docker_compose.py
```

Создаст файл `docker-compose.yml` в корне проекта с сервисами:
- PostgreSQL (порт 5432)
- RabbitMQ (порты 5672, 15672)
- Kafka + Zookeeper (порт 9092)

### Шаг 2: Запуск контейнеров

```bash
cd ../..  # в корень проекта
docker-compose up -d
docker-compose ps
```

### Шаг 3: Инициализация PostgreSQL

```bash
python init_postgres.py
```

Создаст:
- Базы данных: `tdtp_test`, `tdtp_target`
- Пользователя: `tdtp_user` / `tdtp_pass`
- Таблицу `users` с индексами

### Шаг 4: Генерация конфигов

```bash
python generate_configs.py
```

Создаст конфигурационные файлы:
- `config.postgres.tdtp_test.yaml`
- `config.postgres.tdtp_target.yaml`
- `config.rabbitmq.tdtp_test.yaml`
- `config.rabbitmq.tdtp_target.yaml`
- `config.kafka.tdtp_test.yaml`
- `config.kafka.tdtp_target.yaml`

### Шаг 5: Генерация тестовых данных

```bash
# 10 000 записей (по умолчанию)
python generate_test_data.py

# Или указать количество
python generate_test_data.py --count 100000

# В другую базу
python generate_test_data.py --database tdtp_target --count 50000
```

---

## Описание скриптов

### `setup_all.py`
Главный оркестратор - запускает все шаги автоматически.

**Примеры:**
```bash
# Полная установка
python setup_all.py

# Только инициализация БД (Docker уже запущен)
python setup_all.py --skip-docker

# Без генерации данных
python setup_all.py --skip-data
```

### `generate_docker_compose.py`
Создает `docker-compose.yml` с настроенными сервисами.

**Результат:**
- PostgreSQL 15
- RabbitMQ 3 с Management UI
- Kafka 7.5 с Zookeeper

### `init_postgres.py`
Инициализирует PostgreSQL: создает БД, пользователей, таблицы.

**Что делает:**
- Ожидает готовности PostgreSQL
- Создает пользователя `tdtp_user`
- Создает базы `tdtp_test` и `tdtp_target`
- Создает таблицу `users` с индексами

### `generate_configs.py`
Генерирует YAML конфиги для всех сценариев тестирования.

**Создаваемые конфиги:**
- PostgreSQL (источник и приемник)
- RabbitMQ (источник и приемник)
- Kafka (источник и приемник)

### `generate_test_data.py`
Генерирует реалистичные тестовые данные (кириллица).

**Параметры:**
- `--count N` - количество записей
- `--database DB` - целевая база
- `--batch N` - размер батча для вставки

**Генерируемые данные:**
- Русские имена и фамилии
- Реальные города России
- Email, телефоны, ИНН, полисы
- Статусы: active, blocked, pending, verified и т.д.

---

## Проверка окружения

### Проверка Docker

```bash
docker-compose ps
```

Должны быть запущены:
- `tdtp_postgres`
- `tdtp_rabbitmq`
- `tdtp_kafka`
- `tdtp_zookeeper`

### Проверка PostgreSQL

```bash
docker exec -it tdtp_postgres psql -U tdtp_user -d tdtp_test -c "SELECT COUNT(*) FROM users;"
```

### Проверка RabbitMQ

Открыть в браузере: http://localhost:15672
- Логин: `guest`
- Пароль: `guest`

### Проверка Kafka

```bash
docker exec -it tdtp_kafka kafka-topics --bootstrap-server localhost:9092 --list
```

---

## Тестирование TDTP Framework

После настройки окружения:

### 1. Экспорт в файл

```bash
tdtpcli --config config.postgres.tdtp_test.yaml --export users --output users.xml
```

### 2. Импорт из файла

```bash
tdtpcli --config config.postgres.tdtp_target.yaml --import users.xml --strategy replace
```

### 3. Экспорт в RabbitMQ

```bash
tdtpcli --config config.rabbitmq.tdtp_test.yaml --export-broker users
```

### 4. Импорт из RabbitMQ

```bash
tdtpcli --config config.rabbitmq.tdtp_target.yaml --import-broker
```

### 5. Экспорт с фильтром

```bash
tdtpcli --config config.postgres.tdtp_test.yaml --export users \
  --where "status = active AND balance > 100000" \
  --output rich_active.xml
```

---

## Очистка

### Остановка контейнеров

```bash
docker-compose down
```

### Остановка с удалением данных

```bash
docker-compose down -v
```

### Удаление конфигов

```bash
rm -f config.postgres.*.yaml config.rabbitmq.*.yaml config.kafka.*.yaml
```

---

## Troubleshooting

### PostgreSQL не запускается

```bash
docker-compose logs postgres
```

Возможно, порт 5432 занят. Изменить в docker-compose.yml:
```yaml
ports:
  - "5433:5432"  # использовать другой порт
```

### RabbitMQ недоступен

```bash
docker-compose logs rabbitmq
```

Подождать 30-60 секунд после запуска.

### Ошибка подключения к PostgreSQL из Python

Проверить что контейнер запущен:
```bash
docker ps | grep tdtp_postgres
```

Проверить доступность:
```bash
docker exec -it tdtp_postgres pg_isready -U postgres
```

### Ошибка "module not found"

Установить зависимости:
```bash
pip install -r requirements.txt
```

---

## Зависимости

Файл `requirements.txt`:
```
psycopg2-binary>=2.9.0
PyYAML>=6.0
Faker>=19.0.0
```

---

## Дополнительная информация

- [TEST_PLAN.md](../../TEST_PLAN.md) - детальный план тестирования
- [Docker Compose Reference](https://docs.docker.com/compose/)
- [PostgreSQL Documentation](https://www.postgresql.org/docs/)
- [RabbitMQ Documentation](https://www.rabbitmq.com/documentation.html)
