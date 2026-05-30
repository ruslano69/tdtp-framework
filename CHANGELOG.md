# Changelog

All notable changes to tdtp-framework are documented in this file.

## [1.9.7] — 2026-05-30

Модернизация Python-библиотеки: facade-API, CLI-parity in-process и Arrow-мост (read + write).

### Added

- **Arrow columnar bridge** (`exports_d_arrow.go`, `exports_j_columnar.go`, `arrow_ext.py`):
  чтение и запись `pyarrow.Table ↔ TDTP` через типизированные C-буферы и
  векторизованную обработку столбцов. Write-путь (`J_WriteColumnar`) транспонирует
  column-major в row-major внутри Go — **×2.1** к старому `itertuples` на 10k строк.
  API: `Tdtp.read_arrow` / `to_arrow` / `from_arrow` / `write_arrow`.

- **`Tdtp` facade** (`facade.py`): plain-verb API без `J_`-префиксов и ручного
  управления памятью (`read`/`write`/`filter`/`sort`/`merge`/`stamp`/`verify`/…).

- **CLI parity in-process**: `J_Inspect`, `J_Test`, `J_Sort`, `J_Merge`,
  `J_ReadMultipart`, `J_Stamp`, `J_Verify`; `J_ExportAll` расширен compact + compress
  + checksum в одном вызове.

- **Packaging & versioning**: единый источник версии (`pkg/core/version`), `py.typed`
  (PEP 561), стабильные коды ошибок в JSON-envelope, extras `tdtp[arrow]` / `tdtp[pandas]`.
  Lockstep `.so` ↔ пакет: `build-lib` запускает `sync-version` (build-time), импорт
  сверяет `J_GetVersion` с метаданными пакета и предупреждает при рассинхроне (runtime).

- **C# обёртка (`libcs/`) — паритет с новыми экспортами**: в `TdtpWrapper.cs`
  добавлены P/Invoke-объявления и публичные методы `Inspect`, `Test`, `Verify`,
  `Stamp`, `ReadMultipart`, `Sort`, `Merge`, `WriteColumnar`. Версия по-прежнему
  берётся в рантайме через `GetVersion()` — без зашитых констант, всегда в лок-степе
  с ядром. `BUILD.md` дополнен X++-примерами.

### Fixed

- `J_Test` не экспортировался — cgo исключает `*_test.go` (переименован в `exports_j_integrity.go`).
- `J_Inspect` терял флаг compact — `ParseBytes` вместо авто-разворачивающего `ParseFile`.
- `J_Sort` игнорировал направление — `normalizeDirection` теперь возвращает `"ASC"`/`"DESC"`.

### Tests

- `test_arrow.py` (24, read+write roundtrips), `test_facade.py` (13), `test_examples.py`
  (smoke трёх agent recipes), расширен `test_api_j.py`, write-benchmarks в `test_bench.py`.

## [1.9.6] — 2026-05-30

### Added

- **`--to-csv`** (`cmd/tdtpcli/commands/csv.go`): конвертер TDTP → CSV с security gate.

  TDTP остаётся транспортом с полными гарантиями; CSV — адаптер последней мили для
  легаси-систем (1С, SAP, bulk load в БД). Разделитель, кодировка и integrity-проверка
  настраиваются независимо.

  ```bash
  tdtpcli --to-csv report.tdtp.xml -d=';' --cp=1251          # легаси Windows
  tdtpcli --to-csv report.tdtp.xml --bom                      # Excel UTF-8
  tdtpcli --to-csv report.tdtp.xml -d=';' -w 'Balance > 0' -l=100
  ```

  - **Security gate**: `v1.0` — pass-through; `v1.4` — `VerifyAndPrepare` перед записью.
  - **Разделитель** `-d=';'`: работает в PowerShell и bash; RFC 4180 auto-quoting.
  - **Кодировки** `--cp`: `utf8`, `1251`, `866`.
  - **`--bom`**: UTF-8 BOM для Excel.
  - TDTQL-фильтры (`--where`, `--order-by`, `--limit`, `--fields`) работают как для всех команд.
  - **43 интеграционных теста** (`tests/cli/test_csv.py`).

- **TDTQL алиас `-l`** (`cmd/tdtpcli/flags.go`): `-l=10` как сокращение `--limit=10`
  (читается как «lines», аналогично `tail -n`). Алиас `-w` для `--where` сохранён.

- **`--enc` tier — standalone AES-256-GCM encryption** (`cmd/tdtpcli/commands/encrypt.go`):

  Шифрование через xZMercury теперь доступно для всех standalone-команд — не только
  для `--pipeline`. Каждая часть получает собственный UUID; ключ привязывается в
  xZMercury и удаляется после первого чтения получателем (burn-on-read).

  ```bash
  # Producer
  tdtpcli --export payroll --enc --mercury-url http://mercury:3000 --output payroll.tdtp.xml
  # → payroll.tdtp.enc  (binary AES-256-GCM blob)

  # Consumer — import в БД
  tdtpcli --import payroll.tdtp.enc --mercury-url http://mercury:3000

  # Consumer — конвертация
  tdtpcli --to-csv  payroll.tdtp.enc --mercury-url http://mercury:3000
  tdtpcli --to-xlsx payroll.tdtp.enc --mercury-url http://mercury:3000
  tdtpcli --to-html payroll.tdtp.enc --mercury-url http://mercury:3000
  ```

  - **Auto-detect**: расширение `.tdtp.enc` / `.enc` детектируется автоматически во
    всех consumer-командах (`--import`, `--to-csv`, `--to-xlsx`, `--to-html`).
    Передавать дополнительный флаг не требуется.
  - **`encOutputKey`**: выход именуется автоматически — `.tdtp.xml` / `.xml` / `.tdtp`
    → `.tdtp.enc`; уже корректное расширение не меняется.
  - **Burn-on-read**: `POST /api/keys/retrieve` удаляет ключ из Mercury; повторный
    `--import` одного и того же файла провалится с «key not found».
  - **`MERCURY_SERVER_SECRET`**: если задана env-переменная — выполняется HMAC-верификация
    ответа Mercury. Пустое значение → пропускается (dev / internal-only).
  - **stdout guard**: `--enc` с stdout (`--output -`) возвращает ошибку — бинарный блоб
    нельзя пайпить в текстовый поток.
  - **S3 support**: зашифрованный блоб загружается в S3 с метаданными
    `package_uuid`, `protocol: TDTP-ENC 1.0`.
  - Тесты: 10 unit-тестов (`commands/enc_tier_test.go`) — детект расширения,
    round-trip, burn-on-read, все три конвертера с `.tdtp.enc` входом.

