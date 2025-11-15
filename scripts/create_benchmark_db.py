#!/usr/bin/env python3
"""
Скрипт генерации тестовой БД для бенчмаркинга TDTP Framework
Создает SQLite БД с 100k записей для тестирования производительности
"""

import sqlite3
import random
import sys
from datetime import datetime, timedelta

# Конфигурация
DB_FILE = "benchmark_100k.db"
TOTAL_RECORDS = 100000

# Списки для генерации данных
FIRST_NAMES = [
    "Alexander", "Dmitry", "Sergey", "Alexey", "Ivan", "Mikhail", "Andrey",
    "Vladimir", "Nikolay", "Pavel", "Elena", "Olga", "Tatiana", "Maria", "Anna",
    "Natalia", "Irina", "Svetlana", "Ekaterina", "Victoria"
]

LAST_NAMES = [
    "Ivanov", "Petrov", "Sidorov", "Smirnov", "Kuznetsov", "Popov", "Volkov",
    "Sokolov", "Lebedev", "Kozlov", "Novikov", "Morozov", "Pavlov", "Fedorov",
    "Mikhailov", "Andreev", "Alekseev", "Dmitriev", "Egorov", "Stepanov"
]

CITIES = [
    "Moscow", "Saint Petersburg", "Novosibirsk", "Yekaterinburg", "Kazan",
    "Nizhny Novgorod", "Chelyabinsk", "Samara", "Omsk", "Rostov-on-Don",
    "Ufa", "Krasnoyarsk", "Voronezh", "Perm", "Volgograd"
]

DOMAINS = ["mail.ru", "gmail.com", "yandex.ru", "outlook.com", "inbox.ru"]


def generate_name():
    """Генерирует случайное имя"""
    first = random.choice(FIRST_NAMES)
    last = random.choice(LAST_NAMES)
    return f"{first} {last}"


def generate_email(name, idx):
    """Генерирует email на основе имени"""
    parts = name.lower().split()
    domain = random.choice(DOMAINS)
    # Используем индекс записи для гарантии уникальности
    return f"{parts[0]}.{parts[1]}.{idx}@{domain}"


def generate_balance():
    """Генерирует баланс с реалистичным распределением"""
    # 70% - положительный баланс
    # 20% - около нуля
    # 10% - отрицательный
    rand = random.random()
    if rand < 0.7:
        return round(random.uniform(100, 100000), 2)
    elif rand < 0.9:
        return round(random.uniform(-100, 100), 2)
    else:
        return round(random.uniform(-10000, -100), 2)


def generate_date():
    """Генерирует дату регистрации за последние 5 лет"""
    days_ago = random.randint(0, 365 * 5)
    date = datetime.now() - timedelta(days=days_ago)
    return date.strftime("%Y-%m-%d %H:%M:%S")


def create_database():
    """Создает БД и таблицу"""
    print(f"Создание БД: {DB_FILE}")
    
    conn = sqlite3.connect(DB_FILE)
    cursor = conn.cursor()
    
    # Удаляем таблицу если существует
    cursor.execute("DROP TABLE IF EXISTS Users")
    
    # Создаем таблицу
    cursor.execute("""
        CREATE TABLE Users (
            ID INTEGER PRIMARY KEY AUTOINCREMENT,
            Name TEXT NOT NULL,
            Email TEXT NOT NULL UNIQUE,
            City TEXT NOT NULL,
            Balance REAL NOT NULL,
            IsActive INTEGER NOT NULL,
            RegisteredAt TEXT NOT NULL
        )
    """)
    
    # Создаем индексы для ускорения запросов
    cursor.execute("CREATE INDEX idx_balance ON Users(Balance)")
    cursor.execute("CREATE INDEX idx_city ON Users(City)")
    cursor.execute("CREATE INDEX idx_active ON Users(IsActive)")
    cursor.execute("CREATE INDEX idx_registered ON Users(RegisteredAt)")
    
    conn.commit()
    print("✓ Таблица Users создана")
    print("✓ Индексы созданы")
    
    return conn


