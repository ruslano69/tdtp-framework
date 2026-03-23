# Example 07: Redis Orchestration Worker (Python)

Эталонный наблюдатель за событиями пайплайнов TDTP.
Написан на Python — языке DataOps-инженеров, авторов Airflow DAGов и Telegram-ботов.

## Как это работает

Когда пайплайн завершается (успешно или с ошибкой), TDTP-фреймворк публикует в Redis два действия:

```
SET  tdtp:pipeline:<result_name>:state  <JSON>  EX <ttl>   ← для polling
PUB  tdtp:pipeline:<result_name>        <JSON>              ← для event-driven
```

`python_worker.py` подписывается на паттерн `tdtp:pipeline:*` и мгновенно реагирует
на каждое событие — это модель **Push (Event-Driven)**, а не периодический опрос.

### Структура JSON-события

```json
{
  "pipeline_name": "erp_mask_sync",
  "result_name":   "MASK_V001",
  "status":        "success",
  "started_at":    "2025-11-18T10:00:00Z",
  "finished_at":   "2025-11-18T10:00:02Z",
  "duration_ms":   1842,
  "rows_loaded":   15000,
  "rows_exported": 14998
}
```

При ошибке добавляется поле `"error": "..."`.

## Быстрый старт

```bash
# 1. Установить зависимость
pip install redis

# 2. Запустить воркер (Redis на localhost:6379)
python python_worker.py

# 3. Или указать кастомный адрес
python python_worker.py --host 10.0.0.1 --port 6380 --password mypassword
```

## Конфигурация пайплайна

Добавьте секцию `result_log` в YAML-конфиг пайплайна (см. `pipeline_example.yaml`):

```yaml
result_log:
  type: redis
  address: "127.0.0.1:6379"
  name: MASK_V001   # станет именем канала tdtp:pipeline:MASK_V001
  ttl: 3600
```

Запуск пайплайна:

```bash
tdtpcli run --config pipeline_example.yaml
```

## Вывод воркера

```
[TDTP Orchestrator] Подключен к Redis 127.0.0.1:6379
[TDTP Orchestrator] Слушаем события пайплайнов (Ctrl+C для остановки)...
-------------------------------------------------------
[EVENT] erp_mask_sync -> MASK_V001
  Status  : SUCCESS (1.84s)
  Rows    : loaded=15000  exported=14998
  [DQ]    : WARN — 2 строки отброшено валидатором/фильтром
```

## Data Quality Check

Воркер автоматически сравнивает `rows_loaded` и `rows_exported`.
Расхождение означает, что часть данных была отброшена валидатором или фильтром
в трансформации — сигнал для ручного разбора или алерта.

## Точки расширения

В коде обозначены места для подключения внешних систем:

```python
# После успешного завершения:
trigger_airflow_dag(result_name)
upload_to_clickhouse(result_name)

# При ошибке:
send_telegram_alert(f"[TDTP] {pipeline_name} failed: {error_msg}")
create_jira_incident(pipeline_name, error_msg)
```

## Polling-режим (без подписки)

Для health-check эндпоинтов и дашбордов доступна функция `poll_pipeline_state`:

```python
from python_worker import poll_pipeline_state
import redis

r = redis.Redis(decode_responses=True)
state = poll_pipeline_state(r, "MASK_V001")
print(state["status"])   # "success" | "failed" | None если TTL истек
```

## Один воркер — все пайплайны

Благодаря `psubscribe('tdtp:pipeline:*')` один экземпляр воркера
отслеживает все пайплайны одновременно. Добавление нового пайплайна
в ERP не требует изменений в коде наблюдателя.

## Следующие шаги

- **Example 05:** Streaming Export в RabbitMQ
- **Example 03:** Инкрементальная синхронизация
