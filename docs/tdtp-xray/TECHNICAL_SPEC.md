# TDTP X-Ray — Visual ETL Pipeline Constructor

**Версия:** 1.0 FINAL
**Дата:** 2025-02-12
**Статус:** ✅ Утверждено для разработки

## 1. ОБЩЕЕ ОПИСАНИЕ

### 1.1 Назначение
Desktop-приложение для визуального проектирования ETL-пайплайнов на основе TDTP Framework. TDTP (Table Data Transfer Protocol) — это протокол передачи табличных данных через строго типизированный XML-контейнер с самодокументированной схемой.

### 1.2 Целевая аудитория
* Интеграционные архитекторы
* Администраторы баз данных
* Инженеры данных
* Системные аналитики

### 1.3 Ключевая задача
Заменить ручное написание Go-кода или YAML-конфигов на визуальный wizard с:
* Тестированием подключений
* Визуальным построением SQL-запросов (SVG canvas)
* Live preview данных
* Генерацией готовых конфигураций

### 1.4 Ключевые преимущества
* **Скорость разработки:** 10 минут вместо 2-3 часов на pipeline
* **Снижение ошибок:** Валидация на каждом шаге
* **Визуальная отладка:** Preview данных до запуска
* **Архитектурная чистота:** Только TDTP как формат передачи

---

## 2. КЛЮЧЕВЫЕ РЕШЕНИЯ

### 2.1 Платформа
- **Приоритет:** Windows 10/11
- **Технологии:** Wails v2 (Go + HTML/CSS/JS)
- Linux/macOS: опционально (Phase 5)

### 2.2 Visual Designer
- **SVG canvas** для drag-n-drop таблиц
- **Кликабельные JOIN-линии** с popup свойствами
- **Live preview** с debounce 2 сек

### 2.3 Запуск Pipeline
- **X-Ray:** генерирует YAML конфиг + preview
- **Execution:** через существующий `tdtpcli --pipeline config.yaml`
- **Цель:** найти косяки в фреймворке через реальное использование

### 2.4 Режимы работы

#### Mock Mode (эксперименты)
- JSON источники вместо реальных БД
- ⚠️ Warnings, но можно "творить дичь"
- Для обучения и прототипирования

#### Production Mode (боевые)
- Только реальные sources (DB, TDTP, RabbitMQ)
- ❌ Строгая валидация каждого шага
- Нельзя перейти дальше без Test Connection

### 2.5 ТОП-3 Use Cases (шаблоны)

1. **SQLite Export with Filtering**
   ```yaml
   sources:
     - name: users
       type: sqlite
       dsn: "users.db"
       query: "SELECT id, name, email FROM users WHERE active = 1"
   output:
     type: tdtp_file
     file: "users_export.xml"
   ```

2. **MSSQL Multi-Table JOIN**
   ```yaml
   sources:
     - name: orders
       type: mssql
       query: "SELECT * FROM orders"
     - name: products
       type: mssql
       query: "SELECT * FROM products"
   transform:
     sql: |
       SELECT o.*, p.name, p.price
       FROM orders o
       JOIN products p ON o.product_id = p.id
   output:
     type: tdtp_broker
     rabbitmq:
       queue: "orders-enriched"
   ```

3. **RabbitMQ → DB Enrichment → RabbitMQ**
   ```yaml
   sources:
     - name: incoming_orders
       type: tdtp
       transport: rabbitmq
       queue: "raw-orders"
     - name: catalog
       type: mssql
       query: "SELECT * FROM product_catalog"
   transform:
     sql: "SELECT o.*, c.price FROM incoming_orders o JOIN catalog c ON o.product_id = c.id"
   output:
     type: tdtp_broker
     rabbitmq:
       queue: "enriched-orders"
   ```

---

## 3. АРХИТЕКТУРА

### 3.1 Структура проекта
```
cmd/tdtp-xray/
├── main.go                 # Wails entry point
├── app.go                  # Go API
├── services/
│   ├── source_service.go
│   ├── connection_service.go
│   ├── metadata_service.go
│   ├── canvas_service.go   # SVG визуализация
│   ├── yaml_generator.go
│   ├── validator.go
│   └── preview_service.go
└── frontend/
    ├── src/
    │   ├── index.html
    │   ├── wizard/         # 7 шагов
    │   ├── components/
    │   ├── styles/
    │   └── scripts/
    └── wails.json
```

