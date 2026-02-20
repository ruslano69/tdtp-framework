# 🗺️ MAP ПРОЕКТА: TDTP-XRAY vs FRAMEWORK

**Создано:** 2026-02-20
**Инструмент:** funcfinder + анализ кода
**Цель:** Убрать дубликаты, использовать фреймворк правильно

---

## 📊 СТАТИСТИКА

| Компонент | Функций | Строк кода | Дубликатов |
|-----------|---------|------------|------------|
| **pkg/etl** | 80 | ~3640 | 0 |
| **pkg/adapters** | 30+ | ~2000 | 0 |
| **cmd/tdtp-xray** | 100+ | ~4442 | **МНОГО!** |

---

## 🔴 КРИТИЧНЫЕ ДУБЛИКАТЫ

### 1️⃣ РАБОТА С INMEMORY SQLite

**pkg/etl/workspace.go (ПРАВИЛЬНО ✅):**
```
NewWorkspace()              → Создание :memory: workspace
CreateTable()               → CREATE TABLE с типами из схемы
LoadData()                  → Bulk INSERT
ExecuteSQL()                → Выполнение SQL
mapTDTPTypeToSQLite()       → Маппинг типов TDTP→SQLite
```

**cmd/tdtp-xray/app.go (ВЕЛОСИПЕД ❌):**
```
loadSourceToMemory()        → Дубликат LoadData()
createAndFillTable()        → Дубликат CreateTable() + LoadData()
mapTDTPToSQLiteType()       → Дубликат mapTDTPTypeToSQLite()
runPreviewSQL()             → Дубликат ExecuteSQL()
```

**РЕШЕНИЕ:** Удалить все 4 функции, использовать Workspace

---

### 2️⃣ РАБОТА С АДАПТЕРАМИ БД

**pkg/adapters (ПРАВИЛЬНО ✅):**
```
adapters.New()              → Универсальное подключение
Adapter.Connect()           → Подключение к БД
ExportHelper                → Экспорт данных
ImportHelper                → Импорт данных
```

**cmd/tdtp-xray/services (ВЕЛОСИПЕД ❌):**
```
ConnectionService           → Дубликат adapters.New()
mapDriverName()             → Дубликат внутри adapters
TestConnection()            → Есть в adapters.Connect()
```

**РЕШЕНИЕ:** Удалить ConnectionService, использовать adapters

---

### 3️⃣ PREVIEW/QUERY ОПЕРАЦИИ

**pkg/adapters + pkg/etl (ПРАВИЛЬНО ✅):**
```
Workspace.ExecuteSQL()      → SQL с LIMIT
Adapter.ExportTableWithQuery() → SELECT с параметрами
```

**cmd/tdtp-xray/services/preview_service.go (ВЕЛОСИПЕД ❌):**
```
PreviewQuery()              → Дубликат ExecuteSQL()
PreviewTDTPSource()         → Дубликат ImportFromTDTP()
PreviewMockSource()         → Велосипед
addLimitToQuery()           → Есть в adapters
```

**РЕШЕНИЕ:** Удалить PreviewService (500+ строк!), использовать Workspace

---

### 4️⃣ TDTP FILE ОПЕРАЦИИ

**pkg/etl/importer.go (ПРАВИЛЬНО ✅):**
```
ImportFromTDTP()            → Импорт TDTP файла
ParallelImporter            → Параллельный импорт multi-part
```

**cmd/tdtp-xray/services/tdtp_service.go (ВЕЛОСИПЕД ❌):**
```
TestTDTPFile()              → Дубликат ImportFromTDTP()
collectAllParts()           → Дубликат ParallelImporter
decompressPacket()          → Есть в pkg/etl
```

**РЕШЕНИЕ:** Удалить TDTPService, использовать ParallelImporter

---

## 📋 ДЕТАЛЬНЫЙ ПЛАН РЕФАКТОРИНГА

### ФАЗА 1: ПОДГОТОВКА (1 час)

1. ✅ Создать ветку `refactor/use-framework-properly`
2. ✅ Добавить тесты для текущего поведения (regression tests)
3. ✅ Создать migration checklist

### ФАЗА 2: ЗАМЕНА INMEMORY ОПЕРАЦИЙ (3 часа)

