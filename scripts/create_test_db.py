#!/usr/bin/env python3
"""
TDTP Framework - SQLite Test Database Generator
Создает тестовую БД для integration тестов и примеров
"""

import sqlite3
import os
import sys
from datetime import datetime, timedelta

def create_test_database(db_file="testdata/test.db"):
    """Создает тестовую БД с данными"""
    
    # Создаем директорию если нужно
    os.makedirs(os.path.dirname(db_file), exist_ok=True)
    
    # Удаляем старую БД
    if os.path.exists(db_file):
        os.remove(db_file)
    
    print(f"Creating test database: {db_file}")
    
    # Подключаемся
    conn = sqlite3.connect(db_file)
    cursor = conn.cursor()
    
    # Создаем таблицу Users
    print("Creating table: Users")
    cursor.execute("""
        CREATE TABLE Users (
            ID INTEGER PRIMARY KEY,
            Name TEXT NOT NULL,
            Email TEXT,
            Balance NUMERIC(18,2),
            IsActive INTEGER,
            City TEXT,
            CreatedAt DATETIME,
            LastLoginAt DATETIME
        )
    """)
    
    # Вставляем тестовые данные
    print("Inserting test data...")
    
    users = [
        (1, "John Doe", "john@example.com", 1500.00, 1, "Moscow", "2025-01-15 10:00:00", "2025-11-10 15:30:00"),
        (2, "Jane Smith", "jane@example.com", 2000.00, 1, "SPb", "2025-02-20 11:00:00", "2025-11-12 09:15:00"),
        (3, "Bob Johnson", "bob@example.com", 500.00, 0, "Moscow", "2025-03-10 12:00:00", "2025-10-05 14:20:00"),
        (4, "Alice Brown", "alice@example.com", 2500.00, 1, "Kazan", "2025-01-05 09:00:00", "2025-11-13 11:45:00"),
        (5, "Charlie Davis", "charlie@example.com", 800.00, 1, "SPb", "2025-04-12 13:00:00", "2025-11-11 16:30:00"),
        (6, "Emma Wilson", "emma@example.com", 3000.00, 1, "Moscow", "2024-12-20 10:00:00", "2025-11-14 08:00:00"),
        (7, "Frank Miller", "frank@example.com", 1200.00, 0, "Moscow", "2025-05-18 14:00:00", "2025-09-20 10:10:00"),
        (8, "Grace Lee", "grace@example.com", 1800.00, 1, "SPb", "2025-02-28 15:00:00", "2025-11-13 12:20:00"),
        (9, "Henry Taylor", "henry@example.com", 400.00, 1, "Kazan", "2025-06-01 16:00:00", "2025-11-09 17:30:00"),
        (10, "Ivy Anderson", "ivy@example.com", 2200.00, 1, "Moscow", "2025-01-30 17:00:00", "2025-11-14 07:45:00"),
    ]
    
    cursor.executemany("""
        INSERT INTO Users (ID, Name, Email, Balance, IsActive, City, CreatedAt, LastLoginAt)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    """, users)
    
    # Создаем таблицу Orders
    print("Creating table: Orders")
    cursor.execute("""
        CREATE TABLE Orders (
            OrderID INTEGER PRIMARY KEY,
            UserID INTEGER,
            ProductName TEXT,
            Amount NUMERIC(18,2),
            Status TEXT,
            CreatedAt DATETIME,
            FOREIGN KEY (UserID) REFERENCES Users(ID)
        )
    """)
    
    orders = [
        (1, 1, "Laptop", 1500.00, "completed", "2025-11-01 10:00:00"),
        (2, 2, "Phone", 800.00, "pending", "2025-11-05 11:30:00"),
        (3, 4, "Tablet", 600.00, "completed", "2025-11-03 14:15:00"),
        (4, 6, "Monitor", 400.00, "pending", "2025-11-10 09:20:00"),
        (5, 8, "Keyboard", 100.00, "completed", "2025-11-08 16:45:00"),
        (6, 10, "Mouse", 50.00, "pending", "2025-11-12 12:30:00"),
        (7, 1, "Headphones", 200.00, "cancelled", "2025-11-02 13:00:00"),
        (8, 2, "Webcam", 150.00, "completed", "2025-11-09 10:10:00"),
    ]
    
    cursor.executemany("""
        INSERT INTO Orders (OrderID, UserID, ProductName, Amount, Status, CreatedAt)
        VALUES (?, ?, ?, ?, ?, ?)
    """, orders)
    
    # Создаем таблицу Products
    print("Creating table: Products")
    cursor.execute("""
        CREATE TABLE Products (
            ProductID INTEGER PRIMARY KEY,
            Name TEXT NOT NULL,
            Category TEXT,
            Price NUMERIC(18,2),
            Stock INTEGER,
            IsAvailable INTEGER,
            UpdatedAt DATETIME
        )
    """)
    
    products = [
        (1, "Laptop Pro 15", "Electronics", 1500.00, 10, 1, "2025-11-01 10:00:00"),
        (2, "Smartphone X", "Electronics", 800.00, 25, 1, "2025-11-05 11:00:00"),
        (3, "Tablet Ultra", "Electronics", 600.00, 15, 1, "2025-11-03 12:00:00"),
        (4, "Monitor 27inch", "Electronics", 400.00, 8, 1, "2025-11-10 13:00:00"),
        (5, "Mechanical Keyboard", "Accessories", 100.00, 50, 1, "2025-11-08 14:00:00"),
        (6, "Wireless Mouse", "Accessories", 50.00, 100, 1, "2025-11-12 15:00:00"),
        (7, "USB-C Hub", "Accessories", 80.00, 30, 1, "2025-11-07 16:00:00"),
        (8, "Webcam HD", "Electronics", 150.00, 12, 1, "2025-11-09 17:00:00"),
        (9, "Headphones Pro", "Audio", 200.00, 20, 1, "2025-11-02 18:00:00"),
        (10, "Speakers", "Audio", 300.00, 5, 0, "2025-10-15 19:00:00"),
    ]
    
    cursor.executemany("""
        INSERT INTO Products (ProductID, Name, Category, Price, Stock, IsAvailable, UpdatedAt)
        VALUES (?, ?, ?, ?, ?, ?, ?)
    """, products)
    
    # Commit и закрываем
    conn.commit()
    
    # Показываем статистику
    print("\n" + "="*60)
    print("Database created successfully!")
    print("="*60)
    
    cursor.execute("SELECT COUNT(*) FROM Users")
    users_count = cursor.fetchone()[0]
    print(f"Users: {users_count} records")
    
    cursor.execute("SELECT COUNT(*) FROM Orders")
    orders_count = cursor.fetchone()[0]
    print(f"Orders: {orders_count} records")
    
    cursor.execute("SELECT COUNT(*) FROM Products")
    products_count = cursor.fetchone()[0]
    print(f"Products: {products_count} records")
    
    print("="*60)
    print(f"\nDatabase file: {os.path.abspath(db_file)}")
    print(f"Size: {os.path.getsize(db_file)} bytes")
    print("\nReady for TDTP integration tests!")
    
    conn.close()
    
    return db_file


