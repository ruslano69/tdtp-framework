-- Example 01: Basic Export
-- Создаем тестовую таблицу users и заполняем данными

CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    is_active BOOLEAN NOT NULL DEFAULT true
);

-- Вставляем тестовые данные
INSERT INTO users (name, email, created_at, is_active) VALUES
    ('John Doe', 'john.doe@example.com', '2024-01-15 10:30:00', true),
    ('Jane Smith', 'jane.smith@example.com', '2024-01-15 11:00:00', true),
    ('Bob Wilson', 'bob.wilson@example.com', '2024-01-16 09:15:00', true),
    ('Alice Brown', 'alice.brown@example.com', '2024-01-16 14:20:00', false),
    ('Charlie Davis', 'charlie.davis@example.com', '2024-01-17 08:45:00', true);

-- Создаем индексы
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_created_at ON users(created_at);

-- Проверяем что данные созданы
SELECT 'Users table created with ' || COUNT(*) || ' records' as status FROM users;
