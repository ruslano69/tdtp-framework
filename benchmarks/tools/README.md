# Benchmarks Tools

Standalone bench tools and scripts.

## Файлы

| Файл | Описание |
|------|----------|
| `cmd/bench_raw/main.go` | Raw XML generation benchmark |
| `cmd/bench_raw/bench_components_test.go` | Component-level benchmarks |
| `scripts/export_benchmark_py.py` | Python CLI export benchmark |
| `scripts/create_benchmark_db.py` | Создание тестовой БД для bench |

## Запуск

```bash
# Go tool
go run ./cmd/bench_raw/

# Python benchmark
python scripts/export_benchmark_py.py

# Create benchmark DB
python scripts/create_benchmark_db.py
```