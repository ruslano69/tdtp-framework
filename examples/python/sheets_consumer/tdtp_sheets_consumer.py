#!/usr/bin/env python3
"""
TDTP → Google Sheets Consumer
==============================

Сценарий: получаем TDTP-пакеты из RabbitMQ и пишем строки в Google Sheets
без записи временных файлов на диск (используем J_ParseBytes).

Архитектура:
    RabbitMQ → [этот скрипт] → Google Sheets
                  ↓
              libtdtp (J_ParseBytes → J_FilterRows)

Режимы запуска:
    # Слушать RabbitMQ (основной режим)
    python tdtp_sheets_consumer.py

    # Тест без RabbitMQ: читать из файла
    python tdtp_sheets_consumer.py --file users.tdtp.xml

    # Тест: только распечатать что пришло бы в Sheets, не писать
    python tdtp_sheets_consumer.py --file users.tdtp.xml --dry-run

Переменные окружения:
    RABBITMQ_URL        amqp://guest:guest@localhost:5672/  (по умолчанию)
    RABBITMQ_QUEUE      tdtp.export  (по умолчанию)
    GOOGLE_CREDS_FILE   credentials.json  (service account)
    SPREADSHEET_ID      ID таблицы Google Sheets (из URL)
    SHEET_NAME          Название листа (по умолчанию: первый лист)
    LIBTDTP_SO          путь к libtdtp.so (по умолчанию: ищем рядом с репо)

Установка зависимостей:
    pip install pika gspread google-auth

Сборка libtdtp.so:
    cd pkg/python/libtdtp
    GOWORK=off go build -tags compress -buildmode=c-shared -o /tmp/libtdtp.so
"""

import argparse
import ctypes
import json
import logging
import os
import sys
import time
from pathlib import Path

# ─── Logging ─────────────────────────────────────────────────────────────────

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s  %(levelname)-7s  %(message)s",
    datefmt="%H:%M:%S",
)
log = logging.getLogger("tdtp-sheets")

# ─── Пути ────────────────────────────────────────────────────────────────────

SCRIPT_DIR = Path(__file__).parent
REPO_ROOT  = SCRIPT_DIR.parent.parent.parent

_SO_CANDIDATES = [
    Path(os.environ.get("LIBTDTP_SO", "")),
    REPO_ROOT / "pkg" / "python" / "libtdtp" / "libtdtp.so",
    Path("/tmp/libtdtp.so"),
]

# ─── Конфигурация из окружения ────────────────────────────────────────────────

RABBITMQ_URL   = os.environ.get("RABBITMQ_URL",   "amqp://guest:guest@localhost:5672/")
RABBITMQ_QUEUE = os.environ.get("RABBITMQ_QUEUE", "tdtp.export")
CREDS_FILE     = os.environ.get("GOOGLE_CREDS_FILE", "credentials.json")
SPREADSHEET_ID = os.environ.get("SPREADSHEET_ID", "")
SHEET_NAME     = os.environ.get("SHEET_NAME", "")

# ─── libtdtp: ctypes-обёртка ─────────────────────────────────────────────────

