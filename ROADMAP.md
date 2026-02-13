# TDTP Framework - Roadmap

> **Last updated:** February 2026
> **Current version:** v1.6.0

## 🎯 Vision

Полноценная **ETL/ELT платформа** для безопасного обмена данными между различными системами с поддержкой Python, Go и автоматизации DevOps.

---

## ✅ Released (v1.6.0 - Current)

### 🐍 Python Integration
- **Python module** (`pkg/python/libtdtp/`) - CGo-based Python bindings
- **pandas integration** - direct TDTP ↔ DataFrame conversion
- **Fallback XML parser** - pure Python parser when CGo unavailable
- **pip package** - easy installation via `pip install libtdtp`

### 📊 Database Views Support
- **`--list-views`** - browse database views across all adapters
- **View export automation** - export materialized views as TDTP
- **Test view scripts** - automated test data generation
- **Documentation** - complete guide for view-based workflows

### 📈 Performance Improvements
- **SQLite import** - 20-50x faster with batch optimizations
- **Parallel processing** - concurrent import/export pipelines
- **Memory optimization** - reduced allocations in hot paths
- **Connection pooling** - efficient database connection reuse

### 🔧 Developer Tools
- **funcfinder integration** - AI-powered code analysis (1322 functions mapped)
- **Git hooks** - auto-update code maps on commit
- **golangci-lint** - comprehensive linting with performance checks
- **Repository cleanup** - professional project structure

### 🛡️ Security & Reliability
- **Go 1.24.13** - latest security patches (crypto/tls CVE fix)
- **Circuit breaker** - protection against cascading failures
- **Retry mechanisms** - exponential backoff with jitter
- **Dead Letter Queue** - failed message handling
- **Audit logging** - GDPR/HIPAA/SOX compliance

### 💾 Data Processing
- **XLSX ↔ TDTP** - bidirectional Excel conversion
- **Field masking** - email, phone, password masking
- **Field normalization** - phone, email, date normalization
- **Field validation** - regex, range, format validation
- **Compression** - zstd compression for large datasets

### 🗄️ Database Adapters
- **SQLite** - production-ready with optimizations
- **PostgreSQL** - full feature support
- **MS SQL Server** - enterprise-grade adapter
- **MySQL** - complete implementation
- **Incremental sync** - timestamp-based delta exports

### 🚀 Message Brokers
- **RabbitMQ** - production-tested integration
- **Kafka** - high-throughput support
- **MSMQ** - Windows message queue (stub)

### 📝 Recent Bug Fixes (Jan-Feb 2026)
- ETL case mismatch - unified type format handling
- TDTQL case sensitivity - field name normalization
- Multi-part import - schema validation across packets
- SQLite DATE parsing - ISO8601 format support
- Empty strings in NOT NULL - proper validation
- RabbitMQ busy-loop - fixed receive deadlock
- Parallel importer - batch validation fixes

---

## 🚧 In Progress (v1.7.0 - Next Release)

### 📚 Documentation Improvements
- [ ] API reference generation (godoc)
- [ ] Tutorial videos for common workflows
- [ ] Migration guides for v1.x → v2.0
- [ ] Performance tuning guide

### 🧪 Testing Infrastructure
- [ ] Integration tests for all brokers
- [ ] Load testing framework
- [ ] Chaos testing for resilience
- [ ] Benchmark suite for regressions

### 🔄 CI/CD Enhancements
- [ ] Automated benchmarking on PRs
- [ ] Security scanning with Trivy
- [ ] Dependency update automation
- [ ] Release automation with goreleaser

---

## 📋 Planned (v2.0 - Future)

### 🌐 REST API
```yaml
- HTTP server mode for TDTP operations
- RESTful API for remote execution
- Authentication/authorization
- Rate limiting and quotas
- OpenAPI/Swagger documentation
```

### 🎨 Web UI
```yaml
- Visual pipeline builder
- Real-time monitoring dashboard
- Schema designer
- Query builder (TDTQL)
- Audit log viewer
```

### ☁️ Cloud Native
```yaml
- Kubernetes operator
- Helm charts
- Cloud storage support (S3, GCS, Azure Blob)
- Distributed tracing (OpenTelemetry)
- Prometheus metrics
```

### 🔌 Additional Adapters
```yaml
- Oracle Database
- MongoDB
- Redis
- Cassandra
- Snowflake
```

### 🤖 Advanced Automation
```yaml
- Schema migration toolkit
- Data quality monitoring
- Anomaly detection
- Auto-scaling workers
- Smart retry policies based on ML
```

---

## 🎁 Wishlist (Community Requests)

- [ ] GraphQL API for flexible queries
- [ ] Real-time data streaming (WebSockets)
- [ ] Multi-tenant support
- [ ] Data lineage tracking
- [ ] Version control for schemas
- [ ] Diff/merge for data conflicts
- [ ] CLI plugins system
- [ ] Custom processor SDK

---

## 📊 Version History

| Version | Date | Highlights |
|---------|------|------------|
| **v1.6.0** | Feb 2026 | Python module, views support, performance |
| v1.5.0 | Jan 2026 | ETL fixes, XLSX improvements |
| v1.4.0 | Dec 2025 | Resilience features, audit logging |
| v1.3.0 | Nov 2025 | Processors, validation |
| v1.2.0 | Oct 2025 | Incremental sync, retry logic |
| v1.1.0 | Sep 2025 | XLSX converter, MySQL adapter |
| v1.0.0 | Aug 2025 | Initial release |

---

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

**Priority areas:**
1. Testing infrastructure (highest impact)
2. Documentation improvements
3. Performance optimizations
4. Cloud native features

---

## 📞 Support

- **Issues:** https://github.com/ruslano69/tdtp-framework/issues
- **Discussions:** https://github.com/ruslano69/tdtp-framework/discussions
- **Email:** support@tdtp-framework.dev (planned)

---

*This roadmap is updated quarterly based on community feedback and project needs.*
