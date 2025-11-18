#!/usr/bin/env python3
"""
Генератор тестовой SQLite базы данных для тестирования сжатия TDTP.
Создает таблицу users с 100,000 записей.
"""

import sqlite3
import random
import string
from datetime import datetime, timedelta

# Конфигурация
DB_FILE = "test_data.db"
NUM_RECORDS = 100000

# Данные для генерации - расширенные списки
MALE_NAMES = [
    "Александр", "Иван", "Дмитрий", "Сергей", "Андрей", "Михаил", "Алексей",
    "Владимир", "Николай", "Павел", "Артём", "Максим", "Евгений", "Денис",
    "Роман", "Виктор", "Олег", "Игорь", "Константин", "Антон", "Кирилл",
    "Георгий", "Фёдор", "Григорий", "Валерий", "Борис", "Пётр", "Юрий",
    "Вячеслав", "Станислав", "Тимур", "Артур", "Руслан", "Эдуард", "Леонид"
]

FEMALE_NAMES = [
    "Мария", "Елена", "Анна", "Ольга", "Наталья", "Татьяна", "Екатерина",
    "Ирина", "Светлана", "Виктория", "Юлия", "Дарья", "Алина", "Полина",
    "Ксения", "Валентина", "Галина", "Людмила", "Надежда", "Вера", "Любовь",
    "Марина", "Оксана", "Евгения", "Анастасия", "Александра", "Кристина",
    "Варвара", "Софья", "Елизавета", "Диана", "Карина", "Маргарита", "Жанна"
]

MALE_LAST_NAMES = [
    "Иванов", "Петров", "Сидоров", "Козлов", "Новиков", "Морозов", "Волков",
    "Соколов", "Лебедев", "Смирнов", "Кузнецов", "Попов", "Васильев", "Павлов",
    "Семёнов", "Голубев", "Виноградов", "Богданов", "Воробьёв", "Фёдоров",
    "Михайлов", "Беляев", "Тарасов", "Белов", "Комаров", "Орлов", "Киселёв",
    "Макаров", "Андреев", "Ковалёв", "Ильин", "Гусев", "Титов", "Кудрявцев",
    "Баранов", "Куликов", "Алексеев", "Степанов", "Яковлев", "Сорокин"
]

FEMALE_LAST_NAMES = [
    "Иванова", "Петрова", "Сидорова", "Козлова", "Новикова", "Морозова", "Волкова",
    "Соколова", "Лебедева", "Смирнова", "Кузнецова", "Попова", "Васильева", "Павлова",
    "Семёнова", "Голубева", "Виноградова", "Богданова", "Воробьёва", "Фёдорова",
    "Михайлова", "Беляева", "Тарасова", "Белова", "Комарова", "Орлова", "Киселёва",
    "Макарова", "Андреева", "Ковалёва", "Ильина", "Гусева", "Титова", "Кудрявцева",
    "Баранова", "Куликова", "Алексеева", "Степанова", "Яковлева", "Сорокина"
]

DOMAINS = [
    "gmail.com", "yandex.ru", "mail.ru", "outlook.com", "company.ru",
    "rambler.ru", "inbox.ru", "bk.ru", "list.ru", "hotmail.com",
    "yahoo.com", "protonmail.com", "icloud.com", "zoho.com"
]

CITIES = [
    "Москва", "Санкт-Петербург", "Новосибирск", "Екатеринбург", "Казань",
    "Нижний Новгород", "Челябинск", "Самара", "Омск", "Ростов-на-Дону",
    "Уфа", "Красноярск", "Воронеж", "Пермь", "Волгоград", "Краснодар",
    "Саратов", "Тюмень", "Тольятти", "Ижевск", "Барнаул", "Ульяновск",
    "Иркутск", "Хабаровск", "Ярославль", "Владивосток", "Махачкала",
    "Томск", "Оренбург", "Кемерово", "Новокузнецк", "Рязань", "Астрахань",
    "Набережные Челны", "Пенза", "Липецк", "Киров", "Чебоксары", "Тула"
]

STATUSES = ["active", "inactive", "pending", "blocked", "verified", "suspended"]

MARITAL_STATUSES = ["холост", "женат", "не замужем", "замужем", "разведён", "разведена", "вдовец", "вдова"]

INSURANCE_REGIONS = [
    "77", "78", "50", "47", "23", "61", "16", "63", "52", "54",
    "66", "74", "02", "24", "34", "36", "38", "42", "55", "59"
]

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

    # Разнообразие форматов email
    format_type = random.randint(1, 5)
    if format_type == 1:
        return f"{name}.{surname}{idx}@{domain}"
    elif format_type == 2:
        return f"{surname}.{name}{idx}@{domain}"
    elif format_type == 3:
        return f"{name}{idx}@{domain}"
    elif format_type == 4:
        return f"{name}_{surname}{idx}@{domain}"
    else:
        return f"{name[0]}{surname}{idx}@{domain}"

def generate_phone():
    """Генерирует случайный номер телефона"""
    operators = [900, 901, 902, 903, 904, 905, 906, 908, 909, 910, 911, 912, 913, 914, 915, 916, 917, 918, 919, 920, 921, 922, 923, 924, 925, 926, 927, 928, 929, 930, 931, 932, 933, 934, 936, 937, 938, 939, 950, 951, 952, 953, 958, 960, 961, 962, 963, 964, 965, 966, 967, 968, 969, 977, 978, 980, 981, 982, 983, 984, 985, 986, 987, 988, 989, 991, 992, 993, 994, 995, 996, 997, 999]
    return f"+7{random.choice(operators)}{random.randint(1000000, 9999999)}"

