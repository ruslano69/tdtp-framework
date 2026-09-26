# Changelog — CLI v2

> Why two changelogs: v2 (`cmd/tdtpcli_v2`) is a strangler-fig rebuild
> alongside the frozen v1 (`cmd/tdtpcli`). Its history is tracked here
> until wave 4, when this file is merged into `CHANGELOG.md` and the old
> binary is deleted. Same format, same rules.

## [Unreleased]

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