- **v1.4 security gate — все пути импорта** (`cmd/tdtpcli/commands/`):

  Единый helper `applyV14SecurityGate` (`commands/security.go`) применяется теперь
  ко всем командам, записывающим данные в БД:

  | Команда | Файл | Поведение при сбое |
  |---------|------|--------------------|
  | `--import` | `import.go` | ошибка, файл не импортируется |
  | `--listen` (Kafka) | `listen.go` | `NackLast(false)` — пакет не возвращается в очередь |
  | `--import-broker` | `broker.go` | ошибка до первой записи |
  | `--import-broker --keep` | `broker.go` | ошибка внутри `importOne` |

  Ранее gate присутствовал только в `--to-csv`; `--to-xlsx` и `--to-html` были
  без защиты. Теперь все конвертеры и все пути импорта защищены одним кодом.

  - `MercuryURL string` добавлен в `ImportOptions`, `ListenConfig`,
    `ImportBrokerOptions`; wired через `main.go`.
  - Политика: `FallbackDegrade` — Mercury недоступен → локальный xxh3; hash
    не зарегистрирован или подделан → `BLOCK`.
  - Тесты: 20 unit-тестов (`commands/v14_security_test.go`) — CSV, XLSX, HTML
    × v1.0 pass-through / v1.4 valid / tampered / Mercury OK / not-registered / tampered.

- **xZMercury dev-конфиги** (`xzmercury/configs/`):

  ```
  xzmercury.dev.yaml      — dev-сервер: порт 3000, key_ttl 15m, rate_limit 0
  ldap-users.dev.json     — mock-пользователи с полными DN-группами
  pipeline-acl.dev.yaml   — ACL для тестовых пайплайнов
  ```

  LDAP mock требует точного совпадения строк — группы указаны как полные DN
  (`cn=tdtp-pipeline-users,ou=groups,dc=corp,dc=local`).

### Fixed

- **`--from-xlsx` пустой MessageID** (`pkg/xlsx/converter.go`): XLSX-конвертер создавал
  пакет вручную (`DataPacket{}`), пропуская генерацию UUID. Исправлено переходом на
  `packet.NewDataPacket()`.

- **`--limit` на MSSQL compat level 80/90/100** (`pkg/adapters/base/sql_adapter.go`):
  `OFFSET/FETCH NEXT` требует SQL Server 2012+. Для `--limit` без `--offset` теперь
  генерируется `SELECT TOP N` (работает с SQL Server 2000+). `OFFSET/FETCH` остаётся
  только при пагинации с `--offset`.

- **`exportToTDTP` и `exportToKafkaSpool` теряли строки** (`pkg/etl/exporter.go`):
  вызов `packet.ParseRows(dataPacket.Data.Rows, ...)` возвращал 0 строк когда данные
  хранились в `rawRows` (fast-path). Исправлено на `dataPacket.GetRows()`.

- **Windows backslash в YAML** (`tests/integration/xzmercury_pipeline_test.go`):
  пути вида `C:\Users\...` парсились как Unicode-эскейпы. Исправлено через
  `filepath.ToSlash()`.

- **`TestEndToEndExportImport`** (`tests/integration/broker_test.go`):
  `createTestTable` была заглушкой → реализована через `database/sql`.

### Performance

- **In-memory фильтр (`pkg/core/tdtql/`)** — два исправления горячего пути:

  | Что | Было | Стало |
  |-----|------|-------|
  | LIKE regexp (10k строк) | 51 ms · 440k allocs | **5.4 ms · 20k allocs** (9.5×) |
  | Поиск поля в схеме | O(fields) на строку | O(1) map, один раз на вызов |

  `comparator.go`: кеш regexp через `sync.Map`. `filter.go`: `map[string]int` и
  `map[string]FieldDef` строятся один раз в `ApplyFilters`, `FieldDef` не аллоцируется
  на каждой строке.

- **Compact encode/decode (`pkg/core/packet/compact.go`)**:

  | Benchmark (10k строк) | Было | Стало |
  |-----------------------|------|-------|
  | Encode `RowsToCompactData` | 2 594 ms · 2 087 KB | **1 274 ms · 807 KB** (2×) |
  | Decode `ExpandCompactRows` | 7 163 ms · 5 755 KB | **5 755 ms · 4 476 KB** (1.24×) |

  Per-row `[]string` заменены на `[]byte buf` с `buf[:0]` — буфер сохраняет ёмкость
  между строками. `strings.Builder.Reset()` не использован (он сбрасывает `buf = nil`).

