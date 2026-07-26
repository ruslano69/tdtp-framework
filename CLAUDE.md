# Claude Code Notes — tdtp-framework

## Поддержка заметок о задачах (ВАЖНО!)

**Перед началом работы проверяй `TODO_NEXT.md` и заметки в этом файле на актуальность.**

Если задача, описанная в заметке, **уже реализована** — заметку нужно удалить или
заменить актуальной, не дожидаясь напоминания. Признак выполненной задачи: код,
который заметка предлагает написать, уже существует в репозитории.

Как проверять `TODO_NEXT.md`:
1. Прочитать заголовок и план.
2. Грепнуть ключевые артефакты плана (пакеты, файлы, функции, зависимости).
3. Если всё на месте и компилируется — план выполнен → удалить/заменить.

Пример: S3-план (`pkg/storage`, AWS SDK) был полностью отгружен, но `TODO_NEXT.md`
ещё месяц указывал на него как на «следующую» задачу. Не повторять — чистить вовремя.

## Go module downloads (ВАЖНО!)

Если `go build` не может скачать зависимости (blocked proxy, missing zip, etc.) — **СРАЗУ** используй:

```bash
GOPROXY=https://goproxy.io GONOSUMDB='*' go build ...
```

Или выставь переменные в сессии:
```bash
export GOPROXY=https://goproxy.io
export GONOSUMDB='*'
```

`proxy.golang.org` → редиректит на `storage.googleapis.com` → заблокирован (`no_proxy=*.googleapis.com`).
`goproxy.io` отдаёт пакеты напрямую без редиректов — **работает всегда**.

---

## Test databases

### Python-скрипты для БД уже есть в `/scripts/`!

- `scripts/create_postgres_test_db.py` — PostgreSQL (users, orders, products, activity_logs, 100/200/50 rows)
- `scripts/create_test_db.py` — SQLite
- `scripts/generate_test_db.py` — SQLite benchmark
- `scripts/create_benchmark_db.py` — SQLite benchmark (large)
- `tests/compact_v131/setup_db.py` — SQLite для compact-тестов

**Не создавать новые скрипты — использовать существующие!**

Существующий PostgreSQL user/password: `tdtp_user` / `tdtp_dev_pass_2025` (из `create_postgres_test_db.py`).

Config для тестов:
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  user: tdtp_user
  password: tdtp_dev_pass_2025
  database: tdtp_test
  sslmode: disable
