# TDTP Framework v0.5 - Installation & Usage Guide

## 🚀 Быстрый старт

### 1. Установка фреймворка

```bash
# Распаковка архива
tar -xzf tdtp-framework-v0.5.tar.gz
cd tdtp-framework

# Инициализация Go модуля
go mod tidy
```

### 2. Установка SQLite драйвера

**Важно:** SQLite адаптер требует внешний драйвер для работы с БД.

#### Вариант A: Pure Go (без CGO)
```bash
go get modernc.org/sqlite
```

**Преимущества:**
- Не требует C компилятор
- Работает на любой платформе
- Простая установка

**Недостатки:**
- Чуть медленнее CGO версии

#### Вариант B: CGO версия (быстрее)
```bash
go get github.com/mattn/go-sqlite3
```

**Требования:**
- GCC или Clang
- CGO_ENABLED=1

**Преимущества:**
- Максимальная производительность
- Полная совместимость с SQLite

## 📦 Структура проекта

```
tdtp-framework/
├── pkg/                      # Core библиотеки
│   ├── core/
│   │   ├── packet/          # XML парсинг/генерация
│   │   ├── schema/          # Типы и валидация
│   │   └── tdtql/           # SQL → TDTQL + Executor
│   └── adapters/
│       └── sqlite/          # SQLite интеграция
├── examples/                 # Примеры использования
│   ├── basic/               # Packet примеры
│   ├── schema/              # Schema примеры
│   ├── tdtql/               # TDTQL примеры
│   ├── executor/            # Executor примеры
│   └── sqlite/              # SQLite примеры
├── docs/                     # Документация
└── go.mod                    # Go модуль
```

## 🧪 Проверка установки

### Тест core модулей (без SQLite драйвера)

```bash
# Packet module
go test ./pkg/core/packet -v

# Schema module
go test ./pkg/core/schema -v

# TDTQL module
go test ./pkg/core/tdtql -v
```

**Ожидаемый результат:** All tests pass ✅

### Тест с SQLite (требует драйвер)

```bash
# Добавляем драйвер в go.mod
go get modernc.org/sqlite

# Создаем тестовую БД
sqlite3 test.db "CREATE TABLE Users (ID INTEGER PRIMARY KEY, Name TEXT, Balance REAL)"
sqlite3 test.db "INSERT INTO Users VALUES (1, 'John', 1000.0), (2, 'Jane', 2000.0)"

# Запускаем пример
cd examples/sqlite
go run main.go
```

## 💡 Примеры использования

### 1. Создание TDTP пакета

```go
package main

import (
    "fmt"
    "github.com/queuebridge/tdtp/pkg/core/packet"
    "github.com/queuebridge/tdtp/pkg/core/schema"
)

func main() {
    // Создаем схему
    builder := schema.NewBuilder()
    schemaObj := builder.
        AddInteger("ID", true).
        AddText("Name", 100).
        AddDecimal("Balance", 18, 2).
        Build()
    
    // Создаем данные
    rows := [][]string{
        {"1", "Company A", "15000.50"},
        {"2", "Company B", "25000.00"},
    }
    
    // Генерируем пакет
    generator := packet.NewGenerator()
    packets, _ := generator.GenerateReference("Companies", schemaObj, rows)
    
    // Сохраняем в XML
    xml, _ := packets[0].ToXML()
    fmt.Println(xml)
}
```

### 2. Export из SQLite

```go
package main

import (
    "fmt"
    "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
    _ "modernc.org/sqlite" // или _ "github.com/mattn/go-sqlite3"
)

func main() {
    // Подключаемся к БД
    adapter, err := sqlite.NewAdapter("database.db")
    if err != nil {
        panic(err)
    }
    defer adapter.Close()
    
    // Экспортируем таблицу
    packets, err := adapter.ExportTable("Users")
    if err != nil {
        panic(err)
    }
    
    // Сохраняем в файлы
    for i, pkt := range packets {
        xml, _ := pkt.ToXML()
        filename := fmt.Sprintf("users_part_%d.xml", i+1)
        os.WriteFile(filename, []byte(xml), 0644)
    }
    
    fmt.Printf("Exported %d packets\n", len(packets))
}
```

### 3. Import в SQLite

```go
package main

import (
    "os"
    "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
    "github.com/queuebridge/tdtp/pkg/core/packet"
    _ "modernc.org/sqlite"
)

func main() {
    // Подключаемся к БД
    adapter, _ := sqlite.NewAdapter("target.db")
    defer adapter.Close()
    
    // Читаем TDTP файл
    xml, _ := os.ReadFile("users.xml")
    
    // Парсим
    parser := packet.NewParser()
    pkt, _ := parser.Parse(xml)
    
    // Импортируем (с заменой существующих)
    err := adapter.ImportPacket(pkt, sqlite.StrategyReplace)
    if err != nil {
        panic(err)
    }
    
    fmt.Println("Import complete!")
}
```

