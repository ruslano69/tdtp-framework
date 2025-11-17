# MS SQL Server Adapter - Implementation Summary

**Version:** 1.0.0
**Date:** 2025-11-16
**Status:** ✅ COMPLETE

## Overview

Полная реализация адаптера MS SQL Server для TDTP Framework с поддержкой SQL Server 2012+.

## Implemented Features

### ✅ Core Adapter (`pkg/adapters/mssql/adapter.go`)
- [x] Adapter structure with connection management
- [x] Feature detection (server version, compatibility level)
- [x] Compatibility mode system (2012, 2016, 2019, auto)
- [x] Strict/warn modes for production safety
- [x] Effective compatibility calculation
- [x] Lifecycle methods (Connect, Close, Ping)
- [x] Metadata methods (GetDatabaseVersion, GetDatabaseType)
- [x] Factory registration

### ✅ Type Mapping (`pkg/adapters/mssql/types.go`)
- [x] MSSQLToTDTP() - SQL Server → TDTP conversion
- [x] TDTPToMSSQL() - TDTP → SQL Server CREATE TABLE conversion
- [x] ParseMSSQLType() - type string parser
- [x] BuildFieldFromColumn() - INFORMATION_SCHEMA → TDTP
- [x] Support for all SQL Server types:
  - Integer: TINYINT, SMALLINT, INT, BIGINT
  - Decimal: DECIMAL, NUMERIC, MONEY, SMALLMONEY
  - Float: FLOAT, REAL
  - Text: NVARCHAR, VARCHAR, NCHAR, CHAR, TEXT, NTEXT
  - Date/Time: DATE, TIME, DATETIME, DATETIME2, SMALLDATETIME, DATETIMEOFFSET
  - Binary: VARBINARY, BINARY, IMAGE
  - Special: BIT, UNIQUEIDENTIFIER, XML
- [x] Subtype preservation for exact roundtrip
- [x] Prefer modern types (NVARCHAR, DATETIME2, BIGINT)

### ✅ Export Functionality (`pkg/adapters/mssql/export.go`)
- [x] GetTableSchema() - read schema from INFORMATION_SCHEMA
- [x] GetTableNames() - list all tables
- [x] TableExists() - check table existence
- [x] ExportTable() - export entire table
- [x] ExportTableWithQuery() - export with TDTQL filtering
- [x] OFFSET/FETCH pagination (SQL Server 2012+ compatible)
- [x] Automatic packet splitting (~3.8MB max size)
- [x] Schema support (dbo, custom schemas)
- [x] Helper statistics (GetTableRowCount, GetTableSize)

### ✅ Import Functionality (`pkg/adapters/mssql/import.go`)
- [x] ImportPacket() - import single packet
- [x] ImportPackets() - import multiple packets (transaction)
- [x] MERGE for UPSERT (SQL Server 2012+ syntax)
- [x] Import strategies:
  - StrategyReplace - UPSERT via MERGE
  - StrategyIgnore - skip duplicates
  - StrategyFail - error on duplicates
  - StrategyCopy - bulk insert
- [x] Automatic table creation
- [x] Transaction support (BeginTx)
- [x] PK violation detection

### ✅ Integration Tests (`pkg/adapters/mssql/integration_test.go`)
- [x] Basic connection tests
- [x] Export/Import cycle tests
- [x] MERGE (UPSERT) tests
- [x] Compatibility mode validation
- [x] Schema operations tests
- [x] Special types tests
- [x] Configurable test environments (dev/prod sim)

### ✅ Documentation
- [x] `docs/MSSQL_2012_COMPATIBILITY.md` - Compatibility guide (1000+ lines)
- [x] `docs/MSSQL_DEV_VS_PROD.md` - Dev vs prod environment guide
- [x] `docs/MSSQL_COMPATIBILITY_MODES.md` - Architecture document
- [x] `pkg/adapters/mssql/doc.go` - Package documentation

### ✅ Infrastructure
- [x] `docker-compose.mssql.yml` - Docker environment with prod simulation
- [x] `scripts/mssql/init-dev.sql` - Dev database initialization
- [x] `scripts/mssql/init-prod.sql` - Prod simulation (compatibility level 110)

## SQL Server 2012 Compatibility

### ✅ Supported Features (SQL Server 2012+)
- OFFSET/FETCH for pagination
- MERGE for UPSERT
- IIF, TRY_CONVERT, FORMAT
- Table-Valued Parameters
- DATETIME2, DATE, TIME
- UNIQUEIDENTIFIER
- DECIMAL, NUMERIC, MONEY
- NVARCHAR(MAX), VARCHAR(MAX)

### ❌ Not Supported (SQL Server 2016+)
- JSON_VALUE, JSON_QUERY, FOR JSON
- STRING_SPLIT
- DROP IF EXISTS

### ❌ Not Supported (SQL Server 2017+)
- STRING_AGG
- TRIM, CONCAT_WS

