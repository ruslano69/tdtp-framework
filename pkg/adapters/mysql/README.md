# MySQL adapter

A high-throughput adapter for MySQL and MariaDB.

> **Status: fully working** — 58/58 CLI integration tests pass (MySQL 8.4, 2026-04-21)

## What it does

- **The full TDTP specification**
- **Uses `schema.Converter`** for strict typing
- **Every import strategy**: Replace (ON DUPLICATE KEY UPDATE), Ignore (INSERT IGNORE), Fail, Copy
- **TDTQL filtering** pushed down into SQL (WHERE, ORDER BY, LIMIT/OFFSET, IN)
- **Bracket-quoted names** containing spaces and `$` — the NAV, BC and ERP style
- **Errors handled properly**, through the MySQL driver's own types
- **Transactional safety**
- **Every MySQL data type**
- **Compact format** (v1.3.1), zstd and kanzi compression, hash verification
- **Cross-database round-trip**: MySQL to MySQL, and MySQL to SQLite

## Installing

```bash
go get github.com/go-sql-driver/mysql
```

## Quick start

```go
package main

import (
    "context"
    "fmt"

    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/mysql"
)

func main() {
    ctx := context.Background()

    // Connect to MySQL
    adapter, err := adapters.New("mysql", adapters.Config{
        DSN: "user:password@tcp(localhost:3306)/dbname?parseTime=true",
    })
    if err != nil {
        panic(err)
    }
    defer adapter.Close(ctx)

    // Export the data
    packets, err := adapter.ExportTable(ctx, "users")
    if err != nil {
        panic(err)
    }

    fmt.Printf("Exported %d packets\n", len(packets))
}
```

## The DSN

### Basic form
```
user:password@tcp(host:port)/dbname?parameters
```

### Recommended parameters
```
user:password@tcp(localhost:3306)/mydb?parseTime=true&charset=utf8mb4&loc=UTC
```

**The ones that matter:**
- `parseTime=true` — parses DATE, DATETIME and TIMESTAMP for you
- `charset=utf8mb4` — full Unicode
- `loc=UTC` — the time zone for TIMESTAMP

## Supported data types

### TDTP to MySQL

| TDTP type | MySQL type | Notes |
|-----------|------------|------------|
| INTEGER | BIGINT | INT when Length ≤ 4 |
| REAL | FLOAT | - |
| DOUBLE | DOUBLE | - |
| DECIMAL(p,s) | DECIMAL(p,s) | Defaults to (18,2) |
| TEXT | VARCHAR(n) / TEXT | VARCHAR up to 65535 |
| VARCHAR(n) | VARCHAR(n) | Defaults to 255 |
| CHAR(n) | CHAR(n) | Defaults to 1 |
| BOOLEAN | TINYINT(1) | 0/1 |
| DATE | DATE | YYYY-MM-DD |
| DATETIME | DATETIME | With a time zone |
| TIMESTAMP | TIMESTAMP | UTC |
| BLOB | BLOB | Base64 inside TDTP |

## Import strategies

### 1. StrategyReplace (UPSERT)
```go
// INSERT ... ON DUPLICATE KEY UPDATE
err := adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
```
- On a primary-key match → UPDATE the existing row
- With no primary key → REPLACE INTO

### 2. StrategyIgnore
```go
// INSERT IGNORE
err := adapter.ImportPacket(ctx, pkt, adapters.StrategyIgnore)
```
- Duplicates are skipped without an error
- The fastest option

### 3. StrategyFail
```go
// INSERT
err := adapter.ImportPacket(ctx, pkt, adapters.StrategyFail)
```
- Returns an error on a duplicate
- Strict control over what lands

### 4. StrategyCopy
```go
// Equivalent to INSERT — MySQL has no COPY
err := adapter.ImportPacket(ctx, pkt, adapters.StrategyCopy)
```

## TDTQL filtering

### The pushed-down SQL path
```go
query := &packet.Query{
    Filters: &packet.Filters{
        Condition: &packet.Condition{
            Field:    "age",
            Operator: ">",
            Value:    "18",
        },
    },
    Limit: 100,
    Offset: 0,
}

packets, err := adapter.ExportTableWithQuery(ctx, "users", query, "", "")
```

**TDTQL is translated into MySQL SQL automatically**, with:
- backticks around identifiers
- native LIMIT and OFFSET
- the database doing the optimising

