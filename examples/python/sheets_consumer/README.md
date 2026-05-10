# TDTP → Google Sheets Consumer

Пример Python-скрипта, который читает TDTP-пакеты из RabbitMQ и записывает
строки в Google Sheets. Использует `J_ParseBytes` из libtdtp — данные парсятся
прямо из памяти без записи временных файлов на диск.

## Архитектура

```
RabbitMQ
   │  (тело сообщения = TDTP XML bytes)
   ▼
J_ParseBytes(body)          ← libtdtp, без tempfile
   │
J_FilterRows(where)         ← опционально, TDTQL
   │
SheetsWriter.write_packet() ← один batch-запрос на весь пакет
   │
Google Sheets
```

## Установка

```bash
pip install pika gspread google-auth
```

Сборка libtdtp.so (из корня репо):
```bash
cd pkg/python/libtdtp
GOWORK=off go build -tags compress -buildmode=c-shared -o /tmp/libtdtp.so
```

## Настройка Google Sheets

1. Создать Service Account в Google Cloud Console
2. Дать ему доступ к таблице (Editor)
3. Скачать `credentials.json`

## Переменные окружения

| Переменная         | По умолчанию                          | Описание                        |
|--------------------|---------------------------------------|---------------------------------|
| `RABBITMQ_URL`     | `amqp://guest:guest@localhost:5672/`  | URL брокера                     |
| `RABBITMQ_QUEUE`   | `tdtp.export`                         | Имя очереди                     |
| `GOOGLE_CREDS_FILE`| `credentials.json`                    | Service account credentials     |
| `SPREADSHEET_ID`   | —                                     | ID таблицы (из URL)             |
| `SHEET_NAME`       | первый лист                           | Название листа                  |
| `LIBTDTP_SO`       | автопоиск                             | Путь к libtdtp.so               |

## Запуск

```bash
# Основной режим — слушать RabbitMQ
export SPREADSHEET_ID="1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgVE2upms"
export GOOGLE_CREDS_FILE="credentials.json"
python tdtp_sheets_consumer.py

# С фильтром строк (TDTQL)
python tdtp_sheets_consumer.py --where "Balance > 1000"

# Тест без RabbitMQ — читать из файла
python tdtp_sheets_consumer.py --file users.tdtp.xml --dry-run

# Тест с реальной записью из файла
python tdtp_sheets_consumer.py --file users.tdtp.xml
```

## Поведение

- **Заголовок** — пишется один раз при первой записи если лист пустой
- **Батчинг** — весь пакет пишется одним запросом к Sheets API (один вызов = 1 из 60/мин лимита)
- **Переподключение** — при обрыве RabbitMQ автоматически переподключается через 5 с
- **Ошибки пакетов** — битый пакет nack-ается без requeue, не блокирует очередь

## Связь с оркестратором

```bash
# Экспорт с переменными пайплайна
tdtpcli --pipeline dept_export.yaml @dept=97 @month=2025-01

# Получатель проверяет что данные именно для dept=97
tdtpcli --import dept_97.tdtp.xml --expect-var dept=97

# Если проверка не нужна — просто слушаем Sheets consumer
python tdtp_sheets_consumer.py --where "Dept = '97'"
```
