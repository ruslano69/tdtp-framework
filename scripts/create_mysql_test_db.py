#!/usr/bin/env python3
"""
Create MySQL test database for TDTP CLI integration tests.

Tables created:
  users          — 10 rows, basic types
  orders         — 8 rows, FK to users
  products       — 10 rows
  complex_fields — 5 rows, column names with spaces and $ % ?
  ERP$Entry      — 6 rows, table name with $, column names with spaces

Usage:
    python3 scripts/create_mysql_test_db.py
    MYSQL_HOST=localhost MYSQL_PORT=3306 python3 scripts/create_mysql_test_db.py
"""

import os
import sys

try:
    import pymysql
except ImportError:
    print("ERROR: pip install pymysql")
    sys.exit(1)

MYSQL_HOST = os.environ.get("MYSQL_HOST", "localhost")
MYSQL_PORT = int(os.environ.get("MYSQL_PORT", "3306"))
MYSQL_USER = os.environ.get("MYSQL_USER", "tdtp_test")
MYSQL_PASS = os.environ.get("MYSQL_PASS", "tdtp_test_password")
MYSQL_DB   = os.environ.get("MYSQL_DB",   "tdtp_test_db")


def run(conn, sql, params=None):
    with conn.cursor() as cur:
        if params:
            cur.execute(sql, params)
        else:
            cur.execute(sql)
    conn.commit()