### The in-memory fallback
Where the translation is not possible, filtering falls back to memory automatically.

## Performance

### Batch Insert
```go
packets := []*packet.DataPacket{pkt1, pkt2, pkt3}
err := adapter.ImportPackets(ctx, packets, adapters.StrategyReplace)
// Every packet in one transaction
```

### Query handling
- prepared statements throughout
- multiple packets handled in one transaction
- filtering in SQL, so the whole table is never loaded

## Error handling

### Duplicate Key
```go
if err != nil {
    if mysqlErr, ok := err.(*mysql.MySQLError); ok {
        if mysqlErr.Number == 1062 {
            // Handle a duplicate key
        }
    }
}
```

### Error kinds
- **1062** - Duplicate entry (PRIMARY/UNIQUE KEY)
- **1451** - Foreign key constraint
- **1452** - Cannot add or update child row

## Worked examples

### Export with a filter
```go
query := &packet.Query{
    Filters: &packet.Filters{
        Logic: "AND",
        Conditions: []*packet.Condition{
            {Field: "status", Operator: "=", Value: "active"},
            {Field: "created_at", Operator: ">", Value: "2024-01-01"},
        },
    },
    OrderBy: []packet.OrderField{
        {Field: "created_at", Direction: "DESC"},
    },
    Limit: 1000,
}

packets, _ := adapter.ExportTableWithQuery(ctx, "users", query, "sender", "recipient")
```

### Import, creating the table
```go
// The adapter creates the table if it is missing
err := adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
```

### Reading metadata
```go
// The table's schema
schema, err := adapter.GetTableSchema(ctx, "users")

// Row count
count, err := adapter.GetTableRowCount(ctx, "users")

// Table size
size, err := adapter.GetTableSize(ctx, "users")
```

## Implementation notes

### Type Conversion
`schema.Converter` handles:
- type validation
- conversion between TDTP and MySQL
- precision and scale for DECIMAL
- formatting DATE, DATETIME and TIMESTAMP correctly
- base64 for BLOB

### Transaction Safety
- every import runs in a transaction
- ROLLBACK on any error
- several packets can share one transaction

### SQL Generation
- identifiers are escaped automatically (with backticks)
- parameterised queries, against SQL injection
- indexes are used where they help

## 🎓 Best Practices

1. **Always set `parseTime=true`** in the DSN if you touch temporal types
2. **Use `charset=utf8mb4`** for full Unicode
3. **Index the fields you filter on**
4. **Use StrategyReplace** where the operation must be idempotent
5. **Handle errors** through the MySQL driver's types

## Test status

CLI integration tests: **58 / 58 PASS** (MySQL 8.4.9, 2026-04-21)

| Group | What it covers | Tests |
|--------|----------|-------|
| T1 | Basic Export (rows, fields, list) | 4 |
| T2 | TDTQL Filters (WHERE, IN, ORDER BY, LIMIT, bracket-quoted) | 9 |
| T3 | Compression (zstd/kanzi, --hash, corruption) | 6 |
| T4 | MySQL → MySQL Roundtrip (strategies, projection, ERP-style names) | 8 |
| T5 | File Integrity (--test, --inspect) | 3 |
| T6 | Edge Cases (empty result, errors) | 3 |
| T7 | Compact Format v1.3.1 | 4 |
| T8 | MySQL → SQLite Roundtrip (cross-DB) | 5 |
| T9 | Diff | 7 |
| T10 | Merge (union, intersection, append, left/right priority) | 9 |

Running them:
```bash
# 1. Start the container, from the repository root
docker compose up -d mysql

# 2. Run the tests
TDTPCLI_BIN=/tmp/tdtpcli.exe py -3 tests/cli/test_mysql.py

# 3. Just one group
TDTPCLI_BIN=/tmp/tdtpcli.exe py -3 tests/cli/test_mysql.py T4
```

## Compatibility

- ✅ MySQL 5.7+
- ✅ MySQL 8.0+ (integration-tested on 8.4.9)
- ✅ MariaDB 10.3+
- ✅ Percona Server 5.7+

## Links

- [MySQL Driver Documentation](https://github.com/go-sql-driver/mysql)
- [TDTP Specification](../../docs/TDTP_SPEC.md)
- [TDTQL Query Language](../../docs/TDTQL.md)
