# TDTP X-Ray - Visual ETL Pipeline Constructor

🔬 **Desktop application for visual ETL pipeline design** based on TDTP Framework.

## Overview

TDTP X-Ray replaces manual YAML/Go coding with a visual wizard:
- 🧙‍♂️ **7-step wizard** - From sources to output
- 🎨 **SVG canvas designer** - Visual JOINs and filtering
- 👁️ **Live preview** - See data before running
- ⚡ **Quick generation** - 10 minutes instead of 2-3 hours

## Quick Start

### Prerequisites
- Go 1.21+
- [Wails v2](https://wails.io/docs/gettingstarted/installation)
- Windows 10/11 (primary target)

### Installation

```bash
# Install Wails CLI (if not installed)
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Navigate to project
cd tdtp-framework/cmd/tdtp-xray

# Run in development mode
wails dev

# Build for production
wails build
```

## Development Status

### ✅ Phase 1: Foundation (COMPLETE)
- [x] Project structure
- [x] Wails setup
- [x] Go API (app.go)
- [x] Wizard navigation (7 steps)
- [x] Step 1: Project Info (fully functional)
- [x] Mock/Production mode switching
- [x] Windows Forms inspired UI

### 🚧 Phase 2: Core Services (IN PROGRESS)
- [ ] Connection testing (Postgres, MSSQL, MySQL, SQLite)
- [ ] Metadata service (tables, views, schemas)
- [ ] Step 2: Sources UI
- [ ] Preview service with LIMIT detection

### 📅 Phase 3: Visual Designer (PLANNED)
- [ ] SVG canvas for table drag-n-drop
- [ ] Visual JOIN drawing
- [ ] Field filtering UI
- [ ] Live preview panel

### 📅 Phase 4-5: Polish & Release
- [ ] Templates (common use cases)
- [ ] Error handling
- [ ] Windows installer (.exe)

## Architecture

```
cmd/tdtp-xray/
├── main.go              # Wails entry point
├── app.go               # Go API (state + methods)
├── services/            # Business logic services
│   ├── connection_service.go
│   ├── metadata_service.go
│   ├── yaml_generator.go
│   └── preview_service.go
└── frontend/
    ├── src/             # Source files
    │   ├── index.html
    │   ├── styles/
    │   └── scripts/
    └── dist/            # Built files (Wails serves from here)
```

## Key Features

### Mock vs Production Modes

**Mock Mode** (🧪 experimental):
- JSON mock sources
- ⚠️ Warnings only
- For learning/prototyping

**Production Mode** (🏭 strict):
- Real DB/TDTP/RabbitMQ only
- ❌ Validation blocks invalid steps
- Test connection required

### Integration with tdtpcli

X-Ray generates YAML configs, then:
```bash
# X-Ray saves config
configs/my_pipeline.yaml

# Execute via existing CLI
tdtpcli --pipeline configs/my_pipeline.yaml

# Preview in X-Ray (uses tdtpcli)
tdtpcli --pipeline temp.yaml --preview --limit 10
```

## Top 3 Use Cases

### 1. SQLite Export with Filtering
```yaml
sources:
  - name: users
    type: sqlite
    query: "SELECT id, name, email FROM users WHERE active = 1"
output:
  type: tdtp_file
  file: "users.xml"
```

### 2. MSSQL Multi-Table JOIN → RabbitMQ
```yaml
sources:
  - name: orders
    type: mssql
  - name: products
    type: mssql
transform:
  sql: "SELECT o.*, p.name FROM orders o JOIN products p ON ..."
output:
  type: tdtp_broker
  rabbitmq:
    queue: "enriched-orders"
```

### 3. RabbitMQ → DB Enrichment → RabbitMQ
```yaml
sources:
  - name: raw_orders
    type: tdtp
    transport: rabbitmq
  - name: catalog
    type: mssql
transform:
  sql: "SELECT o.*, c.price FROM raw_orders o JOIN catalog c ..."
output:
  type: tdtp_broker
```

## Tech Stack

- **Backend:** Go 1.21+, Wails v2
- **Frontend:** HTML5, CSS3, Vanilla JavaScript (no frameworks)
- **Canvas:** SVG (for visual designer)
- **Style:** Windows Forms inspired (classic desktop look)
- **Integration:** tdtpcli (for execution + preview)

## Documentation

- [Technical Specification](../../docs/tdtp-xray/TECHNICAL_SPEC.md)
- [Development Log](../../docs/tdtp-xray/DEVELOPMENT_LOG.md)
- [TDTP Framework Docs](../../README.md)

## Contributing

See [DEVELOPMENT_LOG.md](../../docs/tdtp-xray/DEVELOPMENT_LOG.md) for development roadmap and current tasks.

## License

Same as TDTP Framework parent project.

---

**Status:** 🚧 Phase 1 Complete | Phase 2 In Progress
**Platform:** Windows 10/11 (Linux optional)
**Version:** 1.0.0-alpha
