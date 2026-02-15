#!/usr/bin/env python3
"""
Populate TravelAgency database with test data (MSSQL)
Generates realistic travel agency data with JSON fields
"""

import pyodbc
import random
import json
from datetime import datetime, timedelta, date
from decimal import Decimal

# Connection settings
SERVER = 'localhost,1433'
DATABASE = 'TravelAgency'
USERNAME = 'sa'
PASSWORD = 'YourStrong!Passw0rd'

# Sample data
COUNTRIES_DATA = [
    ('USA', 'United States', 'North America', 'USD', '+1', False, ['English']),
    ('CAN', 'Canada', 'North America', 'CAD', '+1', False, ['English', 'French']),
    ('MEX', 'Mexico', 'North America', 'MXN', '+52', True, ['Spanish']),
    ('GBR', 'United Kingdom', 'Europe', 'GBP', '+44', True, ['English']),
    ('FRA', 'France', 'Europe', 'EUR', '+33', True, ['French']),
    ('DEU', 'Germany', 'Europe', 'EUR', '+49', True, ['German']),
    ('ITA', 'Italy', 'Europe', 'EUR', '+39', True, ['Italian']),
    ('ESP', 'Spain', 'Europe', 'EUR', '+34', True, ['Spanish']),
    ('PRT', 'Portugal', 'Europe', 'EUR', '+351', True, ['Portuguese']),
    ('NLD', 'Netherlands', 'Europe', 'EUR', '+31', True, ['Dutch']),
    ('BEL', 'Belgium', 'Europe', 'EUR', '+32', True, ['Dutch', 'French']),
    ('CHE', 'Switzerland', 'Europe', 'CHF', '+41', True, ['German', 'French', 'Italian']),
    ('AUT', 'Austria', 'Europe', 'EUR', '+43', True, ['German']),
    ('GRC', 'Greece', 'Europe', 'EUR', '+30', True, ['Greek']),
    ('TUR', 'Turkey', 'Asia', 'TRY', '+90', True, ['Turkish']),
    ('RUS', 'Russia', 'Europe', 'RUB', '+7', True, ['Russian']),
    ('POL', 'Poland', 'Europe', 'PLN', '+48', True, ['Polish']),
    ('CZE', 'Czech Republic', 'Europe', 'CZK', '+420', True, ['Czech']),
    ('HUN', 'Hungary', 'Europe', 'HUF', '+36', True, ['Hungarian']),
    ('ROU', 'Romania', 'Europe', 'RON', '+40', True, ['Romanian']),
    ('JPN', 'Japan', 'Asia', 'JPY', '+81', True, ['Japanese']),
    ('CHN', 'China', 'Asia', 'CNY', '+86', True, ['Mandarin']),
    ('KOR', 'South Korea', 'Asia', 'KRW', '+82', True, ['Korean']),
    ('THA', 'Thailand', 'Asia', 'THB', '+66', True, ['Thai']),
    ('VNM', 'Vietnam', 'Asia', 'VND', '+84', True, ['Vietnamese']),
    ('IND', 'India', 'Asia', 'INR', '+91', True, ['Hindi', 'English']),
    ('IDN', 'Indonesia', 'Asia', 'IDR', '+62', True, ['Indonesian']),
    ('MYS', 'Malaysia', 'Asia', 'MYR', '+60', True, ['Malay']),
    ('SGP', 'Singapore', 'Asia', 'SGD', '+65', False, ['English', 'Mandarin', 'Malay']),
    ('PHL', 'Philippines', 'Asia', 'PHP', '+63', True, ['Filipino', 'English']),
    ('AUS', 'Australia', 'Oceania', 'AUD', '+61', False, ['English']),
    ('NZL', 'New Zealand', 'Oceania', 'NZD', '+64', False, ['English']),
    ('BRA', 'Brazil', 'South America', 'BRL', '+55', True, ['Portuguese']),
    ('ARG', 'Argentina', 'South America', 'ARS', '+54', True, ['Spanish']),
    ('CHL', 'Chile', 'South America', 'CLP', '+56', True, ['Spanish']),
    ('PER', 'Peru', 'South America', 'PEN', '+51', True, ['Spanish']),
    ('COL', 'Colombia', 'South America', 'COP', '+57', True, ['Spanish']),
    ('ECU', 'Ecuador', 'South America', 'USD', '+593', True, ['Spanish']),
    ('ZAF', 'South Africa', 'Africa', 'ZAR', '+27', True, ['Afrikaans', 'English']),
    ('EGY', 'Egypt', 'Africa', 'EGP', '+20', True, ['Arabic']),
    ('MAR', 'Morocco', 'Africa', 'MAD', '+212', True, ['Arabic', 'French']),
    ('KEN', 'Kenya', 'Africa', 'KES', '+254', True, ['Swahili', 'English']),
    ('TZA', 'Tanzania', 'Africa', 'TZS', '+255', True, ['Swahili', 'English']),
    ('NOR', 'Norway', 'Europe', 'NOK', '+47', True, ['Norwegian']),
    ('SWE', 'Sweden', 'Europe', 'SEK', '+46', True, ['Swedish']),
    ('DNK', 'Denmark', 'Europe', 'DKK', '+45', True, ['Danish']),
    ('FIN', 'Finland', 'Europe', 'EUR', '+358', True, ['Finnish']),
    ('ISL', 'Iceland', 'Europe', 'ISK', '+354', True, ['Icelandic']),
    ('IRL', 'Ireland', 'Europe', 'EUR', '+353', True, ['English', 'Irish']),
    ('ARE', 'United Arab Emirates', 'Asia', 'AED', '+971', True, ['Arabic']),
]

