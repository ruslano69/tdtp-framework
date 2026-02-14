#!/usr/bin/env python3
"""
Travel Guide Data Loader for PostgreSQL
Populates PostgreSQL database with cities and tourist attractions
"""

import psycopg2
from psycopg2.extras import Json
import json
from decimal import Decimal

# Database connection settings
# From tests/integration/docker-compose.yml
HOST = 'localhost'
PORT = 5432
DATABASE = 'tdtp_test_db'
USERNAME = 'tdtp_test'
PASSWORD = 'tdtp_test_password'

# Sample cities data with attractions (same as MSSQL version)
CITIES_DATA = [
    {
        'name': 'Paris',
        'country': 'France',
        'latitude': Decimal('48.856614'),
        'longitude': Decimal('2.352222'),
        'population': 2161000,
        'timezone': 'Europe/Paris',
        'attractions': [
            {'name': 'Eiffel Tower', 'price_eur': 26.80, 'rating': 4.6},
            {'name': 'Louvre Museum', 'price_eur': 17.00, 'rating': 4.7},
            {'name': 'Arc de Triomphe', 'price_eur': 13.00, 'rating': 4.5},
            {'name': 'Notre-Dame Cathedral', 'price_eur': 0.00, 'rating': 4.6},
            {'name': 'Versailles Palace', 'price_eur': 19.50, 'rating': 4.7}
        ]
    },
    {
        'name': 'Tokyo',
        'country': 'Japan',
        'latitude': Decimal('35.689487'),
        'longitude': Decimal('139.691706'),
        'population': 13960000,
        'timezone': 'Asia/Tokyo',
        'attractions': [
            {'name': 'Tokyo Skytree', 'price_eur': 22.00, 'rating': 4.5},
            {'name': 'Senso-ji Temple', 'price_eur': 0.00, 'rating': 4.6},
            {'name': 'Meiji Shrine', 'price_eur': 0.00, 'rating': 4.5},
            {'name': 'Tokyo Disneyland', 'price_eur': 68.00, 'rating': 4.7},
            {'name': 'Imperial Palace', 'price_eur': 0.00, 'rating': 4.4}
        ]
    },
    {
        'name': 'New York',
        'country': 'USA',
        'latitude': Decimal('40.712776'),
        'longitude': Decimal('-74.005974'),
        'population': 8336000,
        'timezone': 'America/New_York',
        'attractions': [
            {'name': 'Statue of Liberty', 'price_eur': 21.00, 'rating': 4.7},
            {'name': 'Central Park', 'price_eur': 0.00, 'rating': 4.8},
            {'name': 'Empire State Building', 'price_eur': 38.00, 'rating': 4.6},
            {'name': 'Brooklyn Bridge', 'price_eur': 0.00, 'rating': 4.7},
            {'name': 'Metropolitan Museum', 'price_eur': 25.00, 'rating': 4.8}
        ]
    },
    {
        'name': 'London',
        'country': 'United Kingdom',
        'latitude': Decimal('51.507351'),
        'longitude': Decimal('-0.127758'),
        'population': 8982000,
        'timezone': 'Europe/London',
        'attractions': [
            {'name': 'British Museum', 'price_eur': 0.00, 'rating': 4.7},
            {'name': 'Tower of London', 'price_eur': 29.90, 'rating': 4.6},
            {'name': 'London Eye', 'price_eur': 32.00, 'rating': 4.5},
            {'name': 'Buckingham Palace', 'price_eur': 30.00, 'rating': 4.6},
            {'name': 'Westminster Abbey', 'price_eur': 24.00, 'rating': 4.7}
        ]
    },
    {
        'name': 'Dubai',
        'country': 'UAE',
        'latitude': Decimal('25.204849'),
        'longitude': Decimal('55.270783'),
        'population': 3331000,
        'timezone': 'Asia/Dubai',
        'attractions': [
            {'name': 'Burj Khalifa', 'price_eur': 35.00, 'rating': 4.7},
            {'name': 'Dubai Mall', 'price_eur': 0.00, 'rating': 4.7},
            {'name': 'Palm Jumeirah', 'price_eur': 0.00, 'rating': 4.6},
            {'name': 'Dubai Marina', 'price_eur': 0.00, 'rating': 4.6},
            {'name': 'Gold Souk', 'price_eur': 0.00, 'rating': 4.4}
        ]
    },
    {
        'name': 'Sydney',
        'country': 'Australia',
        'latitude': Decimal('-33.868820'),
        'longitude': Decimal('151.209296'),
        'population': 5312000,
        'timezone': 'Australia/Sydney',
        'attractions': [
            {'name': 'Opera House', 'price_eur': 35.00, 'rating': 4.8},
            {'name': 'Harbour Bridge', 'price_eur': 0.00, 'rating': 4.7},
            {'name': 'Bondi Beach', 'price_eur': 0.00, 'rating': 4.6},
            {'name': 'Taronga Zoo', 'price_eur': 40.00, 'rating': 4.6},
            {'name': 'Royal Botanic Garden', 'price_eur': 0.00, 'rating': 4.7}
        ]
    },
    {
        'name': 'Moscow',
        'country': 'Russia',
        'latitude': Decimal('55.755826'),
        'longitude': Decimal('37.617300'),
        'population': 12506000,
        'timezone': 'Europe/Moscow',
        'attractions': [
            {'name': 'Red Square', 'price_eur': 0.00, 'rating': 4.8},
            {'name': 'Kremlin', 'price_eur': 8.00, 'rating': 4.7},
            {'name': "St. Basil's Cathedral", 'price_eur': 7.00, 'rating': 4.8},
            {'name': 'Bolshoi Theatre', 'price_eur': 50.00, 'rating': 4.7},
            {'name': 'Gorky Park', 'price_eur': 0.00, 'rating': 4.6}
        ]
    },
    {
        'name': 'Barcelona',
        'country': 'Spain',
        'latitude': Decimal('41.385064'),
        'longitude': Decimal('2.173403'),
        'population': 1620000,
        'timezone': 'Europe/Madrid',
        'attractions': [
            {'name': 'Sagrada Familia', 'price_eur': 26.00, 'rating': 4.7},
            {'name': 'Park Güell', 'price_eur': 10.00, 'rating': 4.6},
            {'name': 'La Rambla', 'price_eur': 0.00, 'rating': 4.5},
            {'name': 'Camp Nou', 'price_eur': 26.00, 'rating': 4.6},
            {'name': 'Gothic Quarter', 'price_eur': 0.00, 'rating': 4.7}
        ]
    },
    {
        'name': 'Singapore',
        'country': 'Singapore',
        'latitude': Decimal('1.352083'),
        'longitude': Decimal('103.819836'),
        'population': 5686000,
        'timezone': 'Asia/Singapore',
        'attractions': [
            {'name': 'Marina Bay Sands', 'price_eur': 20.00, 'rating': 4.6},
            {'name': 'Gardens by the Bay', 'price_eur': 25.00, 'rating': 4.7},
            {'name': 'Sentosa Island', 'price_eur': 15.00, 'rating': 4.5},
            {'name': 'Merlion Park', 'price_eur': 0.00, 'rating': 4.4},
            {'name': 'Universal Studios', 'price_eur': 70.00, 'rating': 4.6}
        ]
    },
    {
        'name': 'Rio de Janeiro',
        'country': 'Brazil',
        'latitude': Decimal('-22.906847'),
        'longitude': Decimal('-43.172896'),
        'population': 6748000,
        'timezone': 'America/Sao_Paulo',
        'attractions': [
            {'name': 'Christ the Redeemer', 'price_eur': 12.00, 'rating': 4.8},
            {'name': 'Sugarloaf Mountain', 'price_eur': 25.00, 'rating': 4.7},
            {'name': 'Copacabana Beach', 'price_eur': 0.00, 'rating': 4.6},
            {'name': 'Tijuca Forest', 'price_eur': 0.00, 'rating': 4.7},
            {'name': 'Maracanã Stadium', 'price_eur': 15.00, 'rating': 4.5}
        ]
    }
]


