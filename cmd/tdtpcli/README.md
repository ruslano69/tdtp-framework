# tdtpcli - TDTP Universal Database CLI

Universal command-line interface for working with databases via TDTP adapters.

## Features

- ✅ **Universal**: Works with SQLite and PostgreSQL
- ✅ **Simple**: Easy-to-use command-line interface
- ✅ **Portable**: Single binary, no dependencies
- ✅ **JSON Export**: Export tables to JSON format
- ✅ **Cross-platform**: Works on Linux, macOS, Windows

## Installation

```bash
cd cmd/tdtpcli
go build -o tdtpcli
```

Or install globally:

```bash
go install github.com/queuebridge/tdtp/cmd/tdtpcli@latest
```

## Usage

### List Tables

**SQLite:**
```bash
tdtpcli -type sqlite -dsn database.db -list
```

**PostgreSQL:**
```bash
tdtpcli -type postgres -dsn "postgresql://user:pass@localhost:5432/mydb" -list
```

### Export Table

**Export to JSON:**
```bash
tdtpcli -type sqlite -dsn database.db -export Users > users.json
```

**PostgreSQL:**
```bash
tdtpcli -type postgres -dsn "postgresql://user:pass@localhost/mydb" -export orders > orders.json
```

## Command Reference

### Flags

| Flag | Description | Default | Required |
|------|-------------|---------|----------|
| `-type` | Database type (`sqlite` or `postgres`) | `sqlite` | No |
| `-dsn` | Data Source Name (connection string) | - | Yes |
| `-schema` | Schema name (PostgreSQL only) | `public` | No |
| `-list` | List all tables | - | No |
| `-export` | Export table to JSON | - | No |
| `-version` | Show version | - | No |
| `-help` | Show help | - | No |

### Connection Strings

**SQLite:**
- File: `database.db`
- Memory: `:memory:`
- Absolute path: `/path/to/database.db`

**PostgreSQL:**
- URI: `postgresql://user:password@host:port/database`
- DSN: `host=localhost port=5432 user=user password=pass dbname=mydb`

## Examples

### SQLite

```bash
# List tables
tdtpcli -type sqlite -dsn myapp.db -list

# Export table
tdtpcli -type sqlite -dsn myapp.db -export users

# In-memory database
tdtpcli -type sqlite -dsn :memory: -list
```

### PostgreSQL

```bash
# List tables
tdtpcli -type postgres \
  -dsn "postgresql://tdtp_user:password@localhost:5432/tdtp_test" \
  -list

# Export table from specific schema
tdtpcli -type postgres \
  -dsn "postgresql://user:pass@localhost/mydb" \
  -schema public \
  -export orders

# Using DSN format
tdtpcli -type postgres \
  -dsn "host=localhost port=5432 user=admin password=secret dbname=production" \
  -list
```

### JSON Output

Export produces JSON with schema and data:

```json
{
  "table": "users",
  "type": "reference",
  "schema": {
    "columns": [
      {"name": "id", "type": "INTEGER", "primary_key": true},
      {"name": "name", "type": "TEXT", "size": 100},
      {"name": "email", "type": "TEXT", "size": 255}
    ]
  },
  "rows": [
    {"value": "1|John Doe|john@example.com"},
    {"value": "2|Jane Smith|jane@example.com"}
  ]
}
```

## Integration

### Pipeline with jq

```bash
# Extract specific fields
tdtpcli -type sqlite -dsn data.db -export users | jq '.rows[].value'

# Count rows
tdtpcli -type sqlite -dsn data.db -export users | jq '.rows | length'

# Filter data
tdtpcli -type sqlite -dsn data.db -export users | \
  jq '.rows[] | select(.value | contains("John"))'
```

### Backup Script

```bash
#!/bin/bash
# backup-tables.sh

DB_TYPE="postgres"
DSN="postgresql://user:pass@localhost/mydb"
OUTPUT_DIR="./backups"

# Get all tables
TABLES=$(tdtpcli -type $DB_TYPE -dsn "$DSN" -list | grep "•" | awk '{print $2}')

# Export each table
for table in $TABLES; do
  echo "Exporting $table..."
  tdtpcli -type $DB_TYPE -dsn "$DSN" -export "$table" > "$OUTPUT_DIR/${table}.json"
done
```

## Building

### Standard Build

```bash
go build -o tdtpcli
```

### Optimized Build

```bash
go build -ldflags="-s -w" -o tdtpcli
```

### Cross-Compilation

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o tdtpcli-linux

# macOS
GOOS=darwin GOARCH=amd64 go build -o tdtpcli-macos

# Windows
GOOS=windows GOARCH=amd64 go build -o tdtpcli.exe
```

## Troubleshooting

### Connection Errors

**SQLite "unable to open database file":**
```bash
# Check file exists
ls -la database.db

# Check permissions
chmod 644 database.db
```

**PostgreSQL "connection refused":**
```bash
# Test connection
psql -h localhost -U user -d mydb

# Check if PostgreSQL is running
systemctl status postgresql
```

### Driver Not Found

Make sure drivers are imported in `main.go`:

```go
import (
    _ "github.com/queuebridge/tdtp/pkg/adapters/postgres"
    _ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
)
```

## Version

Current version: **v1.0.0**

Built with TDTP Universal Adapter Architecture v1.0

## License

Same as TDTP framework

## See Also

- [TDTP Framework](../../README.md)
- [Adapter Interface](../../docs/ADAPTER_INTERFACE.md)
- [Benchmarks](../../docs/BENCHMARKS.md)