### Refactoring

- **`packetOverheadSize = 5000`** (`pkg/core/packet/generator.go`, `streaming.go`):
  магическая константа из трёх мест вынесена в `packetOverheadSize`.

### Tests

- `tests/cli/test_csv.py` — 43 интеграционных теста для `--to-csv`.
- `pkg/core/tdtql/filter_bench_test.go` — бенчмарки LIKE и field lookup old/new.
- `pkg/core/packet/compact_bench_test.go` — бенчмарки compact encode/decode old/new.

## [1.9.5] — 2026-05-25

### Added

- **`--to-csv`** (`cmd/tdtpcli/commands/csv.go`): конвертер TDTP → CSV с security gate.

- **TDTQL шортхэнды `-n` и `-w`** (`cmd/tdtpcli/flags.go`):

  ```bash
  -n=10         # alias для --limit=10
  -w 'X > 1'   # alias для --where 'X > 1' (повторяемый, AND-цепочка)
  ```

- **TDTP v1.4 integrity + xzMercury hash notary** (`cmd/tdtpcli/`, `pkg/pipeline/`,
  `xzmercury/`): флаг `--integrity` stampует пакет тремя xxh3_128-хешами
  (Schema / Data / Packet, UUID-соль). `--mercury-url` регистрирует fingerprint
  в xzMercury (`SET NX`). Потребитель верифицирует через `VerifyAndPrepare`.

  ```bash
  tdtpcli --export payroll --integrity --mercury-url http://mercury:3000 --compress
  ```

  Подробнее: [`docs/xZMercury-TDTP-TZ-v1.2.md`](docs/xZMercury-TDTP-TZ-v1.2.md),
  [`xzmercury/README.md`](xzmercury/README.md).

## [1.9.4] — 2026-05-20

### Added
- **TDTP v1.4 Dictionary** (`pkg/core/packet/`): секция `<Dictionary>` в схеме —
  токены `@name` → полные строки (URI namespace, типы домена).
  `ExpandDictionary` / `ContractDictionary`. Обратная совместимость сохранена.
- **`tdtp-svg`** (`cmd/tdtp-svg/`, `pkg/svg/`): SVG ↔ TDTP конвертер.
  Каждый элемент → строка таблицы, дерево через `(id, parent_id, order_idx)`.
  Схема 24 колонки: 6 структурных + `attrs_json` + 17 широких атрибутов.
  Парсер потоковый — O(глубина), не O(размер файла).
  Бенчмарк: 4171 элементов, 580 KB SVG → 87 KB TDTP (kanzi, 8.9×).
- **`--fallback-row-limit N`** (`pkg/adapters/base/export_helper.go`):
  ограничивает `ReadAllRows` при fallback с SQL pushdown. По умолчанию 0.

### Fixed
- **MSSQL full table scan на именах с `$` / пробелами** (`pkg/adapters/base/sql_adapter.go`):
  `AdaptSQL` портил ANSI-quoted имя (`"ZTR$Timesheet Line"` → `"[dbo].[ZTR$Timesheet Line]"`),
  MSSQL отвергал, код падал в `ReadAllRows`. Наблюдалось 17 GB RAM.
  Теперь ANSI-форма заменяется первой. Регрессионный тест добавлен.
- **MSSQL datetime суффикс `Z`** (`pkg/adapters/base/sql_adapter.go`):
  `'2024-08-12T00:00:00Z'` → `'2024-08-12T00:00:00'`.
- **SQL pushdown silent fallback** (`pkg/adapters/base/export_helper.go`):
  молчаливый fallback заменён на `log.Printf WARNING`.

## [1.9.3] — 2026-05-08

### Added

- **PipelineContext в заголовке TDTP-пакета + `--expect-var`** (`pkg/core/packet/types.go`, `pkg/etl/`, `cmd/tdtpcli/`):

  При экспорте через `--pipeline` каждый сгенерированный пакет теперь содержит блок
  `<PipelineContext>` — метаданные о pipeline-источнике, встроенные прямо в XML:

  ```xml
  <PipelineContext>
    <Pipeline name="daily-sync" version="2.1"/>
    <Variables>
      <Var name="dept" value="sales"/>
      <Var name="region" value="EMEA"/>
    </Variables>
  </PipelineContext>
  ```

  В `Variables` попадают **только те переменные, которые фактически используются в конфиге**
  (через `@name` в SQL или `{{name}}` в YAML-полях). Переменные, переданные в CLI но
  не задействованные в конфиге, исключаются.

  Покрытие: `exportToTDTP` (каждая часть), `exportToRabbitMQ`, `exportToKafka` (legacy),
  `exportToKafkaSpool` (каждая часть).

  **Новый флаг `--expect-var name=value`** (`cmd/tdtpcli/`):

  При импорте (`--import`, `--import-broker`) позволяет проверить переменные источника
  **до** любых операций с БД — fail-fast без побочных эффектов:

  ```bash
  # Consumer.py, tdtpcli --import-broker --expect-var region=EMEA --expect-var dept=sales
  tdtpcli --config cfg.yaml --import-broker --expect-var region=EMEA --expect-var dept=sales
  ```

  Если переменная отсутствует или имеет другое значение — импорт прерывается с чётким сообщением:

  ```
  --expect-var check failed (pipeline: daily-sync):
    @dept: expected "hr", got "sales"
    @region: expected "APAC", not present in packet
  ```

  Флаг повторяемый; порядок проверок не важен.

  **`--inspect` теперь показывает PipelineContext** (`cmd/tdtpcli/commands/inspect.go`):

  ```yaml
  pipeline: daily-sync v2.1
  pipeline_vars:
    dept: sales
    region: EMEA
  ```

  Строки выводятся только если `<PipelineContext>` присутствует в пакете — обратная
  совместимость с пакетами, созданными до v1.4, сохранена.

  **Новые функции и методы:**
  - `packet.PipelineContext`, `packet.PipelineInfo`, `packet.PipelineVar` — типы данных
  - `etl.UsedVariables(config, vars)` — возвращает только используемые переменные
  - `Exporter.WithPipelineContext(ctx)` — устанавливает контекст на экспортер
  - `Processor.SetPipelineContext(ctx)` — устанавливает контекст на процессор
  - `commands.CheckPipelineVars(pkt, expectVars)` — проверка перед импортом

