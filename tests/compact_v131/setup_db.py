#!/usr/bin/env python3
"""
setup_db.py — создаёт тестовую SQLite БД для проверки compact-формата TDTP v1.3.1.

Таблицы:
  departments            — справочник отделов (3 записи)
  employees              — сотрудники (15 записей в 3 отделах)
  dept_employees_report  — VIEW (JOIN departments + employees),
                           колонки _dept_id/_dept_name/_location — fixed (prefix)

Использование:
  python3 setup_db.py [path/to/test.db]
"""

import sqlite3
import sys
import os

DB_PATH = sys.argv[1] if len(sys.argv) > 1 else "test.db"


def main():
    if os.path.exists(DB_PATH):
        os.remove(DB_PATH)

    con = sqlite3.connect(DB_PATH)
    cur = con.cursor()

    # ── departments ────────────────────────────────────────────────────────────
    cur.execute("""
        CREATE TABLE departments (
            dept_id   INTEGER PRIMARY KEY,
            dept_name TEXT    NOT NULL,
            location  TEXT    NOT NULL
        )
    """)
    cur.executemany(
        "INSERT INTO departments VALUES (?,?,?)",
        [
            (10, "Sales",       "Moscow"),
            (20, "Engineering", "Saint Petersburg"),
            (30, "HR",          "Kazan"),
        ],
    )

    # ── employees ──────────────────────────────────────────────────────────────
    cur.execute("""
        CREATE TABLE employees (
            emp_id    INTEGER PRIMARY KEY,
            dept_id   INTEGER NOT NULL REFERENCES departments(dept_id),
            full_name TEXT    NOT NULL,
            salary    REAL    NOT NULL,
            hire_date TEXT    NOT NULL
        )
    """)
    cur.executemany(
        "INSERT INTO employees VALUES (?,?,?,?,?)",
        [
            # Sales (dept 10)
            (101, 10, "Ivan Petrov",    45000.00, "2021-03-15"),
            (102, 10, "Anna Sidorova",  52000.00, "2020-07-01"),
            (103, 10, "Boris Kozlov",   48000.00, "2022-01-10"),
            (104, 10, "Elena Novikova", 55000.00, "2019-11-20"),
            (105, 10, "Dmitry Smirnov", 49500.00, "2023-05-05"),
            # Engineering (dept 20)
            (201, 20, "Alice Volkov",   72000.00, "2018-09-01"),
            (202, 20, "Charlie Morozov",65000.00, "2020-02-14"),
            (203, 20, "Diana Popova",   69000.00, "2019-06-30"),
            (204, 20, "Egor Lebedev",   61000.00, "2021-08-22"),
            (205, 20, "Fiona Kuznetsova",78000.00,"2017-04-12"),
            # HR (dept 30)
            (301, 30, "George Orlov",   42000.00, "2022-10-01"),
            (302, 30, "Helen Ivanova",  44000.00, "2021-12-15"),
            (303, 30, "Igor Fedorov",   41000.00, "2023-03-07"),
            (304, 30, "Julia Mikhailova",46000.00,"2020-05-19"),
            (305, 30, "Kirill Sokolov", 43500.00, "2022-07-28"),
        ],
    )

    # ── VIEW: dept_employees_report ─────────────────────────────────────────────
    # Колонки с _ prefix → tdtpcli --compact auto-detect → fixed fields
    cur.execute("""
        CREATE VIEW dept_employees_report AS
        SELECT
            d.dept_id   AS _dept_id,
            d.dept_name AS _dept_name,
            d.location  AS _location,
            e.emp_id,
            e.full_name,
            e.salary,
            e.hire_date
        FROM employees e
        JOIN departments d ON e.dept_id = d.dept_id
        ORDER BY d.dept_id, e.emp_id
    """)

    con.commit()
    con.close()

    print(f"✓ Database created: {DB_PATH}")
    print( "  Tables: departments (3 rows), employees (15 rows)")
    print( "  View:   dept_employees_report (15 rows, _dept_id/_dept_name/_location are fixed)")


if __name__ == "__main__":
    main()