def show_sample_queries(db_file):
    """Показывает примеры SQL запросов для тестирования"""
    
    print("\n" + "="*60)
    print("Sample SQL queries for testing:")
    print("="*60)
    
    queries = [
        ("Active users with balance > 1000", 
         "SELECT * FROM Users WHERE IsActive = 1 AND Balance > 1000"),
        
        ("Users from Moscow or SPb", 
         "SELECT * FROM Users WHERE City IN ('Moscow', 'SPb')"),
        
        ("Top 3 users by balance", 
         "SELECT * FROM Users ORDER BY Balance DESC LIMIT 3"),
        
        ("Pending orders", 
         "SELECT * FROM Orders WHERE Status = 'pending'"),
        
        ("Products updated after 2025-11-01", 
         "SELECT * FROM Products WHERE UpdatedAt > '2025-11-01'"),
        
        ("Complex: Active users in Moscow with recent login",
         """SELECT * FROM Users 
            WHERE IsActive = 1 
              AND City = 'Moscow' 
              AND LastLoginAt > '2025-11-01'
            ORDER BY Balance DESC"""),
    ]
    
    for title, sql in queries:
        print(f"\n{title}:")
        print(f"  {sql}")
    
    print("\n" + "="*60)
    print("Use these queries with:")
    print("  - examples/query_integration/main.go")
    print("  - pkg/adapters/sqlite/integration_test.go")
    print("="*60)


def verify_database(db_file):
    """Проверяет созданную БД"""
    
    print("\n" + "="*60)
    print("Verifying database...")
    print("="*60)
    
    conn = sqlite3.connect(db_file)
    cursor = conn.cursor()
    
    # Проверяем таблицы
    cursor.execute("SELECT name FROM sqlite_master WHERE type='table'")
    tables = cursor.fetchall()
    print(f"\nTables: {[t[0] for t in tables]}")
    
    # Проверяем schema Users
    print("\nUsers table schema:")
    cursor.execute("PRAGMA table_info(Users)")
    for row in cursor.fetchall():
        print(f"  {row[1]:15} {row[2]:10} {'PK' if row[5] else ''}")
    
    # Показываем примеры данных
    print("\nSample data (first 3 users):")
    cursor.execute("SELECT ID, Name, Balance, IsActive, City FROM Users LIMIT 3")
    for row in cursor.fetchall():
        print(f"  ID={row[0]}, Name={row[1]}, Balance={row[2]}, Active={row[3]}, City={row[4]}")
    
    conn.close()
    
    print("\n✅ Database verification complete!")


def main():
    """Main function"""
    
    print("""
╔══════════════════════════════════════════════════════════════╗
║       TDTP Framework - Test Database Generator              ║
║                   SQLite + Python                            ║
╚══════════════════════════════════════════════════════════════╝
    """)
    
    # Определяем путь к БД
    if len(sys.argv) > 1:
        db_file = sys.argv[1]
    else:
        # По умолчанию создаем в testdata/
        script_dir = os.path.dirname(os.path.abspath(__file__))
        db_file = os.path.join(script_dir, "testdata", "test.db")
    
    try:
        # Создаем БД
        db_file = create_test_database(db_file)
        
        # Проверяем
        verify_database(db_file)
        
        # Показываем примеры
        show_sample_queries(db_file)
        
        print("\n" + "="*60)
        print("✅ SUCCESS! Test database is ready.")
        print("="*60)
        print("\nNext steps:")
        print("1. Run integration tests:")
        print("   cd pkg/adapters/sqlite")
        print("   go test -v")
        print()
        print("2. Run query integration example:")
        print("   cd examples/query_integration")
        print("   go run main.go")
        print()
        print("3. Try TDTP export:")
        print("   cd examples/sqlite")
        print("   go run main.go")
        print("="*60)
        
    except Exception as e:
        print(f"\n❌ Error: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)


if __name__ == "__main__":
    main()