- **Pipeline Variables — параметрические пайплайны через CLI** (`pkg/etl/variables.go`):

  Переменные передаются напрямую в командной строке без дополнительного флага:

  ```bash
  ./tdtpcli.exe --pipeline dept_staff.yaml @dept=97-256 @date_from=2025-01-01 @date_to=2025-12-31
  ```

  Синтаксис подстановки:

  | Контекст               | Паттерн     | Пример                                |
  |------------------------|-------------|---------------------------------------|
  | SQL — строковый литерал | `'@name'`   | `WHERE dept = '@dept'`                |
  | SQL — числовой/bare    | `@name`     | `WHERE year = @year`                  |
  | YAML-поля              | `{{name}}`  | `destination: "out/{{dept}}.tdtp.xml"` |

  Кавычки вокруг значения снимаются автоматически (`@dept="97-256"` → `97-256`).
  Для строковых литералов одиночные кавычки внутри значения экранируются (`'` → `''`).

  Подстановка применяется к: `sources[].query`, `sources[].dsn`, `transform.sql`,
  `description`, `output.tdtp.destination`, `output.xlsx.destination` (включая `fallback`-цепочку).

  **Валидация:**
  - Переменная объявлена в конфиге, но не передана в CLI → ошибка с указанием имён.
  - Передана в CLI, но не используется в конфиге → предупреждение (не ошибка).

  Подстановка выполняется **до** SQL-валидатора — инъекции через значения переменных
  блокируются существующим SQL-валидатором (`SELECT/WITH only` в safe mode).

  Вывод при запуске показывает активные переменные:
  ```
  Pipeline: dept-staff-with-hiredate
     Список відділу 97-256 за 2025-01-01 – 2025-12-31
     Variables: @date_from=2025-01-01, @date_to=2025-12-31, @dept=97-256
  ```

- **`pkg/etl/variables_test.go`** — 18 unit-тестов:
  `ParsePipelineVars`, `substituteSQL` (строковый литерал, экранирование кавычек, bare-числовой,
  смешанный, неизвестная переменная), `substituteYAML`, `ApplyVariables` (полная подстановка,
  ошибка при отсутствии переменной, предупреждение при лишней переменной, noop для пустого конфига).

### Fixed

- **NULL-маркер в TIMESTAMP-колонках при импорте в PostgreSQL / MSSQL**
  (`pkg/adapters/postgres/import.go`, `pkg/adapters/mssql/import.go`):

  TDTP кодирует NULL-значения полей как строку `[NULL]` в теле пакета.
  `convertValue` (PostgreSQL) и `stringToValue` (MSSQL) не проверяли этот маркер
  до передачи значения в `schema.Converter.ParseValue()`. В результате `[NULL]`
  доходил до драйвера как строка и вызывал ошибку на уровне БД:

  ```
  ERROR: invalid input syntax for type timestamp: "[NULL]" (SQLSTATE 22007)
  ```

  Исправление: проверка `field.SpecialValues.Null.Marker` добавлена **до**
  вызова `ParseValue` — так же, как это уже реализовано в `base/import_helper.go`
  (используется MySQL-адаптером).

  Обнаружено в `examples/travel-agency` при синхронизации `branch_sales_inbox_staging`,
  где `cancellation_date TIMESTAMP NULL` содержала реальные NULL-значения.

- **Регрессионный тест T4.9** (`tests/cli/test_postgres.py`):
  экспорт таблицы с двумя nullable TIMESTAMP-колонками (5 строк, по 2 NULL в каждой),
  импорт, проверка что NULL-значения сохранены точно.

- **`setup_staging_central.sql`** (`examples/travel-agency`):
  `cancellation_date` в `branch_sales_inbox_staging` изменён с `TEXT` на `TIMESTAMP NULL`;
  соответствующий каст `NULLIF(NULLIF(x,''),'[NULL]')::TIMESTAMP` в `merge_branch_sales_inbox`
  упрощён до прямой передачи значения.

---

## [1.9.2] — 2026-04-21

### Added

- **MySQL adapter — 58/58 CLI integration tests pass** (`tests/cli/test_mysql.py`):
  - T1 Basic Export: export all rows, `--fields` projection, `--list`
  - T2 TDTQL Filters: WHERE, compound AND (multiple `--where`), IN, ORDER BY, LIMIT/OFFSET,
    negative LIMIT (tail mode), bracket-quoted field names with spaces and `$`
  - T3 Compression: zstd level 3/19, kanzi level 6, `--hash` checksum, corruption detection,
    compress from config
  - T4 MySQL→MySQL Roundtrip: plain/compressed import, replace/ignore strategies, `--fields`
    projection, bracket-quoted tables (`[ERP$Entry]`, `[complex_fields]`), bracket-quoted WHERE
  - T5 File Integrity: `--test`, `--test` with checksum, `--inspect`
  - T6 Edge Cases: empty result set, nonexistent table error, import missing file error
  - T7 Compact Format (v1.3.1): `--compact --fixed-fields`, compress+hash roundtrip,
    `--to-compact` conversion, compact MySQL→MySQL roundtrip
  - T8 MySQL→SQLite Roundtrip: plain/compressed cross-DB import, strategies, `--fields`,
    bracket-quoted `[ERP$Entry]`
  - T9 Diff: identical/added/removed/modified, `--ignore-fields`, `--key-fields`, error cases
  - T10 Merge: union (non-overlapping/overlapping), intersection, append, left/right priority
    with `--show-conflicts`, 3-file union, error cases

