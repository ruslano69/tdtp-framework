# TDTP X-Ray Test Databases

Docker Compose setup for testing tdtp-xray with real databases.

## Quick Start

### 1. Start all databases:
```bash
cd cmd/tdtp-xray
docker-compose up -d
```

### 2. Wait for databases to be ready (30 seconds):
```bash
docker-compose ps
```

### 3. Initialize MSSQL manually (required):
```bash
# MSSQL doesn't auto-run init scripts, so run manually:
docker exec -it tdtp-xray-mssql /opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P 'TestPass123!' -i /docker-entrypoint-initdb.d/init.sql
```

### 4. Verify databases are working:
```bash
# PostgreSQL
docker exec tdtp-xray-postgres psql -U testuser -d testdb -c "SELECT * FROM customers;"

# MySQL
docker exec tdtp-xray-mysql mysql -u testuser -ptestpass testdb -e "SELECT * FROM customers;"

# MSSQL
docker exec tdtp-xray-mssql /opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P 'TestPass123!' -d testdb -Q "SELECT * FROM customers;"
```

## DSN Connection Strings for TDTP X-Ray

Use these in Step 2 (Sources) of the wizard:

### PostgreSQL
```
Type: postgres
DSN:  host=localhost port=5432 user=testuser password=testpass dbname=testdb sslmode=disable
Query: SELECT * FROM customers
```

### MySQL
```
Type: mysql
DSN:  testuser:testpass@tcp(localhost:3306)/testdb
Query: SELECT * FROM customers
```

### Microsoft SQL Server
```
Type: mssql
DSN:  sqlserver://sa:TestPass123!@localhost:1433?database=testdb
Query: SELECT * FROM customers
```

### SQLite (local file)
```
Type: sqlite
DSN:  test.db
Query: SELECT * FROM customers
```

## Web Interface (Adminer)

Access database management UI:
```
http://localhost:8080
```

Credentials:
- **PostgreSQL:** Server: `postgres`, User: `testuser`, Password: `testpass`, Database: `testdb`
- **MySQL:** Server: `mysql`, User: `testuser`, Password: `testpass`, Database: `testdb`
- **MSSQL:** Server: `mssql`, User: `sa`, Password: `TestPass123!`, Database: `testdb`

## Test Data Schema

All databases contain:

### Tables:
- **customers**: id, name, email, created_at (5 rows)
- **orders**: id, customer_id, amount, status, created_at (6 rows)
- **products**: id, name, price, stock (5 rows)

### Views:
- **customer_orders**: Aggregated customer order statistics

## Example Queries for Testing

### Simple SELECT
```sql
SELECT * FROM customers;
```

### JOIN Query
```sql
SELECT
    c.name,
    c.email,
    o.amount,
    o.status
FROM customers c
LEFT JOIN orders o ON c.id = o.customer_id
WHERE o.status = 'pending';
```

### Aggregation
```sql
SELECT
    c.name,
    COUNT(o.id) as order_count,
    SUM(o.amount) as total_spent
FROM customers c
LEFT JOIN orders o ON c.id = o.customer_id
GROUP BY c.id, c.name;
```

### View Query
```sql
SELECT * FROM customer_orders;
```

## Stopping and Cleanup

### Stop containers (keep data):
```bash
docker-compose stop
```

### Stop and remove containers (keep data):
```bash
docker-compose down
```

### Remove everything including data:
```bash
docker-compose down -v
```

## Troubleshooting

### PostgreSQL not accepting connections:
```bash
docker-compose restart postgres
docker-compose logs postgres
```

### MySQL not accepting connections:
```bash
docker-compose restart mysql
docker-compose logs mysql
```

### MSSQL not accepting connections:
```bash
docker-compose restart mssql
docker-compose logs mssql
```

### Check container health:
```bash
docker-compose ps
```

All services should show "healthy" status after ~30 seconds.

## Port Conflicts

If ports are already in use, modify `docker-compose.yml`:

- PostgreSQL: Change `5432:5432` to `5433:5432`
- MySQL: Change `3306:3306` to `3307:3306`
- MSSQL: Change `1433:1433` to `1434:1433`
- Adminer: Change `8080:8080` to `8081:8080`

Update DSN strings accordingly.
