#!/usr/bin/env python3
"""
Инициализация PostgreSQL для тестирования TDTP Framework
Создает базы данных, пользователей и таблицы
"""

import psycopg2
from psycopg2.extensions import ISOLATION_LEVEL_AUTOCOMMIT
import time
import sys


class PostgresInitializer:
    def __init__(self, host='localhost', port=5432, admin_user='postgres', admin_password='postgres'):
        self.host = host
        self.port = port
        self.admin_user = admin_user
        self.admin_password = admin_password

    def wait_for_postgres(self, max_attempts=30):
        """Ожидание готовности PostgreSQL"""
        print(f"Ожидание PostgreSQL на {self.host}:{self.port}...")

        for attempt in range(max_attempts):
            try:
                conn = psycopg2.connect(
                    host=self.host,
                    port=self.port,
                    user=self.admin_user,
                    password=self.admin_password,
                    database='postgres'
                )
                conn.close()
                print("✓ PostgreSQL готов!")
                return True
            except psycopg2.OperationalError:
                print(f"  Попытка {attempt + 1}/{max_attempts}...", end='\r')
                time.sleep(2)

        print("\n✗ Не удалось подключиться к PostgreSQL")
        return False

    def create_databases_and_users(self):
        """Создание БД и пользователей"""
        print("\nСоздание баз данных и пользователей...")

        conn = psycopg2.connect(
            host=self.host,
            port=self.port,
            user=self.admin_user,
            password=self.admin_password,
            database='postgres'
        )
        conn.set_isolation_level(ISOLATION_LEVEL_AUTOCOMMIT)
        cursor = conn.cursor()

        # Список БД для создания
        databases = [
            ('tdtp_test', 'tdtp_user', 'tdtp_pass', 'Основная тестовая БД'),
            ('tdtp_target', 'tdtp_user', 'tdtp_pass', 'Целевая БД для импорта'),
        ]

        for db_name, db_user, db_pass, description in databases:
            # Создание пользователя (если не существует)
            cursor.execute(f"""
                DO $$
                BEGIN
                    IF NOT EXISTS (SELECT FROM pg_user WHERE usename = '{db_user}') THEN
                        CREATE USER {db_user} WITH PASSWORD '{db_pass}';
                    END IF;
                END
                $$;
            """)
            print(f"  ✓ Пользователь {db_user}")

            # Удаление БД если существует (для чистого теста)
            cursor.execute(f"DROP DATABASE IF EXISTS {db_name};")

            # Создание БД
            cursor.execute(f"CREATE DATABASE {db_name} OWNER {db_user};")
            print(f"  ✓ База данных {db_name} ({description})")

            # Выдача прав
            cursor.execute(f"GRANT ALL PRIVILEGES ON DATABASE {db_name} TO {db_user};")

        cursor.close()
        conn.close()

    def create_tables(self, database):
        """Создание таблиц в указанной БД"""
        print(f"\nСоздание таблиц в {database}...")

        conn = psycopg2.connect(
            host=self.host,
            port=self.port,
            user='tdtp_user',
            password='tdtp_pass',
            database=database
        )
        cursor = conn.cursor()

        # Таблица Users
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS users (
                id INTEGER PRIMARY KEY,
                first_name VARCHAR(100),
                last_name VARCHAR(100),
                gender CHAR(1),
                birth_date DATE,
                email VARCHAR(255),
                phone VARCHAR(20),
                inn VARCHAR(12),
                insurance_policy VARCHAR(20),
                city VARCHAR(100),
                marital_status VARCHAR(20),
                status VARCHAR(20),
                balance DECIMAL(18,2),
                created_at TIMESTAMP,
                updated_at TIMESTAMP,
                description TEXT
            );
        """)
        print("  ✓ Таблица users")

        # Индексы для производительности
        cursor.execute("CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);")
        cursor.execute("CREATE INDEX IF NOT EXISTS idx_users_city ON users(city);")
        cursor.execute("CREATE INDEX IF NOT EXISTS idx_users_balance ON users(balance);")
        print("  ✓ Индексы созданы")

        conn.commit()
        cursor.close()
        conn.close()

    def show_connection_info(self):
        """Показать информацию для подключения"""
        print("\n" + "=" * 60)
        print("ИНФОРМАЦИЯ ДЛЯ ПОДКЛЮЧЕНИЯ")
        print("=" * 60)

        print("\nБаза: tdtp_test")
        print("  Host: localhost")
        print("  Port: 5432")
        print("  User: tdtp_user")
        print("  Password: tdtp_pass")
        print("  Database: tdtp_test")
        print("\nБаза: tdtp_target")
        print("  Host: localhost")
        print("  Port: 5432")
        print("  User: tdtp_user")
        print("  Password: tdtp_pass")
        print("  Database: tdtp_target")

        print("\nПодключение через psql:")
        print("  docker exec -it tdtp_postgres psql -U tdtp_user -d tdtp_test")

        print("\nПодключение через Python:")
        print("  import psycopg2")
        print("  conn = psycopg2.connect(")
        print("      host='localhost', port=5432,")
        print("      user='tdtp_user', password='tdtp_pass',")
        print("      database='tdtp_test')")

    def run(self):
        """Выполнить всю инициализацию"""
        print("=" * 60)
        print("ИНИЦИАЛИЗАЦИЯ POSTGRESQL ДЛЯ TDTP FRAMEWORK")
        print("=" * 60)

        # Ожидание готовности
        if not self.wait_for_postgres():
            sys.exit(1)

        # Создание БД и пользователей
        self.create_databases_and_users()

        # Создание таблиц в обеих БД
        self.create_tables('tdtp_test')
        self.create_tables('tdtp_target')

        # Показать информацию
        self.show_connection_info()

        print("\n✓ Инициализация завершена успешно!")


if __name__ == '__main__':
    initializer = PostgresInitializer()
    initializer.run()