- **`tests/cli/test_mysql.py`** rewritten: inline `setup_db()` via `docker exec`
  (no external scripts, no `pymysql` dependency), aligned with `test_sqlite.py` structure.
  Test environment: MySQL 8.4 in Docker (`docker compose up -d mysql`).

---

## [1.9.1] — 2026-04-07

### Fixed

- **PostgreSQL TIME type** (`pkg/adapters/postgres/types.go`, `pkg/adapters/base/type_converter.go`):
  - PostgreSQL `time without time zone` column now exports correctly as `08:00:00` instead of
    failing with "invalid timestamp format, expected RFC3339".
  - Root cause: `time` type was mapped to `TIMESTAMP` with subtype `"time"`, but converter
    didn't handle the subtype during validation.
  - Fix: Added `Subtype` field to `schema.FieldDef`, updated all `FieldDef` creation sites
    to copy subtype from `packet.Field`, and modified `parseTimestamp` to check for
    `subtype == "time"` and delegate to new `parseTime` function.
  - Added `pgtype.Time` handler in `DBValueToString` for PostgreSQL driver.

- **Test data reproducibility** (`scripts/create_postgres_test_db.py`):
  - Added `random.seed(42)` for deterministic test data generation.
  - Updated expected values in `tests/cli/test_postgres.py`: `ACTIVE_USERS=73`, `USERS_BALANCE_GT_5000=53`.

### Added

- **35/35 PostgreSQL CLI integration tests pass**:
  - All tests in `tests/cli/test_postgres.py` now pass with deterministic data.
  - Coverage: basic export, TDTQL filters, compression (zstd/kanzi/hash), export/import roundtrip,
    file integrity, edge cases, compact format.

---

## [1.9.0] — 2026-04-06

### Message Broker — Production Release

Kafka broker graduates from `[BETA]` to production-ready.
Full pipeline (DB → Kafka → DB, DB → Kafka → files) benchmarked at **50 000 rows in ~7s**
over localhost with 5 packets; traffic reduced 4× with kanzi vs uncompressed.

#### Export (`--export-broker`)

- **Parallel compress + serialize**: all packets processed in concurrent goroutines
  (`sync.WaitGroup`); each goroutine owns its own `packet.NewGenerator()` instance.
  kanzi: 6.7s → 5.1s (1.3×) on 100k rows.
- **`SendBatch`** (`pkg/brokers/kafka.go`): all serialized packets sent in a single
  `WriteMessages` call — one network roundtrip instead of N sequential sends.
  kafka-go `BatchTimeout` lowered from default 1s to 5ms (eliminates per-packet 1s wait).
  kafka-go `BatchBytes` raised to 100 MB (was 1 MB — caused "Message Size Too Large" on kanzi packets).

#### Import (`--import-broker`)

- **Parallel decompression**: all raw packets buffered first (receive is inherently serial),
  then packets 2…N decompressed in parallel goroutines; results assembled in order.
  ACK: single `CommitLast()` after all processing — for Kafka this commits the highest
  offset, implicitly covering all previous offsets.
- **`--output` mode**: instead of importing to DB, saves decompressed packets as
  `base_part_N_of_Total.tdtp.xml` files compatible with `--import` multi-part convention.
- **`--raw` flag** (`--import-broker --raw --output`): saves queue messages verbatim
  without any parse, decompress, or validation. Peeks the first message header to read
  `TotalParts` for correct `_part_N_of_Total` naming. No DB connection required.

#### Broker Configuration (Kafka)

- `brokers` (list) and `consumer_group` YAML fields added to `BrokerConfig`.
- `StartOffset: kafka.FirstOffset` (was `LastOffset`) — fixes race where reader
  positioned after messages sent during consumer group rebalance.

#### Performance (50k rows, 5 packets, localhost)

| Mode | Export | Import→files | Traffic |
|------|--------|-------------|---------|
| No compression | 3.4s | 3.8s | 7.2 MB |
| zstd level 3 | 3.5s | 3.9s | 2.4 MB (3×) |
| kanzi level 6 | 3.6s | 3.9s | 1.8 MB (4×) |

Import time dominated by receive + XML re-serialize; decompression parallelism eliminates
its contribution entirely at 5 packets.

### Changed

- `--listen` flag: removed `[BETA]` label. Streaming consumer is production-ready for Kafka.
- **`--import-broker` atomicity** (`cmd/tdtpcli/commands/broker.go`): multi-part imports now
  use a single `ImportPackets` transaction by default — all-or-nothing, mirrors `--import`
  (file) behaviour. Previously each part was committed with a separate `ImportPacket` call,
  leaving the table partially updated on failure.
- **`--keep` flag** (`--import-broker --keep`): opt-in streaming mode — each packet is
  received, decompressed, and committed immediately without buffering the full batch in
  memory. On failure, successfully committed parts remain in the table for analysis.
  Implemented in `importBrokerKeep()` as a separate code path (no full-batch buffer).
