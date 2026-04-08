# TDTP Framework Benchmarks

.PHONY: bench bench-all bench-core bench-kafka bench-adapters bench-etl bench-parser bench-db

# Run all benchmarks
bench-all:
	@echo "=== Running all benchmarks ==="
	@go test -bench=. -benchmem ./pkg/core/packet/... 2>&1 | grep -E "^(Benchmark|PASS|ok)"
	@go test -bench=. -benchmem ./pkg/brokers/... -tags='!nokafka' 2>&1 | grep -E "^(Benchmark|PASS|ok)" || true
	@go test -bench=. -benchmem ./pkg/adapters/... 2>&1 | grep -E "^(Benchmark|PASS|ok)"
	@go test -bench=. -benchmem ./pkg/etl/... -tags='!nokafka' 2>&1 | grep -E "^(Benchmark|PASS|ok)" || true

# Core benchmarks (XML parsing, compression)
bench-core:
	@echo "=== Core: XML parsing, compression ==="
	go test -bench=. -benchmem ./pkg/core/packet/...

# Parser specific
bench-parser:
	@echo "=== Parser: XML parsing ==="
	go test -bench=Parse -benchmem ./pkg/core/packet/...

# Broker benchmarks (Kafka, RabbitMQ, MSMQ)
bench-kafka:
	@echo "=== Brokers: Kafka performance ==="
	go test -bench=. -benchmem ./pkg/brokers/... -tags='!nokafka'

# Adapter benchmarks (SQLite, PostgreSQL, MSSQL, MySQL)
bench-adapters:
	@echo "=== Adapters: DB comparison ==="
	go test -bench=. -benchmem ./pkg/adapters/...

# Database specific
bench-db:
	@echo "=== Database: SQLite benchmark ==="
	go test -bench=. -benchmem ./pkg/adapters/sqlite/...

# ETL benchmarks (pipeline, Kafka spool)
bench-etl:
	@echo "=== ETL: Pipeline and spool ==="
	go test -bench=. -benchmem ./pkg/etl/... -tags='!nokafka'

# Default target
bench: bench-all