class LibTDTP:
    """Минимальная обёртка над J_* функциями libtdtp."""

    def __init__(self, so_path: Path):
        self._lib = ctypes.CDLL(str(so_path))

        # J_ParseBytes(data, length) → *C.char  (restype=c_void_p сохраняет указатель)
        self._lib.J_ParseBytes.argtypes = [ctypes.c_char_p, ctypes.c_int]
        self._lib.J_ParseBytes.restype  = ctypes.c_void_p

        # J_ReadFile(path) → *C.char
        self._lib.J_ReadFile.argtypes = [ctypes.c_char_p]
        self._lib.J_ReadFile.restype  = ctypes.c_void_p

        # J_FilterRows(dataJSON, where, limit) → *C.char
        self._lib.J_FilterRows.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_int]
        self._lib.J_FilterRows.restype  = ctypes.c_void_p

        # J_FreeString(*C.char)
        self._lib.J_FreeString.argtypes = [ctypes.c_void_p]
        self._lib.J_FreeString.restype  = None

        log.info("libtdtp загружена: %s", so_path)

    def _read_and_free(self, ptr: int) -> dict:
        """Читает JSON по указателю, освобождает память, возвращает dict."""
        data = json.loads(ctypes.string_at(ptr))
        self._lib.J_FreeString(ptr)
        if "error" in data:
            raise RuntimeError(f"libtdtp error: {data['error']}")
        return data

    def parse_bytes(self, raw: bytes) -> dict:
        """Парсит TDTP XML из байтового буфера. Не создаёт файл на диске."""
        ptr = self._lib.J_ParseBytes(raw, len(raw))
        return self._read_and_free(ptr)

    def read_file(self, path: str) -> dict:
        """Парсит TDTP XML из файла."""
        ptr = self._lib.J_ReadFile(path.encode())
        return self._read_and_free(ptr)

    def filter_rows(self, packet: dict, where: str, limit: int = 0) -> dict:
        """Фильтрует строки по TDTQL WHERE-выражению."""
        data_json = json.dumps(packet).encode()
        ptr = self._lib.J_FilterRows(data_json, where.encode(), limit)
        return self._read_and_free(ptr)


def find_libtdtp() -> Path:
    for candidate in _SO_CANDIDATES:
        if candidate and candidate.exists():
            return candidate
    raise FileNotFoundError(
        "libtdtp.so не найдена. Соберите:\n"
        "  cd pkg/python/libtdtp\n"
        "  GOWORK=off go build -tags compress -buildmode=c-shared -o /tmp/libtdtp.so"
    )


# ─── Google Sheets ────────────────────────────────────────────────────────────

class SheetsWriter:
    """Пишет строки в Google Sheets через gspread."""

    def __init__(self, creds_file: str, spreadsheet_id: str, sheet_name: str = ""):
        try:
            import gspread
            from google.oauth2.service_account import Credentials
        except ImportError:
            sys.exit("Установите зависимости: pip install gspread google-auth")

        scopes = [
            "https://www.googleapis.com/auth/spreadsheets",
            "https://www.googleapis.com/auth/drive",
        ]
        creds  = Credentials.from_service_account_file(creds_file, scopes=scopes)
        client = gspread.authorize(creds)

        self._spreadsheet = client.open_by_key(spreadsheet_id)
        if sheet_name:
            self._sheet = self._spreadsheet.worksheet(sheet_name)
        else:
            self._sheet = self._spreadsheet.get_worksheet(0)

        log.info("Google Sheets: '%s' → лист '%s'", spreadsheet_id, self._sheet.title)
        self._header_written = False

    def _ensure_header(self, field_names: list[str]) -> None:
        """Пишет строку-заголовок если лист пустой."""
        if self._header_written:
            return
        existing = self._sheet.get_all_values()
        if not existing:
            self._sheet.append_row(field_names, value_input_option="RAW")
            log.info("Записан заголовок: %s", field_names)
        self._header_written = True

    def write_packet(self, packet: dict) -> int:
        """
        Пишет все строки пакета в Sheets одним batch-запросом.
        Возвращает количество записанных строк.
        """
        fields = packet["schema"]["Fields"]
        field_names = [f["Name"] for f in fields]
        rows = packet["data"]

        if not rows:
            return 0

        self._ensure_header(field_names)

        # Один запрос API = весь пакет, независимо от числа строк
        self._sheet.append_rows(rows, value_input_option="RAW")
        return len(rows)


class DryRunWriter:
    """Имитирует SheetsWriter: печатает в stdout без обращения к API."""

    def write_packet(self, packet: dict) -> int:
        fields = packet["schema"]["Fields"]
        field_names = [f["Name"] for f in fields]
        rows = packet["data"]
        print(f"\n  Заголовок: {field_names}")
        for i, row in enumerate(rows[:5]):
            print(f"  [{i+1}] {row}")
        if len(rows) > 5:
            print(f"  ... ещё {len(rows) - 5} строк")
        return len(rows)