def insert_records(conn, batch_size=1000):
    """Вставляет записи пакетами"""
    cursor = conn.cursor()
    
    print(f"\nГенерация {TOTAL_RECORDS:,} записей...")
    print("Прогресс: ", end="", flush=True)
    
    records = []
    for i in range(1, TOTAL_RECORDS + 1):
        name = generate_name()
        email = generate_email(name, i)
        city = random.choice(CITIES)
        balance = generate_balance()
        is_active = 1 if random.random() < 0.7 else 0  # 70% активных
        registered_at = generate_date()
        
        records.append((name, email, city, balance, is_active, registered_at))
        
        # Вставка пакетами
        if len(records) >= batch_size:
            try:
                cursor.executemany(
                    "INSERT INTO Users (Name, Email, City, Balance, IsActive, RegisteredAt) VALUES (?, ?, ?, ?, ?, ?)",
                    records
                )
                conn.commit()
                records = []
                
                # Прогресс-бар
                progress = int((i / TOTAL_RECORDS) * 50)
                print(f"\rПрогресс: [{'=' * progress}{' ' * (50 - progress)}] {i:,}/{TOTAL_RECORDS:,}", end="", flush=True)
            
            except sqlite3.IntegrityError:
                # Пропускаем дубликаты email
                records = []
    
    # Вставка остатка
    if records:
        try:
            cursor.executemany(
                "INSERT INTO Users (Name, Email, City, Balance, IsActive, RegisteredAt) VALUES (?, ?, ?, ?, ?, ?)",
                records
            )
            conn.commit()
        except sqlite3.IntegrityError:
            pass
    
    print("\n✓ Записи вставлены")


def print_statistics(conn):
    """Выводит статистику по БД"""
    cursor = conn.cursor()
    
    print("\n" + "=" * 60)
    print("СТАТИСТИКА БД")
    print("=" * 60)
    
    # Общее количество
    cursor.execute("SELECT COUNT(*) FROM Users")
    total = cursor.fetchone()[0]
    print(f"Всего записей: {total:,}")
    
    # Активные/неактивные
    cursor.execute("SELECT COUNT(*) FROM Users WHERE IsActive = 1")
    active = cursor.fetchone()[0]
    print(f"Активных: {active:,} ({active/total*100:.1f}%)")
    print(f"Неактивных: {total - active:,} ({(total-active)/total*100:.1f}%)")
    
    # Баланс
    cursor.execute("SELECT MIN(Balance), MAX(Balance), AVG(Balance) FROM Users")
    min_bal, max_bal, avg_bal = cursor.fetchone()
    print(f"\nБаланс:")
    print(f"  Минимум: {min_bal:,.2f}")
    print(f"  Максимум: {max_bal:,.2f}")
    print(f"  Средний: {avg_bal:,.2f}")
    
    # По городам
    cursor.execute("SELECT City, COUNT(*) FROM Users GROUP BY City ORDER BY COUNT(*) DESC LIMIT 5")
    print(f"\nТоп-5 городов:")
    for city, count in cursor.fetchall():
        print(f"  {city}: {count:,}")
    
    # Размер файла
    cursor.execute("SELECT page_count * page_size as size FROM pragma_page_count(), pragma_page_size()")
    size_bytes = cursor.fetchone()[0]
    size_mb = size_bytes / (1024 * 1024)
    print(f"\nРазмер БД: {size_mb:.2f} MB")
    
    print("=" * 60)


def main():
    """Главная функция"""
    print("=" * 60)
    print("ГЕНЕРАТОР ТЕСТОВОЙ БД ДЛЯ TDTP BENCHMARK")
    print("=" * 60)
    
    try:
        # Создание БД
        conn = create_database()
        
        # Вставка данных
        insert_records(conn)
        
        # Статистика
        print_statistics(conn)
        
        conn.close()
        
        # Вакуум для оптимизации (после закрытия)
        print("\nОптимизация БД...")
        conn2 = sqlite3.connect(DB_FILE)
        conn2.execute("VACUUM")
        conn2.close()
        print("✓ БД оптимизирована")
        
        print(f"\n{'=' * 60}")
        print(f"✓ ГОТОВО! БД сохранена в: {DB_FILE}")
        print(f"{'=' * 60}")
        
        print("\nДля использования в бенчмарках:")
        print(f"  adapter, _ := sqlite.NewAdapter(\"{DB_FILE}\")")
        
        return 0
        
    except Exception as e:
        print(f"\n✗ ОШИБКА: {e}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())