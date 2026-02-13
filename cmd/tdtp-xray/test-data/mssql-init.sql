-- Microsoft SQL Server test data initialization
-- Note: This script will be run manually after container starts

USE master;
GO

IF NOT EXISTS (SELECT name FROM sys.databases WHERE name = 'testdb')
BEGIN
    CREATE DATABASE testdb;
END
GO

USE testdb;
GO

-- Create test tables
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'customers')
BEGIN
    CREATE TABLE customers (
        id INT IDENTITY(1,1) PRIMARY KEY,
        name NVARCHAR(100) NOT NULL,
        email NVARCHAR(100) UNIQUE NOT NULL,
        created_at DATETIME2 DEFAULT GETDATE()
    );
END
GO

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'orders')
BEGIN
    CREATE TABLE orders (
        id INT IDENTITY(1,1) PRIMARY KEY,
        customer_id INT FOREIGN KEY REFERENCES customers(id),
        amount DECIMAL(10, 2) NOT NULL,
        status NVARCHAR(20) DEFAULT 'pending',
        created_at DATETIME2 DEFAULT GETDATE()
    );
END
GO

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'products')
BEGIN
    CREATE TABLE products (
        id INT IDENTITY(1,1) PRIMARY KEY,
        name NVARCHAR(100) NOT NULL,
        price DECIMAL(10, 2) NOT NULL,
        stock INT DEFAULT 0
    );
END
GO

-- Insert test data
IF NOT EXISTS (SELECT * FROM customers)
BEGIN
    INSERT INTO customers (name, email) VALUES
        ('John Doe', 'john@example.com'),
        ('Jane Smith', 'jane@example.com'),
        ('Bob Johnson', 'bob@example.com'),
        ('Alice Williams', 'alice@example.com'),
        ('Charlie Brown', 'charlie@example.com');
END
GO

IF NOT EXISTS (SELECT * FROM products)
BEGIN
    INSERT INTO products (name, price, stock) VALUES
        ('Laptop', 999.99, 50),
        ('Mouse', 29.99, 200),
        ('Keyboard', 79.99, 150),
        ('Monitor', 299.99, 75),
        ('Headphones', 149.99, 100);
END
GO

IF NOT EXISTS (SELECT * FROM orders)
BEGIN
    INSERT INTO orders (customer_id, amount, status) VALUES
        (1, 999.99, 'completed'),
        (1, 29.99, 'completed'),
        (2, 79.99, 'pending'),
        (3, 299.99, 'shipped'),
        (4, 149.99, 'completed'),
        (5, 999.99, 'pending');
END
GO

-- Create view for testing
IF NOT EXISTS (SELECT * FROM sys.views WHERE name = 'customer_orders')
BEGIN
    EXEC('
    CREATE VIEW customer_orders AS
    SELECT
        c.id as customer_id,
        c.name as customer_name,
        c.email,
        COUNT(o.id) as order_count,
        ISNULL(SUM(o.amount), 0) as total_spent
    FROM customers c
    LEFT JOIN orders o ON c.id = o.customer_id
    GROUP BY c.id, c.name, c.email
    ');
END
GO
