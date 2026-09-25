# Changelog — CLI v2

> Why two changelogs: v2 (`cmd/tdtpcli_v2`) is a strangler-fig rebuild
> alongside the frozen v1 (`cmd/tdtpcli`). Its history is tracked here
> until wave 4, when this file is merged into `CHANGELOG.md` and the old
> binary is deleted. Same format, same rules.

## [Unreleased]

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
