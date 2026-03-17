.PHONY: help start stop start-s3 stop-s3 logs-s3 s3-shell test test-verbose test-basic test-e2e test-s3 test-xzmercury clean logs status ui mssql-shell pg-shell

COMPOSE := docker compose

# Default target
help:
	@echo "TDTP Integration Tests - Make Commands"
	@echo ""
	@echo "Setup (all services):"
	@echo "  make start        - Start all test services (RabbitMQ, MS SQL, PostgreSQL, SeaweedFS)"
	@echo "  make stop         - Stop all services"
	@echo "  make clean        - Stop and remove all containers and volumes"
	@echo "  make status       - Show status of all services"
	@echo "  make logs         - Show logs from all services"
	@echo ""
	@echo "Setup (S3 only):"
	@echo "  make start-s3     - Start SeaweedFS S3 container only"
	@echo "  make stop-s3      - Stop SeaweedFS container"
	@echo "  make logs-s3      - Show SeaweedFS logs"
	@echo "  make s3-shell     - Open shell in SeaweedFS container"
	@echo ""
	@echo "Testing:"
	@echo "  make test         - Run all integration tests"
	@echo "  make test-verbose - Run tests with verbose output"
	@echo "  make test-basic   - Run basic broker connection tests only"
	@echo "  make test-e2e     - Run end-to-end broker tests only"
	@echo "  make test-s3      - Run S3 storage tests (requires start-s3)"
	@echo "  make test-xzmercury - Run xzmercury pipeline tests"
	@echo ""
	@echo "Utilities:"
	@echo "  make ui           - Show RabbitMQ management UI URL"
	@echo "  make mssql-shell  - Connect to MS SQL shell"
	@echo "  make pg-shell     - Connect to PostgreSQL shell"

# ── Service management ────────────────────────────────────────────────────────

start:
	@echo "Starting all test services..."
	$(COMPOSE) up -d
	@echo "Waiting for services to be healthy..."
	@sleep 5
	@$(COMPOSE) ps

stop:
	@echo "Stopping test services..."
	$(COMPOSE) stop

clean:
	@echo "Cleaning up all containers and volumes..."
	$(COMPOSE) down -v

status:
	@$(COMPOSE) ps

logs:
	$(COMPOSE) logs --tail=100 -f

# ── SeaweedFS S3 ──────────────────────────────────────────────────────────────

start-s3:
	@echo "Starting SeaweedFS S3..."
	$(COMPOSE) up -d seaweedfs
	@echo "Waiting for SeaweedFS to become healthy..."
	@for i in $$(seq 1 30); do \
		if $(COMPOSE) ps seaweedfs 2>/dev/null | grep -q "healthy"; then \
			echo "SeaweedFS is ready"; break; \
		fi; \
		echo "  waiting... ($$i/30)"; sleep 2; \
	done
	@echo ""
	@echo "  S3 endpoint : http://localhost:8333"
	@echo "  Access key  : tdtp_test_key"
	@echo "  Secret key  : tdtp_test_secret_2025"
	@echo "  Test bucket : tdtp-test  (auto-created by tests)"

stop-s3:
	@echo "Stopping SeaweedFS..."
	$(COMPOSE) stop seaweedfs

logs-s3:
	$(COMPOSE) logs --tail=200 -f seaweedfs

s3-shell:
	docker exec -it tdtp-seaweedfs-test sh

# ── Tests ─────────────────────────────────────────────────────────────────────

test:
	@echo "Running integration tests..."
	cd ../.. && go test -v -timeout 120s ./tests/integration/...

test-verbose:
	@echo "Running integration tests (verbose)..."
	cd ../.. && go test -v -timeout 120s -count=1 ./tests/integration/...

test-basic:
	@echo "Running basic broker connection tests..."
	cd ../.. && go test -v -timeout 30s ./tests/integration -run "TestRabbitMQBasicConnection|TestRabbitMQSendReceive"

test-e2e:
	@echo "Running end-to-end broker tests..."
	cd ../.. && go test -v -timeout 60s ./tests/integration -run "TestEndToEndExportImport"

test-s3:
	@echo "Running S3 storage tests (SeaweedFS must be running: make start-s3)..."
	cd ../.. && go test -v -timeout 60s ./tests/integration -run "TestS3"

test-xzmercury:
	@echo "Running xzmercury pipeline integration test..."
	cd ../.. && go test -v -timeout 120s ./tests/integration -run "TestXzmercuryPipeline"

# ── Utilities ─────────────────────────────────────────────────────────────────

ui:
	@echo "RabbitMQ Management UI:"
	@echo "  URL:  http://localhost:15672"
	@echo "  User: tdtp_test"
	@echo "  Pass: tdtp_test_password"
	@command -v xdg-open >/dev/null && xdg-open http://localhost:15672 || \
	 command -v open >/dev/null && open http://localhost:15672 || \
	 echo "  Open http://localhost:15672 in your browser"

mssql-shell:
	docker exec -it tdtp-mssql-test /opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P 'YourStrong!Passw0rd'

pg-shell:
	docker exec -it tdtp-postgres-test psql -U tdtp_test -d tdtp_test_db
