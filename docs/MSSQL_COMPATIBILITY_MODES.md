# MS SQL Server Compatibility Modes

**Дата:** 16.11.2025
**Версия:** 1.0

## 🎯 Концепция

**Один адаптер** для всех версий SQL Server с **явным указанием compatibility mode**.

## 📋 Поддерживаемые режимы

### Mode: "2012" (SQL Server 2012-2014)
```yaml
database:
  type: mssql
  compatibility_mode: "2012"  # ← Явно указываем
```

**Доступные функции:**
- ✅ OFFSET/FETCH
- ✅ MERGE
- ✅ IIF, TRY_CONVERT, FORMAT
- ✅ Table-Valued Parameters
- ❌ JSON функции
- ❌ STRING_SPLIT
- ❌ STRING_AGG

**Use case:** Production SQL Server 2012

### Mode: "2016" (SQL Server 2016-2017)
```yaml
database:
  type: mssql
  compatibility_mode: "2016"
```

**Дополнительно к 2012:**
- ✅ JSON_VALUE, JSON_QUERY, FOR JSON
- ✅ STRING_SPLIT
- ✅ DROP IF EXISTS
- ❌ STRING_AGG
- ❌ TRIM

**Use case:** Средние версии SQL Server

### Mode: "2019" (SQL Server 2019+)
```yaml
database:
  type: mssql
  compatibility_mode: "2019"
```

**Все функции:**
- ✅ Все из 2012 + 2016
- ✅ STRING_AGG
- ✅ TRIM, CONCAT_WS
- ✅ Table variables deferred compilation

**Use case:** Современные инсталляции

### Mode: "auto" (автоопределение)
```yaml
database:
  type: mssql
  compatibility_mode: "auto"  # По умолчанию
```

**Поведение:**
- Определяет версию сервера при подключении
- Использует доступные функции автоматически
- **Опасно для production:** может работать в dev, не работать в prod!

**Use case:** Development, testing

## 🔧 CLI Поддержка

### Основной синтаксис
```bash
# Явное указание SQL Server 2012
tdtpcli --type mssql --compat 2012 --export Users

# Короткая форма
tdtpcli --mssql-2012 --export Users

# Или через config
tdtpcli --config mssql2012.yaml --export Users
```

### Флаги

**--compat <mode>**
```bash
tdtpcli --compat 2012 --export Users  # SQL Server 2012
tdtpcli --compat 2016 --export Users  # SQL Server 2016
tdtpcli --compat 2019 --export Users  # SQL Server 2019
tdtpcli --compat auto --export Users  # Auto-detect
```

**Shortcuts:**
```bash
--mssql-2012  # Эквивалент --type mssql --compat 2012
--mssql-2016  # Эквивалент --type mssql --compat 2016
--mssql-2019  # Эквивалент --type mssql --compat 2019
```

## 📝 Примеры конфигурации

### Production (SQL Server 2012)
```yaml
# config-prod.yaml
database:
  type: mssql
  compatibility_mode: "2012"  # ← ЯВНО указываем
  host: sql-prod.company.local
  port: 1433
  user: tdtp_user
  password: ${MSSQL_PASSWORD}
  dbname: ProductionDB
  schema: dbo

  # Дополнительные опции
  strict_compatibility: true  # Ошибка при попытке использовать недоступные функции
  warn_on_incompatible: true  # Предупреждение в логах
```

### Development (Auto-detect)
```yaml
# config-dev.yaml
database:
  type: mssql
  compatibility_mode: "auto"  # Определяется автоматически
  host: localhost
  port: 1433
  user: sa
  password: DevPassword123!
  dbname: DevDB

  # В dev можем использовать auto
  # Но лучше установить 2012 для совместимости!
```

### Рекомендуемый Development (безопасный)
```yaml
# config-dev-safe.yaml
database:
  type: mssql
  compatibility_mode: "2012"  # ← Как в production!
  host: localhost
  port: 1434  # prod simulation контейнер
  user: sa
  password: ProdPassword123!
  dbname: ProdSimDB

  strict_compatibility: true  # Ошибка если используем недоступные функции
```

## 🔍 Feature Detection vs Explicit Mode

### Feature Detection (внутри адаптера)
```go
func (a *Adapter) detectCompatibility() {
    // 1. Читаем версию сервера
    var version string
    a.db.QueryRow("SELECT SERVERPROPERTY('ProductVersion')").Scan(&version)
    // "11.0.2100" → serverVersion = 11 (SQL Server 2012)

    // 2. Читаем compatibility level БД
    var compatLevel int
    a.db.QueryRow(`
        SELECT compatibility_level
        FROM sys.databases
        WHERE name = DB_NAME()
    `).Scan(&compatLevel)
    // compatLevel = 110 (SQL Server 2012)

    // 3. Используем минимум из двух
    a.effectiveCompat = min(serverVersion, compatLevel)
}
```

### Explicit Mode (из конфига)
```go
func NewAdapter(cfg Config) (*Adapter, error) {
    a := &Adapter{}

    // Feature detection
    a.detectCompatibility()

    // Если указан явный режим - используем его
    if cfg.CompatibilityMode != "auto" {
        explicitLevel := parseCompatMode(cfg.CompatibilityMode)

        // КРИТИЧНО: Если explicit > detected
        if explicitLevel > a.effectiveCompat {
            if cfg.StrictCompatibility {
                return nil, fmt.Errorf(
                    "requested compatibility %d, but server only supports %d",
                    explicitLevel, a.effectiveCompat)
            } else {
                log.Warnf("Requested SQL Server %s, but server is %s",
                    cfg.CompatibilityMode, a.serverVersionStr)
            }
        }

        // Используем минимум для безопасности
        a.effectiveCompat = min(explicitLevel, a.effectiveCompat)
    }

    return a, nil
}
```

