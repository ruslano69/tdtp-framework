#!/usr/bin/env python3
"""
Генерация тестовых данных для PostgreSQL
"""

import psycopg2
import random
from datetime import datetime, timedelta
from faker import Faker


class TestDataGenerator:
    def __init__(self, host='localhost', port=5432, user='tdtp_user', password='tdtp_pass', database='tdtp_test'):
        self.host = host
        self.port = port
        self.user = user
        self.password = password
        self.database = database
        self.fake = Faker('ru_RU')

        # Справочники
        self.cities = [
            'Москва', 'Санкт-Петербург', 'Новосибирск', 'Екатеринбург', 'Казань',
            'Нижний Новгород', 'Челябинск', 'Самара', 'Омск', 'Ростов-на-Дону',
            'Уфа', 'Красноярск', 'Воронеж', 'Пермь', 'Волгоград'
        ]

        self.statuses = ['active', 'inactive', 'blocked', 'pending', 'verified', 'suspended']

        self.marital_statuses_male = ['холост', 'женат', 'разведён', 'вдовец']
        self.marital_statuses_female = ['не замужем', 'замужем', 'разведена', 'вдова']

    def connect(self):
        """Подключение к БД"""
        return psycopg2.connect(
            host=self.host,
            port=self.port,
            user=self.user,
            password=self.password,
            database=self.database
        )

    def generate_user(self, user_id):
        """Генерация одного пользователя"""
        gender = random.choice(['М', 'Ж'])

        if gender == 'М':
            first_name = self.fake.first_name_male()
            last_name = self.fake.last_name_male()
            marital_status = random.choice(self.marital_statuses_male)
        else:
            first_name = self.fake.first_name_female()
            last_name = self.fake.last_name_female()
            marital_status = random.choice(self.marital_statuses_female)

        birth_date = self.fake.date_of_birth(minimum_age=18, maximum_age=80)
        email = f"{self.fake.user_name()}{user_id}@{random.choice(['mail.ru', 'yandex.ru', 'gmail.com'])}"
        phone = f"+7{random.randint(9000000000, 9999999999)}"
        inn = f"{random.randint(100000000000, 999999999999)}"
        insurance_policy = f"{random.randint(1000, 9999)} {random.randint(100000, 999999)}"
        city = random.choice(self.cities)
        status = random.choice(self.statuses)
        balance = round(random.uniform(10000, 500000), 2)

        created_at = datetime.now() - timedelta(days=random.randint(1, 365 * 5))
        updated_at = created_at + timedelta(days=random.randint(0, 365 * 2))

        description = f"{random.choice(['junior', 'middle', 'senior', 'lead', 'principal', 'head', 'chief'])} " \
                      f"{self.fake.job()}, отдел {random.choice(['IT', 'HR', 'Finance', 'Sales', 'Marketing'])}"

        return (
            user_id, first_name, last_name, gender, birth_date,
            email, phone, inn, insurance_policy, city,
            marital_status, status, balance, created_at, updated_at, description
        )

    def generate_batch(self, count=10000, batch_size=1000):
        """Генерация и вставка пакета пользователей"""
        print(f"\nГенерация {count} записей...")

        conn = self.connect()
        cursor = conn.cursor()

        # Очистка таблицы
        cursor.execute("TRUNCATE TABLE users;")
        print("  ✓ Таблица очищена")

        total_inserted = 0

        for batch_start in range(1, count + 1, batch_size):
            batch_end = min(batch_start + batch_size, count + 1)
            batch_data = [self.generate_user(i) for i in range(batch_start, batch_end)]

            # Bulk insert
            insert_query = """
                INSERT INTO users (
                    id, first_name, last_name, gender, birth_date,
                    email, phone, inn, insurance_policy, city,
                    marital_status, status, balance, created_at, updated_at, description
                ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
            """

            cursor.executemany(insert_query, batch_data)
            conn.commit()

            total_inserted += len(batch_data)
            print(f"  → {total_inserted}/{count} записей", end='\r')

        print(f"\n  ✓ Вставлено {total_inserted} записей")

        # Статистика
        cursor.execute("SELECT status, COUNT(*) FROM users GROUP BY status ORDER BY COUNT(*) DESC;")
        stats = cursor.fetchall()

        print("\n  Статистика по статусам:")
        for status, count in stats:
            print(f"    {status}: {count}")

        cursor.close()
        conn.close()

    def show_sample(self, limit=5):
        """Показать примеры данных"""
        conn = self.connect()
        cursor = conn.cursor()

        cursor.execute(f"""
            SELECT id, first_name, last_name, gender, city, status, balance
            FROM users
            ORDER BY RANDOM()
            LIMIT {limit};
        """)

        rows = cursor.fetchall()

        print(f"\nПримеры данных ({limit} случайных записей):")
        print("-" * 80)
        for row in rows:
            print(f"  ID: {row[0]}, {row[1]} {row[2]}, {row[3]}, {row[4]}, {row[5]}, баланс: {row[6]}")

        cursor.close()
        conn.close()


def main():
    import argparse

    parser = argparse.ArgumentParser(description='Генерация тестовых данных для PostgreSQL')
    parser.add_argument('--count', type=int, default=10000, help='Количество записей (по умолчанию: 10000)')
    parser.add_argument('--database', type=str, default='tdtp_test', help='Имя базы данных')
    parser.add_argument('--batch', type=int, default=1000, help='Размер батча для вставки')

    args = parser.parse_args()

    print("=" * 80)
    print("ГЕНЕРАЦИЯ ТЕСТОВЫХ ДАННЫХ")
    print("=" * 80)
    print(f"База данных: {args.database}")
    print(f"Количество записей: {args.count}")
    print(f"Размер батча: {args.batch}")

    try:
        generator = TestDataGenerator(database=args.database)
        generator.generate_batch(count=args.count, batch_size=args.batch)
        generator.show_sample(limit=5)

        print("\n✓ Генерация завершена успешно!")

    except Exception as e:
        print(f"\n✗ Ошибка: {e}")
        import traceback
        traceback.print_exc()


if __name__ == '__main__':
    main()
