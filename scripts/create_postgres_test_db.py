#!/usr/bin/env python3
"""
Создание тестовой базы данных PostgreSQL для TDTP Framework
Использует специфичные для PostgreSQL типы данных:
- UUID
- JSONB
- ARRAY
- SERIAL/BIGSERIAL
- TIMESTAMP WITH TIME ZONE
- NUMERIC с precision/scale
"""

import psycopg2
from psycopg2 import sql
import random
from datetime import datetime, timedelta
import json
import uuid

# Параметры подключения (из docker-compose.yml)
DB_CONFIG = {
    'host': 'localhost',
    'port': 5432,
    'user': 'tdtp_user',
    'password': 'tdtp_dev_pass_2025',
    'database': 'tdtp_test'
}

def create_connection():
    """Создает подключение к PostgreSQL"""
    try:
        conn = psycopg2.connect(**DB_CONFIG)
        return conn
    except psycopg2.Error as e:
        print(f"❌ Ошибка подключения: {e}")
        return None

def create_test_tables(conn):
    """Создает тестовые таблицы со специфичными типами PostgreSQL"""
    cursor = conn.cursor()
    
    # Таблица 1: Пользователи с UUID и JSONB
    print("📋 Creating table: users...")
    cursor.execute("""
        DROP TABLE IF EXISTS users CASCADE;
        CREATE TABLE users (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            username VARCHAR(100) NOT NULL,
            email VARCHAR(255) NOT NULL,
            age SMALLINT,
            balance NUMERIC(12, 2) DEFAULT 0.00,
            is_active BOOLEAN DEFAULT true,
            metadata JSONB,
            created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
        );
    """)
    
    # Таблица 2: Заказы с SERIAL и ARRAY
    print("📋 Creating table: orders...")
    cursor.execute("""
        DROP TABLE IF EXISTS orders CASCADE;
        CREATE TABLE orders (
            order_id SERIAL PRIMARY KEY,
            user_id UUID REFERENCES users(id),
            order_number VARCHAR(50) UNIQUE NOT NULL,
            total_amount NUMERIC(15, 2) NOT NULL,
            status VARCHAR(20) DEFAULT 'pending',
            tags TEXT[] DEFAULT '{}',
            items JSONB,
            order_date TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
        );
    """)
    
    # Таблица 3: Продукты с различными числовыми типами
    print("📋 Creating table: products...")
    cursor.execute("""
        DROP TABLE IF EXISTS products CASCADE;
        CREATE TABLE products (
            product_id BIGSERIAL PRIMARY KEY,
            sku VARCHAR(50) UNIQUE NOT NULL,
            name VARCHAR(200) NOT NULL,
            description TEXT,
            price NUMERIC(10, 2) NOT NULL,
            quantity INTEGER DEFAULT 0,
            weight REAL,
            dimensions JSONB,
            categories TEXT[],
            is_available BOOLEAN DEFAULT true,
            created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
        );
    """)
    
    # Таблица 4: Логи с BIGSERIAL и большим объемом данных
    print("📋 Creating table: activity_logs...")
    cursor.execute("""
        DROP TABLE IF EXISTS activity_logs CASCADE;
        CREATE TABLE activity_logs (
            log_id BIGSERIAL PRIMARY KEY,
            user_id UUID,
            action VARCHAR(100) NOT NULL,
            details JSONB,
            ip_address INET,
            timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
        );
    """)
    
    conn.commit()
    cursor.close()
    print("✅ Tables created successfully")