## File Statistics

```
pkg/adapters/mssql/adapter.go         ~380 lines
pkg/adapters/mssql/types.go           ~450 lines
pkg/adapters/mssql/export.go          ~530 lines
pkg/adapters/mssql/import.go          ~540 lines
pkg/adapters/mssql/integration_test.go ~570 lines
pkg/adapters/mssql/doc.go             ~100 lines
─────────────────────────────────────────────────
TOTAL:                                ~2570 lines
```

## Configuration Example

### Development (SQL Server 2019)
```yaml
database:
  type: mssql
  dsn: "server=localhost,1433;user id=sa;password=Pass;database=DevDB"
  compatibility_mode: "auto"  # Auto-detect
  strict_compatibility: false
```

### Production (SQL Server 2012)
```yaml
database:
  type: mssql
  dsn: "server=prod-server;user id=sa;password=Pass;database=ProdDB"
  compatibility_mode: "2012"  # Explicit!
  strict_compatibility: true   # Error on incompatible functions
  warn_on_incompatible: true
```

## Testing

### Run Integration Tests

**Prerequisites:**
- Docker and Docker Compose installed
- Ports 1433, 1434, 1435 available

**Start Test Environment:**
```bash
docker-compose -f docker-compose.mssql.yml up -d
```

**Run Tests:**
```bash
# All tests
go test -v ./pkg/adapters/mssql/

# Specific test
go test -v ./pkg/adapters/mssql/ -run TestIntegration_ExportImport

# With custom DSN
MSSQL_TEST_DSN_DEV="server=localhost,1433;..." go test -v ./pkg/adapters/mssql/
```

**Stop Test Environment:**
```bash
docker-compose -f docker-compose.mssql.yml down
```

## Usage Example

```go
package main

import (
    "context"
    "log"

    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/mssql"
)

func main() {
    ctx := context.Background()

    // Configure for SQL Server 2012 production
    cfg := adapters.Config{
        Type: "mssql",
        DSN:  "server=localhost;user id=sa;password=Pass;database=MyDB",
        CompatibilityMode: "2012",
        StrictCompatibility: true,
    }

    // Create adapter
    adapter, err := adapters.New(ctx, cfg)
    if err != nil {
        log.Fatal(err)
    }
    defer adapter.Close(ctx)

    // Export table
    packets, err := adapter.ExportTable(ctx, "Users")
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Exported %d packet(s)", len(packets))

    // Import to another database
    err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
    if err != nil {
        log.Fatal(err)
    }
}
```

## Next Steps

### Phase 1: MS SQL Adapter ✅ COMPLETE
- [x] Core adapter implementation
- [x] Type mapping
- [x] Export functionality
- [x] Import functionality (MERGE)
- [x] Integration tests
- [x] Documentation

### Phase 2: Message Brokers (FUTURE)
- [ ] MSMQ Integration (локальный брокер)
- [ ] RabbitMQ Integration (удаленный брокер)
- [ ] Message queue export/import
- [ ] Retry mechanisms
- [ ] Error handling and logging

### Phase 3: CLI Integration (FUTURE)
- [ ] CLI commands for MS SQL
- [ ] Export to message queue
- [ ] Import from message queue
- [ ] Configuration management

## Known Limitations

1. **TDTQL to SQL Conversion** - Currently basic implementation, needs full parser
2. **Bulk Insert Optimization** - StrategyCopy uses regular INSERT, can be optimized with Table-Valued Parameters
3. **Binary Data Handling** - Basic implementation, may need encoding improvements
4. **Error Messages** - Could be more descriptive with context

## Performance Considerations

- **Packet Size**: Default ~3.8MB per packet
- **Batch Size**: Default 1000 rows per batch
- **Transaction Support**: All imports use transactions for atomicity
- **Connection Pool**: Configurable via Config.MaxConns

## Security Notes

- **SQL Injection**: Parameterized queries used throughout
- **Credentials**: Never logged or exposed in errors
- **SSL/TLS**: Supported via Config.SSL
- **Permissions**: Requires CREATE TABLE, INSERT, UPDATE, DELETE, SELECT

## Compatibility Matrix

| SQL Server Version | Support | Notes |
|-------------------|---------|-------|
| 2008 R2 | ❌ | Not tested, may work |
| 2012 | ✅ | Full support, tested |
| 2014 | ✅ | Full support |
| 2016 | ✅ | Full support + JSON |
| 2017 | ✅ | Full support + STRING_AGG |
| 2019 | ✅ | Full support + all features |
| 2022 | ✅ | Full support |

## Credits

**Implementation:** Claude (Anthropic AI)
**Requested by:** ruslano69
**Use Case:** Export data from MS SQL Server to cloud document management system (СЭД) via MSMQ/RabbitMQ
**Special Requirement:** Backward compatibility with SQL Server 2012

## License

Part of TDTP Framework - see main project LICENSE
