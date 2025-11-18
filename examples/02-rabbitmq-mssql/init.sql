-- Example 02: Integration Test
-- Создаем таблицу со ВСЕМИ типами данных для жесткого теста

USE master;
GO

-- Создаем базу если не существует
IF NOT EXISTS (SELECT name FROM sys.databases WHERE name = 'example02_db')
BEGIN
    CREATE DATABASE example02_db;
END
GO

USE example02_db;
GO

-- Таблица со всеми типами данных
CREATE TABLE orders (
    -- Числовые типы
    order_id INT PRIMARY KEY IDENTITY(1,1),
    quantity BIGINT NOT NULL,
    price DECIMAL(18,2) NOT NULL,
    discount_rate FLOAT,
    tax_rate REAL,
    
    -- Строковые типы
    order_number VARCHAR(50) NOT NULL UNIQUE,
    customer_name NVARCHAR(100) NOT NULL,
    customer_email VARCHAR(255) NOT NULL,
    customer_phone VARCHAR(20),
    billing_address NVARCHAR(MAX),
    notes TEXT,
    
    -- Чувствительные данные для маскирования
    credit_card CHAR(19),
    ssn VARCHAR(11),
    
    -- Дата и время
    order_date DATE NOT NULL,
    order_datetime DATETIME NOT NULL,
    created_at DATETIME2 NOT NULL DEFAULT GETDATE(),
    updated_at DATETIMEOFFSET,
    
    -- Булевы и флаги
    is_paid BIT NOT NULL DEFAULT 0,
    is_shipped BIT NOT NULL DEFAULT 0,
    is_cancelled BIT,
    
    -- Binary данные
    signature VARBINARY(MAX),
    
    -- Специальные типы
    order_status TINYINT NOT NULL,  -- 0=новый, 1=обработан, 2=отгружен
    priority SMALLINT DEFAULT 5
);
GO

-- Индексы для быстрого поиска
CREATE INDEX idx_orders_date ON orders(order_date);
CREATE INDEX idx_orders_status ON orders(order_status, is_paid);
CREATE INDEX idx_orders_customer ON orders(customer_email);
GO

-- Вставляем тестовые данные с разными типами
INSERT INTO orders (
    quantity, price, discount_rate, tax_rate,
    order_number, customer_name, customer_email, customer_phone, billing_address, notes,
    credit_card, ssn,
    order_date, order_datetime, updated_at,
    is_paid, is_shipped, is_cancelled,
    order_status, priority
) VALUES
-- Обычные заказы
(
    5, 150.50, 0.10, 0.2,
    'ORD-2024-001', N'Иван Иванов', 'ivan.ivanov@example.com', '+7-495-123-4567', 
    N'Москва, ул. Ленина, д. 10, кв. 5', 'Позвонить перед доставкой',
    '4532-1234-5678-9010', '123-45-6789',
    '2024-01-15', '2024-01-15 10:30:00', '2024-01-15 10:30:00 +03:00',
    1, 1, 0,
    2, 5
),
(
    10, 299.99, 0.15, 0.2,
    'ORD-2024-002', 'Jane Smith', 'jane.smith@example.com', '+1-555-987-6543',
    '123 Main St, New York, NY 10001, USA', NULL,
    '5412-9876-5432-1098', '987-65-4321',
    '2024-01-16', '2024-01-16 14:20:00', '2024-01-16 14:25:00 -05:00',
    1, 0, 0,
    1, 3
),
-- Заказ с NULL значениями
(
    1, 49.99, NULL, NULL,
    'ORD-2024-003', N'Петр Петров', 'petr@example.com', NULL,
    NULL, NULL,
    NULL, NULL,
    '2024-01-17', '2024-01-17 09:15:00', NULL,
    0, 0, 0,
    0, 5
),
-- Заказ с экстремальными значениями
(
    9999999999, 999999999.99, 0.999, 0.999,
    'ORD-2024-004', 'Bob Wilson', 'bob.wilson@test.org', '+44-20-1234-5678',
    'Very long address that might cause issues with different encodings and special chars: Москва, улица Пушкина дом Колотушкина',
    'Very long notes with special characters: <>&"'' 測試 テスト',
    '3782-8224-6310-005', '555-55-5555',
    '2024-01-18', '2024-01-18 23:59:59', '2024-01-18 23:59:59 +00:00',
    1, 1, 0,
    2, 1
),
-- Отмененный заказ
(
    3, 75.00, 0.05, 0.2,
    'ORD-2024-005', N'Анна Сидорова', 'anna.sidorova@mail.ru', '+7-812-555-9999',
    N'Санкт-Петербург, Невский пр., 100', 'Отменен покупателем',
    '6011-1111-1111-1111', '222-22-2222',
    '2024-01-19', '2024-01-19 12:00:00', '2024-01-19 13:00:00 +03:00',
    0, 0, 1,
    0, 5
),
-- Заказы с разными статусами
(
    2, 199.99, 0.20, 0.2,
    'ORD-2024-006', 'Charlie Brown', 'charlie@peanuts.com', '+1-555-123-0000',
    '1 Peanuts Way, Anytown, USA', 'Handle with care',
    '4111-1111-1111-1111', '111-11-1111',
    '2024-01-20', '2024-01-20 08:00:00', '2024-01-20 08:05:00 -08:00',
    1, 0, 0,
    1, 2
),
(
    50, 1250.00, 0.25, 0.18,
    'ORD-2024-007', N'李明', 'li.ming@example.cn', '+86-10-1234-5678',
    N'北京市朝阳区建国路100号', N'大批量订单',
    '5500-0000-0000-0004', '000-00-0000',
    '2024-01-21', '2024-01-21 16:30:00', '2024-01-21 16:35:00 +08:00',
    0, 0, 0,
    0, 1
),
-- Заказ с минимальными значениями
(
    1, 0.01, 0.00, 0.00,
    'ORD-2024-008', 'Test User', 'test@test.test', '+1-000-000-0000',
    'Test Address', 'Test Order',
    '0000-0000-0000-0001', '000-00-0001',
    '2024-01-22', '2024-01-22 00:00:00', '2024-01-22 00:00:00 +00:00',
    0, 0, 0,
    0, 5
);
GO

-- Статистика
SELECT 
    COUNT(*) as total_orders,
    SUM(CASE WHEN is_paid = 1 THEN 1 ELSE 0 END) as paid_orders,
    SUM(CASE WHEN is_shipped = 1 THEN 1 ELSE 0 END) as shipped_orders,
    SUM(CASE WHEN is_cancelled = 1 THEN 1 ELSE 0 END) as cancelled_orders,
    SUM(quantity * price) as total_value
FROM orders;
GO

PRINT 'Database example02_db initialized successfully';
PRINT 'Table orders created with 8 test records';
GO