### 3.2 Tech Stack
- **Backend:** Go 1.21+, Wails v2
- **Frontend:** HTML5, CSS3, Vanilla JS
- **Canvas:** SVG (не Canvas API)
- **Style:** Windows Forms inspired

---

## 4. WORKFLOW (7 STEPS)

### Step 1: Project Info
Метаданные pipeline (name, version, description)

### Step 2: Configure Sources
Добавление источников:
- PostgreSQL, MSSQL, MySQL, SQLite
- TDTP from File/RabbitMQ/MSMQ/Kafka

**Mock Mode:**
```json
{
  "type": "mock",
  "schema": [
    {"name": "id", "type": "int"},
    {"name": "name", "type": "string"}
  ],
  "data": [
    [1, "Alice"],
    [2, "Bob"]
  ]
}
```

### Step 3: Visual Designer (SVG Canvas)
- Drag таблицы на canvas
- Рисовать JOIN линии (SVG `<line>`)
- Клик на линию → popup свойств
- Live preview справа

### Step 4: Transform SQL
SQL трансформация (агрегация, GROUP BY)

### Step 5: Configure Output
- TDTP to File
- TDTP to RabbitMQ
- Direct to Database
- XLSX (emergency)

### Step 6: Performance & Settings
- Timeout, batch size
- Error handling
- Data processors (mask, validate)

### Step 7: Review & Save
- YAML preview
- Quick test (LIMIT 10)
- Save config
- **Run via tdtpcli**

---

## 5. ВАЛИДАЦИЯ

### Mock Mode
```
⚠️ Warning: Using mock source "fake_users"
   This will not work in production.
   [Continue Anyway] [Switch to Real Source]
```

### Production Mode
```
❌ Error: Cannot proceed to Step 4
   Reason: Source "orders" not tested
   Action: Click [Test Connection] in Step 2
```

---

## 6. ИНТЕГРАЦИЯ С TDTPCLI

### Генерация конфига
```bash
# X-Ray сохраняет
configs/my_pipeline.yaml
```

### Запуск
```bash
# Через существующий CLI
tdtpcli --pipeline configs/my_pipeline.yaml
```

### Preview в X-Ray
```go
// Запускаем tdtpcli с флагом --preview
cmd := exec.Command("tdtpcli", "--pipeline", configPath, "--preview", "--limit", "10")
output, err := cmd.CombinedOutput()
```

---

## 7. DEVELOPMENT PHASES

### Phase 1: Foundation ✅
- Структура проекта
- Wails setup
- Wizard navigation

### Phase 2: Core Services
- Connection testing
- Metadata retrieval
- YAML generation

### Phase 3: Visual Designer (SVG)
- Canvas engine
- JOIN visualization
- Field filters

### Phase 4: Preview & Testing
- Live preview
- LIMIT detection
- Mock sources

### Phase 5: Polish
- Templates
- Error handling
- Windows packaging

---

## 8. ТЕХНИЧЕСКИЕ ДЕТАЛИ

### 8.1 SVG Canvas Example
```html
<svg id="canvas" width="800" height="600">
  <!-- Таблица orders -->
  <g id="table-orders" transform="translate(50, 50)">
    <rect width="150" height="200" fill="#f0f0f0" stroke="#333"/>
    <text x="10" y="20">orders (o)</text>
    <line x1="0" y1="30" x2="150" y2="30" stroke="#333"/>
    <text x="10" y="50">👁 order_id</text>
    <text x="10" y="70">👁 product_id</text>
  </g>

  <!-- JOIN линия (кликабельная) -->
  <line id="join-1"
        x1="200" y1="120"
        x2="350" y2="120"
        stroke="#007acc" stroke-width="3"
        marker-end="url(#arrow)"
        onclick="showJoinProperties('join-1')"/>
</svg>
```

### 8.2 Mock Source JSON
```json
{
  "name": "mock_orders",
  "type": "mock",
  "schema": {
    "fields": [
      {"name": "order_id", "type": "int", "key": true},
      {"name": "user_id", "type": "int"},
      {"name": "total", "type": "decimal"}
    ]
  },
  "data": [
    {"order_id": 1, "user_id": 101, "total": 150.00},
    {"order_id": 2, "user_id": 102, "total": 89.99}
  ]
}
```

---

## ROADMAP

- [x] Техническое задание
- [ ] Phase 1: Foundation (в процессе)
- [ ] Phase 2: Core Services
- [ ] Phase 3: Visual Designer
- [ ] Phase 4: Preview
- [ ] Phase 5: Release

**Следующий шаг:** Создать базовый Wails проект
