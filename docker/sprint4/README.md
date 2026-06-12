# Sprint 4 — тестове середовище для `--map` (ZTR-Live → EDM)

Перевірка крос-системної інтеграції: вивантаження співробітника з ZTR-Live (MSSQL)
та upsert у тестову таблицю EDM (PostgreSQL) через нову команду `tdtpcli --map`.

## Варіант A — окремий ізольований стек (чисте середовище)

```bash
docker compose -f docker/sprint4/docker-compose.yml up -d
```

Підіймає PostgreSQL 16 (`edm_test` / `edm` / `edm123`, порт 5432) + Redis 7,
автоматично застосовує `init.sql` (схема `edm`, таблиця `edm_employees`).
DSN у `mappings/edm_mapping.yaml` треба узгодити з цими credentials.

## Варіант B — переви­користати наявний контейнер (як у нашому тесті)

Якщо порт 5432 вже зайнятий робочим контейнером `tdtp-postgres`
(`tdtp` / `tdtp` / `tdtp`), не піднімайте четвертий — застосуйте DDL у нього:

```bash
docker exec -i tdtp-postgres psql -U tdtp -d tdtp < docker/sprint4/init.sql
```

`mappings/edm_mapping.yaml` у репозиторії налаштований саме на цей варіант
(`dbname=tdtp user=tdtp password=tdtp`).

## Прогін

```bash
# одиничний співробітник (емуляція кнопки UI)
python scripts/emulate_button.py 1072

# або вручну по кроках
tdtpcli --pipeline pipelines/export-single-employee.yaml @emp_code=1072
tdtpcli --map mappings/edm_mapping.yaml --input out/emp_1072.tdtp.xml

# dry-run (без запису)
tdtpcli --map mappings/edm_mapping.yaml --input out/emp_1072.tdtp.xml --dry-run
```

## Перевірка результату

```bash
docker exec -i tdtp-postgres psql -U tdtp -d tdtp \
  -c "SELECT ext_id, display_name, department, contract_type FROM edm.edm_employees;"
```

## Навантажувальний тест

```bash
python scripts/load_test.py            # реальні дані: 100/500/1000/1478 осіб
python scripts/load_test.py stress     # синтетика: 5k/10k/50k/100k рядків
```

Реальний режим: export з ZTR-Live + `--map`, фази export / insert / upsert.
Stress-режим: ампліфікує реальний bulk-export (з кирилицею) до великих обсягів
з унікальними `ext_id` і міряє лише write-шлях у PostgreSQL.

Орієнтовні результати (локальний Docker PostgreSQL 16):
- реальні 1478 осіб: повний sync < 0.4с
- синтетика: лінійно до 100k рядків, стабільно ~7 500 рядків/с
- стеля throughput — `INSERT ... ON CONFLICT` батчами по 1000 (COPY не вміє upsert)

Використовує `mappings/edm_mapping_load.yaml` (`min_interval: 0s`).

⚠️ Унікальний ключ `ext_id` = `employee_code` (особа). У ZTR-Live на одну особу
може бути кілька записів Employment History (основна + суміщення), тому
bulk-export бере лише `Employment Type = 1` і дедупить `GROUP BY employee_code` —
інакше `ON CONFLICT DO UPDATE` падає на дублях у межах одного батчу.

## Loop Guard

`--map` веде журнал у `~/.tdtp/mapping_log.json`. Повторний запуск того самого
маппінгу швидше за `loop_guard.min_interval` блокується (захист від рекурсивних
обмінів A→B→A). Скинути журнал для тестів: видалити цей файл.
