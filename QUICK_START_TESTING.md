# Быстрый старт тестирования TDTP Framework

## Один скрипт для полной настройки

```bash
# Установка зависимостей
pip install -r scripts/test_setup/requirements.txt

# Запуск полной автоматической настройки
python scripts/test_setup/setup_all.py
```

**Что произойдет автоматически:**
1. ✅ Проверка Docker
2. ✅ Создание docker-compose.yml
3. ✅ Запуск PostgreSQL, RabbitMQ, Kafka
4. ✅ Создание БД и таблиц
5. ✅ Генерация конфигов
6. ✅ Генерация 10 000 тестовых записей

**Время выполнения:** ~2-3 минуты

---

## Результат

После выполнения вы получите:

**Сервисы:**
- PostgreSQL на localhost:5432
- RabbitMQ на localhost:5672 (UI: http://localhost:15672)
- Kafka на localhost:9092

**Базы данных:**
- `tdtp_test` - источник с 10 000 записей
- `tdtp_target` - пустой приемник

**Конфигурационные файлы:**
- `config.postgres.tdtp_test.yaml`
- `config.postgres.tdtp_target.yaml`
- `config.rabbitmq.tdtp_test.yaml`
- `config.rabbitmq.tdtp_target.yaml`
- `config.kafka.tdtp_test.yaml`
- `config.kafka.tdtp_target.yaml`

---

## Готовые команды для тестирования

### Тест 1: Экспорт/импорт через файл

```bash
# Экспорт всей таблицы
tdtpcli --config config.postgres.tdtp_test.yaml --export users --output users.xml

# Импорт в другую БД
tdtpcli --config config.postgres.tdtp_target.yaml --import users.xml --strategy replace

# Проверка
docker exec -it tdtp_postgres psql -U tdtp_user -d tdtp_target -c "SELECT COUNT(*) FROM users;"
```

### Тест 2: Экспорт с фильтром

```bash
# Только активные пользователи
tdtpcli --config config.postgres.tdtp_test.yaml --export users \
  --where "status = active" \
  --output active_users.xml

# Богатые пользователи
tdtpcli --config config.postgres.tdtp_test.yaml --export users \
  --where "balance > 300000" \
  --output rich_users.xml

# Москва
tdtpcli --config config.postgres.tdtp_test.yaml --export users \
  --where "city = Москва" \
  --output moscow_users.xml
```

### Тест 3: RabbitMQ очередь

```bash
# Экспорт в очередь
tdtpcli --config config.rabbitmq.tdtp_test.yaml --export-broker users

# Проверить очередь в UI: http://localhost:15672
# Логин: guest / Пароль: guest

# Импорт из очереди
tdtpcli --config config.rabbitmq.tdtp_target.yaml --import-broker --strategy replace

# Проверка
docker exec -it tdtp_postgres psql -U tdtp_user -d tdtp_target -c "SELECT COUNT(*) FROM users;"
```

### Тест 4: Сжатие данных

```bash
# Экспорт со сжатием
tdtpcli --config config.postgres.tdtp_test.yaml --export users \
  --compress --compress-level 5 \
  --output users_compressed.xml

# Сравнить размеры
ls -lh users*.xml
```

### Тест 5: Производительность

```bash
# Сгенерировать 100k записей
python scripts/test_setup/generate_test_data.py --count 100000

# Замерить время экспорта
time tdtpcli --config config.postgres.tdtp_test.yaml --export users --output big_export.xml

# Замерить время импорта
time tdtpcli --config config.postgres.tdtp_target.yaml --import big_export.xml --strategy replace
```

---

## Опции setup_all.py

```bash
# Docker уже запущен, только настроить БД
python scripts/test_setup/setup_all.py --skip-docker

# Без генерации данных (использовать существующие)
python scripts/test_setup/setup_all.py --skip-data

# Сгенерировать 100 000 записей
python scripts/test_setup/setup_all.py --count 100000
```

---

## Управление Docker

```bash
# Статус контейнеров
docker-compose ps

# Логи
docker-compose logs -f postgres
docker-compose logs -f rabbitmq

# Перезапуск
docker-compose restart

# Остановка
docker-compose down

# Остановка с удалением данных
docker-compose down -v
```

---

## Подключение к PostgreSQL

```bash
# Через Docker
docker exec -it tdtp_postgres psql -U tdtp_user -d tdtp_test

# Примеры SQL
SELECT COUNT(*) FROM users;
SELECT status, COUNT(*) FROM users GROUP BY status;
SELECT * FROM users WHERE city = 'Москва' LIMIT 10;
```

---

## Troubleshooting

**Порт 5432 занят:**
```bash
# Проверить кто использует
netstat -ano | findstr :5432   # Windows
lsof -i :5432                    # Linux/Mac

# Изменить порт в docker-compose.yml на 5433
```

**PostgreSQL не стартует:**
```bash
docker-compose logs postgres
docker-compose restart postgres
```

**RabbitMQ недоступен:**
```bash
# Подождать 30-60 секунд после запуска
docker-compose logs rabbitmq
```

---

## Дополнительная документация

- [TEST_PLAN.md](TEST_PLAN.md) - детальный план тестирования
- [scripts/test_setup/README.md](scripts/test_setup/README.md) - документация скриптов

---

## Чек-лист перед началом

- [ ] Docker установлен и запущен
- [ ] Python 3.7+ установлен
- [ ] pip установлен
- [ ] Свободны порты: 5432, 5672, 15672, 9092
- [ ] Установлены зависимости: `pip install -r scripts/test_setup/requirements.txt`

**Теперь запустите:** `python scripts/test_setup/setup_all.py` ✨