- Help (`help_full.txt`, `help_short.txt`): broker section expanded with `--raw`,
  `--output` multi-part naming, `--keep` semantics, parallel processing notes, kanzi
  traffic comparison.

### Fixed

- **`--fields` bracket-quoting** (`cmd/tdtpcli/main.go`): `splitCommaSeparated` now parses
  `[Field Name]` syntax for field names containing spaces or commas, matching the
  bracket-quoting already supported in `--where` (TDTQL lexer).
  - `--fields "id,[Birth Date],status"` → `["id", "Birth Date", "status"]`
  - `--fields "[First, Last],[Birth Date]"` → `["First, Last", "Birth Date"]`
  - Same parser used for `--key-fields`, `--ignore-fields`, `--fixed-fields`.
- **SELECT projection quoting** (`pkg/core/tdtql/sql_generator.go`): field names in
  `query.Fields` were joined bare into `SELECT f1, f2 FROM ...` — a name like `Birth Date`
  produced invalid SQL. Now each field passes through `quoteFieldName()` (same function
  already used for WHERE and ORDER BY), producing `SELECT "Birth Date", id FROM ...`.
  MSSQL/MySQL dialect adapters convert ANSI double-quotes downstream as before.

---

## [1.8.2] — 2026-04-05

### Performance

#### Import pipeline — 2× speedup (1.55s → 0.77s, 100k rows × 7 fields, SQLite)

- **Streaming import** (`cmd/tdtpcli/commands/import.go`): parts processed one at a time —
  read → parse → insert → release. Previously all parts were buffered in memory
  simultaneously before any inserts began. Memory usage is now constant regardless
  of part count; GC pauses during insertion eliminated.

- **`GetRowValues` fast path** (`pkg/core/packet/parser.go`): rows without escape
  characters (`\|`, `\\`, `\n`) — the vast majority of real data — are split via
  index scan returning subslices of the original string with zero per-field
  allocations. Benchmark: `simple_10_fields` 409 ns/11 allocs → 150 ns/1 alloc (2.7×);
  `many_fields_100` 5034 ns/105 allocs → 1079 ns/1 alloc (4.7×).

- **Parser/Converter singletons** (`pkg/adapters/base/import_helper.go`,
  `pkg/adapters/postgres/import.go`, `pkg/adapters/mssql/import.go`):
  `packet.NewParser()` and `schema.NewConverter()` were allocated on every single row
  in all adapters. Both structs are stateless (`{}`); replaced with package-level
  singletons. Eliminates ~2 allocs × 100k rows per import.

- **`PrepareContext` for SQLite batch INSERT** (`pkg/adapters/sqlite/import.go`):
  the 994-parameter INSERT query was re-parsed by SQLite on every batch call
  (~700 calls for 100k rows). Now prepared once; reused for all full batches.
  Args slice reused across batches. Raw benchmark: 1043 ms → 433 ms (2.4×).

#### Misc

- **`help.go` refactor**: ~100 `fmt.Println` calls replaced with two embedded text
  files (`help_short.txt`, `help_full.txt`) via `//go:embed`. Version injected via
  `strings.ReplaceAll("{VERSION}", version)` at runtime.

### Infrastructure

- **Pre-commit hook** (`.git/hooks/pre-commit`): runs `gofmt`, `golint`, `go vet`
  on staged `.go` files before every commit. `gofmt` and `go vet` are blocking;
  `golint` is advisory.

---

## [1.8.1-beta] — 2026-04-02

### Added

#### Field Name Sanitizer (`--translit`, `--clear`)
- `pkg/sanitize` — new package with `ApplyToSchema()` single entry point
  - `--clear`: symbol map replacement (`%` → `_pct_`, `$` → `_usd_`, `&` → `_and_`, `@` → `_at_`, `#` → `_xh_`, `?` → `_is_`, `~` → `_not_`, spaces/dots/dashes → `_`; consecutive `__` collapsed)
  - `--translit`: non-ASCII transliteration via `github.com/mozillazg/go-unidecode v0.2.0` (Cyrillic, European diacritics)
  - Combined mode: `--translit` runs first, then `--clear`
  - Applied **only on `--import`** — `--export` always preserves original field names (source of truth)
- `cmd/tdtpcli/flags.go`: `--translit` and `--clear` CLI flags
- `cmd/tdtpcli/commands/import.go`: `SanitizeClear` / `SanitizeTranslit` options, applied after field whitelist
- `pkg/etl/config.go`: `SanitizeFieldsConfig` struct; `sanitize.translit/clear` keys in ETL source YAML
- `pkg/etl/processor.go`: per-source sanitization in `populateWorkspace`
- `pkg/core/packet/types.go`: `OriginalName string` runtime field on `packet.Field` (never serialized)
- DB column comments preserving original names:
  - PostgreSQL: `COMMENT ON COLUMN t.col IS 'original: ...'`
  - MySQL: inline `COMMENT 'original: ...'` in column definition
- Test XMLs: `tests/sanitize/` — `access_fields.tdtp.xml`, `cyrillic_fields.tdtp.xml`, `exotic_mixed.tdtp.xml`, `safe_import.tdtp.xml`
- `pkg/sanitize/fieldname_test.go` — 7 unit tests covering all sanitizer modes

#### TDTQL: Bracket-Quoted Identifiers
- `pkg/core/tdtql/lexer.go`: support for `[Field Name]` syntax (MSSQL/Access style)
  - `[` token now reads to `]` and emits `TokenIdent` with the inner name (brackets stripped)
  - Fixes: `--where "[Termination Date] = '1753-01-01'"` — was "parse error: expected field name, got 1"
