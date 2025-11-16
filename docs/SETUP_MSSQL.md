# MS SQL Server Adapter - Setup Guide

## Quick Start

### 1. Обновить зависимости

После клонирования или обновления проекта из GitHub выполните:

```bash
# Обновить go.mod и go.sum
go mod tidy

# Скачать зависимости
go mod download
```

**Важно:** В PR ветке `claude/add-mssql-driver-dependency-01D8SVFXquUqZnfFw3sboLtP` уже добавлена зависимость:
- `github.com/denisenkom/go-mssqldb v0.12.3` - MS SQL Server драйвер
- `github.com/golang-sql/civil` - SQL types
- `github.com/golang-sql/sqlexp` - SQL expression

После мержа PR в main, просто запустите `go mod download`.

### 2. Проверить сборку

```bash
# Собрать MS SQL адаптер
go build ./pkg/adapters/mssql/

# Собрать весь проект
go build ./...
```

### 3. Запустить тестовое окружение

```bash
# Запустить Docker контейнеры с MS SQL Server
docker-compose -f docker-compose.mssql.yml up -d

# Подождать ~30 секунд пока SQL Server стартует
sleep 30

# Проверить что контейнеры запущены
docker-compose -f docker-compose.mssql.yml ps
```

Доступные контейнеры:
- **mssql-dev** (port 1433) - SQL Server 2019 для разработки
- **mssql-prod-sim** (port 1434) - SQL Server 2019 в режиме SQL Server 2012 (compatibility level 110)
- **mssql-2022** (port 1435) - SQL Server 2022 (latest)

### 4. Запустить тесты

```bash
# Все тесты MS SQL адаптера
go test -v ./pkg/adapters/mssql/

# Конкретный тест
go test -v ./pkg/adapters/mssql/ -run TestIntegration_ExportImport

# С кастомным DSN
MSSQL_TEST_DSN_DEV="server=localhost,1433;user id=sa;password=YourStrong!Passw0rd;database=TestDB" \
go test -v ./pkg/adapters/mssql/
```

### 5. Остановить окружение

```bash
# Остановить контейнеры
docker-compose -f docker-compose.mssql.yml down

# Остановить и удалить volumes
docker-compose -f docker-compose.mssql.yml down -v
```

## Configuration Examples

### Development (auto-detect mode)
```go
cfg := adapters.Config{
    Type: "mssql",
    DSN:  "server=localhost,1433;user id=sa;password=YourStrong!Passw0rd;database=DevDB;encrypt=disable",
    CompatibilityMode: "auto",  // Auto-detect version
    StrictCompatibility: false,  // Allow fallbacks
}
```

### Production (SQL Server 2012 strict mode)
```go
cfg := adapters.Config{
    Type: "mssql",
    DSN:  "server=prod-server;user id=sa;password=SecurePass;database=ProdDB",
    CompatibilityMode: "2012",  // Explicit SQL Server 2012
    StrictCompatibility: true,   // Error on incompatible functions
    WarnOnIncompatible: true,    // Show warnings
}
```

### Connection String Formats

**Standard format:**
```
server=localhost,1433;user id=sa;password=Pass;database=MyDB;encrypt=disable
```

**With encryption:**
```
server=localhost,1433;user id=sa;password=Pass;database=MyDB;encrypt=true;TrustServerCertificate=false
```

**With instance name:**
```
server=localhost\SQLEXPRESS;user id=sa;password=Pass;database=MyDB;encrypt=disable
```

**Azure SQL Database:**
```
server=myserver.database.windows.net;user id=user@myserver;password=Pass;database=MyDB;encrypt=true
```

## Usage Example