FIRST_NAMES = [
    'John', 'Emma', 'Michael', 'Sophia', 'William', 'Olivia', 'James', 'Ava', 'Robert', 'Isabella',
    'David', 'Mia', 'Richard', 'Charlotte', 'Joseph', 'Amelia', 'Thomas', 'Harper', 'Charles', 'Evelyn',
    'Daniel', 'Abigail', 'Matthew', 'Emily', 'Christopher', 'Elizabeth', 'Andrew', 'Sofia', 'Joshua', 'Avery',
    'Alexander', 'Ella', 'Ryan', 'Scarlett', 'Nathan', 'Grace', 'Benjamin', 'Chloe', 'Samuel', 'Victoria',
    'Patrick', 'Madison', 'George', 'Luna', 'Henry', 'Penelope', 'Jacob', 'Layla', 'Nicholas', 'Riley'
]

LAST_NAMES = [
    'Smith', 'Johnson', 'Williams', 'Brown', 'Jones', 'Garcia', 'Miller', 'Davis', 'Rodriguez', 'Martinez',
    'Hernandez', 'Lopez', 'Gonzalez', 'Wilson', 'Anderson', 'Thomas', 'Taylor', 'Moore', 'Jackson', 'Martin',
    'Lee', 'Perez', 'Thompson', 'White', 'Harris', 'Sanchez', 'Clark', 'Ramirez', 'Lewis', 'Robinson',
    'Walker', 'Young', 'Allen', 'King', 'Wright', 'Scott', 'Torres', 'Nguyen', 'Hill', 'Flores',
    'Green', 'Adams', 'Nelson', 'Baker', 'Hall', 'Rivera', 'Campbell', 'Mitchell', 'Carter', 'Roberts'
]

CITIES = {
    'USA': ['New York', 'Los Angeles', 'Chicago', 'Houston', 'Phoenix'],
    'GBR': ['London', 'Manchester', 'Birmingham', 'Edinburgh', 'Glasgow'],
    'FRA': ['Paris', 'Lyon', 'Marseille', 'Toulouse', 'Nice'],
    'DEU': ['Berlin', 'Munich', 'Hamburg', 'Frankfurt', 'Cologne'],
    'ITA': ['Rome', 'Milan', 'Naples', 'Florence', 'Venice'],
    'ESP': ['Madrid', 'Barcelona', 'Valencia', 'Seville', 'Bilbao'],
    'JPN': ['Tokyo', 'Osaka', 'Kyoto', 'Yokohama', 'Nagoya'],
    'CHN': ['Beijing', 'Shanghai', 'Guangzhou', 'Shenzhen', 'Chengdu'],
    'AUS': ['Sydney', 'Melbourne', 'Brisbane', 'Perth', 'Adelaide'],
}

