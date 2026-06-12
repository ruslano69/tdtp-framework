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

## Loop Guard

`--map` веде журнал у `~/.tdtp/mapping_log.json`. Повторний запуск того самого
маппінгу швидше за `loop_guard.min_interval` блокується (захист від рекурсивних
обмінів A→B→A). Скинути журнал для тестів: видалити цей файл.
