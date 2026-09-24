# Changelog — CLI v2

> Why two changelogs: v2 (`cmd/tdtpcli_v2`) is a strangler-fig rebuild
> alongside the frozen v1 (`cmd/tdtpcli`). Its history is tracked here
> until wave 4, when this file is merged into `CHANGELOG.md` and the old
> binary is deleted. Same format, same rules.

## [Unreleased]

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
