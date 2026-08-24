#!/usr/bin/env python3
"""
Скрипт генерации тестовой БД для бенчмаркинга TDTP Framework
Создает SQLite БД с 100k записей для тестирования производительности
"""

import argparse
import sqlite3
import random
import sys
import time
from datetime import datetime, timedelta

# Конфигурация (переопределяется ключами --out/--rows/--no-dates)
DB_FILE = "benchmark_100k.db"
TOTAL_RECORDS = 100000

# Колонки с настоящими датовыми decltype. Адаптер SQLite разбирает тип из
# объявления колонки (pkg/adapters/sqlite/types.go): DATE -> TypeDate,
# DATETIME/TIMESTAMP -> TypeTimestamp, всё прочее -> TypeText. Поэтому
# RegisteredAt TEXT датовый путь не задействует вовсе, и колонки ниже
# добавлены именно чтобы его задействовать. Флаг --no-dates воспроизводит
# старый набор из шести колонок для сравнения.
WITH_DATES = True

# Единая точка отсчёта для всех дат. Раньше datetime.now() звался на каждое
# поле каждой строки — это и лишняя работа, и разъезд: строки, сгенерированные
# в начале и в конце прогона, отсчитывались от разных моментов.
NOW = datetime.now()

# Точка отсчёта для --seed. Одного сида мало: даты считаются от «сейчас», так
# что без фиксации момента набор всё равно выходил бы разным. С обоими
# фиксированными --seed даёт побайтово тот же файл.
SEEDED_EPOCH = datetime(2026, 1, 1, 0, 0, 0)

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
    """Дата регистрации за последние 5 лет, TEXT.

    Время суток разыгрывается отдельно, а не наследуется от точки отсчёта.
    Вычитание целых дней давало бы у всех ста тысяч строк одинаковый хвост
    "00:00:00" — не свойство данных, а подарок компрессору. Замерено, и цена
    велика: сжатый zstd 3 набор выходил на 18% меньше, kanzi 6 на 22%. То есть
    пятая часть заявленной плотности бралась бы из артефакта генератора.
    """
    delta = timedelta(days=random.randint(0, 365 * 5),
                      seconds=random.randint(0, 86399))
    return (NOW - delta).strftime("%Y-%m-%d %H:%M:%S")


def generate_birth_date():
    """DATE — только календарная дата, без времени. Возраст 18..70 лет."""
    days_ago = random.randint(365 * 18, 365 * 70)
    return (NOW - timedelta(days=days_ago)).strftime("%Y-%m-%d")


def generate_last_login():
    """DATETIME, 10% NULL — не каждый пользователь входил хоть раз.

    NULL здесь не для красоты: пустая датовая ячейка идёт по другой ветке,
    чем заполненная, и без неё бенчмарк её не меряет.
    """
    if random.random() < 0.10:
        return None
    seconds_ago = random.randint(0, 86400 * 365)
    return (NOW - timedelta(seconds=seconds_ago)).strftime("%Y-%m-%d %H:%M:%S")


def generate_updated_at():
    """TIMESTAMP с долями секунды — миллисекунды обязаны пережить round-trip."""
    delta = timedelta(seconds=random.randint(0, 86400 * 90),
                      milliseconds=random.randint(0, 999))
    return (NOW - delta).strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]


def create_database():
    """Создает БД и таблицу. Индексы строятся отдельно, ПОСЛЕ вставки."""
    print(f"Создание БД: {DB_FILE}")

    conn = sqlite3.connect(DB_FILE)

    # Это одноразовый тестовый набор: durability здесь не нужна, а стоит она
    # дорого — журнал и fsync на каждой транзакции были основной статьёй
    # расходов. Потеря БД при падении процесса ничем не грозит, она
    # пересоздаётся одной командой.
    conn.execute("PRAGMA journal_mode = MEMORY")
    conn.execute("PRAGMA synchronous = OFF")

    cursor = conn.cursor()
    cursor.execute("DROP TABLE IF EXISTS Users")

    date_columns = """,
            BirthDate DATE NOT NULL,
            LastLoginAt DATETIME,
            UpdatedAt TIMESTAMP NOT NULL""" if WITH_DATES else ""

    cursor.execute("""
        CREATE TABLE Users (
            ID INTEGER PRIMARY KEY AUTOINCREMENT,
            Name TEXT NOT NULL,
            Email TEXT NOT NULL UNIQUE,
            City TEXT NOT NULL,
            Balance REAL NOT NULL,
            IsActive INTEGER NOT NULL,
            RegisteredAt TEXT NOT NULL%s
        )
    """ % date_columns)

    conn.commit()
    print("✓ Таблица Users создана")

    return conn


def create_indexes(conn):
    """Строит индексы после вставки.

    Индекс, существующий во время загрузки, переписывается на каждой строке:
    шесть индексов — это шесть B-деревьев, которые правятся 100 000 раз.
    Построенные разом по готовой таблице, они обходятся заметно дешевле, а
    результат тот же.
    """
    cursor = conn.cursor()
    cursor.execute("CREATE INDEX idx_balance ON Users(Balance)")
    cursor.execute("CREATE INDEX idx_city ON Users(City)")
    cursor.execute("CREATE INDEX idx_active ON Users(IsActive)")
    cursor.execute("CREATE INDEX idx_registered ON Users(RegisteredAt)")
    if WITH_DATES:
        cursor.execute("CREATE INDEX idx_birth ON Users(BirthDate)")
        cursor.execute("CREATE INDEX idx_lastlogin ON Users(LastLoginAt)")
    conn.commit()
    print("✓ Индексы построены")