### 4. SQL → TDTQL трансляция

```go
package main

import (
    "fmt"
    "github.com/queuebridge/tdtp/pkg/core/tdtql"
)

func main() {
    sql := `
        SELECT * FROM Users
        WHERE IsActive = 1 AND Balance > 1000
        ORDER BY Balance DESC
        LIMIT 100
    `
    
    translator := tdtql.NewTranslator()
    query, err := translator.Translate(sql)
    if err != nil {
        panic(err)
    }
    
    // query теперь содержит TDTQL фильтры
    fmt.Printf("Filters: %+v\n", query.Filters)
    fmt.Printf("OrderBy: %+v\n", query.OrderBy)
    fmt.Printf("Limit: %d\n", *query.Limit)
}
```

### 5. Выполнение TDTQL запроса

```go
package main

import (
    "fmt"
    "github.com/queuebridge/tdtp/pkg/core/tdtql"
)

func main() {
    // Подготовка данных
    schema := packet.Schema{...}
    rows := [][]string{...}
    
    // SQL запрос
    sql := "SELECT * FROM Users WHERE Balance > 1000 ORDER BY Balance DESC"
    
    // Трансляция
    translator := tdtql.NewTranslator()
    query, _ := translator.Translate(sql)
    
    // Выполнение
    executor := tdtql.NewExecutor()
    result, _ := executor.Execute(query, rows, schema)
    
    fmt.Printf("Total: %d\n", result.TotalRows)
    fmt.Printf("After filters: %d\n", result.FilteredRows)
    fmt.Printf("Returned: %d\n", len(result.FilteredRows))
    fmt.Printf("More available: %v\n", result.MoreAvailable)
}
```

## 🔧 Настройка окружения

### Go версия

Требуется Go 1.22.2 или выше:

```bash
go version
# go version go1.22.2 linux/amd64
```

### Переменные окружения

```bash
# Для CGO версии SQLite драйвера
export CGO_ENABLED=1

# Для pure Go версии (по умолчанию)
export CGO_ENABLED=0
```

## 📚 Документация

Полная документация в директории `docs/`:

- **PACKET_MODULE.md** - работа с TDTP пакетами
- **SCHEMA_MODULE.md** - типы данных и валидация
- **TDTQL_TRANSLATOR.md** - SQL → TDTQL трансляция
- **SQLITE_ADAPTER.md** - интеграция с SQLite

## 🐛 Troubleshooting

### "go: module not found"

```bash
# Очистка модулей
go clean -modcache

# Повторная загрузка
go mod download
go mod tidy
```

### "sqlite3 driver not found"

Установите драйвер:

```bash
# Pure Go
go get modernc.org/sqlite

# Или CGO
go get github.com/mattn/go-sqlite3
```

И добавьте импорт в ваш код:

```go
import _ "modernc.org/sqlite"
// или
import _ "github.com/mattn/go-sqlite3"
```

### CGO ошибки (для mattn/go-sqlite3)

**Linux:**
```bash
apt install gcc
```

**macOS:**
```bash
xcode-select --install
```

**Windows:**
```bash
# Установите MinGW или TDM-GCC
```

Или используйте pure Go версию (modernc.org/sqlite)

### "network forbidden" при go get

Если в вашей среде ограничен доступ к сети:

1. Скачайте модули на другой машине
2. Скопируйте $GOPATH/pkg/mod в целевую среду
3. Используйте GOPROXY=direct

```bash
GOPROXY=direct go get modernc.org/sqlite
```

## 📞 Поддержка

- **GitHub Issues**: https://github.com/queuebridge/tdtp/issues
- **Email**: support@queuebridge.io
- **Документация**: /docs/*.md

## 🎓 Дополнительные материалы

### Архитектура TDTP

1. **Packet** - XML сообщения с самоописанием
2. **Schema** - типизированные данные с валидацией
3. **TDTQL** - SQL-like язык запросов
4. **Adapters** - двунаправленная интеграция с БД
5. **Brokers** (v1.0) - интеграция с очередями

### Use Cases

- **Legacy системы** - интеграция старых и новых систем
- **Микросервисы** - обмен справочниками
- **Event-driven** - синхронизация данных через события
- **Backup/Restore** - экспорт/импорт данных в универсальном формате

---

**Версия:** v0.5
**Дата:** 14.11.2025
**Статус:** Beta - Core Complete, Production Ready for SQLite

Приятной работы! 🚀
