# Example 09 — S3 Pipeline Chain: Extract → Split by Region

**Сложность:** ⭐ Начальный
**Время:** 3 минуты

Цепочка из двух пайплайнов и оркестрирующего скрипта:

```
[PostgreSQL]
     │  pipeline_1_extract.yaml
     ▼
[S3: pipeline/orders_full.tdtp.xml]   ← полный набор, все регионы
     │
     │  run_chain.sh — цикл по регионам
     │
     ├── pipeline 2 (NORTH) → S3: by_region/NORTH/orders.tdtp.xml
     ├── pipeline 2 (SOUTH) → S3: by_region/SOUTH/orders.tdtp.xml
     ├── pipeline 2 (EAST)  → S3: by_region/EAST/orders.tdtp.xml
     └── pipeline 2 (WEST)  → S3: by_region/WEST/orders.tdtp.xml
```

## Файлы

| Файл | Назначение |
|------|------------|
| `pipeline_1_extract.yaml` | PostgreSQL → S3 (полный экспорт) |
| `pipeline_2_split_template.yaml` | Шаблон: S3-in → фильтр по `__REGION__` → S3-out |
| `run_chain.sh` | Оркестратор: запускает pipeline 1, получает список регионов, запускает pipeline 2 для каждого |

## Быстрый старт

### 1. Настройте S3-совместимое хранилище

Пример с SeaweedFS (localhost:8333):
```bash
# Запустить SeaweedFS (разработка)
weed server -s3 -s3.port=8333

# Создать бакет
s3cmd mb s3://my-bucket --host=localhost:8333 --no-ssl --host-bucket=''
```

Для MinIO или AWS S3 — измените `endpoint` в YAML-файлах.

### 2. Отредактируйте конфигурацию S3

В обоих YAML-файлах найдите блок `s3:` и поставьте свои значения:
```yaml
s3:
  endpoint: "http://localhost:8333"   # пусто для AWS S3
  bucket: "my-bucket"
  access_key: "my_access_key"
  secret_key: "my_secret_key"
  disable_ssl: true                   # false для HTTPS
```

### 3. Запустите цепочку

```bash
cd examples/09-s3-pipeline-chain
bash run_chain.sh
```

Или по шагам вручную:

```bash
# Шаг 1 — полный экспорт в S3
tdtpcli --pipeline pipeline_1_extract.yaml

# Шаг 2 — разделить по каждому региону
for REGION in NORTH SOUTH EAST WEST; do
  sed "s/__REGION__/${REGION}/g" pipeline_2_split_template.yaml \
    > /tmp/pipeline_2_${REGION}.yaml
  tdtpcli --pipeline /tmp/pipeline_2_${REGION}.yaml
done
```

## Как адаптировать

**Разделить по другому полю** (например, `status` вместо `region`):
В шаблоне замените `region` на `status` и измените список значений в `run_chain.sh`:
```bash
REGIONS="pending
processing
shipped
delivered
cancelled"
```

**Добавить сжатие:**
`compression: true` уже включено по умолчанию в обоих пайплайнах.

**Добавить маскирование PII перед записью в S3:**
В `pipeline_1_extract.yaml` добавьте секцию `processors`:
```yaml
processors:
  pre_export:
    - type: mask
      fields: [email]
```

## Ожидаемая структура S3 после запуска

```
my-bucket/
└── pipeline/
    ├── orders_full.tdtp.xml            ← pipeline 1
    └── by_region/
        ├── NORTH/
        │   └── orders.tdtp.xml         ← pipeline 2 × NORTH
        ├── SOUTH/
        │   └── orders.tdtp.xml
        ├── EAST/
        │   └── orders.tdtp.xml
        └── WEST/
            └── orders.tdtp.xml
```

## Требования

- `tdtpcli` в PATH (или `TDTP_CLI=/path/to/tdtpcli bash run_chain.sh`)
- PostgreSQL с тестовой БД (`python3 scripts/create_postgres_test_db.py`)
- S3-совместимое хранилище (SeaweedFS, MinIO, Ceph или AWS S3)
- `psql` для автоматического определения регионов (опционально — можно задать список вручную)
