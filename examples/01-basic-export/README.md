# Basic Export Example

## ❌ Что было не так в старом примере:

Старый пример **вообще не компилировался!**

```go
import (
    "github.com/queuebridge/tdtp/pkg/adapter"  // ❌ Пакет не существует!
    "github.com/queuebridge/tdtp/pkg/tdtp"     // ❌ Пакет не существует!
)

// Пустые структуры без подключения к БД
return &adapter.PostgreSQLAdapter{}  // ❌ Не работает
return &adapter.FileAdapter{}        // ❌ Не работает
```

**Ошибка компиляции:**
```
main.go:8:2: no required module provides package github.com/queuebridge/tdtp/pkg/adapter
```

## ✅ Что изменилось:

Полностью переписан с использованием **реального TDTP Framework API**:

### 1. Правильные импорты
```go
import (
    "github.com/queuebridge/tdtp/pkg/adapters"           // ✅ Реальный пакет
    _ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"  // ✅ Драйвер SQLite
    "github.com/queuebridge/tdtp/pkg/core/packet"        // ✅ TDTP пакеты
    "github.com/queuebridge/tdtp/pkg/core/schema"        // ✅ Схемы
)
```

### 2. Реальное подключение к БД
```go
adapter, err := adapters.New(ctx, adapters.Config{
    Type: "sqlite",
    DSN:  "example.db",
})
```

### 3. Реальный экспорт через TDTP
```go
// Framework автоматически:
// - Определяет схему таблицы
// - Выполняет SQL запрос
// - Конвертирует результат в TDTP пакеты
packets, err := adapter.ExportTable(ctx, "users")
```

### 4. Сериализация в XML
```go
// Конвертация TDTP пакета в XML
xml, err := packet.ToXML(pkt)
```

## Что демонстрирует пример:

1. **Automatic schema detection** - фреймворк сам определяет структуру таблицы
2. **Database → TDTP conversion** - автоматическое преобразование данных из БД в TDTP формат
3. **XML serialization** - сериализация TDTP пакетов в XML файл
4. **Cross-database compatibility** - один и тот же код работает с любой СУБД (просто измените `Type: "postgres"`)

## Как запустить:

```bash
cd examples/01-basic-export

# Запустить (создаст example.db автоматически)
go run .

# Результат
ls -la output/users.tdtp.xml
```

## Вывод:

**Теперь пример РАБОТАЕТ!**

Вместо пустышек и несуществующих пакетов - полностью рабочий код, который:
- ✅ Компилируется
- ✅ Запускается
- ✅ Создает реальную БД
- ✅ Экспортирует данные через TDTP
- ✅ Сохраняет результат в XML

Это демонстрирует силу TDTP Framework - простой экспорт данных с автоматическим определением схемы!