def generate_date(start_year=2020, end_year=2024):
    """Генерирует случайную дату"""
    start = datetime(start_year, 1, 1)
    end = datetime(end_year, 12, 31)
    delta = end - start
    random_days = random.randint(0, delta.days)
    return (start + timedelta(days=random_days)).strftime("%Y-%m-%d %H:%M:%S")

def generate_birth_date():
    """Генерирует дату рождения (возраст от 18 до 80 лет)"""
    today = datetime.now()
    min_age = 18
    max_age = 80

    days_in_range = (max_age - min_age) * 365
    random_days = random.randint(min_age * 365, max_age * 365)
    birth_date = today - timedelta(days=random_days)

    return birth_date.strftime("%Y-%m-%d")

def generate_inn():
    """Генерирует случайный ИНН (12 цифр для физ. лица)"""
    # Код региона (первые 2 цифры)
    region = random.randint(1, 92)
    # Код налоговой (следующие 2 цифры)
    inspection = random.randint(1, 99)
    # Номер записи (6 цифр)
    record = random.randint(100000, 999999)
    # Контрольные цифры (2 цифры)
    control = random.randint(10, 99)

    return f"{region:02d}{inspection:02d}{record:06d}{control:02d}"

def generate_insurance_policy():
    """Генерирует номер страхового полиса (формат: XXXX XXXXXX)"""
    series = random.randint(1000, 9999)
    number = random.randint(100000, 999999)
    return f"{series} {number}"

def generate_description():
    """Генерирует случайное описание"""
    words = [
        "разработчик", "менеджер", "аналитик", "дизайнер", "тестировщик",
        "архитектор", "консультант", "инженер", "специалист", "эксперт",
        "администратор", "координатор", "руководитель", "директор", "бухгалтер",
        "юрист", "маркетолог", "логист", "экономист", "переводчик"
    ]
    levels = ["junior", "middle", "senior", "lead", "principal", "chief", "head"]
    departments = [
        "IT", "HR", "Sales", "Marketing", "Finance", "Operations",
        "Legal", "Support", "R&D", "QA", "Security", "Analytics"
    ]

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

    # Создаем таблицу с новыми полями
    cursor.execute("""
        CREATE TABLE users (
            id INTEGER PRIMARY KEY,
            first_name TEXT NOT NULL,
            last_name TEXT NOT NULL,
            gender TEXT NOT NULL,
            birth_date TEXT NOT NULL,
            email TEXT NOT NULL UNIQUE,
            phone TEXT,
            inn TEXT NOT NULL,
            insurance_policy TEXT NOT NULL,
            city TEXT,
            marital_status TEXT,
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
    cursor.execute("CREATE INDEX idx_users_inn ON users(inn)")
    cursor.execute("CREATE INDEX idx_users_gender ON users(gender)")
    cursor.execute("CREATE INDEX idx_users_marital ON users(marital_status)")

    print("Генерация данных...")

    # Генерируем данные
    records = []
    for i in range(1, NUM_RECORDS + 1):
        # Определяем пол
        gender = random.choice(["М", "Ж"])

        if gender == "М":
            first_name = random.choice(MALE_NAMES)
            last_name = random.choice(MALE_LAST_NAMES)
            marital_status = random.choice(["холост", "женат", "разведён", "вдовец"])
        else:
            first_name = random.choice(FEMALE_NAMES)
            last_name = random.choice(FEMALE_LAST_NAMES)
            marital_status = random.choice(["не замужем", "замужем", "разведена", "вдова"])

        birth_date = generate_birth_date()
        email = generate_email(first_name, last_name, i)
        phone = generate_phone()
        inn = generate_inn()
        insurance_policy = generate_insurance_policy()
        city = random.choice(CITIES)
        status = random.choice(STATUSES)
        balance = round(random.uniform(0, 500000), 2)
        created_at = generate_date(2018, 2023)
        updated_at = generate_date(2023, 2024)
        description = generate_description()

        records.append((
            i, first_name, last_name, gender, birth_date, email, phone,
            inn, insurance_policy, city, marital_status, status, balance,
            created_at, updated_at, description
        ))

        if i % 10000 == 0:
            print(f"  Сгенерировано {i} записей...")

    # Вставляем данные
    cursor.executemany("""
        INSERT INTO users (id, first_name, last_name, gender, birth_date, email, phone,
                          inn, insurance_policy, city, marital_status, status, balance,
                          created_at, updated_at, description)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
    print(f"  Размер файла: {file_size / 1024 / 1024:.1f} MB")

    # Показываем пример данных
    conn = sqlite3.connect(DB_FILE)
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM users LIMIT 3")
    print("\nПример данных:")
    for row in cursor.fetchall():
        print(f"  {row}")

    # Статистика по полям
    print("\nСтатистика:")
    cursor.execute("SELECT gender, COUNT(*) FROM users GROUP BY gender")
    for row in cursor.fetchall():
        print(f"  Пол {row[0]}: {row[1]}")

    cursor.execute("SELECT marital_status, COUNT(*) FROM users GROUP BY marital_status ORDER BY COUNT(*) DESC LIMIT 5")
    print("  Семейное положение (топ 5):")
    for row in cursor.fetchall():
        print(f"    {row[0]}: {row[1]}")

    conn.close()
    print(f"\nГотово! Файл: {DB_FILE}")

if __name__ == "__main__":
    main()