```go
package main

import (
    "context"
    "log"

    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/mssql"  // Register MS SQL adapter
)

func main() {
    ctx := context.Background()

    // Configure adapter
    cfg := adapters.Config{
        Type: "mssql",
        DSN:  "server=localhost,1433;user id=sa;password=YourStrong!Passw0rd;database=TestDB;encrypt=disable",
        CompatibilityMode: "2012",
        StrictCompatibility: true,
    }

    // Create adapter
    adapter, err := adapters.New(ctx, cfg)
    if err != nil {
        log.Fatalf("Failed to create adapter: %v", err)
    }
    defer adapter.Close(ctx)

    // Check version
    version, _ := adapter.GetDatabaseVersion(ctx)
    log.Printf("Connected to: %s", version)

    // Export table
    packets, err := adapter.ExportTable(ctx, "Users")
    if err != nil {
        log.Fatalf("Export failed: %v", err)
    }

    log.Printf("Exported %d packets with %d rows total",
        len(packets),
        countRows(packets))

    // Import to another table (UPSERT)
    err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
    if err != nil {
        log.Fatalf("Import failed: %v", err)
    }

    log.Println("✓ Export/Import successful")
}

func countRows(packets []*packet.DataPacket) int {
    total := 0
    for _, pkt := range packets {
        total += len(pkt.Data)
    }
    return total
}
```

## Troubleshooting

### "Login failed for user" error
- Проверьте credentials в DSN
- Убедитесь что SQL Server Authentication включен
- Проверьте что пользователь имеет доступ к database

### "Network error" или "Connection refused"
- Проверьте что SQL Server запущен: `docker-compose -f docker-compose.mssql.yml ps`
- Проверьте порт: `telnet localhost 1433`
- Проверьте firewall правила

### "Compatibility mode" ошибки
- Используйте `StrictCompatibility: false` для fallback
- Проверьте фактический compatibility level: `SELECT compatibility_level FROM sys.databases WHERE name = DB_NAME()`
- Для продакшена всегда указывайте явный `CompatibilityMode: "2012"`

### "Package not found" ошибка
```bash
# Переустановить зависимости
go clean -modcache
go mod download
go mod tidy
```

### Docker контейнеры не стартуют
```bash
# Проверить логи
docker-compose -f docker-compose.mssql.yml logs

# Пересоздать контейнеры
docker-compose -f docker-compose.mssql.yml down -v
docker-compose -f docker-compose.mssql.yml up -d
```

## Performance Tips

1. **Batch Size**: Используйте оптимальный размер батча (default: 1000 rows)
   ```go
   // Для больших таблиц можно увеличить
   const maxPacketSize = 5000
   ```

2. **Connection Pool**: Настройте connection pool
   ```go
   cfg.MaxConns = 10  // Максимум открытых подключений
   cfg.MinConns = 2   // Минимум idle подключений
   ```

3. **Transactions**: Всегда используйте транзакции для импорта
   ```go
   err := adapter.ImportPackets(ctx, packets, adapters.StrategyReplace)
   // Все пакеты в одной транзакции
   ```

4. **Indexes**: Для больших таблиц создайте indexes перед экспортом
   ```sql
   CREATE INDEX idx_export ON MyTable (timestamp) WHERE exported = 0;
   ```

## Security

1. **Never hardcode credentials**
   ```go
   // ❌ Bad
   DSN: "server=prod;user id=sa;password=hardcoded"

   // ✅ Good
   DSN: os.Getenv("MSSQL_DSN")
   ```

2. **Use encryption in production**
   ```go
   DSN: "server=prod;...;encrypt=true;TrustServerCertificate=false"
   ```

3. **Least privilege principle**
   - Не используйте `sa` пользователя
   - Создайте dedicated user с минимальными правами
   ```sql
   CREATE USER tdtp_user WITH PASSWORD = 'StrongPass';
   GRANT SELECT, INSERT, UPDATE ON DATABASE::MyDB TO tdtp_user;
   ```

## Documentation

- [MS SQL 2012 Compatibility Guide](./MSSQL_2012_COMPATIBILITY.md)
- [Dev vs Prod Environments](./MSSQL_DEV_VS_PROD.md)
- [Compatibility Modes Architecture](./MSSQL_COMPATIBILITY_MODES.md)
- [Implementation Summary](./MSSQL_IMPLEMENTATION_SUMMARY.md)

## Support

- GitHub Issues: https://github.com/ruslano69/tdtp-framework/issues
- MS SQL Server Docs: https://docs.microsoft.com/sql/
- Driver Docs: https://github.com/denisenkom/go-mssqldb
