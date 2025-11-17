# AuditLogger

Comprehensive audit logging system for tracking all operations, changes, and access events in your application. Essential for compliance (GDPR, HIPAA, SOX), security auditing, and forensics.

## Features

- **Multiple Appenders**: File, Database, Console, or custom implementations
- **Flexible Logging Levels**: Minimal, Standard, Full for different security requirements
- **Async/Sync Modes**: High-performance async logging with buffering
- **File Rotation**: Automatic log rotation with configurable size and backups
- **Database Support**: SQL storage with querying, filtering, and cleanup
- **Batch Processing**: Efficient batch inserts for high-volume scenarios
- **Builder Pattern**: Fluent API for creating audit entries
- **Thread-Safe**: Safe for concurrent use
- **GDPR Compliant**: Filterable sensitive data at different levels

## Installation

```bash
go get github.com/queuebridge/tdtp/pkg/audit
```

## Quick Start

### Basic File Logging

```go
package main

import (
    "context"
    "github.com/queuebridge/tdtp/pkg/audit"
)

func main() {
    // Create file appender
    fileAppender, err := audit.NewFileAppender(audit.FileAppenderConfig{
        FilePath:   "./logs/audit.log",
        MaxSize:    100, // 100 MB
        MaxBackups: 10,
        Level:      audit.LevelStandard,
        FormatJSON: true,
    })
    if err != nil {
        panic(err)
    }
    defer fileAppender.Close()

    // Create logger
    config := audit.DefaultConfig()
    logger := audit.NewLogger(config, fileAppender)
    defer logger.Close()

    // Log an export operation
    entry := logger.LogSuccess(context.Background(), audit.OpExport)
    entry.WithUser("john.doe").
        WithSource("mysql://prod-db").
        WithTarget("s3://backups/data.csv").
        WithResource("users").
        WithRecordsAffected(1000)
}
```

### Database Logging

```go
import (
    "database/sql"
    _ "github.com/lib/pq" // PostgreSQL driver
)

func setupDatabaseAudit(db *sql.DB) *audit.AuditLogger {
    // Create database appender
    dbAppender, err := audit.NewDatabaseAppender(audit.DatabaseAppenderConfig{
        DB:              db,
        TableName:       "audit_log",
        Level:           audit.LevelFull,
        BatchSize:       100, // Batch 100 entries
        AutoCreateTable: true,
    })
    if err != nil {
        panic(err)
    }

    // Create logger with database appender
    config := audit.DefaultConfig()
    config.AsyncMode = true
    config.BufferSize = 1000

    return audit.NewLogger(config, dbAppender)
}
```

## Core Concepts

### Operations

Pre-defined operation types for common audit scenarios:

```go
const (
    OpExport       Operation = "export"       // Data export
    OpImport       Operation = "import"       // Data import
    OpSync         Operation = "sync"         // Data synchronization
    OpTransform    Operation = "transform"    // Data transformation
    OpValidate     Operation = "validate"     // Data validation
    OpDelete       Operation = "delete"       // Data deletion
    OpUpdate       Operation = "update"       // Data update
    OpCreate       Operation = "create"       // Data creation
    OpAccess       Operation = "access"       // Data access
    OpAuthenticate Operation = "authenticate" // Authentication
)
```

### Status

```go
const (
    StatusSuccess Status = "success" // Operation succeeded
    StatusFailure Status = "failure" // Operation failed
    StatusPartial Status = "partial" // Partial success
)
```

### Logging Levels

```go
const (
    // LevelMinimal - Only basic information (operation, status, timestamp)
    // Excludes: Metadata, Data, IPAddress, SessionID
    LevelMinimal Level = iota

    // LevelStandard - Standard audit information
    // Excludes: Data (sensitive payloads)
    LevelStandard

    // LevelFull - Complete audit trail with all information
    LevelFull
)
```

## Usage Examples

### 1. Multiple Appenders

Log to both file and database simultaneously:

```go
// Create appenders
fileAppender, _ := audit.NewFileAppender(audit.FileAppenderConfig{
    FilePath:   "./logs/audit.log",
    MaxSize:    100,
    MaxBackups: 10,
    Level:      audit.LevelStandard,
    FormatJSON: true,
})

dbAppender, _ := audit.NewDatabaseAppender(audit.DatabaseAppenderConfig{
    DB:              db,
    TableName:       "audit_log",
    Level:           audit.LevelFull,
    BatchSize:       100,
    AutoCreateTable: true,
})

// Combine with MultiAppender
multiAppender := audit.NewMultiAppender(fileAppender, dbAppender)

// Create logger
config := audit.DefaultConfig()
logger := audit.NewLogger(config, multiAppender)
defer logger.Close()
```