def generate_test_data(conn, num_users=100, num_products=50, num_orders=200):
    """Генерирует тестовые данные"""
    cursor = conn.cursor()
    
    print(f"\n📊 Generating test data...")
    print(f"   Users: {num_users}")
    print(f"   Products: {num_products}")
    print(f"   Orders: {num_orders}")
    
    # Генерация пользователей
    print("\n👥 Inserting users...")
    user_ids = []
    for i in range(num_users):
        user_uuid = str(uuid.uuid4())  # Конвертируем UUID в строку для PostgreSQL
        user_ids.append(user_uuid)
        
        metadata = {
            'preferences': {
                'theme': random.choice(['light', 'dark', 'auto']),
                'language': random.choice(['en', 'ru', 'de', 'fr']),
                'notifications': random.choice([True, False])
            },
            'last_login': datetime.now().isoformat(),
            'login_count': random.randint(1, 1000)
        }
        
        cursor.execute("""
            INSERT INTO users (id, username, email, age, balance, is_active, metadata)
            VALUES (%s, %s, %s, %s, %s, %s, %s)
        """, (
            user_uuid,
            f'user_{i+1}',
            f'user{i+1}@example.com',
            random.randint(18, 80),
            round(random.uniform(0, 10000), 2),
            random.choice([True, True, True, False]),  # 75% активных
            json.dumps(metadata)
        ))
    
    # Генерация продуктов
    print("📦 Inserting products...")
    product_ids = []
    categories_list = ['Electronics', 'Books', 'Clothing', 'Food', 'Toys', 'Sports', 'Home', 'Garden']
    
    for i in range(num_products):
        dimensions = {
            'length': round(random.uniform(10, 100), 2),
            'width': round(random.uniform(10, 100), 2),
            'height': round(random.uniform(5, 50), 2),
            'unit': 'cm'
        }
        
        cursor.execute("""
            INSERT INTO products (sku, name, description, price, quantity, weight, dimensions, categories, is_available)
            VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)
            RETURNING product_id
        """, (
            f'SKU-{1000+i}',
            f'Product {i+1}',
            f'Description for product {i+1}',
            round(random.uniform(9.99, 999.99), 2),
            random.randint(0, 1000),
            round(random.uniform(0.1, 50.0), 2),
            json.dumps(dimensions),
            random.sample(categories_list, k=random.randint(1, 3)),
            random.choice([True, True, True, False])
        ))
        product_ids.append(cursor.fetchone()[0])
    
    # Генерация заказов
    print("🛒 Inserting orders...")
    for i in range(num_orders):
        user_id = random.choice(user_ids)
        num_items = random.randint(1, 5)
        
        items = []
        total = 0
        for _ in range(num_items):
            product_id = random.choice(product_ids)
            quantity = random.randint(1, 3)
            price = round(random.uniform(10, 500), 2)
            total += price * quantity
            
            items.append({
                'product_id': product_id,
                'quantity': quantity,
                'price': price
            })
        
        tags = random.sample(['urgent', 'gift', 'wholesale', 'express', 'fragile'], k=random.randint(0, 3))
        
        cursor.execute("""
            INSERT INTO orders (user_id, order_number, total_amount, status, tags, items)
            VALUES (%s, %s, %s, %s, %s, %s)
        """, (
            user_id,
            f'ORD-{10000+i}',
            round(total, 2),
            random.choice(['pending', 'processing', 'shipped', 'delivered', 'cancelled']),
            tags,
            json.dumps(items)
        ))
    
    # Генерация логов активности
    print("📝 Inserting activity logs...")
    actions = ['login', 'logout', 'view_product', 'add_to_cart', 'purchase', 'update_profile']
    for i in range(num_orders * 3):  # Больше логов чем заказов
        user_id = random.choice(user_ids)
        action = random.choice(actions)
        
        details = {
            'action': action,
            'timestamp': (datetime.now() - timedelta(days=random.randint(0, 365))).isoformat(),
            'user_agent': random.choice(['Chrome', 'Firefox', 'Safari', 'Edge'])
        }
        
        cursor.execute("""
            INSERT INTO activity_logs (user_id, action, details, ip_address)
            VALUES (%s, %s, %s, %s)
        """, (
            user_id,
            action,
            json.dumps(details),
            f'{random.randint(1,255)}.{random.randint(1,255)}.{random.randint(1,255)}.{random.randint(1,255)}'
        ))
    
    conn.commit()
    cursor.close()
    print("✅ Test data inserted successfully")