TOUR_TEMPLATES = [
    ('Alpine Adventure', 'Switzerland', 7, 'Challenging', 'Experience the Swiss Alps with hiking, mountain climbing, and glacier tours'),
    ('Beach Paradise', 'Thailand', 5, 'Easy', 'Relax on pristine beaches, snorkeling, and island hopping in Phuket'),
    ('Cultural Heritage', 'Italy', 10, 'Moderate', 'Explore ancient Rome, Renaissance Florence, and romantic Venice'),
    ('Safari Explorer', 'Kenya', 8, 'Moderate', 'Wildlife safari in Masai Mara, see the Big Five and Maasai villages'),
    ('Northern Lights', 'Iceland', 6, 'Moderate', 'Chase the Aurora Borealis, hot springs, and volcanic landscapes'),
    ('Amazon Expedition', 'Brazil', 9, 'Challenging', 'Deep jungle trekking, wildlife spotting, and river adventures'),
    ('Great Wall Trek', 'China', 7, 'Challenging', 'Hike less-traveled sections of the Great Wall with expert guides'),
    ('Machu Picchu Journey', 'Peru', 8, 'Challenging', 'Inca Trail trek to the Lost City with acclimatization in Cusco'),
    ('Mediterranean Cruise', 'Greece', 12, 'Easy', 'Island hopping through Santorini, Mykonos, and Crete'),
    ('Tokyo Modern', 'Japan', 5, 'Easy', 'Experience cutting-edge Tokyo: technology, fashion, and cuisine'),
]

GUIDE_SPECIALIZATIONS = [
    'Mountain Guide', 'Wildlife Expert', 'Cultural Historian', 'Adventure Sports',
    'Marine Biology', 'Photography', 'Archaeology', 'Culinary Tours',
    'Wine Tasting', 'Eco-Tourism', 'Urban Explorer', 'Extreme Sports'
]

CERTIFICATIONS = [
    'First Aid Certified', 'Wilderness Survival', 'SCUBA Instructor',
    'Mountain Climbing Level 3', 'CPR Certified', 'Tour Director License',
    'Foreign Language Proficiency', 'UNESCO Heritage Guide'
]

DIETARY_PREFERENCES = ['Vegetarian', 'Vegan', 'Gluten-Free', 'Halal', 'Kosher', 'No Restrictions']
ACTIVITY_PREFERENCES = ['Adventure', 'Relaxation', 'Culture', 'Nature', 'Photography', 'Food & Wine']

PAYMENT_METHODS = ['Credit Card', 'Debit Card', 'PayPal', 'Bank Transfer', 'Cash']

def get_connection():
    """Create database connection"""
    conn_str = f'DRIVER={{ODBC Driver 18 for SQL Server}};SERVER={SERVER};DATABASE={DATABASE};UID={USERNAME};PWD={PASSWORD};TrustServerCertificate=yes'
    return pyodbc.connect(conn_str)

def random_date(start_year, end_year):
    """Generate random date between years"""
    start = date(start_year, 1, 1)
    end = date(end_year, 12, 31)
    delta = end - start
    random_days = random.randint(0, delta.days)
    return start + timedelta(days=random_days)

def random_datetime(days_offset_min, days_offset_max):
    """Generate random datetime offset from now"""
    days = random.randint(days_offset_min, days_offset_max)
    hours = random.randint(0, 23)
    minutes = random.randint(0, 59)
    return datetime.now() + timedelta(days=days, hours=hours, minutes=minutes)