### 2. Rich Audit Entries

```go
// Create detailed audit entry with builder pattern
entry := audit.NewEntry(audit.OpExport, audit.StatusSuccess).
    WithUser("john.doe@company.com").
    WithSource("postgresql://prod-db/users").
    WithTarget("s3://backups/users-2024-01-15.csv").
    WithResource("users").
    WithRecordsAffected(15420).
    WithDuration(2 * time.Minute).
    WithMetadata("query", "SELECT * FROM users WHERE active=true").
    WithMetadata("compression", "gzip").
    WithIPAddress("192.168.1.100").
    WithSessionID("session-abc-123")

logger.Log(context.Background(), entry)
```

### 3. Error Logging

```go
// Log failure with error details
err := performSensitiveOperation()
if err != nil {
    entry := logger.LogFailure(context.Background(), audit.OpDelete, err)
    entry.WithUser("admin").
        WithResource("customer_data").
        WithMetadata("reason", "insufficient_permissions")
}
```

### 4. Async Logging with Auto-Flush

```go
config := audit.LoggerConfig{
    AsyncMode:     true,
    BufferSize:    5000,
    FlushInterval: 10 * time.Second, // Auto-flush every 10s
    DefaultUser:   "system",
    OnError: func(err error) {
        log.Printf("Audit error: %v", err)
    },
}

logger := audit.NewLogger(config, appender)

// Logs are buffered and written asynchronously
for i := 0; i < 10000; i++ {
    logger.LogSuccess(context.Background(), audit.OpSync)
}

// Graceful shutdown - waits for all entries to be written
logger.Close()
```

### 5. Database Querying

```go
dbAppender, _ := audit.NewDatabaseAppender(config)

// Query audit logs with filters
entries, err := dbAppender.Query(context.Background(), audit.QueryFilter{
    Operation: audit.OpExport,
    Status:    audit.StatusSuccess,
    User:      "john.doe",
    StartTime: time.Now().Add(-24 * time.Hour),
    EndTime:   time.Now(),
    Limit:     100,
})

for _, entry := range entries {
    fmt.Printf("Operation: %s, Records: %d\n",
        entry.Operation, entry.RecordsAffected)
}

// Count audit entries
count, err := dbAppender.Count(context.Background(), audit.QueryFilter{
    Operation: audit.OpDelete,
    StartTime: startOfMonth,
})

// Cleanup old entries (GDPR retention policies)
deleted, err := dbAppender.DeleteOlderThan(
    context.Background(),
    time.Now().Add(-90 * 24 * time.Hour), // 90 days retention
)
```

### 6. File Rotation

```go
fileAppender, _ := audit.NewFileAppender(audit.FileAppenderConfig{
    FilePath:   "./logs/audit.log",
    MaxSize:    50,  // 50 MB per file
    MaxBackups: 20,  // Keep 20 backup files
    Level:      audit.LevelStandard,
    FormatJSON: true,
})

// Files will be automatically rotated:
// audit.log        <- current
// audit.log.1      <- previous
// audit.log.2      <- older
// ...
// audit.log.20     <- oldest (deleted when .21 created)
```

### 7. Console Logging (Development)

```go
consoleAppender := audit.NewConsoleAppender(audit.LevelFull, true)

logger := audit.NewLogger(audit.SyncConfig(), consoleAppender)

// Logs to stdout with formatting
logger.LogSuccess(context.Background(), audit.OpExport).
    WithUser("dev-user").
    WithRecordsAffected(100)

// Output:
// [2024-01-15T10:30:45Z] export success dev-user (resource=, records=100, duration=0s)
```

## Integration with TDTP Framework

### Adapter Integration

```go
import (
    "github.com/queuebridge/tdtp/pkg/adapter"
    "github.com/queuebridge/tdtp/pkg/audit"
)

type AuditedAdapter struct {
    adapter.Adapter
    logger *audit.AuditLogger
}

func (a *AuditedAdapter) Read(ctx context.Context) ([]map[string]interface{}, error) {
    start := time.Now()

    data, err := a.Adapter.Read(ctx)

    // Log the operation
    status := audit.StatusSuccess
    if err != nil {
        status = audit.StatusFailure
    }

    entry := a.logger.LogOperation(ctx, audit.OpExport, status).
        WithSource(a.Adapter.Name()).
        WithRecordsAffected(int64(len(data))).
        WithDuration(time.Since(start))

    if err != nil {
        entry.ErrorMessage = err.Error()
    }

    return data, err
}
```