```

### Запуск PostgreSQL
```bash
pg_ctlcluster 16 main start
pg_isready
```

---

## Сжатие (zstd + kanzi)

Бенчмарк на 100k строк SQLite (benchmark_100k.db, синтетические данные Users):

| Режим              | Время    | Размер | Коэф. |
|--------------------|----------|--------|-------|
| Без сжатия         | 673 мс   | 9.9 MB | —     |
| zstd level 3       | 751 мс   | 2.9 MB | 3.4×  |
| zstd level 19      | 2363 мс  | 2.4 MB | 4.1×  |
| kanzi level 6      | 1279 мс  | 1.5 MB | 6.6×  |
| kanzi level 7      | 1449 мс  | 1.4 MB | 7.1×  |

**Вывод по алгоритмам:**
- `zstd level 3` — дефолт для потоков реального времени: почти бесплатен, 3× экономия
- `kanzi level 6` — оптимум для архивов и бэкапов: **в 2 раза плотнее zstd3**, быстрее zstd19
- `kanzi level 7` — максимум плотности, +170 мс к level 6, выгоден только при медленном канале

На реальных данных с разнородным текстом (кадровые приказы, нарративные описания) kanzi
показывает x10-12 против исходного размера — BWT разворачивается на полную мощность.
На синтетических коротких строках — 6-7×, но это всё равно **на 30-50% плотнее zstd**.

`compress: true` и `compress_level: 3` — дефолт в шаблоне конфига (`CreateSampleConfig`). Не менять.
Для архивных задач: `--compress-algo kanzi --compress-level 6 --hash`.

---

## Build tags

- `nokafka` — исключает kafka-go и его зависимости (для офлайн-сборок / без Kafka)
- `nosqlite` — исключает modernc.org/sqlite (для сборок без SQLite)

Быстрая сборка без Kafka:
```bash
GOPROXY=https://goproxy.io GONOSUMDB='*' go build -tags nokafka -o H:\Ruslan\Code\Go\TDTP\tdtp-main-clean\tdtpcli.exe ./cmd/tdtpcli/
```

---

## Dev branch

Feature branches: `claude/wonderful-fermi-4qXUT`

---

## Размеры пакета: что ограничено, а что нет (ВАЖНО!)

Ограничена **только схема**. Секция Data не ограничена ничем — сколько строк
влезло в бюджет, столько и пишется, остальное уходит в следующую часть.

| Величина | К чему относится | Где |
|---|---|---|
| `DefaultMaxMessageSize` = 3 800 000 | бюджет части → **~1.9 МБ реального XML** | `generator.go` |
| `packetOverheadSize` = 5000 | пол резерва под конверт | `generator.go` |
| `MaxSchemaBytes` = 200 КБ | Schema **на записи** — отказ | `generator.go` |
| `MaxSchemaBytesRead` = 1 МБ | Schema **на чтении** — отказ; между 200 КБ и 1 МБ читается с `WARNING` | `parser.go` |
| `maxBufferedParse` = 64 МБ | порог выбора пути разбора, **не** предел размера | `parser.go` |

**Бюджет считается в единицах, вдвое больших байт UTF-8.** `estimateRowSize`
делает `len(value) * 2`, а `len()` в Go — это байты, не символы. Поэтому
соотношение «бюджет → реальный XML = /2» держится на любом алфавите; комментарий
про UTF-16 в коде вводит в заблуждение. Резерв 2× нужен под конверт, а не под
перекодировку.

**Пороги записи и чтения намеренно разные.** Предел записи — правило формата,
касается только новых пакетов. Тот же предел на чтении отверг бы уже
существующие данные: всё, записанное раньше, ничем не ограничено. Верхний порог
чтения — защита от патологического входа, а не правило формата.

**Размер пакета задаётся пользователем и сверху не ограничен:**
`--packet-size` у экспорта в брокер (`broker.go`, умножает на 2) и `packet_kb`
в pipeline YAML (`etl/exporter.go`, **не** умножает — пользователь получает
вдвое меньше запрошенного; несогласованность не исправлена).

---

## Парсер: гибридный разбор (ВАЖНО!)

Writer давно снял reflection с горячего участка (`xmlwriter.go`): Header и Schema
идут через `xml.Marshal` (они ~200 байт), а тысячи `<R>` пишутся вручную.
Парсер теперь зеркалит это (`parser_fast.go`).

`tryFastParse` вырезает тело Data и собирает «скелет» — тот же пакет с пустой
Data-секцией, ~500 байт независимо от числа строк. Скелет отдаётся
`xml.Unmarshal`, который разбирает корневые атрибуты, Header, Query,
QueryContext, Schema, атрибуты Data и AlarmDetails. Вручную сканируются только
строки.

**Откат обязателен и не декоративен.** Любая неожиданная форма → `ok=false` →
обычный `xml.Unmarshal`, включая выдачу ошибки на битом XML. Откат срабатывает
на: CDATA, комментарии, сырой `<` внутри строки, `<R/>`, `<Data/>`,
нераспознанную сущность, второй корневой `<Data>`, несобравшийся скелет.

`ParseBytes`: 34.5 мс → 3.57 мс на 10k×10 (**9.7×**), 686 MB/s.
`Parse`/`ParseFile` читают вход целиком до `maxBufferedParse` и идут тем же
путём; выше порога — прежний потоковый `xml.Decoder` (перематывать reader не
нужно, прочитанное подставляется через `io.MultiReader`).

**Не мерить размер схемы через `xml.Marshal` в парсере.** Marshal схемы на
10 полей стоит ~13.7 мкс против ~51 мкс на разбор небольшого пакета целиком —
проверка съела бы четверть разбора. Используется `schemaSpanSize`: span
`<Schema>...</Schema>` находится двумя `bytes.Index` прямо в исходных байтах и
даёт то же число, что меряет запись (тест это фиксирует).

**Асимметрия, закреплённая тестом:** `Parse`/`ParseFile` разворачивают
compact-формат, `ParseBytes` — намеренно нет.

---

## Разбиение на части и словарь (ВАЖНО!)

`Schema` копируется в **каждую** часть (для самодостаточности файлового
экспорта), а с v1.4 внутри Schema лежит Dictionary. Отсюда два неочевидных
правила в `rowBudget`, оба закреплены тестами:

1. **Резерв под конверт ограничен половиной бюджета.** Честное вычитание
   крупного конверта делает хуже: каждая новая часть добавляет ещё одну копию
   Schema, поэтому дробление не приближает части к бюджету, а отдаляет.
   Замеряно: словарь на 400 записей при бюджете 200 КБ дал 1 часть → **4000**
   (по строке в каждой, каждая тащит 431 КБ словаря).
2. **Если Schema не влезает в бюджет — не дробить вообще.** Решение принимается
   по `serializedSchemaSize` (без пола), а не по `measureEnvelopeSize` (с полом):
   иначе консервативный пол 5000 запрещает дробление при маленьком бюджете и
   крошечной схеме.

Для схемы без словаря `measureEnvelopeSize` упирается в тот же пол 5000, то есть
границы частей **не изменились** (тест `TestPartitionRows_UnchangedWithoutDictionary`
воспроизводит старую формулу и требует совпадения).

**Автопостроения словаря в фреймворке нет.** Есть только `ValidateDictionary`,
`ExpandDictionary`, `ContractDictionary`. Реально словарь заполняется в двух
местах: одна служебная запись `@MRC` с адресом Mercury (`export.go`) и
статический словарь SVG на 5 записей. Крупный словарь может прийти только от
внешнего продюсера через библиотечный API. Когда построение появится — оно
должно укладываться в `MaxSchemaBytes`.

---

## SeaweedFS S3 (локальное тестирование)

### Бинарник
```
/tmp/weed   (version 30GB 3.80, linux amd64)
```

### ВАЖНО: `-ip` не работает в `weed server` — запускать компоненты отдельно!

`weed server -ip=127.0.0.1` **игнорирует флаг** и всё равно использует 192.0.2.2 (внешний IP).
Envoy-прокси sandbox блокирует gRPC между компонентами через внешний IP.
**Решение** — запускать каждый компонент отдельно с явным `-ip=127.0.0.1`:

```bash
# 1. Master (порт 9333)
/tmp/weed master -ip=127.0.0.1 -defaultReplication=000 -volumeSizeLimitMB=100 -port=9333 &
sleep 18  # ждём выборов лидера (~15с)

