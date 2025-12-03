# Incremental Sync Example - TDTP Framework

## Проблема в старом примере

**Старый пример НЕ использовал фреймворк!** Там был просто mock код:
- Пустые структуры адаптеров `&adapter.PostgreSQLAdapter{}`
- Ручная генерация mock данных
- `time.Sleep()` вместо реального импорта
- Комментарии с SQL запросами, которые НЕ выполнялись

## Что изменилось в новом примере

### ✅ Теперь используется РЕАЛЬНЫЙ TDTP Framework:

1. **Создание адаптеров через фабрику**:
```go
cfg := adapters.Config{
    Type: "sqlite",
    DSN:  "source.db",
}
sourceAdapter, err := adapters.New(ctx, cfg)
```

2. **Экспорт через TDTP протокол**:
```go
// Фреймворк автоматически:
// - Строит SQL запрос с WHERE updated_at > last_value
// - Конвертирует результат в TDTP пакеты
// - Обрабатывает маппинг типов
packets, newLastValue, err := sourceAdapter.ExportTableIncremental(
    ctx,
    tableName,
    syncConfig,
)
```

3. **Импорт через TDTP протокол**:
```go
// Фреймворк автоматически:
// - Создает таблицу если не существует (из схемы TDTP)
// - Делает маппинг типов (PostgreSQL → SQLite)
// - Выполняет bulk insert с UPSERT стратегией
err = targetAdapter.ImportPackets(ctx, packets, adapters.StrategyReplace)
```

## Что демонстрирует пример

### 🎯 Основные возможности TDTP Framework:

1. **Cross-database sync** - синхронизация между разными СУБД (PostgreSQL ↔ SQLite)
2. **Automatic schema detection** - автоматическое определение схемы таблицы
3. **Type mapping** - автоматический маппинг типов данных между СУБД
4. **Incremental sync** - инкрементальная синхронизация по timestamp
5. **Checkpoint management** - управление checkpoint'ами для возможности возобновления
6. **Audit trail** - полный аудит всех операций

### 📊 Workflow:

```
┌─────────────────────────────────────────────────────────────┐
│  1. Load checkpoint from sync_state.json                    │
│     └─ last_value: "2024-01-15T10:30:00Z"                  │
└─────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│  2. ExportTableIncremental(source, "users", config)         │
│     Framework auto-generates:                               │
│     SELECT * FROM users                                     │
│     WHERE updated_at > '2024-01-15T10:30:00Z'              │
│     ORDER BY updated_at LIMIT 1000                          │
└─────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│  3. Convert to TDTP packets                                 │
│     ┌──────────────────────────────────────┐               │
│     │ TDTP Packet                          │               │
│     │ ├─ Schema (auto-detected)            │               │
│     │ │  ├─ id: INTEGER (PK)               │               │
│     │ │  ├─ name: TEXT(100)                │               │
│     │ │  ├─ email: TEXT(100)               │               │
│     │ │  └─ updated_at: TIMESTAMP          │               │
│     │ └─ Data (pipe-delimited rows)        │               │
│     │    ├─ 1|John Doe|john@...|2024...    │               │
│     │    └─ 2|Jane Smith|jane@...|2024...  │               │
│     └──────────────────────────────────────┘               │
└─────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│  4. ImportPackets(target, packets, StrategyReplace)         │
│     Framework auto-generates:                               │
│     CREATE TABLE IF NOT EXISTS users (...)                  │
│     INSERT OR REPLACE INTO users VALUES (...)               │
└─────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│  5. Save checkpoint to sync_state.json                      │
│     └─ new_last_value: "2024-01-15T12:45:00Z"             │
└─────────────────────────────────────────────────────────────┘
```

## Как запустить

```bash
cd examples/03-incremental-sync

# Запустить пример (создаст source.db и target.db автоматически)
go run .

# После первого запуска - изменить данные в source.db
sqlite3 source.db "UPDATE users SET updated_at = datetime('now') WHERE id = 1"

# Запустить снова - синхронизируется только измененная запись!
go run .
```

## Ключевые отличия от старого кода

| Старый пример | Новый пример |
|--------------|--------------|
| `&adapter.PostgreSQLAdapter{}` | `adapters.New(ctx, cfg)` |
| Mock данные в коде | Реальные данные из БД |
| `time.Sleep()` | `targetAdapter.ImportPackets()` |
| Ручные SQL запросы в комментариях | Автоматические SQL через TDTP |
| Не работает вообще | Полностью рабочий пример |

## Вывод

**Теперь пример демонстрирует РЕАЛЬНУЮ работу TDTP Framework!**

Вместо ручной перекачки данных через SQL, фреймворк:
- Автоматически экспортирует данные в универсальный формат TDTP
- Автоматически импортирует из TDTP в любую целевую СУБД
- Обрабатывает маппинг типов
- Управляет checkpoint'ами
- Ведет полный аудит

Это и есть сила TDTP Framework - абстракция над разными СУБД через универсальный протокол обмена данными!
