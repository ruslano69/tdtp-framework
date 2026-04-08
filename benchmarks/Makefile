# TDTP Framework Benchmarks

.PHONY: bench bench-all bench-core bench-kafka bench-adapters bench-etl bench-parser bench-db

# Run all benchmarks
bench-all:
	@echo "=== Running all benchmarks ==="
	@go test -bench=. -benchmem ./pkg/core/packet/... 2>&1 | grep -E "^(Benchmark|PASS|ok)"
	@go test -bench=. -benchmem ./pkg/brokers/... 2>&1 | grep -E "^(Benchmark|PASS|ok|SKIP)" || true
	@go test -bench=. -benchmem ./pkg/adapters/... 2>&1 | grep -E "^(Benchmark|PASS|ok)"
	@go test -bench=. -benchmem ./pkg/etl/... 2>&1 | grep -E "^(Benchmark|PASS|ok|SKIP)" || true

# Core benchmarks (XML parsing, compression)
bench-core:
	@echo "=== Core: XML parsing, compression ==="
	go test -bench=. -benchmem ./pkg/core/packet/...

# Parser specific
bench-parser:
	@echo "=== Parser: XML parsing ==="
	go test -bench=Parse -benchmem ./pkg/core/packet/...

# Broker benchmarks (Kafka, RabbitMQ, MSMQ)
# Tests will skip automatically if Kafka is not available
bench-kafka:
	@echo "=== Brokers: Kafka performance ==="
	go test -bench=. -benchmem ./pkg/brokers/...

# Adapter benchmarks (SQLite, PostgreSQL, MSSQL, MySQL)
bench-adapters:
	@echo "=== Adapters: DB comparison ==="
	go test -bench=. -benchmem ./pkg/adapters/...

# Database specific
bench-db:
	@echo "=== Database: SQLite benchmark ==="
	go test -bench=. -benchmem ./pkg/adapters/sqlite/...

# ETL benchmarks (pipeline, Kafka spool)
# Tests will skip automatically if Kafka is not available
bench-etl:
	@echo "=== ETL: Pipeline and spool ==="
	go test -bench=. -benchmem ./pkg/etl/...

# Default target
bench: bench-all