def connect_to_database():
    """Establish connection to PostgreSQL"""
    try:
        conn = psycopg2.connect(
            host=HOST,
            port=PORT,
            database=DATABASE,
            user=USERNAME,
            password=PASSWORD
        )
        print(f"✓ Connected to {DATABASE} database")
        return conn
    except psycopg2.Error as e:
        print(f"✗ Connection failed: {e}")
        print("\nTroubleshooting:")
        print("1. Check if PostgreSQL container is running: docker ps")
        print("2. Verify port mapping (default: 5432)")
        print("3. Update PASSWORD in this script if needed")
        print("4. Install psycopg2: pip install psycopg2-binary")
        raise


def insert_cities(conn):
    """Insert cities data with JSONB attractions"""
    cursor = conn.cursor()

    insert_query = """
        INSERT INTO cities (name, country, latitude, longitude, population, timezone, attractions)
        VALUES (%s, %s, %s, %s, %s, %s, %s)
    """

    inserted_count = 0
    for city in CITIES_DATA:
        # PostgreSQL handles JSON natively with psycopg2.extras.Json
        cursor.execute(
            insert_query,
            (
                city['name'],
                city['country'],
                city['latitude'],
                city['longitude'],
                city['population'],
                city['timezone'],
                Json(city['attractions'])  # Convert to JSONB
            )
        )
        inserted_count += 1
        print(f"  ✓ Inserted: {city['name']}, {city['country']} ({len(city['attractions'])} attractions)")

    conn.commit()
    print(f"\n✓ Successfully inserted {inserted_count} cities")
    return inserted_count


