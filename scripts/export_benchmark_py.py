#!/usr/bin/env python3
"""
Экспорт таблицы из SQLite в TDTP-файлы через Python-библиотеку tdtp.

Все данные передаются в J_export_all одним вызовом — фреймворк сам
разбивает на части по байтовому размеру (~3.8 MB, как в tdtpcli),
применяет zstd-сжатие и XXH3 контрольные суммы для каждой части.
"""

import os
import sqlite3

import pandas as pd

from tdtp import TDTPClientJSON

# ---------------------------------------------------------------------------
# Конфигурация
# ---------------------------------------------------------------------------
DB_PATH    = os.path.join(os.path.dirname(__file__), "..", "benchmark_100k.db")
TABLE_NAME = "Users"
OUTPUT_DIR = "/tmp/benchmark_export_py"


def main():
    os.makedirs(OUTPUT_DIR, exist_ok=True)
    client = TDTPClientJSON()
    conn   = sqlite3.connect(DB_PATH)

    print(f"Читаем таблицу {TABLE_NAME}…")
    df = pd.read_sql_query(f"SELECT * FROM {TABLE_NAME}", conn)
    conn.close()
    print(f"Загружено строк: {len(df):,}")

    # Библиотека строит schema/header из DataFrame автоматически
    data = client.J_from_pandas(df, table_name=TABLE_NAME)

    # Один вызов — фреймворк сам партиционирует, жмёт, добавляет контрольные суммы
    base = os.path.join(OUTPUT_DIR, f"{TABLE_NAME}.tdtp.xml")
    result = client.J_export_all(data, base, compress=True, checksum=True, level=3)

    print(f"\nИтого файлов: {result['total_parts']}")
    total_size = 0
    for i, f in enumerate(result["files"], 1):
        size_kb = os.path.getsize(f) / 1024
        total_size += os.path.getsize(f)
        print(f"  [{i}/{result['total_parts']}] {f}  ({size_kb:.0f} KB)")

    print(f"Суммарный размер: {total_size / (1024 * 1024):.2f} MB")


if __name__ == "__main__":
    main()