### Pipeline Auditing

```go
func setupPipelineAudit(pipeline *tdtp.Pipeline, logger *audit.AuditLogger) {
    pipeline.OnSuccess(func(ctx context.Context, stats tdtp.PipelineStats) {
        logger.LogSuccess(ctx, audit.OpSync).
            WithSource(stats.Source).
            WithTarget(stats.Target).
            WithRecordsAffected(stats.RecordsProcessed).
            WithDuration(stats.Duration).
            WithMetadata("pipeline", stats.PipelineName)
    })

    pipeline.OnError(func(ctx context.Context, err error) {
        logger.LogFailure(ctx, audit.OpSync, err).
            WithMetadata("pipeline", pipeline.Name())
    })
}
```

## Compliance Scenarios

### GDPR Compliance

```go
// Use Standard level to exclude sensitive data payloads
config := audit.LoggerConfig{
    AsyncMode:    true,
    DefaultLevel: audit.LevelStandard, // No Data field
}

logger := audit.NewLogger(config, appender)

// Log access to personal data
entry := logger.LogSuccess(ctx, audit.OpAccess).
    WithUser("support@company.com").
    WithResource("customer_profile").
    WithMetadata("customer_id", "12345").
    WithMetadata("reason", "support_request_#98765")
// Data payload NOT logged (GDPR compliant)
```

### HIPAA Audit Trail

```go
// Full audit trail for healthcare data access
fileAppender, _ := audit.NewFileAppender(audit.FileAppenderConfig{
    FilePath:   "/var/log/hipaa/audit.log",
    MaxSize:    100,
    MaxBackups: 365, // 1 year retention
    Level:      audit.LevelFull,
    FormatJSON: true,
})

logger := audit.NewLogger(audit.DefaultConfig(), fileAppender)

// Log patient record access
logger.LogSuccess(ctx, audit.OpAccess).
    WithUser("dr.smith@hospital.com").
    WithResource("patient_records").
    WithMetadata("patient_id", "P-12345").
    WithMetadata("record_type", "medical_history").
    WithIPAddress(req.RemoteAddr).
    WithSessionID(session.ID)
```

### SOX Financial Audit

```go
// Dual logging: file + database for financial operations
dbAppender, _ := audit.NewDatabaseAppender(audit.DatabaseAppenderConfig{
    DB:              db,
    TableName:       "financial_audit",
    Level:           audit.LevelFull,
    BatchSize:       0, // No batching for financial data
    AutoCreateTable: true,
})

fileAppender, _ := audit.NewFileAppender(audit.FileAppenderConfig{
    FilePath:   "/var/log/financial/audit.log",
    MaxSize:    200,
    MaxBackups: 2000, // 7 year retention
    Level:      audit.LevelFull,
    FormatJSON: true,
})

multiAppender := audit.NewMultiAppender(dbAppender, fileAppender)
logger := audit.NewLogger(audit.SyncConfig(), multiAppender) // Synchronous for financial

// Log financial transaction
logger.LogSuccess(ctx, audit.OpCreate).
    WithUser("finance@company.com").
    WithResource("invoice").
    WithMetadata("invoice_id", "INV-2024-001").
    WithMetadata("amount", 15000.00).
    WithMetadata("currency", "USD")
```

## Performance Considerations

### High-Volume Scenarios

```go
// Optimized for high throughput
config := audit.LoggerConfig{
    AsyncMode:     true,
    BufferSize:    10000, // Large buffer
    FlushInterval: 30 * time.Second,
    DefaultLevel:  audit.LevelMinimal, // Less data
}

dbAppender, _ := audit.NewDatabaseAppender(audit.DatabaseAppenderConfig{
    DB:        db,
    BatchSize: 500, // Batch inserts
    Level:     audit.LevelMinimal,
})

logger := audit.NewLogger(config, dbAppender)

// Can handle 100,000+ logs/second
```

### Low-Latency Requirements

