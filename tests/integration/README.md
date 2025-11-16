# Integration Tests for TDTP Framework

This directory contains integration tests for the TDTP framework, specifically testing:
- RabbitMQ message broker integration
- End-to-end data flow (Database → Queue → Database)
- Queue parameter validation
- MS SQL Server adapter integration

## Prerequisites

- Docker and Docker Compose
- Go 1.24+

## Setup

1. Start the test environment:
```bash
docker-compose up -d
```

This will start:
- RabbitMQ (port 5672, management UI on 15672)
- MS SQL Server 2019 (port 1433)
- PostgreSQL 16 (port 5432)

2. Wait for services to be healthy:
```bash
docker-compose ps
```

All services should show "healthy" status.

## Running Tests

### All integration tests:
```bash
go test -v ./tests/integration/...
```

### Specific test:
```bash
go test -v ./tests/integration -run TestRabbitMQBasicConnection
```

### With timeout:
```bash
go test -v -timeout 30s ./tests/integration/...
```

## Test Scenarios

### 1. TestRabbitMQBasicConnection
Tests basic connection to RabbitMQ broker.

**What it does:**
- Creates RabbitMQ broker instance
- Connects to RabbitMQ
- Verifies ping functionality

**Expected result:** ✅ Successfully connected

### 2. TestRabbitMQSendReceive
Tests message sending and receiving through RabbitMQ.

**What it does:**
- Creates sender and receiver broker instances
- Sends XML test message
- Receives and validates message

**Expected result:** ✅ Message sent and received correctly

### 3. TestEndToEndExportImport
Full end-to-end workflow test.

**What it does:**
1. Creates test table in MS SQL Server
2. Exports table to TDTP packets
3. Sends packets to RabbitMQ queue
4. Receives packets from queue
5. Imports packets back to database
6. Validates data integrity

**Expected result:** ✅ Complete workflow successful

### 4. TestQueueParametersMatching
Tests that queue parameters must match existing queue.

**What it does:**
- Creates queue with specific parameters (durable=true)
- Attempts to connect with different parameters (durable=false)
- Verifies RabbitMQ rejects connection

**Expected result:** ✅ Parameter mismatch detected

## RabbitMQ Management UI

Access the RabbitMQ management UI at: http://localhost:15672

**Credentials:**
- Username: `tdtp_test`
- Password: `tdtp_test_password`

Use this to:
- Monitor queue messages
- View connections
- Debug message flow

## Database Connections

### MS SQL Server
```
Server: localhost,1433
User: sa
Password: YourStrong!Passw0rd
Database: master
```

**Connection string:**
```
sqlserver://sa:YourStrong!Passw0rd@localhost:1433?database=master
```

### PostgreSQL
```
Host: localhost
Port: 5432
User: tdtp_test
Password: tdtp_test_password
Database: tdtp_test_db
```

**Connection string:**
```
postgres://tdtp_test:tdtp_test_password@localhost:5432/tdtp_test_db
```

## Cleanup

Stop and remove all containers:
```bash
docker-compose down -v
```

The `-v` flag removes volumes (clears all data).

## Troubleshooting

### Services not starting
Check logs:
```bash
docker-compose logs rabbitmq
docker-compose logs mssql
```

### Connection refused errors
Wait for services to be fully ready:
```bash
# Check RabbitMQ
docker exec tdtp-rabbitmq-test rabbitmq-diagnostics ping

# Check MS SQL
docker exec tdtp-mssql-test /opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P 'YourStrong!Passw0rd' -Q 'SELECT @@VERSION'
```

### RabbitMQ queue parameter errors
If you see errors like "inequivalent arg 'durable'", delete the queue manually:
```bash
# Via management UI or CLI
docker exec tdtp-rabbitmq-test rabbitmqadmin delete queue name=test_queue_name -u tdtp_test -p tdtp_test_password
```

## CI/CD Integration

For CI pipelines, use this workflow:

```bash
# Start services in background
docker-compose up -d

# Wait for health checks
timeout 60 sh -c 'until docker-compose ps | grep -q "healthy"; do sleep 2; done'

# Run tests
go test -v -timeout 60s ./tests/integration/...

# Cleanup
docker-compose down -v
```

## Notes

- Tests use temporary queues with `auto_delete=true` to clean up automatically
- Integration tests may take 10-30 seconds to complete
- Ensure ports 5672, 15672, 1433, and 5432 are available
- For Windows, ensure Docker Desktop is running with Linux containers