def main():
    conn = pymysql.connect(
        host=MYSQL_HOST, port=MYSQL_PORT,
        user=MYSQL_USER, password=MYSQL_PASS,
        database=MYSQL_DB, charset="utf8mb4",
        autocommit=False,
    )
    print(f"Connected to MySQL {MYSQL_HOST}:{MYSQL_PORT}/{MYSQL_DB}")

    # ── users ─────────────────────────────────────────────────────────────────
    print("Creating table: users...")
    run(conn, "DROP TABLE IF EXISTS users")
    run(conn, """
        CREATE TABLE users (
            ID       INT          PRIMARY KEY,
            Name     VARCHAR(100) NOT NULL,
            Email    VARCHAR(200),
            Balance  DECIMAL(18,2),
            IsActive TINYINT(1),
            City     VARCHAR(100),
            CreatedAt  DATETIME,
            LastLoginAt DATETIME
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
    """)
    users = [
        (1,  "John Doe",      "john@example.com",    1500.00, 1, "Moscow", "2025-01-15 10:00:00", "2025-11-10 15:30:00"),
        (2,  "Jane Smith",    "jane@example.com",    2000.00, 1, "SPb",    "2025-02-20 11:00:00", "2025-11-12 09:15:00"),
        (3,  "Bob Johnson",   "bob@example.com",      500.00, 0, "Moscow", "2025-03-10 12:00:00", "2025-10-05 14:20:00"),
        (4,  "Alice Brown",   "alice@example.com",   2500.00, 1, "Kazan",  "2025-01-05 09:00:00", "2025-11-13 11:45:00"),
        (5,  "Charlie Davis", "charlie@example.com",  800.00, 1, "SPb",    "2025-04-12 13:00:00", "2025-11-11 16:30:00"),
        (6,  "Emma Wilson",   "emma@example.com",    3000.00, 1, "Moscow", "2024-12-20 10:00:00", "2025-11-14 08:00:00"),
        (7,  "Frank Miller",  "frank@example.com",   1200.00, 0, "Moscow", "2025-05-18 14:00:00", "2025-09-20 10:10:00"),
        (8,  "Grace Lee",     "grace@example.com",   1800.00, 1, "SPb",    "2025-02-28 15:00:00", "2025-11-13 12:20:00"),
        (9,  "Henry Taylor",  "henry@example.com",    400.00, 1, "Kazan",  "2025-06-01 16:00:00", "2025-11-09 17:30:00"),
        (10, "Ivy Anderson",  "ivy@example.com",     2200.00, 1, "Moscow", "2025-01-30 17:00:00", "2025-11-14 07:45:00"),
    ]
    with conn.cursor() as cur:
        cur.executemany(
            "INSERT INTO users VALUES (%s,%s,%s,%s,%s,%s,%s,%s)", users)
    conn.commit()

    # ── orders ────────────────────────────────────────────────────────────────
    print("Creating table: orders...")
    run(conn, "DROP TABLE IF EXISTS orders")
    run(conn, """
        CREATE TABLE orders (
            OrderID     INT          PRIMARY KEY,
            UserID      INT,
            ProductName VARCHAR(200),
            Amount      DECIMAL(18,2),
            Status      VARCHAR(50),
            CreatedAt   DATETIME
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
    """)
    orders = [
        (1, 1, "Laptop",     1500.00, "completed", "2025-11-01 10:00:00"),
        (2, 2, "Phone",       800.00, "pending",   "2025-11-05 11:30:00"),
        (3, 4, "Tablet",      600.00, "completed", "2025-11-03 14:15:00"),
        (4, 6, "Monitor",     400.00, "pending",   "2025-11-10 09:20:00"),
        (5, 8, "Keyboard",    100.00, "completed", "2025-11-08 16:45:00"),
        (6, 10, "Mouse",       50.00, "pending",   "2025-11-12 12:30:00"),
        (7, 1, "Headphones",  200.00, "cancelled", "2025-11-02 13:00:00"),
        (8, 2, "Webcam",      150.00, "completed", "2025-11-09 10:10:00"),
    ]
    with conn.cursor() as cur:
        cur.executemany(
            "INSERT INTO orders VALUES (%s,%s,%s,%s,%s,%s)", orders)
    conn.commit()

    # ── products ──────────────────────────────────────────────────────────────
    print("Creating table: products...")
    run(conn, "DROP TABLE IF EXISTS products")
    run(conn, """
        CREATE TABLE products (
            ProductID   INT          PRIMARY KEY,
            Name        VARCHAR(200) NOT NULL,
            Category    VARCHAR(100),
            Price       DECIMAL(18,2),
            Stock       INT,
            IsAvailable TINYINT(1),
            UpdatedAt   DATETIME
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
    """)
    products = [
        (1,  "Laptop Pro 15",       "Electronics",  1500.00, 10,  1, "2025-11-01 10:00:00"),
        (2,  "Smartphone X",        "Electronics",   800.00, 25,  1, "2025-11-05 11:00:00"),
        (3,  "Tablet Ultra",        "Electronics",   600.00, 15,  1, "2025-11-03 12:00:00"),
        (4,  "Monitor 27inch",      "Electronics",   400.00,  8,  1, "2025-11-10 13:00:00"),
        (5,  "Mechanical Keyboard", "Accessories",   100.00, 50,  1, "2025-11-08 14:00:00"),
        (6,  "Wireless Mouse",      "Accessories",    50.00, 100, 1, "2025-11-12 15:00:00"),
        (7,  "USB-C Hub",           "Accessories",    80.00, 30,  1, "2025-11-07 16:00:00"),
        (8,  "Webcam HD",           "Electronics",   150.00, 12,  1, "2025-11-09 17:00:00"),
        (9,  "Headphones Pro",      "Audio",         200.00, 20,  1, "2025-11-02 18:00:00"),
        (10, "Speakers",            "Audio",         300.00,  5,  0, "2025-10-15 19:00:00"),
    ]
    with conn.cursor() as cur:
        cur.executemany(
            "INSERT INTO products VALUES (%s,%s,%s,%s,%s,%s,%s)", products)
    conn.commit()

    # ── complex_fields ────────────────────────────────────────────────────────
    print("Creating table: complex_fields (spaces and $ % ? in column names)...")
    run(conn, "DROP TABLE IF EXISTS `complex_fields`")
    run(conn, """
        CREATE TABLE `complex_fields` (
            `Order ID`       INT          PRIMARY KEY,
            `Customer Name`  VARCHAR(100),
            `Total Cost $`   DECIMAL(12,2),
            `Discount %`     DECIMAL(5,2),
            `Is Active?`     TINYINT(1)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
    """)
    with conn.cursor() as cur:
        cur.executemany(
            "INSERT INTO `complex_fields` VALUES (%s,%s,%s,%s,%s)", [
                (1, "Alice",  150.00, 0.10, 1),
                (2, "Bob",    200.00, 0.00, 1),
                (3, "Carol",   80.00, 0.20, 0),
                (4, "Dave",   320.00, 0.05, 1),
                (5, "Eve",     50.00, 0.00, 0),
            ])
    conn.commit()

    # ── ERP$Entry ─────────────────────────────────────────────────────────────
    print("Creating table: `ERP$Entry` ($ in table name, spaces in column names)...")
    run(conn, "DROP TABLE IF EXISTS `ERP$Entry`")
    run(conn, """
        CREATE TABLE `ERP$Entry` (
            `No_`           INT  PRIMARY KEY,
            `Document Type` INT,
            `Posting Date`  DATE,
            `Description`   VARCHAR(200),
            `Amount`        DECIMAL(14,2)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
    """)
    with conn.cursor() as cur:
        cur.executemany(
            "INSERT INTO `ERP$Entry` VALUES (%s,%s,%s,%s,%s)", [
                (1, 1, "2025-01-10", "Invoice payment",  1200.00),
                (2, 2, "2025-01-15", "Credit memo",       -300.00),
                (3, 1, "2025-02-03", "Invoice payment",   800.00),
                (4, 3, "2025-02-20", "Reminder fee",        15.00),
                (5, 1, "2025-03-01", "Invoice payment",  2500.00),
                (6, 2, "2025-03-10", "Credit memo",       -150.00),
            ])
    conn.commit()

    conn.close()

    print("\nDatabase ready. Config for tests:")
    print(f"""
database:
  type: mysql
  host: {MYSQL_HOST}
  port: {MYSQL_PORT}
  user: {MYSQL_USER}
  password: {MYSQL_PASS}
  database: {MYSQL_DB}
""")
    print("Tables: users(10) orders(8) products(10) complex_fields(5) ERP$Entry(6)")


if __name__ == "__main__":
    main()