## 💡 Рекомендации

### ДЛЯ PRODUCTION:

**1. ВСЕГДА указывайте явный compatibility mode:**
```yaml
compatibility_mode: "2012"  # НЕ используйте "auto"!
```

**2. Включайте strict mode:**
```yaml
strict_compatibility: true  # Ошибка при несовместимых функциях
```

**3. Тестируйте на том же режиме:**
```bash
# Dev окружение
docker-compose -f docker-compose.mssql.yml up -d mssql-prod-sim

# Config для dev с тем же compat mode что в prod
compatibility_mode: "2012"
```

### ДЛЯ DEVELOPMENT:

**Вариант A: Безопасный (рекомендуется)**
```yaml
# Используем тот же режим что в production
compatibility_mode: "2012"
strict_compatibility: true
```

**Вариант B: Гибкий**
```yaml
# Auto-detect, но с предупреждениями
compatibility_mode: "auto"
warn_on_incompatible: true
```

## 🚨 Strict vs Warn Mode

### Strict Mode (production)
```yaml
strict_compatibility: true
```

**Поведение:**
```go
// Если пытаемся использовать недоступную функцию
if a.effectiveCompat < 130 && usesJSONFunctions(query) {
    return errors.New("JSON functions not available in SQL Server 2012")
}
```

**Use case:** Production - лучше упасть, чем работать некорректно

### Warn Mode (development)
```yaml
strict_compatibility: false
warn_on_incompatible: true
```

**Поведение:**
```go
if a.effectiveCompat < 130 && usesJSONFunctions(query) {
    log.Warnf("WARNING: Using JSON functions not available in SQL Server 2012")
    // Продолжаем выполнение
}
```

**Use case:** Development - показываем предупреждения, но работаем

## 📊 Comparison Table

| Mode | Версии | OFFSET/FETCH | MERGE | JSON | STRING_SPLIT | STRING_AGG |
|------|--------|--------------|-------|------|--------------|------------|
| 2012 | 2012-2014 | ✅ | ✅ | ❌ | ❌ | ❌ |
| 2016 | 2016-2017 | ✅ | ✅ | ✅ | ✅ | ❌ |
| 2019 | 2019+ | ✅ | ✅ | ✅ | ✅ | ✅ |
| auto | Любые | Зависит от сервера | | | | |

## 🎯 Примеры использования

### Example 1: Production с SQL Server 2012
```go
cfg := adapters.Config{
    Type:             "mssql",
    DSN:              "server=prod-sql;...",
    CompatibilityMode: "2012",
    StrictCompatibility: true,
}

adapter, err := adapters.New(ctx, cfg)
// Adapter будет использовать ТОЛЬКО SQL Server 2012 функции
// Попытка использовать JSON → ERROR
```

### Example 2: Development с auto-detect
```go
cfg := adapters.Config{
    Type:             "mssql",
    DSN:              "server=localhost,1433;...",
    CompatibilityMode: "auto",
    WarnOnIncompatible: true,
}

adapter, err := adapters.New(ctx, cfg)
// Определит версию сервера автоматически
// Покажет предупреждения если используются несовместимые функции
```

### Example 3: CLI с явным режимом
```bash
# Создаем конфиг для SQL Server 2012
tdtpcli --create-config-ms --compat 2012

# Результат: config-mssql-2012.yaml
database:
  type: mssql
  compatibility_mode: "2012"
  strict_compatibility: true
  ...

# Используем
tdtpcli --config config-mssql-2012.yaml --export Users
```

## 🔧 Implementation Details

### Config Structure
```go
type Config struct {
    Type              string `yaml:"type"`
    DSN               string `yaml:"dsn"`

    // Compatibility settings
    CompatibilityMode    string `yaml:"compatibility_mode"` // "2012", "2016", "2019", "auto"
    StrictCompatibility  bool   `yaml:"strict_compatibility"`
    WarnOnIncompatible   bool   `yaml:"warn_on_incompatible"`
}
```

### Adapter Structure
```go
type Adapter struct {
    db                *sql.DB
    serverVersion     int    // 11, 13, 14, 15, 16
    compatLevel       int    // 110, 130, 140, 150, 160
    effectiveCompat   int    // Actual используемый уровень
    strictMode        bool
    warnMode          bool
}

// Feature checks
func (a *Adapter) SupportsJSON() bool {
    return a.effectiveCompat >= 130 // SQL Server 2016
}

func (a *Adapter) SupportsStringSplit() bool {
    return a.effectiveCompat >= 130
}

func (a *Adapter) SupportsStringAgg() bool {
    return a.effectiveCompat >= 140 // SQL Server 2017
}
```

## ✅ Benefits

**Одна кодовая база:**
- Нет дублирования
- Проще поддержка
- Меньше багов

**Гибкость:**
- Работает со всеми версиями
- Можно явно указать режим
- Auto-detect для удобства

**Безопасность:**
- Strict mode предотвращает ошибки
- Явное указание compatibility mode
- Невозможно случайно использовать недоступные функции

**Удобство:**
- CLI флаги (`--mssql-2012`)
- Config файлы
- Автоматические проверки

---

**Рекомендация:** Использовать **один адаптер** с **explicit compatibility mode** + **strict mode** для production.

**Версия:** 1.0
**Статус:** Recommended approach
