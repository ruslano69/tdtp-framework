# Compact v1.3.1 Integration Tests

Ручные интеграционные тесты для TDTP v1.3.1 compact-формата.

## Что тестируется

| # | Сценарий |
|---|----------|
| 1 | Обычный экспорт (baseline v1.0, compact отсутствует) |
| 2 | Экспорт VIEW с `_prefix` колонками → auto-detect fixed fields |
| 3 | Структура compact-строк (12 из 15 строк с пропусками `\|\|`) |
| 4 | Экспорт с явным `--fixed-fields` |
| 5 | Импорт compact-файла в новую таблицу (auto-expand) |
| 6 | Конвертация `--to-compact` (v1.x → v1.3.1) |
| 7 | Импорт converted-файла в новую таблицу |
| 8 | Сравнение размеров файлов |

## Использование

```bash
# 1. Создать тестовую базу
python3 setup_db.py

# 2. Собрать бинарь
go build -o /tmp/tdtpcli ../../cmd/tdtpcli/

# 3. Запустить тесты
bash run_tests.sh
```

## Структура БД

- `departments` — 3 отдела (dept_id, dept_name, location)
- `employees` — 15 сотрудников (emp_id, dept_id, full_name, salary, hire_date)
- `dept_employees_report` — VIEW: JOIN с `_dept_id`/`_dept_name`/`_location` prefix → compact auto-detect