**Удалить из app.go:**
- `loadSourceToMemory()` → использовать `Workspace.ImportFromTDTP()`
- `createAndFillTable()` → использовать `Workspace.CreateTable()` + `LoadData()`
- `mapTDTPToSQLiteType()` → использовать `workspace.mapTDTPTypeToSQLite()`
- `runPreviewSQL()` → использовать `Workspace.ExecuteSQL()`

**Новый код app.go:**
```go
import "github.com/ruslano69/tdtp-framework/pkg/etl"

func (a *App) PreviewQueryResult() services.PreviewResult {
    // ✅ Было: 100+ строк велосипеда
    // ✅ Стало: 10 строк через framework

    ws, err := etl.NewWorkspace(ctx)
    defer ws.Close(ctx)

    for _, source := range a.sources {
        stats, err := ws.ImportFromTDTP(ctx, reader, source.Name)
    }

    result, err := ws.ExecuteSQL(ctx, sqlQuery, "preview")
    return convertToPreviewResult(result)
}
```

**Экономия:** -200 строк

---

### ФАЗА 3: ЗАМЕНА ADAPTERS СИСТЕМЫ (2 часа)

**Удалить services/connection_service.go (300 строк):**
```go
// ❌ Удалить:
type ConnectionService struct {}
func (cs *ConnectionService) mapDriverName(dbType string) string
func (cs *ConnectionService) TestConnection(dbType, dsn string)

// ✅ Заменить на:
import "github.com/ruslano69/tdtp-framework/pkg/adapters"

adapter, err := adapters.New(ctx, adapters.Config{
    Type: dbType,
    DSN: dsn,
})
```

**Экономия:** -300 строк

---

### ФАЗА 4: ЗАМЕНА PREVIEW SERVICE (4 часа)

**Удалить services/preview_service.go (500 строк):**
```go
// ❌ Удалить всё:
type PreviewService struct {}
func (ps *PreviewService) PreviewQuery()
func (ps *PreviewService) PreviewTDTPSource()
func (ps *PreviewService) addLimitToQuery()

// ✅ Заменить на Workspace:
ws, _ := etl.NewWorkspace(ctx)
result, _ := ws.ExecuteSQL(ctx, "SELECT * FROM table LIMIT 100", "preview")
```

**Экономия:** -500 строк

---

### ФАЗА 5: ЗАМЕНА TDTP SERVICE (2 часа)

**Удалить services/tdtp_service.go (400 строк):**
```go
// ❌ Удалить:
type TDTPService struct {}
func (ts *TDTPService) TestTDTPFile()
func (ts *TDTPService) collectAllParts()

// ✅ Заменить на:
import "github.com/ruslano69/tdtp-framework/pkg/etl"

importer := etl.NewParallelImporter(config)
stats, err := importer.ImportFromTDTP(ctx, reader, tableName)
```

**Экономия:** -400 строк

---

### ФАЗА 6: ФИНАЛЬНАЯ ОЧИСТКА (1 час)

1. Удалить неиспользуемые утилиты
2. Обновить импорты
3. Запустить тесты
4. Обновить документацию

---

## 📈 ИТОГОВАЯ ЭКОНОМИЯ

| Метрика | До | После | Экономия |
|---------|-----|-------|----------|
| Строк кода | 4442 | ~2500 | **-44%** |
| Файлов | 15 | 8 | **-47%** |
| Дубликатов | 10+ функций | 0 | **-100%** |
| Поддержка | Сложно | Просто | **+∞** |
| Тесты | Отдельные | Фреймворковые | **Переиспользование** |
| Баги | Много (типы TEXT) | Меньше | **Унификация** |

---

## ⚠️ РИСКИ

1. **Breaking changes** в UI → Нужны regression тесты
2. **Изменение поведения** → Нужна документация миграции
3. **Временные баги** → Поэтапный rollout

---

## ✅ КРИТЕРИИ УСПЕХА

- [ ] Все тесты проходят
- [ ] Размер кода < 2500 строк
- [ ] 0 дубликатов функций
- [ ] Типы сохраняются для всех источников
- [ ] Performance не хуже (лучше!)
- [ ] Документация обновлена

---

## 🎯 ПРИОРИТЕТЫ

**КРИТИЧНО (сделать первым):**
1. Фаза 2: InMemory операции (баг с типами TEXT!)
2. Фаза 4: Preview Service (500 строк велосипеда)

**ВАЖНО:**
3. Фаза 3: Adapters система
4. Фаза 5: TDTP Service

**МОЖНО ОТЛОЖИТЬ:**
5. Фаза 6: Финальная очистка