# 2. Volume server (порт 8080)
/tmp/weed volume -ip=127.0.0.1 -dir=/tmp/seaweedfs-data -mserver=127.0.0.1:9333 -port=8080 &
sleep 2

# 3. Filer (порт 8888) — создаёт filerldb2/ в CWD, добавлен в .gitignore
/tmp/weed filer -ip=127.0.0.1 -master=127.0.0.1:9333 -port=8888 &
sleep 3

# 4. S3 gateway (порт 8333) — флаг -ip не поддерживается, используем -ip.bind
/tmp/weed s3 -ip.bind=127.0.0.1 -filer=127.0.0.1:8888 -port=8333 &
sleep 2

# Проверка
curl -s http://127.0.0.1:9333/cluster/status   # {"IsLeader":true,...}
curl -s http://127.0.0.1:8333/                 # <ListAllMyBucketsResult>...
```

### Существующий бакет
```
tdtp-test   — уже содержит volume, доступен для записи
```
Бакет `tdtp-new-bucket` создан, но без выделенного volume (записи падают с 500).
**Использовать `tdtp-test`** для всех S3-тестов.

### Credentials
Weed в dev-режиме принимает любые ключи:
```
access_key: any
secret_key: any
```

### ВАЖНО: `curl -u` НЕ работает для проверки S3 авторизации!

`curl -u "tdtp_access:tdtp_secret" http://127.0.0.1:8333/` всегда возвращает `AccessDenied` —
потому что curl отправляет **HTTP Basic Auth**, а S3 требует **AWS Signature V4**.

**Проверять доступ только через boto3 или tdtpcli:**
```python
import boto3, botocore.config
s3 = boto3.client('s3', endpoint_url='http://127.0.0.1:8333',
    aws_access_key_id='tdtp_access', aws_secret_access_key='tdtp_secret',
    config=botocore.config.Config(signature_version='s3v4'), region_name='us-east-1')
print([b['Name'] for b in s3.list_buckets()['Buckets']])
```

### S3 для travel-agency (H:\Ruslan\Code\Go\TDTP\tdtp-framework\weed)

Бинарник, данные и конфиги лежат в `H:\Ruslan\Code\Go\TDTP\tdtp-framework\weed\`:
```
weed.exe          — SeaweedFS 30GB 4.17 (Windows)
s3.json           — credentials: tdtp_access / tdtp_secret
data/             — volume данные и filerldb2/
config.yaml       — tdtpcli config для MSSQL + S3 (endpoint 8333, bucket tdtp-exports)
```

Запуск (компонентами отдельно, из папки weed/):
```powershell
cd H:\Ruslan\Code\Go\TDTP\tdtp-framework\weed

.\weed.exe master -ip=127.0.0.1 -defaultReplication=000 -volumeSizeLimitMB=30000 -port=9333
# ждать ~18с до выборов лидера

.\weed.exe volume -ip=127.0.0.1 -dir=./data -mserver=127.0.0.1:9333 -port=8080
.\weed.exe filer  -ip=127.0.0.1 -master=127.0.0.1:9333 -port=8888
.\weed.exe s3     -ip.bind=127.0.0.1 -filer=127.0.0.1:8888 -port=8333 -config=./s3.json
```

**Бакеты:** `travel-agency`, `tdtp-test`, `tdtp-exports`

### Config для тестов
```yaml
storage:
  type: s3
  s3:
    endpoint: "http://127.0.0.1:8333"
    region: "us-east-1"
    bucket: "tdtp-test"
    access_key: "any"
    secret_key: "any"
    path_style: true
    disable_ssl: true
```

### Проверка --test с S3
```bash
H:\Ruslan\Code\Go\TDTP\tdtp-main-clean\tdtpcli.exe --config /tmp/test_s3_cfg.yaml \
  --export users --output "s3://tdtp-test/ci/users.tdtp.xml" --compress --hash

H:\Ruslan\Code\Go\TDTP\tdtp-main-clean\tdtpcli.exe --config /tmp/test_s3_cfg.yaml \
  --test "s3://tdtp-test/ci/users.tdtp.xml"
# ✓ algo=zstd, 10 rows, decompressed 0s, checksum OK
```

### Лог-файлы
```
/tmp/seaweed.log        — предыдущая сессия (данные с 17 марта 2026)
/tmp/seaweedfs-data/    — volume данные (8.dat, 8.idx, 8.vif)
filerldb2/              — LevelDB filer (в .gitignore)
```

