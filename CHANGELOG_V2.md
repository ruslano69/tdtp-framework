# Changelog — CLI v2

> Why two changelogs: v2 (`cmd/tdtpcli_v2`) is a strangler-fig rebuild
> alongside the frozen v1 (`cmd/tdtpcli`). Its history is tracked here
> until wave 4, when this file is merged into `CHANGELOG.md` and the old
> binary is deleted. Same format, same rules.

## [Unreleased]

### Fixed — Oracle with packets from other engines

Found running the adapter against Oracle XE 18c and 21c with a SQLite
packet; the existing contract test only used Oracle-made tables
(upper-case columns, sized types), where none of this shows.

- **Every `--where`/`--order-by` on an imported table read the whole table
  into memory.** Imported columns keep their names quoted (`"name"`); the
  pushdown SQL left them bare, Oracle folded `name` to `NAME`, ORA-00904,
  and the export fell back to a full scan — refusing outright past
  `--fallback-row-limit`. Columns are now quoted with their exact schema
  spelling (case-insensitive lookup).
- **Every date filter did the same, on any table.** `born > '1995-01-01'`
  went through the session's `NLS_DATE_FORMAT` (DD-MON-RR): ORA-01861.
  DATE/DATETIME/TIMESTAMP comparisons now use ANSI literals.
- **A lengthless text key made the import impossible** — SQLite
  `TEXT PRIMARY KEY`, PostgreSQL `text`: it became a CLOB key (ORA-02329).
  Now `VARCHAR2(255 CHAR)`; non-key lengthless text stays CLOB.
- **`REAL` lost precision**: `BINARY_FLOAT` turned 1234.56789012345 into
  1234.5679. Now `BINARY_DOUBLE`.
- **`INTEGER` came back as plain `DECIMAL` after a pass through Oracle**
  and turned into `NUMERIC(19,…)` elsewhere. Oracle now reports
  `NUMBER(19,0)` as `DECIMAL` + subtype `bigint` (the subtype PostgreSQL and
  MSSQL already use); PostgreSQL, MSSQL and MySQL create `BIGINT` for it,
  SQLite ignores the hint. `DECIMAL` keeps a native value above int64 exact;
  such a value makes a `BIGINT` target refuse the row. The value travels as
  an exact `int64` (`packet.BigintValue`) — the DECIMAL import path rounds
  through `float64`, which turned 9007199254740993 into …992. Live:
  INTEGER → Oracle → MSSQL `BIGINT` with 2^53+1 and min int64 exact, and
  9500000000000000000 refused.
- **`replace`/`ignore` without key fields left an empty table behind.**
  The refusal came from the row insert, after `CREATE TABLE` — which Oracle
  commits implicitly. Now checked before any DDL.
- The shared `tdtql.SQLGenerator` gained two optional hooks (`FieldName`,
  `Value`) that an export dialect can supply; unset, generation is
  byte-identical, so the other adapters are untouched.
- `TestOracleLiveCrossEngine` pins all four on 18c and 21c with the
  fallback limit at one row, so a failed pushdown cannot hide behind a
  correct result. Disabling any fix fails it.


### Merged CLI v2 inspect and sync work

- `inspect-table --json` now returns the complete table report, including
  columns, keys and row counts. The text report remains shared with v1.
- `sync` remains an alias for `sync-incremental`.

### Complete CLI help

- General `--help` now explains global flags, commands, examples, and exit
  codes. `--help export`, `help export`, and `export --help` use one command
  formatter with the command's actual flags and defaults.
- Every registered command has a valid v2 synopsis and example. Unknown help
  topics return usage exit code 2; tests cover all commands and help forms.

### Preview completion: incremental sync, Kafka listener, request processing

- Added native `sync-incremental` with checkpoint and broker output options,
  standalone Kafka `listen`, and `process-request`, including flat-flag
  compatibility. Their progress follows the v2 text/quiet/JSON output contract.
- `listen` uses the application context for graceful shutdown and skips
  automatic retries of a running daemon. Live Kafka sync-to-broker/listener
  and SQLite import are covered by an opt-in end-to-end test.
- `process-request` checks the recipient database adapter against the active
  license, rejects recipient path traversal, and emits a TDTP response packet
  rather than a reference packet. The shared response fix applies to v1 too.
- Added `import --strict-schema`; the PostgreSQL CLI suite verifies that a
  constrained source column keeps its declared length on import.
