# Example 08 — ETL Pipeline + xzmercury (integration)

**Сложность**: ⭐⭐ Средний
**Время**: 5 минут

Минимальный пример полной интеграции ETL-пайплайна с xzmercury:
embedded mock-сервер выдаёт ключ, `tdtpcli --pipeline` шифрует результат AES-256-GCM.

## Архитектура

```
[embedded xzmercury-mock]
         │
         │ POST /api/keys/bind → key (AES-256)
         ▼
tdtpcli --pipeline pipeline.yaml
   │
   ├── load: employees.tdtp.xml
   ├── load: departments.tdtp.xml
   ├── workspace: SQLite :memory:
   ├── transform: JOIN + GROUP BY → dept_report
   └── output: AES-256-GCM → /tmp/dept_report_encrypted.tdtp
         │
         │ POST /api/keys/retrieve (burn-on-read)
         ▼
   key deleted from xzmercury
```

## Запуск

```bash
# из корня репозитория
go run ./examples/08-pipeline-encrypted/
```

Никаких внешних зависимостей — mock-сервер запускается в том же процессе,
источники данных — TDTP-файлы из `examples/encryption-test/`.

## Что происходит

| Шаг | Кто | Действие |
|-----|-----|----------|
| 1 | `main.go` | Запускает xzmercury-mock на случайном порту |
| 2 | `main.go` | Подставляет URL mock-сервера в pipeline.yaml → tmp файл |
| 3 | `main.go` | Вызывает `tdtpcli --pipeline <tmp>.yaml` |
| 4 | `tdtpcli` | Загружает TDTP-источники в SQLite workspace |
| 5 | `tdtpcli` | Выполняет SQL-трансформацию (JOIN + GROUP BY) |
| 6 | `tdtpcli` | `POST /api/keys/bind` → получает AES-256 ключ |
| 7 | `tdtpcli` | Шифрует результат, пишет `/tmp/dept_report_encrypted.tdtp` |
| 8 | `tdtpcli` | `POST /api/keys/retrieve` → burn-on-read |
| 9 | `main.go` | Проверяет заголовок выходного файла (algo=0x01 = AES-256-GCM) |

## С реальным xzmercury

```bash
# Терминал 1: запускаем настоящий mock-сервер
go run ./cmd/xzmercury-mock/ --addr :3000 --secret dev-secret

# Терминал 2: правим pipeline.yaml
#   mercury_url: "http://localhost:3000"
# и запускаем только tdtpcli
tdtpcli --pipeline examples/08-pipeline-encrypted/pipeline.yaml
```

## Сравнение с другими примерами

| Пример | Пайплайн | Шифрование | Внешние зависимости |
|--------|----------|------------|---------------------|
| `encryption-test/pipeline-enc.yaml` | только YAML | ✅ | xzmercury-mock (отдельный процесс) |
| `02b-rabbitmq-mssql-etl/` | YAML + Go | ❌ | MSSQL + RabbitMQ |
| **`08-pipeline-encrypted/`** | YAML + Go | ✅ | нет (embedded mock) |
