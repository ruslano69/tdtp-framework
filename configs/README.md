# Configuration Files

Example configuration files for different databases and scenarios.

## Files

- `config.example.yaml` - Basic configuration template
- `config.mssql.yaml` - Microsoft SQL Server configuration
- `config.postgres.yaml` - PostgreSQL configuration
- `config.sqlite-import.yaml` - SQLite import configuration
- `sqlite_config.yaml` - SQLite configuration

## Usage

Copy the example config and customize for your environment:

```bash
cp configs/config.example.yaml config.yaml
# Edit config.yaml with your database credentials
```

**Note:** `config.yaml` in the root is ignored by git for security.
