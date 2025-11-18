#!/usr/bin/env python3
"""
Генератор тестовой SQLite базы данных для тестирования сжатия TDTP.
Создает таблицу users с 10,000 записей.
"""

import sqlite3
import random
import string
from datetime import datetime, timedelta

# Конфигурация
DB_FILE = "test_data.db"
NUM_RECORDS = 10000

# Данные для генерации
FIRST_NAMES = ["Александр", "Мария", "Иван", "Елена", "Дмитрий", "Анна", "Сергей", "Ольга",
               "Андрей", "Наталья", "Михаил", "Татьяна", "Алексей", "Екатерина", "Владимир"]
LAST_NAMES = ["Иванов", "Петров", "Сидоров", "Козлов", "Новиков", "Морозов", "Волков",
              "Соколов", "Лебедев", "Козлова", "Новикова", "Морозова", "Волкова", "Соколова"]
DOMAINS = ["gmail.com", "yandex.ru", "mail.ru", "outlook.com", "company.ru"]
CITIES = ["Москва", "Санкт-Петербург", "Новосибирск", "Екатеринбург", "Казань",
          "Нижний Новгород", "Челябинск", "Самара", "Омск", "Ростов-на-Дону"]
STATUSES = ["active", "inactive", "pending", "blocked"]

def generate_email(first_name, last_name, idx):
    """Генерирует email на основе имени"""
    transliterate = {
        'а': 'a', 'б': 'b', 'в': 'v', 'г': 'g', 'д': 'd', 'е': 'e', 'ё': 'e',
        'ж': 'zh', 'з': 'z', 'и': 'i', 'й': 'y', 'к': 'k', 'л': 'l', 'м': 'm',
        'н': 'n', 'о': 'o', 'п': 'p', 'р': 'r', 'с': 's', 'т': 't', 'у': 'u',
        'ф': 'f', 'х': 'h', 'ц': 'ts', 'ч': 'ch', 'ш': 'sh', 'щ': 'sch',
        'ъ': '', 'ы': 'y', 'ь': '', 'э': 'e', 'ю': 'yu', 'я': 'ya'
    }

    def transliterate_text(text):
        result = ""
        for char in text.lower():
            result += transliterate.get(char, char)
        return result

    name = transliterate_text(first_name)
    surname = transliterate_text(last_name)
    domain = random.choice(DOMAINS)

    return f"{name}.{surname}{idx}@{domain}"

def generate_phone():
    """Генерирует случайный номер телефона"""
    return f"+7{random.randint(900, 999)}{random.randint(1000000, 9999999)}"

def generate_date(start_year=2020, end_year=2024):
    """Генерирует случайную дату"""
    start = datetime(start_year, 1, 1)
    end = datetime(end_year, 12, 31)
    delta = end - start
    random_days = random.randint(0, delta.days)
    return (start + timedelta(days=random_days)).strftime("%Y-%m-%d %H:%M:%S")

def generate_description():
    """Генерирует случайное описание"""
    words = ["разработчик", "менеджер", "аналитик", "дизайнер", "тестировщик",
             "архитектор", "консультант", "инженер", "специалист", "эксперт"]
    levels = ["junior", "middle", "senior", "lead", "principal"]
    departments = ["IT", "HR", "Sales", "Marketing", "Finance", "Operations"]

    return f"{random.choice(levels)} {random.choice(words)}, отдел {random.choice(departments)}"

def main():
    print(f"Создание базы данных: {DB_FILE}")
    print(f"Количество записей: {NUM_RECORDS}")

    # Удаляем старую базу если есть
    import os
    if os.path.exists(DB_FILE):
        os.remove(DB_FILE)

    # Создаем подключение
    conn = sqlite3.connect(DB_FILE)
    cursor = conn.cursor()

    # Создаем таблицу
    cursor.execute("""
        CREATE TABLE users (
            id INTEGER PRIMARY KEY,
            first_name TEXT NOT NULL,
            last_name TEXT NOT NULL,
            email TEXT NOT NULL UNIQUE,
            phone TEXT,
            city TEXT,
            status TEXT DEFAULT 'active',
            balance REAL DEFAULT 0.0,
            created_at TEXT NOT NULL,
            updated_at TEXT NOT NULL,
            description TEXT
        )
    """)

    # Создаем индексы
    cursor.execute("CREATE INDEX idx_users_email ON users(email)")
    cursor.execute("CREATE INDEX idx_users_status ON users(status)")
    cursor.execute("CREATE INDEX idx_users_city ON users(city)")

    print("Генерация данных...")

    # Генерируем данные
    records = []
    for i in range(1, NUM_RECORDS + 1):
        first_name = random.choice(FIRST_NAMES)
        last_name = random.choice(LAST_NAMES)
        email = generate_email(first_name, last_name, i)
        phone = generate_phone()
        city = random.choice(CITIES)
        status = random.choice(STATUSES)
        balance = round(random.uniform(0, 100000), 2)
        created_at = generate_date(2020, 2023)
        updated_at = generate_date(2023, 2024)
        description = generate_description()

        records.append((
            i, first_name, last_name, email, phone, city,
            status, balance, created_at, updated_at, description
        ))

        if i % 1000 == 0:
            print(f"  Сгенерировано {i} записей...")

    # Вставляем данные
    cursor.executemany("""
        INSERT INTO users (id, first_name, last_name, email, phone, city,
                          status, balance, created_at, updated_at, description)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    """, records)

    conn.commit()

    # Проверяем результат
    cursor.execute("SELECT COUNT(*) FROM users")
    count = cursor.fetchone()[0]

    # Получаем размер файла
    conn.close()
    file_size = os.path.getsize(DB_FILE)

    print(f"\n✓ База данных создана успешно!")
    print(f"  Записей: {count}")
    print(f"  Размер файла: {file_size / 1024:.1f} KB")

    # Показываем пример данных
    conn = sqlite3.connect(DB_FILE)
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM users LIMIT 3")
    print("\nПример данных:")
    for row in cursor.fetchall():
        print(f"  {row}")

    conn.close()
    print(f"\nГотово! Файл: {DB_FILE}")

if __name__ == "__main__":
    main()
