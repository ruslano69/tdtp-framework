.PHONY: help start stop test test-verbose test-basic test-e2e clean logs status ui

# Default target
help:
	@echo "TDTP Integration Tests - Make Commands"
	@echo ""
	@echo "Setup:"
	@echo "  make start        - Start all test services (RabbitMQ, MS SQL, PostgreSQL)"
	@echo "  make stop         - Stop all services"
	@echo "  make clean        - Stop and remove all containers and volumes"
	@echo "  make status       - Show status of all services"
	@echo "  make logs         - Show logs from all services"
	@echo ""
	@echo "Testing:"
	@echo "  make test         - Run all integration tests"
	@echo "  make test-verbose - Run tests with verbose output"
	@echo "  make test-basic   - Run basic connection tests only"
	@echo "  make test-e2e     - Run end-to-end tests only"
	@echo ""
	@echo "Utilities:"
	@echo "  make ui           - Open RabbitMQ management UI"
	@echo "  make mssql-shell  - Connect to MS SQL shell"
	@echo "  make pg-shell     - Connect to PostgreSQL shell"

# Start all services
start:
	@echo "🚀 Starting test services..."
	docker-compose up -d
	@echo "⏳ Waiting for services to be healthy..."
	@sleep 5
	@docker-compose ps

# Stop all services
stop:
	@echo "🛑 Stopping test services..."
	docker-compose stop

# Clean up everything
clean:
	@echo "🧹 Cleaning up all containers and volumes..."
	docker-compose down -v

# Show service status
status:
	@docker-compose ps

# Show logs
logs:
	docker-compose logs --tail=100 -f

# Run all tests
test:
	@echo "🧪 Running integration tests..."
	cd ../.. && go test -v -timeout 60s ./tests/integration/...

# Run tests with verbose output
test-verbose:
	@echo "🧪 Running integration tests (verbose)..."
	cd ../.. && go test -v -timeout 60s ./tests/integration/... -args -test.v

# Run basic connection tests
test-basic:
	@echo "🧪 Running basic connection tests..."
	cd ../.. && go test -v -timeout 30s ./tests/integration -run "TestRabbitMQBasicConnection|TestRabbitMQSendReceive"

# Run end-to-end tests
test-e2e:
	@echo "🧪 Running end-to-end tests..."
	cd ../.. && go test -v -timeout 60s ./tests/integration -run "TestEndToEndExportImport"

# Run xzmercury + pipeline integration test (no Docker required)
test-xzmercury:
	@echo "🔐 Running xzmercury pipeline integration test..."
	cd ../.. && go test -v -timeout 120s ./tests/integration -run "TestXzmercuryPipeline"

# Open RabbitMQ UI
ui:
	@echo "🌐 Opening RabbitMQ Management UI..."
	@echo "   URL: http://localhost:15672"
	@echo "   User: tdtp_test"
	@echo "   Pass: tdtp_test_password"
	@command -v xdg-open >/dev/null && xdg-open http://localhost:15672 || \
	 command -v open >/dev/null && open http://localhost:15672 || \
	 echo "   Please open http://localhost:15672 in your browser"

# Connect to MS SQL shell
mssql-shell:
	@echo "🗄️  Connecting to MS SQL Server..."
	docker exec -it tdtp-mssql-test /opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P 'YourStrong!Passw0rd'

# Connect to PostgreSQL shell
pg-shell:
	@echo "🗄️  Connecting to PostgreSQL..."
	docker exec -it tdtp-postgres-test psql -U tdtp_test -d tdtp_test_db