# ─── Обработка пакета ─────────────────────────────────────────────────────────

def process_packet(
    packet: dict,
    writer,
    where_filter: str = "",
) -> None:
    """Применяет опциональный фильтр и пишет пакет в Sheets."""
    table = packet.get("header", {}).get("table_name", "?")
    rows_in = len(packet.get("data", []))

    if where_filter:
        packet = libtdtp.filter_rows(packet, where_filter)
        rows_after = len(packet.get("data", []))
        log.info("Фильтр '%s': %d → %d строк", where_filter, rows_in, rows_after)
    else:
        rows_after = rows_in

    if rows_after == 0:
        log.info("Пакет '%s': нет строк после фильтрации, пропускаем", table)
        return

    written = writer.write_packet(packet)
    log.info("Таблица '%s': записано %d строк в Sheets", table, written)


# ─── RabbitMQ consumer ───────────────────────────────────────────────────────

def run_rabbitmq(writer, where_filter: str = "") -> None:
    try:
        import pika
    except ImportError:
        sys.exit("Установите зависимости: pip install pika")

    log.info("Подключаемся к RabbitMQ: %s", RABBITMQ_URL)
    params = pika.URLParameters(RABBITMQ_URL)

    while True:
        try:
            connection = pika.BlockingConnection(params)
            channel    = connection.channel()
            channel.queue_declare(queue=RABBITMQ_QUEUE, durable=True)

            log.info("Слушаем очередь '%s' ...", RABBITMQ_QUEUE)

            def on_message(ch, method, properties, body: bytes):
                try:
                    packet = libtdtp.parse_bytes(body)
                    process_packet(packet, writer, where_filter)
                    ch.basic_ack(delivery_tag=method.delivery_tag)
                except Exception as exc:
                    log.error("Ошибка обработки сообщения: %s", exc)
                    # nack без requeue чтобы не зациклиться на битом пакете
                    ch.basic_nack(delivery_tag=method.delivery_tag, requeue=False)

            channel.basic_qos(prefetch_count=1)
            channel.basic_consume(queue=RABBITMQ_QUEUE, on_message_callback=on_message)
            channel.start_consuming()

        except Exception as exc:
            log.warning("Соединение потеряно: %s — переподключаемся через 5 с", exc)
            time.sleep(5)


# ─── Точка входа ─────────────────────────────────────────────────────────────

def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="TDTP → Google Sheets consumer")
    p.add_argument("--file",    metavar="PATH", help="читать из файла вместо RabbitMQ")
    p.add_argument("--where",   metavar="EXPR", help="TDTQL фильтр строк, напр. \"Balance > 1000\"")
    p.add_argument("--dry-run", action="store_true", help="не писать в Sheets, только печатать")
    return p.parse_args()


if __name__ == "__main__":
    args = parse_args()

    # Загружаем libtdtp
    libtdtp = LibTDTP(find_libtdtp())

    # Инициализируем writer
    if args.dry_run:
        log.info("Режим dry-run: данные в Sheets не пишутся")
        writer = DryRunWriter()
    else:
        if not SPREADSHEET_ID:
            sys.exit("Укажите SPREADSHEET_ID в переменных окружения")
        if not Path(CREDS_FILE).exists():
            sys.exit(f"Не найден файл credentials: {CREDS_FILE}")
        writer = SheetsWriter(CREDS_FILE, SPREADSHEET_ID, SHEET_NAME)

    # Запуск
    if args.file:
        log.info("Режим файла: %s", args.file)
        packet = libtdtp.read_file(args.file)
        process_packet(packet, writer, args.where or "")
    else:
        run_rabbitmq(writer, args.where or "")
