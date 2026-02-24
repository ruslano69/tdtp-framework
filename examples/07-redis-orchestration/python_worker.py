"""
TDTP Redis Orchestration Worker
Эталонный наблюдатель за событиями пайплайнов TDTP через Redis Pub/Sub.

Зависимости:
    pip install redis

Использование:
    python python_worker.py
    python python_worker.py --host 10.0.0.1 --port 6380

Каналы Redis:
    PUBLISH  tdtp:pipeline:<result_name>         — событие завершения пайплайна
    SET      tdtp:pipeline:<result_name>:state   — последнее состояние (GET для polling)
"""

import argparse
import json
import sys

import redis


def listen_to_tdtp_events(host: str = "127.0.0.1", port: int = 6379, db: int = 0, password: str = None):
    r = redis.Redis(host=host, port=port, db=db, password=password, decode_responses=True)

    try:
        r.ping()
    except redis.ConnectionError as e:
        print(f"[ERROR] Не удалось подключиться к Redis {host}:{port} — {e}", file=sys.stderr)
        sys.exit(1)

    pubsub = r.pubsub()

    # psubscribe охватывает все текущие и будущие пайплайны без изменения кода
    pubsub.psubscribe("tdtp:pipeline:*")

    print(f"[TDTP Orchestrator] Подключен к Redis {host}:{port}")
    print("[TDTP Orchestrator] Слушаем события пайплайнов (Ctrl+C для остановки)...")

    for message in pubsub.listen():
        # Нас интересуют только pmessage — реальные события из каналов
        if message["type"] != "pmessage":
            continue

        channel: str = message["channel"]

        # Пропускаем ключи состояния — они создаются командой SET, а не PUBLISH.
        # Keyspace notifications (если включены) генерируют "__keyevent@*__:set",
        # но через psubscribe tdtp:pipeline:* к нам попадают только явные PUBLISH.
        # Фильтр оставлен на случай, если в будущем добавятся keyspace notifications.
        if channel.endswith(":state"):
            continue

        try:
            data: dict = json.loads(message["data"])
        except json.JSONDecodeError as e:
            print(f"[WARN] Ошибка парсинга JSON из канала {channel}: {e}")
            continue

        pipeline_name = data.get("pipeline_name", "UNKNOWN")
        result_name   = data.get("result_name",   "UNKNOWN")
        status        = data.get("status",         "unknown")

        print("-" * 55)
        print(f"[EVENT] {pipeline_name} -> {result_name}")

        if status == "success":
            rows_in   = data.get("rows_loaded",   0)
            rows_out  = data.get("rows_exported", 0)
            duration  = data.get("duration_ms",   0)

            print(f"  Status  : SUCCESS ({duration / 1000:.2f}s)")
            print(f"  Rows    : loaded={rows_in}  exported={rows_out}")

            # Data Quality Check: расхождение строк сигнализирует о фильтрации или ошибке
            if rows_in != rows_out:
                dropped = rows_in - rows_out
                print(f"  [DQ]    : WARN — {dropped} строк отброшено валидатором/фильтром")

            # --- Точка расширения ---
            # trigger_airflow_dag(result_name)
            # upload_to_clickhouse(result_name)

        elif status == "failed":
            error_msg = data.get("error", "Неизвестная ошибка")
            print(f"  Status  : FAILED")
            print(f"  Error   : {error_msg}")

            # --- Точка расширения ---
            # send_telegram_alert(f"[TDTP] {pipeline_name} failed: {error_msg}")
            # create_jira_incident(pipeline_name, error_msg)

        else:
            print(f"  Status  : {status} (неизвестный статус)")


def poll_pipeline_state(r: redis.Redis, result_name: str) -> dict | None:
    """
    Альтернативный режим: однократный GET последнего состояния пайплайна.
    Полезен для health-check эндпоинтов и дашбордов без подписки.

    Пример:
        state = poll_pipeline_state(r, "MASK_V001")
        if state and state["status"] == "success":
            ...
    """
    key = f"tdtp:pipeline:{result_name}:state"
    raw = r.get(key)
    if raw is None:
        return None
    return json.loads(raw)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="TDTP Redis Orchestration Worker")
    parser.add_argument("--host",     default="127.0.0.1", help="Redis host (default: 127.0.0.1)")
    parser.add_argument("--port",     type=int, default=6379, help="Redis port (default: 6379)")
    parser.add_argument("--db",       type=int, default=0,    help="Redis DB index (default: 0)")
    parser.add_argument("--password", default=None,           help="Redis password (optional)")
    args = parser.parse_args()

    try:
        listen_to_tdtp_events(
            host=args.host,
            port=args.port,
            db=args.db,
            password=args.password,
        )
    except KeyboardInterrupt:
        print("\n[TDTP Orchestrator] Остановка мониторинга.")