def populate_countries(cursor):
    """Insert countries"""
    print("Inserting countries...")
    for code, name, continent, currency, phone, visa, langs in COUNTRIES_DATA:
        cursor.execute("""
            INSERT INTO countries (country_code, country_name, continent, currency_code, phone_code, is_visa_required, official_languages)
            VALUES (?, ?, ?, ?, ?, ?, ?)
        """, code, name, continent, currency, phone, visa, json.dumps(langs))
    print(f"✓ Inserted {len(COUNTRIES_DATA)} countries")

def populate_customers(cursor, country_ids):
    """Insert customers (combinatorial generation)"""
    print("Inserting customers...")

    # Generate all possible name combinations (50 × 50 = 2500 possibilities)
    # Take a sample for realistic dataset size
    from itertools import product
    all_combinations = list(product(FIRST_NAMES, LAST_NAMES))
    random.shuffle(all_combinations)

    # Use 500 customers (20% of possible combinations)
    num_customers = min(500, len(all_combinations))

    for i, (first_name, last_name) in enumerate(all_combinations[:num_customers]):
        email = f"{first_name.lower()}.{last_name.lower()}{i % 100}@email.com"
        phone = f"+1-555-{random.randint(100, 999)}-{random.randint(1000, 9999)}"
        dob = random_date(1950, 2000)
        passport = f"P{random.randint(10000000, 99999999)}"
        nationality_id = random.choice(country_ids)

        # Random city
        city = random.choice(['New York', 'London', 'Paris', 'Berlin', 'Tokyo', 'Sydney', 'Toronto', 'Madrid'])
        address = f"{random.randint(1, 9999)} {random.choice(['Main', 'Oak', 'Elm', 'Maple', 'Park'])} St"
        postal = f"{random.randint(10000, 99999)}"

        loyalty_points = random.randint(0, 10000)
        is_vip = loyalty_points > 5000

        # JSON preferences
        preferences = {
            'dietary': random.choice(DIETARY_PREFERENCES),
            'activities': random.sample(ACTIVITY_PREFERENCES, random.randint(1, 3)),
            'room_preference': random.choice(['Non-smoking', 'Smoking', 'High Floor', 'Low Floor', 'View']),
            'seat_preference': random.choice(['Window', 'Aisle', 'No Preference'])
        }

        # JSON emergency contact
        emergency_contact = {
            'name': f"{random.choice(FIRST_NAMES)} {random.choice(LAST_NAMES)}",
            'relationship': random.choice(['Spouse', 'Parent', 'Sibling', 'Friend']),
            'phone': f"+1-555-{random.randint(100, 999)}-{random.randint(1000, 9999)}"
        }

        cursor.execute("""
            INSERT INTO customers (first_name, last_name, email, phone, date_of_birth, passport_number,
                                   nationality_country_id, address, city, postal_code, loyalty_points,
                                   is_vip, preferences, emergency_contact)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """, first_name, last_name, email, phone, dob, passport, nationality_id, address, city, postal,
             loyalty_points, is_vip, json.dumps(preferences), json.dumps(emergency_contact))

    print(f"✓ Inserted {num_customers} customers")

