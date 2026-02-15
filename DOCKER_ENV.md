# TDTP Unified Test Environment

Единая среда тестирования для всех коннекторов TDTP Framework.

## 🚀 Быстрый старт

```powershell
# 1. Очистить старые контейнеры (если есть)
docker stop $(docker ps -aq)
docker rm $(docker ps -aq)

# 2. Запустить всю среду
cd G:\DEV\Go\TDTP\tdtp-framework
docker-compose up -d

# 3. Проверить статус
docker-compose ps
```

## 📦 Что включено

| Сервис | Контейнер | Порт | Credentials |
|--------|-----------|------|-------------|
| **RabbitMQ** | tdtp-rabbitmq-test | 5672, 15672 | User: `tdtp_test`<br>Pass: `tdtp_test_password` |
| **MS SQL Server** | tdtp-mssql-test | 1433 | User: `sa`<br>Pass: `YourStrong!Passw0rd` |
| **PostgreSQL** | tdtp-postgres-test | 5432 | User: `tdtp_test`<br>Pass: `tdtp_test_password`<br>DB: `tdtp_test_db` |
| **MySQL** | tdtp-mysql-test | 3306 | User: `tdtp_test`<br>Pass: `tdtp_test_password`<br>DB: `tdtp_test_db` |
| **Adminer** | tdtp-adminer | 8080 | Web UI: http://localhost:8080 |

## 🗄️ Настройка тестовых баз данных

### Microsoft SQL Server - TravelGuide

```powershell
cd examples\travel-guide

# Создать базу и таблицу
Get-Content setup_database.sql | docker exec -i tdtp-mssql-test /opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P 'YourStrong!Passw0rd'

# Заполнить данными (10 городов)
python populate_data.py

# Проверить
docker exec -it tdtp-mssql-test /opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P 'YourStrong!Passw0rd' -d TravelGuide -Q "SELECT * FROM cities"
```

### PostgreSQL - TravelGuide

```powershell
cd examples\travel-guide

# Создать базу
docker exec -i tdtp-postgres-test psql -U tdtp_test -d tdtp_test_db -c "CREATE DATABASE TravelGuide;"

# Создать таблицу
Get-Content setup_database_postgres.sql | docker exec -i tdtp-postgres-test psql -U tdtp_test -d TravelGuide

# Заполнить данными
python populate_data_postgres.py

# Проверить
docker exec -it tdtp-postgres-test psql -U tdtp_test -d TravelGuide -c "SELECT * FROM cities;"
```

### MySQL - TravelGuide (TODO)

```powershell
# Coming soon...
```

## 🌐 Web UI

- **RabbitMQ Management**: http://localhost:15672
  - Login: `tdtp_test` / `tdtp_test_password`

- **Adminer**: http://localhost:8080
  - System: PostgreSQL / MySQL / MS SQL
  - Server: `tdtp-postgres-test` / `tdtp-mysql-test` / `tdtp-mssql-test`
  - Username: см. таблицу выше

## 🔧 Управление

```powershell
# Запустить все сервисы
docker-compose up -d

# Остановить все
docker-compose stop

# Перезапустить
docker-compose restart

# Просмотр логов
docker-compose logs -f [service_name]

# Удалить всё (включая данные!)
docker-compose down -v

# Пересоздать с нуля
docker-compose down -v && docker-compose up -d
```

## 🧪 Тестирование коннекторов в tdtp-xray

### MSSQL

1. Step 2 → Add Source → Microsoft SQL Server
2. Server: `localhost`
3. Port: `1433`
4. User: `sa`
5. Password: `YourStrong!Passw0rd`
6. Database: `TravelGuide`
7. Test Connection → выбрать таблицу `cities`

### PostgreSQL

1. Step 2 → Add Source → PostgreSQL
2. Host: `localhost`
3. Port: `5432`
4. User: `tdtp_test`
5. Password: `tdtp_test_password`
6. Database: `TravelGuide`
7. Test Connection → выбрать таблицу `cities`

### MySQL

1. Step 2 → Add Source → MySQL
2. Host: `localhost`
3. Port: `3306`
4. User: `tdtp_test`
5. Password: `tdtp_test_password`
6. Database: `tdtp_test_db`
7. Test Connection

## 📊 Хранение данных

Данные сохраняются в Docker volumes и **не удаляются** при `docker-compose stop`:

- `tdtp-mssql-data` - MS SQL Server
- `tdtp-postgres-data` - PostgreSQL
- `tdtp-mysql-data` - MySQL

Для полной очистки используйте: `docker-compose down -v`

## 🔍 Отладка

```powershell
# Войти в контейнер
docker exec -it tdtp-mssql-test /bin/bash
docker exec -it tdtp-postgres-test /bin/bash
docker exec -it tdtp-mysql-test /bin/bash

# Просмотр логов
docker logs tdtp-mssql-test
docker logs tdtp-postgres-test
docker logs tdtp-mysql-test
docker logs tdtp-rabbitmq-test

# Проверка health status
docker inspect tdtp-mssql-test | grep -A 10 Health
```

## ⚠️ Важно

1. **Порты должны быть свободны**: 1433, 3306, 5432, 5672, 8080, 15672
2. **Пароли тестовые**: НЕ использовать в production!
3. **Данные volume**: Занимают место на диске, периодически чистите
4. **Windows Firewall**: Может блокировать порты при первом запуске

## 🧹 Полная очистка старых контейнеров

```powershell
# Остановить ВСЕ контейнеры
docker stop $(docker ps -aq)

# Удалить ВСЕ контейнеры
docker rm $(docker ps -aq)

# Удалить неиспользуемые образы
docker image prune -a

# Удалить неиспользуемые volumes
docker volume prune
```