- `pkg/core/tdtql/sql_generator.go`: `quoteFieldName()` helper
  - Names with non-safe chars → ANSI `"field name"` in generated SQL
  - Applied in `generateFilterCondition`, `generateOrderByClause`, `generateReversedOrderByClause`
- `pkg/adapters/base/sql_adapter.go`: `MSSQLAdapter.AdaptSQL` now converts ANSI-quoted `"field"` → `[field]`
  - `StandardSQLAdapter` MySQL mode: existing `ReplaceAll("\"", "`")` handles ANSI → backtick conversion

### Fixed
- `pkg/brokers/kafka_stub.go`: removed unused `config Config` field; added doc comments to all exported methods (revive lint)
- `pkg/processors/compression_test.go`: removed trailing blank line (gofmt)
- `.git/hooks/pre-commit`: `golangci-lint run --tags` → `--build-tags` (golangci-lint v2 rename)

### Documentation
- `docs/USER_GUIDE.md`: added `--test` command section, `--translit`/`--clear` section, bracket-quoted WHERE section, parallel export note, pre-import workflow `--inspect → --test → --import`
- `AGENTS.md`: added `--test` workflow, `--import --translit/--clear` skills, bracket-quoted `--where` examples
- `cmd/tdtpcli/help.go`: bracket-quoted `--where` examples, `--test`/`--inspect` pre-import workflow in EXAMPLES section

### Tests
- `tests/cli/test_sqlite.py`: added `complex_fields` table (column names with spaces and special chars); T2.8 and T2.9 tests for bracket-quoted `--where` on this table

---

## [1.8.0-beta] — 2026-03-31

### Added

#### Object Storage (S3)
- `pkg/storage` — ObjectStorage interface, factory, and S3 driver (`aws-sdk-go-v2`)
- `--output s3://bucket/key` on export — upload multi-part TDTP directly to S3
- `--import s3://bucket/key` — download + auto-discover all `_part_N_of_M` siblings from S3
- `--inspect s3://bucket/key` — inspect packet metadata from S3 in-memory (no temp file)
- `--to-xlsx / --export-xlsx --output s3://` — XLSX output directly to S3
- ETL pipeline source type `tdtp-s3`: load compressed multi-part TDTP sets from S3 into workspace
- Compatible with SeaweedFS, MinIO, Ceph RGW, AWS S3 (path-style and virtual-hosted)
- Build tag `nos3` to exclude driver and drop `aws-sdk-go-v2` dependency

#### File Integrity (`--test`)
- `--test <file>` — dry-run integrity check of TDTP files (no database required)
  - Multi-part file discovery: auto-resolves `_part_N_of_M` siblings from any part path
  - Missing part detection: reports which parts are absent before validating
  - Batch consistency: all parts must share the same `InReplyTo` UUID and `TableName`
  - Row count validation: actual `<R>` count vs `RecordsInPart` header field
  - XXH3 checksum validation for files exported with `--hash`
  - Decompression integrity: dry decompress in memory for zstd and kanzi files
  - Duplicate `MessageID` detection across parts

#### Compression
- `compress_algo` YAML config field in `ExportConfig` — set default algorithm in config file
  - Flag `--compress-algo` takes precedence over config file value
  - Example: `compress_algo: kanzi` in config enables kanzi without CLI flags

#### CLI Integration Tests
- `tests/cli/test_sqlite.py` — 31 integration tests for SQLite source
  - T1: Basic Export (3 tests) — row counts, field projection, `--list`
  - T2: TDTQL Filters (7 tests) — WHERE, AND, IN, ORDER BY, LIMIT, OFFSET, tail mode
  - T3: Compression (6 tests) — zstd levels, kanzi, hash, corrupt file detection
  - T4: Export/Import Roundtrip (5 tests) — data identity, strategies (replace/ignore), field subset
  - T5: File Integrity (3 tests) — `--test` on plain/compressed/checksum files, `--inspect`
  - T6: Edge Cases (3 tests) — empty result, nonexistent table/file error handling
  - T7: Compact Format (4 tests) — protocol v1.3.1, compact+compress pipeline, `--to-compact`, roundtrip
- `tests/cli/test_postgres.py` — 32 integration tests for PostgreSQL source
  - Same T1–T7 structure; T4 roundtrip imports into same PG database
  - Preflight check: `pg_isready` + row count verification + auto-setup via `create_postgres_test_db.py`
  - Dynamic WHERE assertions: expected counts queried live via psql subprocess
  - Run a single group: `python3 tests/cli/test_postgres.py T3`

#### Kanzi Compression (from v1.7.x)
- `--compress-algo kanzi` — kanzi-go compression alongside existing zstd
- Compression levels 6–7 for kanzi (6× ratio vs raw, vs 3× for zstd level 3)
- `pkg/python/libtdtp` — multi-algorithm support in Python bindings compress/decompress paths
- Build tag `nokafka` for offline builds without Kafka dependency

#### S3 + Pipeline Features
- `examples/09-s3-pipeline-chain` — extract → split by region pipeline example
- ETL `output.type: tdtp` with S3 output
- Smart Failover in ETL — fallback delivery channel with circuit breaker
- `--fast` flag to skip SpecialValues detection on export

### Changed
- `CreateSampleConfig` includes `CompressAlgo: "zstd"` in default template
- `--test` is an early-exit command: no database connection required
- `commandWasSpecified()` updated to include `--test`