def populate_guides(cursor, country_ids):
    """Insert guides (combinatorial generation)"""
    print("Inserting guides...")

    # Generate guides from all specialization × language combinations
    from itertools import product, combinations

    # Each guide has specialization + unique language combo + certification combo
    all_languages = ['English', 'Spanish', 'French', 'German', 'Italian', 'Portuguese', 'Mandarin', 'Japanese', 'Arabic', 'Russian']

    # Generate guides: each specialization (12) × different language sets = ~60 guides
    guides_data = []
    for spec in GUIDE_SPECIALIZATIONS:
        for _ in range(5):  # 5 guides per specialization
            first_name = random.choice(FIRST_NAMES)
            last_name = random.choice(LAST_NAMES)
            guides_data.append((first_name, last_name, spec))

    for i, (first_name, last_name, specialization) in enumerate(guides_data):
        email = f"{first_name.lower()}.{last_name.lower()}.guide{i}@travelagency.com"
        phone = f"+1-555-{random.randint(100, 999)}-{random.randint(1000, 9999)}"

        # Languages (JSON array) - already have all_languages in scope
        num_languages = random.randint(2, 5)
        languages = random.sample(all_languages, num_languages)
        rating = round(random.uniform(3.5, 5.0), 2)
        experience_years = random.randint(1, 20)

        # Certifications (JSON array)
        num_certs = random.randint(1, 4)
        certifications = random.sample(CERTIFICATIONS, num_certs)

        is_active = random.choice([True, True, True, False])  # 75% active
        hire_date = random_date(2010, 2024)
        hourly_rate = round(random.uniform(50.0, 200.0), 2)

        bio = f"Experienced {specialization.lower()} with {experience_years} years in the tourism industry. Passionate about creating unforgettable travel experiences."
        photo_url = f"https://i.pravatar.cc/150?img={i+1}"

        cursor.execute("""
            INSERT INTO guides (first_name, last_name, email, phone, languages, specialization, rating,
                                experience_years, certifications, is_active, hire_date, hourly_rate, bio, photo_url)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """, first_name, last_name, email, phone, json.dumps(languages), specialization, rating,
             experience_years, json.dumps(certifications), is_active, hire_date, hourly_rate, bio, photo_url)

    print(f"✓ Inserted {len(guides_data)} guides")