def iter_records(total):
    """Порождает записи по одной, не собирая их в список.

    Прогресс печатается раз в 5000 строк, а не на каждой пачке: сам вывод в
    консоль стоил заметной доли времени вставки.
    """
    for i in range(1, total + 1):
        name = generate_name()
        row = [
            name,
            generate_email(name, i),
            random.choice(CITIES),
            generate_balance(),
            1 if random.random() < 0.7 else 0,  # 70% активных
            generate_date(),
        ]
        if WITH_DATES:
            row += [generate_birth_date(), generate_last_login(), generate_updated_at()]
        yield tuple(row)

        if i % 5000 == 0 or i == total:
            done = int(i / total * 50)
            print(f"\rПрогресс: [{'=' * done}{' ' * (50 - done)}] {i:,}/{total:,}",
                  end="", flush=True)


def insert_records(conn):
    """Вставляет все записи одной транзакцией.

    Прежняя версия коммитила каждую 1000 строк и глушила IntegrityError,
    выбрасывая при этом всю пачку целиком. Это молча давало БД меньше
    заказанного размера — для эталонного набора худший из возможных отказов,
    потому что он не виден. Уникальность Email обеспечена индексом записи в
    generate_email, так что ловить там нечего, а если однажды будет —
    пусть падает.
    """
    cols = ["Name", "Email", "City", "Balance", "IsActive", "RegisteredAt"]
    if WITH_DATES:
        cols += ["BirthDate", "LastLoginAt", "UpdatedAt"]
    insert_sql = "INSERT INTO Users (%s) VALUES (%s)" % (
        ", ".join(cols), ", ".join(["?"] * len(cols)))

    print(f"\nГенерация {TOTAL_RECORDS:,} записей...")

    with conn:  # одна транзакция: коммит на выходе, откат при исключении
        conn.executemany(insert_sql, iter_records(TOTAL_RECORDS))

    inserted = conn.execute("SELECT COUNT(*) FROM Users").fetchone()[0]
    if inserted != TOTAL_RECORDS:
        raise RuntimeError(f"вставлено {inserted:,} строк вместо {TOTAL_RECORDS:,}")
    print(f"\n✓ Вставлено {inserted:,} записей")


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
    
    # Датовые колонки
    if WITH_DATES:
        cursor.execute("SELECT MIN(BirthDate), MAX(BirthDate) FROM Users")
        bmin, bmax = cursor.fetchone()
        cursor.execute("SELECT COUNT(*) FROM Users WHERE LastLoginAt IS NULL")
        nulls = cursor.fetchone()[0]
        cursor.execute("SELECT UpdatedAt FROM Users LIMIT 1")
        sample = cursor.fetchone()[0]
        print(f"\nДаты:")
        print(f"  BirthDate   (DATE):      {bmin} .. {bmax}")
        print(f"  LastLoginAt (DATETIME):  NULL у {nulls:,} ({nulls/total*100:.1f}%)")
        print(f"  UpdatedAt   (TIMESTAMP): {sample}")

    # Размер файла
    cursor.execute("SELECT page_count * page_size as size FROM pragma_page_count(), pragma_page_size()")
    size_bytes = cursor.fetchone()[0]
    size_mb = size_bytes / (1024 * 1024)
    print(f"\nРазмер БД: {size_mb:.2f} MB")
    
    print("=" * 60)


def parse_args():
    ap = argparse.ArgumentParser(description="Генератор тестовой БД для TDTP benchmark")
    ap.add_argument("--out", default=DB_FILE, help=f"файл БД (по умолчанию {DB_FILE})")
    ap.add_argument("--rows", type=int, default=TOTAL_RECORDS,
                    help=f"число записей (по умолчанию {TOTAL_RECORDS})")
    ap.add_argument("--no-dates", action="store_true",
                    help="без колонок DATE/DATETIME/TIMESTAMP — старый набор из шести колонок")
    ap.add_argument("--seed", type=int, default=None,
                    help="детерминированный набор: фиксирует и генератор случайных чисел, "
                         "и точку отсчёта дат, поэтому файл выходит побайтово тем же")
    return ap.parse_args()


def main():
    """Главная функция"""
    global DB_FILE, TOTAL_RECORDS, WITH_DATES, NOW
    args = parse_args()
    DB_FILE, TOTAL_RECORDS, WITH_DATES = args.out, args.rows, not args.no_dates
    if args.seed is not None:
        random.seed(args.seed)
        NOW = SEEDED_EPOCH

    print("=" * 60)
    print("ГЕНЕРАТОР ТЕСТОВОЙ БД ДЛЯ TDTP BENCHMARK")
    print("=" * 60)
    
    try:
        # Создание БД
        conn = create_database()
        
        # Вставка данных, затем индексы — именно в этом порядке
        t0 = time.monotonic()
        insert_records(conn)
        create_indexes(conn)
        print(f"✓ Загрузка заняла {time.monotonic() - t0:.1f} с")
        
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
        
        print("\nКолонки: 7 базовых" + (" + BirthDate/LastLoginAt/UpdatedAt" if WITH_DATES else " (без дат)"))
        print("\nДля использования в бенчмарках:")
        print(f"  adapter, _ := sqlite.NewAdapter(\"{DB_FILE}\")")
        
        return 0
        
    except Exception as e:
        print(f"\n✗ ОШИБКА: {e}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())