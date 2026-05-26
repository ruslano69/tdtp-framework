-- Seed data for central travel agency DB
-- Run after setup_database_postgres.sql + setup_central_additions.sql

-- ─── Guides ───────────────────────────────────────────────────────────────────
INSERT INTO guides (first_name, last_name, email, phone, languages, specialization, experience_years, rating, hire_date, is_active) VALUES
('Olena',   'Kovalchuk',  'o.kovalchuk@travel.ua',  '+380671234001', '["uk","en","de"]', 'Cultural Tours',     8,  4.8, '2016-03-01', TRUE),
('Mykola',  'Petrenko',   'm.petrenko@travel.ua',   '+380671234002', '["uk","en","pl"]', 'Adventure Tourism', 12, 4.9, '2012-05-15', TRUE),
('Sofia',   'Bondar',     's.bondar@travel.ua',      '+380671234003', '["uk","en","fr"]', 'City Tours',         5,  4.6, '2019-01-10', TRUE),
('Andriy',  'Kravchenko', 'a.kravchenko@travel.ua', '+380671234004', '["uk","en"]',      'Beach Resorts',      7,  4.7, '2017-06-20', TRUE),
('Iryna',   'Shevchenko', 'i.shevchenko@travel.ua', '+380671234005', '["uk","en","tr"]', 'Eco Tourism',        4,  4.5, '2020-02-01', TRUE),
('Vasyl',   'Tkachenko',  'v.tkachenko@travel.ua',  '+380671234006', '["uk","en","it"]', 'Culinary Tours',     6,  4.4, '2018-09-01', TRUE),
('Natalia', 'Melnyk',     'n.melnyk@travel.ua',     '+380671234007', '["uk","en","es"]', 'Historical Tours',   9,  4.7, '2015-04-12', TRUE),
('Dmytro',  'Savchenko',  'd.savchenko@travel.ua',  '+380671234008', '["uk","en"]',      'Ski Resorts',        3,  4.3, '2021-11-01', TRUE)
ON CONFLICT DO NOTHING;

-- ─── Tours ────────────────────────────────────────────────────────────────────
INSERT INTO tours (tour_code, tour_name, destination_country_id, duration_days, description, base_price, max_group_size, difficulty_level, is_active)
SELECT
    code, name,
    (SELECT country_id FROM countries WHERE country_name = country_nm LIMIT 1),
    days, descr, price, mx, diff, TRUE
FROM (VALUES
    ('PARIS-5D',  'Paris Classic',        'France',         5,  'Eiffel Tower, Louvre, Versailles',          890.00,  12, 'Easy'),
    ('ROME-7D',   'Rome Eternal City',    'Italy',          7,  'Colosseum, Vatican, Trastevere',           1050.00,  10, 'Easy'),
    ('ANTALYA-7', 'Turkey All Inclusive', 'Turkey',         7,  'Beach resort, excursions to ruins',         750.00,  20, 'Easy'),
    ('EGYPT-10',  'Egypt Pyramids',       'Egypt',          10, 'Pyramids, Luxor, Nile cruise',             1200.00,  15, 'Moderate'),
    ('THAI-14',   'Thailand Explorer',    'Thailand',       14, 'Bangkok, Chiang Mai, Phuket',              1800.00,  12, 'Moderate'),
    ('CZECH-5',   'Prague & Brno',        'Czech Republic', 5,  'Old Town, Karlstejn Castle, beer tours',    650.00,  18, 'Easy'),
    ('SPAIN-10',  'Spain Grand Tour',     'Spain',          10, 'Madrid, Barcelona, Seville, Granada',     1350.00,  14, 'Moderate'),
    ('SKIING-7',  'Alpine Ski Week',      'Germany',        7,  'Bavarian Alps, ski lessons, apres ski',   1600.00,   8, 'Challenging'),
    ('LONDON-5',  'London Highlights',    'United Kingdom', 5,  'Tower, Big Ben, Thames cruise, West End',   900.00,  15, 'Easy'),
    ('KYIV-3',    'Kyiv City Break',      'Ukraine',        3,  'Maidan, Lavra, Andriyivsky, food tour',     350.00,  20, 'Easy')
) AS t(code, name, country_nm, days, descr, price, mx, diff)
WHERE (SELECT country_id FROM countries WHERE country_name = country_nm LIMIT 1) IS NOT NULL
ON CONFLICT DO NOTHING;