def verify_data(conn):
    """Verify inserted data"""
    cursor = conn.cursor()

    # Count total cities
    cursor.execute("SELECT COUNT(*) FROM cities")
    total_cities = cursor.fetchone()[0]

    # Get sample city with JSONB query
    cursor.execute("""
        SELECT
            name,
            country,
            population,
            attractions->0 as first_attraction
        FROM cities
        ORDER BY population DESC
        LIMIT 1
    """)

    row = cursor.fetchone()
    if row:
        name, country, population, first_attraction = row
        print(f"\n✓ Data verification:")
        print(f"  Total cities: {total_cities}")
        print(f"  Largest city: {name} ({country}) - {population:,} people")
        if first_attraction:
            attraction = json.loads(first_attraction) if isinstance(first_attraction, str) else first_attraction
            print(f"  Sample attraction: {attraction['name']} - €{attraction['price_eur']}")

    # Show table structure
    cursor.execute("""
        SELECT column_name, data_type, character_maximum_length
        FROM information_schema.columns
        WHERE table_name = 'cities'
        ORDER BY ordinal_position
    """)

    print(f"\n✓ Table structure:")
    for row in cursor.fetchall():
        col_name, data_type, max_length = row
        length_info = f"({max_length})" if max_length else ""
        print(f"  - {col_name}: {data_type}{length_info}")


def main():
    """Main execution function"""
    print("=" * 60)
    print("Travel Guide Database Loader (PostgreSQL)")
    print("=" * 60)
    print()

    try:
        # Connect to database
        conn = connect_to_database()

        # Insert data
        print("\nInserting cities data...")
        insert_cities(conn)

        # Verify
        verify_data(conn)

        # Close connection
        conn.close()
        print("\n" + "=" * 60)
        print("✓ Database populated successfully!")
        print("=" * 60)
        print("\nYou can now test PostgreSQL connector in tdtp-xray:")
        print(f"  Host: {HOST}")
        print(f"  Port: {PORT}")
        print(f"  Database: {DATABASE}")
        print(f"  Table: cities")
        print()

    except Exception as e:
        print(f"\n✗ Error: {e}")
        return 1

    return 0


if __name__ == "__main__":
    exit(main())