```go
// Minimal overhead with NullAppender (testing/staging)
logger := audit.NewLogger(
    audit.SyncConfig(),
    audit.NewNullAppender(),
)

// Or file-only for production
fileAppender, _ := audit.NewFileAppender(audit.FileAppenderConfig{
    FilePath:   "./audit.log",
    MaxSize:    500,
    Level:      audit.LevelMinimal,
    FormatJSON: false, // Text is faster than JSON
})

logger := audit.NewLogger(audit.SyncConfig(), fileAppender)
```

## Database Schema

When using `DatabaseAppender` with `AutoCreateTable: true`, the following schema is created:

```sql
CREATE TABLE audit_log (
    id VARCHAR(255) PRIMARY KEY,
    timestamp TIMESTAMP NOT NULL,
    operation VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL,
    user_name VARCHAR(255),
    source VARCHAR(255),
    target VARCHAR(255),
    resource VARCHAR(255),
    records_affected BIGINT DEFAULT 0,
    duration_ms BIGINT DEFAULT 0,
    error_message TEXT,
    metadata TEXT,
    data TEXT,
    ip_address VARCHAR(50),
    session_id VARCHAR(255)
);

-- Indexes for performance
CREATE INDEX idx_audit_log_timestamp ON audit_log(timestamp);
CREATE INDEX idx_audit_log_operation ON audit_log(operation);
CREATE INDEX idx_audit_log_status ON audit_log(status);
CREATE INDEX idx_audit_log_user ON audit_log(user_name);
CREATE INDEX idx_audit_log_resource ON audit_log(resource);
```

## API Reference

### Logger Interface

```go
type Logger interface {
    Log(ctx context.Context, entry *Entry) error
    LogOperation(ctx context.Context, operation Operation, status Status) *Entry
    LogSuccess(ctx context.Context, operation Operation) *Entry
    LogFailure(ctx context.Context, operation Operation, err error) *Entry
    Flush() error
    Close() error
}
```

### Appender Interface

```go
type Appender interface {
    Append(ctx context.Context, entry *Entry) error
    Close() error
}
```

### Entry Builder Methods

```go
func NewEntry(operation Operation, status Status) *Entry
func (e *Entry) WithUser(user string) *Entry
func (e *Entry) WithSource(source string) *Entry
func (e *Entry) WithTarget(target string) *Entry
func (e *Entry) WithResource(resource string) *Entry
func (e *Entry) WithRecordsAffected(count int64) *Entry
func (e *Entry) WithDuration(duration time.Duration) *Entry
func (e *Entry) WithError(err error) *Entry
func (e *Entry) WithMetadata(key string, value interface{}) *Entry
func (e *Entry) WithData(data interface{}) *Entry
func (e *Entry) WithIPAddress(ip string) *Entry
func (e *Entry) WithSessionID(sessionID string) *Entry
```

## Testing

```bash
# Run all tests
go test -v ./pkg/audit/...

# Run with coverage
go test -cover ./pkg/audit/...

# Run specific test
go test -v -run TestAuditLogger_Async ./pkg/audit/...
```

## Best Practices

1. **Always use async mode in production** for better performance
2. **Set appropriate logging levels** - use Minimal for high-volume, Full for sensitive operations
3. **Configure auto-flush** to prevent data loss on crashes
4. **Use MultiAppender** for critical systems (file + database)
5. **Implement retention policies** with `DeleteOlderThan` for GDPR compliance
6. **Monitor appender errors** with `OnError` callback
7. **Use batch mode** for database appenders in high-volume scenarios
8. **Always call Close()** on shutdown to flush pending entries

## Troubleshooting

### Buffer Overflow

```go
config.OnError = func(err error) {
    if errors.Is(err, audit.ErrBufferFull) {
        // Increase buffer size or reduce logging volume
        metrics.IncrementCounter("audit_buffer_overflow")
    }
}
```

### Performance Issues

- Use `LevelMinimal` to reduce data size
- Enable `BatchSize` for database appenders
- Increase `BufferSize` for async mode
- Use text format instead of JSON for files

### Disk Space

- Configure `MaxSize` and `MaxBackups` appropriately
- Implement cleanup with `DeleteOlderThan`
- Monitor file sizes with `CurrentSize()`

## License

MIT License - see LICENSE file for details

## Contributing

Contributions welcome! Please open an issue or submit a pull request.

## See Also

- [CircuitBreaker](../resilience/README.md) - Resilience patterns
- [Adapters](../adapter/README.md) - Data source/sink adapters
- [TDTP Framework](../../README.md) - Main documentation
