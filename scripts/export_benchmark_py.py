#!/usr/bin/env python3
"""
Экспорт таблицы из SQLite в TDTP-файлы через Python-библиотеку tdtp.
Схема и структура пакета создаются автоматически через J_from_pandas.
В Python не создаётся ничего вручную — только вызовы библиотечных функций.
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
BATCH_SIZE = 20000
OUTPUT_DIR = "/tmp/benchmark_export_py"


def main():
    os.makedirs(OUTPUT_DIR, exist_ok=True)
    client = TDTPClientJSON()
    conn   = sqlite3.connect(DB_PATH)

    total_rows  = pd.read_sql_query(f"SELECT COUNT(*) AS n FROM {TABLE_NAME}", conn).iloc[0, 0]
    total_parts = (total_rows + BATCH_SIZE - 1) // BATCH_SIZE
    print(f"Строк в таблице: {total_rows:,}  →  {total_parts} файл(ов) по {BATCH_SIZE:,}")

    written_files = []
    for part_num in range(1, total_parts + 1):
        offset = (part_num - 1) * BATCH_SIZE

        df = pd.read_sql_query(
            f"SELECT * FROM {TABLE_NAME} LIMIT {BATCH_SIZE} OFFSET {offset}",
            conn,
        )

        # Библиотека сама строит schema и header из DataFrame
        data = client.J_from_pandas(df, table_name=TABLE_NAME)
        # Сжатие zstd + XXH3 контрольная сумма
        data = client.J_apply_processor(data, "compress", level=3)

        out = os.path.join(OUTPUT_DIR, f"{TABLE_NAME}_part_{part_num}_of_{total_parts}.tdtp.xml")
        client.J_write(data, out)

        size_kb = os.path.getsize(out) / 1024
        print(f"  [{part_num}/{total_parts}] {len(df):>6,} строк  →  {out}  ({size_kb:.0f} KB)")
        written_files.append(out)

    conn.close()

    print(f"\nИтого файлов: {len(written_files)}")
    total_size = sum(os.path.getsize(f) for f in written_files) / (1024 * 1024)
    print(f"Суммарный размер: {total_size:.2f} MB")


if __name__ == "__main__":
    main()