- The release workflow now builds `tdtpcli_v2-preview-*` for the same five
  platforms as v1 and includes those files in release checksums. Stable
  `tdtpcli` assets remain v1 during the preview.
- Community licensing remains as currently implemented for the preview. Row
  limit, S3, and pipeline source/ETL enforcement decisions are deferred until
  before 2.0. See [migration guide](docs/CLI_V2_MIGRATION.md).

### Wave 3.5: config once per run, config-file processors, production tag

- `Deps` parses the YAML once per run (`loadConfig` cached); every
  accessor derives from it with byte-identical error texts.
- `Deps.processors()`: flags first, config-file `processors:` section
  as fallback per type (that section was dead in v1 — parsed, never
  read). New `RowProcessors.AddMaskRules/AddValidateRules/
  AddNormalizeRules`; unknown validate types fail loud. Proven live
  (config mask masks emails, announces "from config").
- `production` tag verified for v2: suite green under
  `-tags "production nokafka"`, `--enc-dev` absent from prod help.
- The next tagged release ships a separate `tdtpcli_v2` preview alongside
  v1 with the same build flags; the stable-name switch remains wave 4.

### Fixed: one result for text and JSON reports

- `list` now queries the database once; its JSON names come from the same
  listing as the text report, including when the database changes mid-run.
- `inspect` reads and parses each local or S3 packet once. JSON for S3 now
  includes the schema and row details already available to the text report.
- `diff` compares the two packets once and uses that result for both formats.
  The shared v1 reporting entry points retain their signatures and output.

### Wave 3.7: `map` and broker loop modes

- Added native `map mapping.yaml --input SRC` with `--dry-run`, `--drain`,
  `--listen`, and `--mercury-url`; legacy `--map` resolves through the
  compatibility shim. The mapping YAML supplies its own target DSN, and v2
  checks that target adapter against the license before connecting.
- Mapping progress uses the app output streams. `--json` keeps stdout as a
  single verdict; `--quiet` keeps the row total. Broker loop mode records
  audit entries for each processed message.
- Tested file dry-run and SQLite import, plus live RabbitMQ one-shot,
  `--drain`, and `--listen` with SQLite and per-message audit. Shutdown,
  ACK, and NACK paths also have broker-independent tests.

### Wave 3.7: `steps` workflows

- `tdtpcli_v2 steps workflow.yaml [@name=value...]` runs the shared
  dependency-wave engine; the `--steps` form resolves through the one-release
  compatibility shim. Children use `os.Executable()`, so a v2 workflow runs
  v2 commands, including a v2-only `to-json` step.
- Workflow progress and child output use the app's streams. In `--json` mode
  progress goes to stderr and stdout contains one verdict; `--quiet` reaches
  child commands. The shared runner defaults to its former process streams
  for v1 callers.
- A binary end-to-end test covers the v2-only child, dependencies,
  `on_error: skip`, compatibility, quiet propagation and JSON output.
- Porting exposed a quiet-mode parity gap in `test`: v1 keeps the integrity
  verdict under `--quiet`, while v2 suppressed it with the human preamble.
  The same gap hid the entire `inspect` schema report. V2 now keeps both
  results, so quiet workflows retain their useful output.

### Wave 3.6: `--mercury-caller` (export, export-broker)

- Sender identity on Mercury registration. `export` plumbs the flag
  straight through (v1 parity). `export-broker` honors it for real:
  sender is the caller when given, else the table (v1 accepts the flag
  and silently ignores it there — same class as `--translit`).
- Proven live: registered sender reads back `customcaller` (file),
  `brokercaller` (broker), and `users` by default.

### Wave 3.6: `--integrity` on `export-broker` (both features work)

- `--hash` stays a v1-identical no-op (the XXH3-64 checksum of the
  compressed blob rides with `--compress` automatically); `--integrity`
  is new and real: v1.4 xxh3-128 hashes stamped before compression,
  registered in Mercury with `--mercury-url` (local-only without it).
  The two answer different questions — transport intact vs content
  authentic — and the file export already had both.
- Engine: `IntegrityV14` in `brokerSendOptions` (internal, no signature
  churn) plus `ExportToBrokerWithOptions`; the 14-positional
  `ExportToBroker` delegates with `false`, so v1 (frozen) is untouched.