### Performance (from v1.7.x)
- Parallel packet processing for file/S3 export
- Skip `GetRowCount` in TDTQL export when no LIMIT is set
- Single-pass XML escaping with schema-aware escape mask
- Manual `bufio` writer replacing `xml.MarshalIndent` in data section
- `strconv` replacing `fmt.Sprintf` in hot data conversion path
- DATE/DATETIME scanned as string in SQLite (skip `modernc.parseTime`)
- PostgreSQL full-export benchmark infrastructure (`cmd/bench_raw`)

---

## [1.7.1-beta] — 2025 Q4

### Added
- `--compact` — TDTP v1.3.1 compact format on export (fixed fields written once per group)
- `--to-compact <file>` — convert existing TDTP v1.x file to compact v1.3.1 in-place
- `--compact-tail` — tail + carry attributes for streaming support
- `--fields <col1,col2>` — column projection on export and import
- `--inspect <file>` — YAML metadata summary of a TDTP file or S3 object
- `--listen` — streaming consumer daemon (v1.7.1-beta)
- `--where` flag repeatable — multiple conditions combined with AND
- `--where` supports `IN (...)` operator
- `--limit` with negative value — tail mode (last N rows)
- `--list` accepts optional glob pattern for table name filtering
- `--validate` and `--normalize` YAML-based processors
- `FieldValidator` with `on_error` strategy: fail / filter / warn
- SpecialValues v1.3.1: `[NULL]`, `NaN`, `INF`, `-INF`, `0000-00-00` markers
  - Auto-detected on export; correctly restored to NULL/native on import
  - Excel data-integrity traps handled automatically (BIGINT, dates pre-1900, formula strings)
- RabbitMQ: flexible queue config, TLS skip-verify, passive declare
- MSMQ broker support (`queue_path` config field)
- xZMercury AES-256-GCM encryption layer for pipeline output
- `tdtpserve` — standalone HTTP encrypted TDTP data viewer
- Python bindings: `J_ExportAll`, `read_pandas` / `write_pandas`, zstd+XXH3 support
- C# .NET 3.5 P/Invoke wrapper for `libtdtp.dll`
- Redis result publisher for pipeline state reporting

### Fixed
- `RecordsInPart=0` in `ExecuteRawQuery` and `workspace.ExecuteSQL`
- rawRows regression: `ImportPacket` importing nothing after fast-path optimization
- Compact format auto-expansion at parser boundary (broker, ETL importer, diff/merge, HTML, XLSX)
- `--fields` projection applied to `<Schema>` and `<R>` in MSSQL export
- `StrategyReplace` = full table swap (TRUNCATE + INSERT), not UPSERT
- `StrategyCopy` = full replace; other strategies = UPSERT accumulate
- Batch-aware broker import — match by batchID, nack foreign packets
- Compression: `SetRows(GetRows())` clearing `rawRows` fixed
- DATE type detection and rowversion filtering in MSSQL adapter
- Scientific notation handling in DECIMAL parser

---

## [1.6.0] — 2025 Q3

### Added
- `--where` TDTQL filter with SQL-to-TDTQL translation
- `pkg/cliquery` — WHERE/fields parsing with unit tests
- PostgreSQL `--fields` projection in `ExportTableWithQuery`
- `pkg/etl` — ETL pipeline with workspace, smart failover, processor chain (mask → normalize → compact → compress → encrypt → hash)
- MS Access adapter (ODBC, 32-bit, Windows-1251, ADOX schema via VBScript)
- kanzi-go compression (direct dependency)
- `--packet-size` flag
- `--hash` flag — XXH3 checksum embedded in packet header
- Pagination: `ExportTableWithQuery` with Limit/Offset/MoreDataAvailable
- TDTP HTML viewer (`--to-html`)
- TDTP XLSX export/import (`--to-xlsx`, `--export-xlsx`)
- Zero Trust encryption layer (xZMercury)

---

## Version History Summary

| Version | Highlights |
|---------|-----------|
| 1.9.6 | `--to-csv`, `-l` alias, MSSQL TOP N fix, xlsx MessageID fix, rawRows data-loss fix, filter 9.5×, compact 2×, `--enc` tier |
| 1.9.5 | `--to-csv` (prev), TDTQL `-n`/`-w`, v1.4 integrity + xzMercury |
| 1.9.4 | TDTP v1.4 Dictionary, tdtp-svg (SVG↔TDTP), MSSQL 17GB fix, --fallback-row-limit |
| 1.9.3 | PipelineContext + --expect-var, pipeline variables @name=value |
| 1.9.1 | PostgreSQL TIME type fix, test data reproducibility (seed=42), 35/35 tests pass |
| 1.9.0 | Kafka production-ready, parallel compress/decompress, `--raw`, `SendBatch`, `--output` multi-part save |
| 1.8.2 | 2× import speedup, streaming import, `PrepareContext`, embedded help files |
| 1.8.1-beta | `--translit`/`--clear` sanitization, bracket-quoted identifiers, ETL sanitize |
| 1.8.0-beta | S3 object storage, `--test` integrity check, `compress_algo` config, Python CLI test suites |
| 1.7.1-beta | Compact v1.3.1, `--compact`/`--to-compact`, `--inspect`, `--listen`, SpecialValues, xZMercury |
| 1.7.0 | kanzi compression, `--fields`, MSMQ, `--packet-size` |
| 1.6.0 | TDTQL `--where`, ETL pipeline, Access adapter, `--hash` |
| 1.3.1 | TDTP protocol v1.3.1 — compact format specification |
| 1.0–1.3 | Core protocol, XML serialization, SQLite/PostgreSQL/MSSQL adapters |