-- ─── Tour Schedule (next 6 months) ────────────────────────────────────────────
INSERT INTO tour_schedule (tour_id, guide_id, start_date, end_date, available_slots, booked_slots, price_modifier, status)
SELECT
    t.tour_id,
    g.guide_id,
    start_d,
    start_d + (t.duration_days || ' days')::interval,
    t.max_group_size,
    0,
    modifier,
    'Scheduled'
FROM tours t
CROSS JOIN (
    VALUES
    (CURRENT_DATE + 20,  0.95),
    (CURRENT_DATE + 45,  1.00),
    (CURRENT_DATE + 75,  1.05),
    (CURRENT_DATE + 105, 1.00),
    (CURRENT_DATE + 135, 0.98),
    (CURRENT_DATE + 165, 1.10)
) AS sched(start_d, modifier)
JOIN LATERAL (
    SELECT guide_id FROM guides WHERE is_active = TRUE ORDER BY RANDOM() LIMIT 1
) g ON TRUE
WHERE t.is_active = TRUE
ON CONFLICT DO NOTHING;

-- ─── Sample Customers ─────────────────────────────────────────────────────────
INSERT INTO customers (customer_uuid, first_name, last_name, email, phone, date_of_birth, nationality_country_id, city, loyalty_points, is_vip, preferences, last_updated)
SELECT
    gen_random_uuid(),
    fn, ln,
    lower(fn) || '.' || lower(ln) || seq::text || '@gmail.com',
    '+38050' || (1000000 + seq * 37)::text,
    '1975-01-01'::date + (seq * 233 || ' days')::interval,
    (SELECT country_id FROM countries WHERE country_code = ccode LIMIT 1),
    city,
    (seq * 13) % 1000,
    seq % 10 = 0,
    '{"dietary": "none", "activities": ["cultural"]}',
    NOW()
FROM (
    VALUES
    (1,  'Alice',   'Smith',      'UKR', 'Kyiv'),
    (2,  'Bob',     'Jones',      'POL', 'Lviv'),
    (3,  'Charlie', 'Brown',      'UKR', 'Odesa'),
    (4,  'Diana',   'Davis',      'UKR', 'Kyiv'),
    (5,  'Eve',     'Miller',     'DEU', 'Kharkiv'),
    (6,  'Frank',   'Wilson',     'UKR', 'Dnipro'),
    (7,  'Grace',   'Moore',      'GBR', 'Lviv'),
    (8,  'Hank',    'Taylor',     'UKR', 'Kyiv'),
    (9,  'Iris',    'Anderson',   'FRA', 'Odesa'),
    (10, 'Jack',    'Thomas',     'UKR', 'Kyiv'),
    (11, 'Kate',    'Jackson',    'UKR', 'Lviv'),
    (12, 'Leo',     'White',      'ITA', 'Kharkiv'),
    (13, 'Mia',     'Harris',     'UKR', 'Kyiv'),
    (14, 'Nick',    'Martin',     'UKR', 'Dnipro'),
    (15, 'Olivia',  'Kovalenko',  'UKR', 'Lviv'),
    (16, 'Paul',    'Shevchenko', 'UKR', 'Kyiv'),
    (17, 'Quinn',   'Bondarenko', 'POL', 'Odesa'),
    (18, 'Rose',    'Tkachenko',  'UKR', 'Kyiv'),
    (19, 'Sam',     'Kravchenko', 'UKR', 'Lviv'),
    (20, 'Tina',    'Savchenko',  'UKR', 'Kyiv')
) AS c(seq, fn, ln, ccode, city)
ON CONFLICT DO NOTHING;

-- ─── Verify ───────────────────────────────────────────────────────────────────
SELECT 'countries'    AS tbl, COUNT(*) FROM countries
UNION ALL SELECT 'guides',        COUNT(*) FROM guides
UNION ALL SELECT 'tours',         COUNT(*) FROM tours
UNION ALL SELECT 'tour_schedule', COUNT(*) FROM tour_schedule
UNION ALL SELECT 'customers',     COUNT(*) FROM customers;