- Proven: unit on the wire bytes (1.4 + fingerprint, no broker needed);
  live MSMQ roundtrip (10 rows), local-only and Mercury-registered
  (`registered:true, match:true` on query).
- Consumer enforcement verified live, not just wired: a tampered packet
  (one row value changed, stale hashes) placed in the queue is refused
  with `packet refused (message …): data hash mismatch` (stored vs
  computed shown), rc=1, nothing written; the untampered twin imports
  10 rows. The message ID rides in the refusal so the audit trail names
  the poisoned packet. Both consume paths (batch and `--keep`) run
  `applyV14SecurityGate`; only `--raw` skips it, by definition
  (bytes as-is).
- Known, out of scope: `import-broker --output` re-marshal resets the
  version to 1.0 (hashes survive, `--test` passes) — pre-existing.

### Wave 3.6: `export-broker` `--batch` / `--hash` (deprecated no-ops)

- Accepted and ignored, exactly as v1 (both are `[deprecated, no-op]`
  there; the engine never receives them). Pinned by a parse-level unit
  test — the point is they must not exit 2.

### Wave 3.6/3.7: `--translit` that works, `inspect-table`, `version`, `init-config`, `to-xlsx` over S3

- `--translit` on `to-csv`/`to-xlsx`/`export-xlsx` is real in v2:
  non-ASCII field names become ASCII headers after filtering/projection
  (row values untouched). Deliberate difference: v1 accepts the flag on
  these commands and silently ignores it (its allowlist claims them, no
  engine code reads it — the exact class `flagscope.go` was built to
  catch). Proven live (`Имя,Фамилия` → `Imia,Familiia`).
- `inspect-table TABLE --config` (new `InspectTableTo` in shared code,
  v1 delegates; reports byte-identical on sqlite and mssql).
- `version` (command plus bare `--version` flag, from `pkg/core/version`)
  and `init-config (postgres|mssql|mysql|sqlite)` (one command for v1's
  four `create-config-*`, byte-identical files via the shared builders).
- `to-xlsx` reads and writes `s3://` through `--config` storage (the
  engine already did; v2 never passed it `StorageCfg`). Proven live
  against weed both directions.

### Wave 3.6: `--enc` on export, `--enc13` dropped, `--enc-dev` dev-gated

- `export` grew `--enc` (license `enc` gate via `Features()`, Mercury URL
  from flag with config `security.mercury_url` fallback, same engine) —
  the last `--enc` writer missing in v2. Legacy whole-blob writing is
  disabled everywhere in 2.0: `--enc13` exists on no v2 command (a
  foreign flag fails at parse, exit 2); old v1.3 files still decrypt on
  import. `pipeline --enc-dev` arrived under the `!production` tag, mirroring
  v1's `flags_dev.go` (present in dev builds, absent in prod).
- Proven live against a real xzMercury `--dev` (real HMAC, no mock
  bypass): `test_encryption.py` 15/15 on both binaries.

### Wave 3.5: `s3://` (storage config + driver)