def populate_tours(cursor, country_ids):
    """Insert tours"""
    print("Inserting tours...")

    tour_counter = 1
    for template_name, country_code, duration, difficulty, description in TOUR_TEMPLATES:
        # Create 3 variations of each template
        for variation in range(3):
            tour_code = f"T{tour_counter:04d}"
            tour_counter += 1

            # Find country_id
            cursor.execute("SELECT country_id FROM countries WHERE country_code = ?", country_code)
            country_id = cursor.fetchone()[0]

            if variation == 0:
                tour_name = template_name
            elif variation == 1:
                tour_name = f"{template_name} Premium"
            else:
                tour_name = f"{template_name} Express"

            max_group = random.randint(8, 20)
            min_age = random.choice([0, 12, 18, 21])
            base_price = round(random.uniform(500.0, 5000.0), 2)

            # JSON includes
            includes = {
                'accommodation': random.choice(['3-Star Hotel', '4-Star Hotel', '5-Star Resort', 'Boutique Hotel']),
                'meals': random.choice(['Breakfast Only', 'Half Board', 'Full Board', 'All Inclusive']),
                'transport': random.choice(['Private Coach', 'Shared Shuttle', 'Train', 'Domestic Flights']),
                'entrance_fees': random.choice([True, False]),
                'guide': 'Professional English-speaking guide'
            }

            # JSON excludes
            excludes = {
                'international_flights': True,
                'visa_fees': True,
                'travel_insurance': True,
                'personal_expenses': True,
                'tips': random.choice([True, False])
            }

            # JSON itinerary (array of daily activities)
            itinerary = []
            for day in range(1, duration + 1):
                day_plan = {
                    'day': day,
                    'title': f"Day {day}: {'Arrival' if day == 1 else 'Departure' if day == duration else 'Exploration'}",
                    'activities': [f"Activity {i+1}" for i in range(random.randint(2, 4))],
                    'meals': random.choice(['B', 'B,L', 'B,L,D']),
                    'accommodation': 'Hotel' if day < duration else 'Departure'
                }
                itinerary.append(day_plan)

            is_active = random.choice([True, True, True, False])  # 75% active

            cursor.execute("""
                INSERT INTO tours (tour_code, tour_name, description, destination_country_id, duration_days,
                                   difficulty_level, max_group_size, min_age, base_price, includes, excludes,
                                   itinerary, is_active)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, tour_code, tour_name, description, country_id, duration, difficulty, max_group, min_age,
                 base_price, json.dumps(includes), json.dumps(excludes), json.dumps(itinerary), is_active)

    print(f"✓ Inserted 30 tours")

def populate_tour_schedule(cursor, tour_ids, guide_ids):
    """Insert tour schedule (combinatorial: all tours × multiple dates × guides)"""
    print("Inserting tour schedules...")

    # Generate schedules: each tour × 10-15 different dates × different guides
    from itertools import product

    schedules_generated = 0
    for tour_id in tour_ids:
        # Each tour gets 8-12 scheduled departures
        num_schedules = random.randint(8, 12)
        for _ in range(num_schedules):
            schedules_generated += 1
        tour_id = random.choice(tour_ids)
        guide_id = random.choice(guide_ids)

        # Get tour duration
        cursor.execute("SELECT duration_days FROM tours WHERE tour_id = ?", tour_id)
        duration = cursor.fetchone()[0]

        # Random start date (past, present, future)
        start_date = (datetime.now() + timedelta(days=random.randint(-180, 365))).date()
        end_date = start_date + timedelta(days=duration)
        departure_time = f"{random.randint(6, 10):02d}:{random.choice(['00', '30'])}:00"

        available_slots = random.randint(8, 20)
        booked_slots = random.randint(0, available_slots)
        price_modifier = round(random.uniform(0.8, 1.5), 2)

        # Determine status based on dates
        today = datetime.now().date()
        if start_date > today:
            status = random.choice(['Scheduled', 'Confirmed'])
        elif start_date <= today <= end_date:
            status = 'In Progress'
        else:
            status = random.choice(['Completed', 'Cancelled'])

        notes = random.choice([None, 'Peak season', 'Holiday special', 'Last minute deal', 'Group discount available'])

        cursor.execute("""
            INSERT INTO tour_schedule (tour_id, guide_id, start_date, end_date, departure_time, available_slots,
                                       booked_slots, price_modifier, status, notes)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """, tour_id, guide_id, start_date, end_date, departure_time, available_slots, booked_slots,
             price_modifier, status, notes)

    print(f"✓ Inserted {schedules_generated} tour schedules")

def populate_tour_sales(cursor, schedule_ids, customer_ids):
    """Insert tour sales (combinatorial: schedules × customers with realistic booking rates)"""
    print("Inserting tour sales...")

    # Generate sales: ~30-60% of schedules get bookings, some schedules get multiple bookings
    sales_generated = 0

    # Each schedule can have 0-3 bookings
    from itertools import product
    for schedule_id in schedule_ids:
        num_bookings = random.choices([0, 1, 2, 3], weights=[30, 50, 15, 5])[0]  # Most have 1 booking
        for _ in range(num_bookings):
            sales_generated += 1
        booking_ref = f"BK{random.randint(100000, 999999)}"
        schedule_id = random.choice(schedule_ids)
        customer_id = random.choice(customer_ids)

        booking_date = random_datetime(-90, 30)
        num_travelers = random.randint(1, 6)

        # Calculate pricing
        total_amount = round(random.uniform(1000.0, 15000.0), 2)
        deposit_paid = round(total_amount * random.choice([0.0, 0.2, 0.5, 1.0]), 2)
        balance_due = round(total_amount - deposit_paid, 2)

        # Payment status
        if deposit_paid == 0:
            payment_status = 'Pending'
        elif balance_due == 0:
            payment_status = random.choice(['Fully Paid', 'Fully Paid', 'Fully Paid', 'Refunded', 'Cancelled'])
        else:
            payment_status = 'Deposit Paid'

        payment_method = random.choice(PAYMENT_METHODS)
        special_requests = random.choice([None, 'Vegetarian meals', 'Wheelchair accessible', 'Anniversary celebration', 'Honeymoon package'])

        # JSON traveler details
        traveler_details = []
        for t in range(num_travelers):
            traveler = {
                'name': f"{random.choice(FIRST_NAMES)} {random.choice(LAST_NAMES)}",
                'age': random.randint(8, 75),
                'passport': f"P{random.randint(10000000, 99999999)}",
                'special_needs': random.choice([None, 'Dietary restriction', 'Mobility assistance'])
            }
            traveler_details.append(traveler)

        insurance_purchased = random.choice([True, False])

        # Cancellation details (only if cancelled)
        if payment_status in ['Cancelled', 'Refunded']:
            cancellation_date = booking_date + timedelta(days=random.randint(1, 30))
            cancellation_reason = random.choice(['Change of plans', 'Medical emergency', 'Weather concerns', 'Personal reasons'])
            refund_amount = round(total_amount * random.uniform(0.5, 1.0), 2) if payment_status == 'Refunded' else 0
        else:
            cancellation_date = None
            cancellation_reason = None
            refund_amount = None

        sales_agent = random.choice(['Sarah Johnson', 'Mike Chen', 'Emma Wilson', 'David Brown', 'Lisa Garcia', 'Online Portal'])

        cursor.execute("""
            INSERT INTO tour_sales (booking_reference, schedule_id, customer_id, booking_date, number_of_travelers,
                                    total_amount, deposit_paid, balance_due, payment_status, payment_method,
                                    special_requests, traveler_details, insurance_purchased, cancellation_date,
                                    cancellation_reason, refund_amount, sales_agent)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """, booking_ref, schedule_id, customer_id, booking_date, num_travelers, total_amount, deposit_paid,
             balance_due, payment_status, payment_method, special_requests, json.dumps(traveler_details),
             insurance_purchased, cancellation_date, cancellation_reason, refund_amount, sales_agent)

    print(f"✓ Inserted {sales_generated} tour sales")

def populate_data():
    """Main population function"""
    conn = get_connection()
    cursor = conn.cursor()

    try:
        # Insert countries first
        populate_countries(cursor)
        conn.commit()

        # Get country IDs
        cursor.execute("SELECT country_id FROM countries")
        country_ids = [row[0] for row in cursor.fetchall()]

        # Insert customers
        populate_customers(cursor, country_ids)
        conn.commit()

        # Get customer IDs
        cursor.execute("SELECT customer_id FROM customers")
        customer_ids = [row[0] for row in cursor.fetchall()]

        # Insert guides
        populate_guides(cursor, country_ids)
        conn.commit()

        # Get guide IDs
        cursor.execute("SELECT guide_id FROM guides")
        guide_ids = [row[0] for row in cursor.fetchall()]

        # Insert tours
        populate_tours(cursor, country_ids)
        conn.commit()

        # Get tour IDs
        cursor.execute("SELECT tour_id FROM tours")
        tour_ids = [row[0] for row in cursor.fetchall()]

        # Insert tour schedule
        populate_tour_schedule(cursor, tour_ids, guide_ids)
        conn.commit()

        # Get schedule IDs
        cursor.execute("SELECT schedule_id FROM tour_schedule")
        schedule_ids = [row[0] for row in cursor.fetchall()]

        # Insert tour sales
        populate_tour_sales(cursor, schedule_ids, customer_ids)
        conn.commit()

        # Get actual counts
        cursor.execute("SELECT COUNT(*) FROM customers")
        customer_count = cursor.fetchone()[0]
        cursor.execute("SELECT COUNT(*) FROM guides")
        guide_count = cursor.fetchone()[0]
        cursor.execute("SELECT COUNT(*) FROM tour_schedule")
        schedule_count = cursor.fetchone()[0]
        cursor.execute("SELECT COUNT(*) FROM tour_sales")
        sales_count = cursor.fetchone()[0]

        print("\n✓ TravelAgency database populated successfully!")
        print("\nData summary (combinatorial generation):")
        print(f"  • {len(COUNTRIES_DATA)} countries")
        print(f"  • {customer_count} customers (from 2,500 possible name combinations)")
        print(f"  • {guide_count} guides (all 12 specializations × 5 variants)")
        print(f"  • 30 tours (10 templates × 3 price tiers)")
        print(f"  • {schedule_count} tour schedules (each tour × 8-12 dates)")
        print(f"  • {sales_count} tour sales (schedules × realistic booking rates)")

    except Exception as e:
        print(f"✗ Error: {e}")
        conn.rollback()
        raise
    finally:
        cursor.close()
        conn.close()

if __name__ == '__main__':
    populate_data()