def print_statistics(conn):
    """Выводит статистику по таблицам"""
    cursor = conn.cursor()
    
    print("\n📊 Database Statistics:")
    print("=" * 60)
    
    tables = ['users', 'products', 'orders', 'activity_logs']
    for table in tables:
        cursor.execute(f"SELECT COUNT(*) FROM {table}")
        count = cursor.fetchone()[0]
        
        cursor.execute(f"""
            SELECT pg_size_pretty(pg_total_relation_size('{table}'))
        """)
        size = cursor.fetchone()[0]
        
        print(f"  {table:20} | Rows: {count:6} | Size: {size}")
    
    print("=" * 60)
    
    # Примеры данных с PostgreSQL специфичными типами
    print("\n🔍 Sample Data (PostgreSQL specific types):")
    print("-" * 60)
    
    # UUID пример
    cursor.execute("SELECT id, username, email FROM users LIMIT 3")
    print("\n📌 Users (with UUID):")
    for row in cursor.fetchall():
        print(f"   UUID: {row[0]} | {row[1]} | {row[2]}")
    
    # JSONB пример
    cursor.execute("SELECT username, metadata FROM users WHERE metadata IS NOT NULL LIMIT 2")
    print("\n📌 Users (with JSONB metadata):")
    for row in cursor.fetchall():
        print(f"   {row[0]}: {row[1]}")
    
    # ARRAY пример
    cursor.execute("SELECT order_number, tags FROM orders WHERE tags != '{}' LIMIT 3")
    print("\n📌 Orders (with TEXT[] tags):")
    for row in cursor.fetchall():
        print(f"   {row[0]}: {row[1]}")
    
    # NUMERIC пример
    cursor.execute("SELECT username, balance FROM users ORDER BY balance DESC LIMIT 3")
    print("\n📌 Users (with NUMERIC balance):")
    for row in cursor.fetchall():
        print(f"   {row[0]}: ${row[1]}")
    
    cursor.close()

def main():
    print("=" * 60)
    print("🐘 PostgreSQL Test Database Creator for TDTP Framework")
    print("=" * 60)
    
    # Подключение
    print("\n🔌 Connecting to PostgreSQL...")
    conn = create_connection()
    if not conn:
        print("❌ Failed to connect. Make sure PostgreSQL container is running:")
        print("   docker-compose up -d postgres")
        return
    
    print("✅ Connected successfully")
    print(f"   Host: {DB_CONFIG['host']}")
    print(f"   Port: {DB_CONFIG['port']}")
    print(f"   Database: {DB_CONFIG['database']}")
    print(f"   User: {DB_CONFIG['user']}")
    
    # Создание таблиц
    try:
        create_test_tables(conn)
        
        # Генерация данных
        generate_test_data(
            conn,
            num_users=100,
            num_products=50,
            num_orders=200
        )
        
        # Статистика
        print_statistics(conn)
        
        print("\n" + "=" * 60)
        print("✅ PostgreSQL test database created successfully!")
        print("=" * 60)
        print("\n📝 Connection details for config.yaml:")
        print(f"""
database:
  type: postgres
  host: localhost
  port: 5432
  user: tdtp_user
  password: tdtp_dev_pass_2025
  dbname: tdtp_test
  schema: public
  sslmode: disable
        """)
        
        print("\n🚀 Ready for TDTP CLI testing:")
        print("   tdtpcli --create-config-pg")
        print("   tdtpcli --list")
        print("   tdtpcli --export users --output users.tdtp.xml")
        
    except Exception as e:
        print(f"\n❌ Error: {e}")
        import traceback
        traceback.print_exc()
        conn.rollback()
    finally:
        conn.close()
        print("\n👋 Connection closed")

if __name__ == "__main__":
    main()