- `drivers_s3.go` (`!nos3`, one blank import, mirrors v1) — without it
  every S3 path died with `unknown storage type` (found live: pipeline
  to S3 failed in the engine's storage factory).
- `Deps.storageConfig()` + `remoteStorage()` (bucket-from-URI-wins,
  v1's main.go pattern) wired into `export` (output), `import` (input,
  local-missing check still first), `test` and `inspect` (remote needs
  `--config`, the old "wave 2" refusals are gone).
- Fixed alongside, shared code: `pkg/audit.OpenDatabaseSink` sqlite
  branch hardened — `_pragma` busy_timeout in the DSN (every pooled
  connection), `SetMaxOpenConns(1)`, and `journal_mode=WAL` through a
  bounded busy-only retry. `PRAGMA journal_mode` bypasses the busy
  handler (proven: fails in ~1ms under lock), so concurrent first-opens
  failed all but one; `-race` on the audit parallel test caught it.
- Proven: T8 `test_sqlite.py` 5/5 live against weed, full suite 127/127,
  `-race` clean over the touched packages.

### Wave 3.5: resilience (middleware)

- `resilienceMiddleware`, last in the chain (`recover → license → audit →
  resilience`) so the trail records the post-retry outcome: breaker inside,
  retry outside, both from the config's `resilience:` section, both off
  unless configured — field-for-field v1's `initCircuitBreaker` /
  `initRetryManager` / `ExecuteWithResilience` semantics (init failure
  fails the run, exhaustion reports `max retry attempts (N) exceeded`
  at exit 1). Per-run instances like v1's per-process ones.
- Deliberate difference: breaker transitions go to `out.Notice` (stderr
  in text mode, silent under `--quiet`/`--json`); v1 prints them always.
- Proven: attempt-count unit tests (retry-then-success, exhaustion,
  breaker-alone, constructor validation), `-race` clean, `test_sqlite.py`
  122/122 unchanged (default configs take the passthrough), plus a live
  retry demo (3 attempts, backoff delays, rc=1).

### Wave 3.5: audit (middleware + `Audited`)

- One chain element (`recover → license → audit`): init on entry (logger
  from the config's `audit:` section, `WithOpMetrics` side channel),
  one entry on exit with operation, metadata, resource, row count and
  duration — success or failure. Logger init failure is fatal like v1;
  a failed write or Close only warns, never fails the command.
- Both sinks from one entry, like v1: console/file text appenders plus
  the database appender. The DB connection opener (driver selection,
  sqlite `_time_format`, `busy_timeout`-before-WAL) moved to shared
  `pkg/audit.OpenDatabaseSink`; v1 delegates to it, zero behaviour change.
- Commands opt in via `AuditInfo(d, args)` with v1's operation + metadata
  per branch (17 commands). The file verdicts (`test`, `inspect`,
  `validate`) stay out, like v1's early return. Native v2 stays strict
  where v1 warned: `pipeline --mask` does not parse.
- Proven by the acceptance suite itself: `test_audit_database.py` 8/8
  against `tdtpcli_v2` (incl. A3 — 8 parallel processes, one audit DB),
  plus in-process parallel writers and `-race` over the touched packages.
- `-race` on the new parallel test found a real shared-code race:
  `commands.ResolveLicense` reassigned the process-wide license on every
  run; now `atomic.Pointer` (same shape as `quietOutput`). Fixes both CLIs.

### Wave 3.6: `--mask` / `--validate` / `--normalize`

- One bundle (`processorflags.go`, like `queryFlags`) on `export`,
  `export-broker`, `export-xlsx`, `import`, `import-xlsx`; built in `Run`
  before any database work; a bad or section-less rules file is a usage
  error (exit 2).
- Not on `pipeline` — processors belong in its YAML; `pipeline --mask` does
  not parse (v1 accepted it and did nothing).
- A test parses the package: every command holding the bundle must build it
  in `Run`, and the five holders are pinned. That the built chain is then
  passed on, the compiler enforces (an unused `procs` does not compile).
- Porting it surfaced three bugs in the shared chain (broker never masked,
  filter kept removed rows, escaped pipes shifted masked columns) — fixed
  for both CLIs, see `CHANGELOG.md`.

### Security — the license gate v2 did not have

- v2 never resolved `tdtp.lic`. On the Community floor
  `tdtpcli_v2 --config pg.yaml export t` read PostgreSQL, and
  `pipeline --enc` / `--unsafe` and `export-broker --enc` ran — v1 refuses
  all of them before any work.
- `licenseMiddleware` (chain: recover → license) resolves the license the
  way v1 does (`--license`, `TDTP_LICENSE`, `./tdtp.lic`, Community); an
  invalid file is fatal. Commands declare licensed features through
  `FeatureGated`; the adapter gate lives in `Deps.databaseConfig`, now the
  only place that builds `adapters.Config`. Refusals use v1's texts
  (shared `commands.CheckFeature`/`CheckAdapter`) and exit 1; in `--json`
  mode they arrive in-band like any other failure.
- Held by tests that do not rely on a hand-kept list: one parses the
  package and fails if `adapters.Config` is built anywhere else; another
  derives every DB command from the source and requires a Community
  refusal test for it. A paid license is tested with `license.New`
  through an injectable resolver, no vendor key needed.
- New global `--license` (the compat shim hoists it from anywhere, like
  `--config`); the global-flag list the shim consults is one function
  now, not three copies.
- Deliberate differences from v1: the adapter is gated where a database
  is used, not whenever a config file is loaded; the `License:` banner
  is a stderr notice (`Output.Notice`), since stdout carries data.
- `database.strict_schema` from the config now reaches the adapter —
  both old builders dropped it.
- Found, not changed (`TODO_NEXT_V2.md` 3.5): in both CLIs the Community
  50 000-row cap is never enforced, `s3` is never gated, pipeline source
  adapters are never gated, and a mistyped `--license` path silently
  means Community.

### Changed — the engines moved to `pkg/cli/commands`

- `cmd/tdtpcli/commands` → `pkg/cli/commands` (`git mv`, package name
  unchanged, only import paths). v2 was importing its engines from under
  the v1 binary, so the planned wave-4 `rm -rf cmd/tdtpcli` would have
  deleted the code v2 runs on. Both binaries build under every tag
  combination (`production`, `nokafka nosqlite`); `tests/cli` sqlite/csv/
  xlsx pass against both. Path references in comments, live docs and
  `.golangci.yml` exclusions follow; CHANGELOG history is left as written.
- `TODO_NEXT_V2.md` rewritten from a wave sketch into the remaining plan:
  status, wave 3.5 (license — a bypass today —, audit, resilience, a real
  `Deps`, S3/production build parity), 3.6 (flag gaps per ported
  command), 3.7 (unported commands by risk), and a wave-4 checklist.

### Fixed — silent failures in the framework itself

- An unknown or foreign flag exited 2 with **nothing on stderr**: pflag
  prints parse errors only when it is *not* `ContinueOnError`, the one
  mode v2 uses. `TestApp_ForeignFlagRejected` checked the exit code alone,
  so "a foreign flag fails at parse time" held — silently. The error and a
  `help <command>` hint are printed now.
- `--json` failures a command did not render itself (missing config,
  unreadable input, DB down) produced no output on either stream. App now
  emits `{valid:false, error, exit_code}` on stdout unless the command
  already wrote its own verdict (validate's `{valid:false, errors}` is not
  followed by a second document).
- Compat shim: `--config`/`--quiet`/`--json` after the v1 verb
  (`--export users --config c.yaml`, valid in v1 where every flag was
  global) are hoisted in front of the command; `--import ... --limit -5`
  drops the negative value with the flag instead of leaving `-5` behind
  for pflag to reject.
- `merge --sort` put NULLs **last**: a declared `[NULL]` marker was
  compared as text and sorts after `9`. Markers from `SpecialValues` now
  rank before comparison — NULL/NoDate first, then -Infinity, values,
  +Infinity, NaN (PostgreSQL's order), flipped with `--order desc`.
- `to-json --pretty` left a blank line after `[`.
- `export_cmd.go` was not gofmt-clean.
- `diff`/`merge`: an input that cannot be opened is operational, exit 1,
  the same as `inspect`/`test` and `docs/CLI_V2.md`. It was exit 3 (the
  engines fold every failure into one error). A file that was read and
  rejected (malformed, incomparable) stays a data verdict, exit 3.

### Changed — `export`: a given flag beats the config

- `--compress`, `--compress-level` and `--compress-algo` win over the
  config's `export:` section when they are **given**, checked with pflag's
  `Changed`. v1 (and v2 until now) compared the value with the default
  instead: an explicit `--compress-level 3` or `--compress-algo zstd` lost
  to the config, and `--compress=false` could not switch off
  `compress: true`. Invocations that leave the flags alone produce the
  same file as before; the tests/cli suites pass unchanged.

### Compat shim: the `tests/cli` suites now run against v2 unchanged

- The shim was unreachable: `parseGlobals` rejected a bare v1 flag
  (`tdtpcli_v2 --to-csv f.xml`) and anything after `--config` short of a
  bare command name (`--config f.yaml --export ...`, the shape every suite
  uses) before `compatResolve` ever ran. `tryCompat` now resolves both,
  plus flags-before-the-verb (`--ignore-fields B --diff a b`, a documented
  v1 quirk the suites rely on) and the `--verb=value` spelling.
- `export` grew the v1 flags it was missing: `--hash` (no-op, checksum
  already rides with `--compress`), `--packet-size`, `--stream`,
  `--fallback-row-limit` (default 1 000 000, like v1), and the
  flag-over-config compression merge (`export.compress`,
  `compress_level`, `compress_algo`).
- `merge` accepts `--merge-strategy` as a deprecated alias of `--strategy`.
- `import` drops `--limit`/`--offset` in the shim with a notice (v1
  warnUnusedFlags: accepted, nothing acted on it — pinned by sqlite
  T14.7). Native v2 stays strict: a foreign flag does not parse there.
- Proven by the porting rule itself: `test_sqlite.py` 122/122,
  `test_xlsx.py` 51/51, `test_csv.py` 43/43 against `tdtpcli_v2` via
  `TDTPCLI_BIN` swap, from a clean outdir. DB suites (postgres/mysql)
  use the same mechanism; they need live servers.

### v2-native: deterministic `merge --sort`

- Union order follows Go map iteration (fast, random per run — proven:
  two v1 runs of the same merge disagree). `--sort fields` +
  `--order asc|desc` order the merged rows before writing.
- Comparison is TYPE-AWARE via `schema.Converter`, not `ParseFloat`
  heuristics: INTEGER/REAL/DECIMAL numerically, DATE/DATETIME
  chronologically, BOOLEAN false < true, TEXT lexicographically
  (`010,10,9` — a postal-code column must not sort as numbers), NULL
  first (flipped with direction, like PostgreSQL). Unknown columns fail
  loudly. New `SortFields`/`SortDesc` on shared `MergeOptions`; v1
  untouched (no --sort flag there).

### Wave 3: `diff` / `merge` (file-only)

- Same engines (`DiffFilesTo`, `MergeFilesTo`; shared printers gained
  `io.Writer` like the rest). `diff` exits 0 whether files match or not
  (v1 semantics); `--json` carries `{equal, added, removed, modified}`.
  `merge` takes positionals (v1 takes one comma value — the compat shim
  splits it), `--output` required.
- Proven identical to v1 modulo the banner; merge compared as row sets —
  union order is nondeterministic in the engine itself (map iteration;
  v1↔v1 runs differ too).

### v2-native: `to-json` (no v1 predecessor)

- New shared `pkg/tdtpjson`: TDTP → JSON array of objects with schema
  field names as keys and honestly typed values (numbers, bools, nulls,
  ISO dates; >2^53 ints as strings, NaN/Inf as null). Streams row by row
  — constant memory on 100 MB pages. Same `queryFlags` bundle for
  filtering/sorting/projection; `--pretty` for humans, compact default
  for pipelines; `-o -` streams to stdout.
- `Output.Stdout` joined the render contract so data-to-stdout stays
  capturable in tests.

### Wave 2: `export` (database → file)

- `pkg/cliconfig`: the v1 YAML model moved out of `package main`
  (unimportable) with zero behaviour change — v1 keeps working through
  type aliases, proven by its own tests. v2 reads the same file format.
- Same engine (`commands.ExportTable`): table/positional, full
  `queryFlags`, compress (+level/algo), compact (+fixed-fields/tail),
  integrity (+mercury-url), columnar, readonly-fields, fast.
  Mask/processors and encryption travel later (config-driven).
- Verified on live postgres (`hr_portal`, 160 tables): filtered and
  compressed exports identical to v1 modulo MessageID/Timestamp
  (normalized comparison).

### Wave 2: `import` (file → database)

- Same engine (`commands.ImportFile`): strategies replace/ignore/fail/copy
  (`--strategy`, default replace), `--table` override, `--fields`
  whitelist, `--clear`/`--translit` sanitising, repeatable `--expect-var`
  (same `name=value` grammar as v1). Mask/processors travel later.
- Exit codes: unknown strategy or malformed expect-var → 2 (usage);
  unreadable input or database failure → 1; the engine's own import
  errors stay operational.
- Round-trip proven on sqlite (export → import, replace-is-idempotent,
  fail-on-duplicate, whitelist narrows the table).

### Wave 3: `pipeline` (ETL from YAML)

- Same engine (`commands.ExecutePipeline`): safe mode by default,
  `--unsafe`/`--unsafe-cert`, `--enc`/`--enc13` overrides, `@name=value`
  variables with v1's grammar (quotes stripped, stray args rejected).
- Proven identical to v1 on sqlite (normalized comparison, variables
  included). Unsafe/admin paths intentionally untested (need privileges).

### Wave 3: `export-broker` / `import-broker`

- Same engines (`ExportToBroker`, `ImportFromBroker`); the queue comes
  exclusively from config (same security rule as v1). Shared
  `commands.BrokerConfigFromCliconfig` (v1 delegates to it now);
  `SetQuietOutput` wired so `--quiet`/`--json` silence engine progress.
- `import-broker`: strategy/table/output/raw/keep/expect-var/mercury-url.
- Proven on live RabbitMQ both directions: v2→v1 and v1→v2 round-trips
  carry identical rows. Live test gated by `TDTP_BROKER_TEST=1` (CI has
  no broker).
- Output format follows the FIRST file: merging compressed inputs keeps
  their algorithm (explicit `--compress` still forces zstd). Also fixed
  alongside: `--compress` on merge was a no-op (dead generator path) —
  now compresses explicitly with checksum and version 1.2.

### Wave 3: `to-tdtp` / `to-compact` (file→file transforms)

- Same engines (`ConvertTDTPToTDTP`, `ConvertToCompact`) with the shared
  `queryFlags` bundle; `--v1`/`--v13`/`--v14` resolution and in-place
  default (no `--output` overwrites the input) mirror v1 exactly.
- Proven identical to v1 (normalized comparison, filters and
  `--fixed-fields` included).

### Wave 3: `export-xlsx` / `import-xlsx` / `from-xlsx`

- Same engines (`ExportTableToXLSX`, `ConvertXLSXToTDTP`,
  `ImportXLSXToTable`); `--sheet`/`--strategy` passed through.
- Fixed a real v1 bug on the way: `GenerateReference` keeps rows in the
  unexported `rawRows` fast-path, and `ExportTableToXLSX` merged empty
  `Data.Rows` — `--export-xlsx` wrote empty sheets. Fixed by
  materializing before the merge plus a defensive `MaterializeRows` in
  `xlsx.ToXLSX`; pinned by a round-trip test. v1 benefits identically.

### Wave 0: skeleton + `validate`

- New binary `cmd/tdtpcli_v2`: registry dispatcher, per-command flag
  sets, typed errors (`UsageError` → 2, `DataError` → 3, operational →
  1), middleware chain (panic recovery), `--quiet`/`--json` output
  contract, v1 compat shim mechanism, generated help.
- `validate`/`check`: thin wrapper over `pkg/validate` (moved out of
  `cmd/tdtp-validate` so both binaries share it — the old binary is now
  a thin frontend too). Human text is byte-identical between the two;
  exit codes differ by design (INVALID is 1 in v1, `DataError`/3 in v2).
- `inspect`/`show`, `test`/`check-integrity`: thin wrappers over the
  shared `commands.InspectFileTo`/`TestFileTo` (the v1 printers gained an
  `io.Writer` parameter; v1 entry points unchanged). Human text is
  byte-identical to v1 modulo the license banner and timing values;
  `--json` renders structured verdicts. Integrity failure is `DataError`
  (exit 3), unreadable input stays operational (exit 1).
- v1 `--inspect`/`--test` resolve through the compat shim with a
  deprecation notice.

### Wave 1b: `to-csv` / `to-xlsx`

- Shared `queryFlags` bundle (`--where`/`-w`, `--order-by`, `--limit`/`-l`,
  `--offset`, `--fields`) built on `pkg/cliquery` + `tdtql.SplitFieldList`
  exactly like v1; `pflag` replaces stdlib `flag` (interspersed flags and
  real shorthands — stdlib stops at the first positional). Produced files
  are byte-identical to v1 (proven with `fc`); stdout differs by the v1
  license banner only.
- `--help`/`-h` per command (exit 0); globals parsed by hand so the global
  set never chokes on command flags.

### Wave 1c: `to-html`

- Same pattern: `commands.ConvertTDTPToHTML` directly, `--open`/`--row`
  passed through (`--row` parsed exactly like v1, invalid silently 0),
  produced file byte-identical to v1 (`fc`: no differences).

### Wave 1d: `list` (+ config sharing)

- `pkg/cliconfig`: the v1 YAML model moved out of `package main`
  (unimportable) with zero behaviour change — v1 keeps working through
  type aliases in `cmd/tdtpcli/config_alias.go`, proven by v1's own
  tests. v2 reads the same file format; first command needing a DB.
- Shared `commands.MatchesPattern` exported (was unexported) so both
  contracts agree on glob/`%` filtering.
- Adapter registration mirrored (`drivers*.go` with the same tags).
- `--json` carries table/view names via a direct adapter query
  (`Output.JSONEnabled` skips it in text mode).
- `docs/CLI_V2.md`: philosophy, command checklist, exit codes.
