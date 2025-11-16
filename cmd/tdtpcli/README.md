# TDTP CLI Utility

Command-line tool for working with TDTP (Table Data Transfer Protocol) and databases.

## Features

- ✅ **Configuration File** - Uses `config.yaml` for database connection
- ✅ **Multiple Databases** - PostgreSQL, SQLite (MS SQL, MySQL, Miranda SQL - in development)
- ✅ **Safe Import** - Uses temporary tables before replacing production data
- ✅ **TDTP Format** - Native TDTP.XML export/import
- ✅ **Database-Specific Templates** - Separate config templates for each DB type

## Installation

```bash
go build -o tdtpcli ./cmd/tdtpcli
```

## First Time Setup

### For PostgreSQL:
```bash
./tdtpcli --create-config-pg
# Edit default_config.yaml
mv default_config.yaml config.yaml
```

### For SQLite:
```bash
./tdtpcli --create-config-sl
# Edit default_config.yaml
mv default_config.yaml config.yaml
```

## Configuration Templates

| Flag | Database | Status |
|------|----------|--------|
| `--create-config-pg` | PostgreSQL | ✅ Supported |
| `--create-config-sl` | SQLite | ✅ Supported |
| `--create-config-ms` | MS SQL Server | 🚧 Under development |
| `--create-config-my` | MySQL | 🚧 Under development |
| `--create-config-mi` | Miranda SQL | 🚧 Under development |

## Configuration Examples

### PostgreSQL (config.yaml)
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  user: tdtp_user
  password: secure_password
  dbname: tdtp_test
  schema: public
  sslmode: disable
```

### SQLite (config.yaml)
```yaml
database:
  type: sqlite
  path: ./database.db
```

## Usage

### List Tables
```bash
./tdtpcli --list
```

### Export Table
```bash
# To stdout
./tdtpcli --export Users

# To file (auto-adds .tdtp.xml extension)
./tdtpcli --export Users --output users
# Creates: users.tdtp.xml

# Explicit extension
./tdtpcli --export Orders --output orders.tdtp.xml
```

### Import Data
```bash
./tdtpcli --import users.tdtp.xml
```

**ВАЖНО**: При импорте используются временные таблицы:
1. Данные загружаются в `table_name_tmp_YYYYMMDD_HHMMSS`
2. После проверки временная таблица заменяет продакшен
3. Старая версия удаляется
4. При ошибке - автоматический откат

## Command Reference

| Command | Description |
|---------|-------------|
| `--list` | List all tables |
| `--export <table>` | Export table to TDTP format |
| `--import <file>` | Import TDTP file |
| `--create-config-XX` | Create config template for database type |
| `--version` | Show version |
| `--help` | Show help |

## Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--config <path>` | Config file path | `config.yaml` |
| `--output <file>` | Output file | `stdout` |

## File Format

**TDTP uses `.tdtp.xml` extension:**
- ✅ `users.tdtp.xml` - Correct
- ❌ `users.xml` - Wrong (generic XML)
- ❌ `users.json` - Wrong (not TDTP format)

The CLI automatically adds `.tdtp.xml` if not specified:
```bash
./tdtpcli --export Users --output users
# Creates: users.tdtp.xml
```

## Examples

```bash
# PostgreSQL setup
./tdtpcli --create-config-pg
nano default_config.yaml  # Edit settings
mv default_config.yaml config.yaml

# SQLite setup
./tdtpcli --create-config-sl
nano default_config.yaml  # Edit path
mv default_config.yaml config.yaml

# List tables
./tdtpcli --list

# Export
./tdtpcli --export Users --output users.tdtp.xml

# Import
./tdtpcli --import users.tdtp.xml

# Custom config location
./tdtpcli --config /path/to/config.yaml --list
```

## Safety Features

### Temporary Tables
All imports use temporary tables for safety:

```
1. Create: Users_tmp_20251116_143000
2. Load data into temporary table
3. Validate data integrity
4. Atomic replace:
   Users → Users_old
   Users_tmp_20251116_143000 → Users
5. Cleanup: DROP Users_old
```

**Benefits:**
- ✅ No data loss if import fails
- ✅ Atomic replacement (no partial updates)
- ✅ Automatic rollback on error
- ✅ Production table always consistent

## Troubleshooting

### Adapter Not Supported
```bash
⚠️  WARNING: mssql adapter is under development
💡 Currently supported: PostgreSQL, SQLite
```
**Solution**: Use PostgreSQL or SQLite, or wait for adapter release

### Connection Failed
```bash
❌ Failed to connect: connection refused
```
**Solution**: 
- For PostgreSQL: Check host, port, user, password in config.yaml
- For SQLite: Check path to database file

### Table Not Found
```bash
❌ Table 'Users' does not exist
```
**Solution**: Run `--list` to see available tables

### Config Not Found
```bash
❌ config.yaml not found
```
**Solution**: Run `--create-config-XX` and rename default_config.yaml

## Supported Databases

| Database | Type | Status | Config Flag |
|----------|------|--------|-------------|
| PostgreSQL | postgres | ✅ Full support | `--create-config-pg` |
| SQLite | sqlite | ✅ Full support | `--create-config-sl` |
| MS SQL Server | mssql | 🚧 In development | `--create-config-ms` |
| MySQL | mysql | 🚧 In development | `--create-config-my` |
| Miranda SQL | miranda | 🚧 In development | `--create-config-mi` |

## Building

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o tdtpcli ./cmd/tdtpcli

# Windows
GOOS=windows GOARCH=amd64 go build -o tdtpcli.exe ./cmd/tdtpcli

# macOS
GOOS=darwin GOARCH=amd64 go build -o tdtpcli ./cmd/tdtpcli
```

## TDTP Format

**TDTP (Table Data Transfer Protocol)** is a specialized XML format for database synchronization:

- Self-documenting (schema included in message)
- Stateless (each response contains full context)
- Broker-oriented (designed for message queues)
- Paginated (automatic splitting for large tables)

### Example TDTP file:
```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>reference</Type>
    <TableName>Users</TableName>
    <MessageID>REF-2025-abc123</MessageID>
  </Header>
  <Schema>
    <Field name="id" type="INTEGER" key="true"/>
    <Field name="name" type="TEXT" length="100"/>
  </Schema>
  <Data>
    <R>1|Alice</R>
    <R>2|Bob</R>
  </Data>
</DataPacket>
```

## License

Part of TDTP Framework
