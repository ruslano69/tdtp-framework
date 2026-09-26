# TODO_NEXT_V2 — CLI v2.0 (`cmd/tdtpcli_v2`)

Strangler-fig rebuild of the CLI. The old `cmd/tdtpcli` (1150-line `main.go`,
flat global `Flags`, `flagscope.go` allowlists) is frozen — new work goes here.
Shared domain logic stays in `pkg/` (imported by both): a bug fixed there is
fixed in 1.XX and 2.0 at once. New behaviour lands in v2 first, backported
to v1 only on demand.

## Architecture (the whole bet)

- **A flag belongs to its command.** Each command declares its own
  `pflag.FlagSet`; there is no global flag soup. `flagscope.go` /
  `warnUnusedFlags` die as a class — a foreign flag simply does not parse.
- **Thin lifecycle in `app.go`:** parse globals → match command →
  `command.Validate` → middleware (license → audit → resilience) → `Run` →
  typed error → exit code. `main.go` stays small.
- **Typed errors:** `UsageError` → exit 2, `DataError` → exit 3,
  IO/DB/network → exit 1. Commands return them; `App` maps to code and
  format (text vs `--json`).
- **DI container (`deps.go`):** config, adapters, storage, Mercury,
  processors — built once, lazily (file-only commands never touch a DB).
- **Output contract:** `--quiet` / `--json` global. Humans read text,
  pipelines read JSON (`{file, valid, errors[]}`-shaped per command; App
  emits `{valid:false, error, exit_code}` for a failure the command did
  not render itself).

## Layout

```
cmd/tdtpcli_v2/
  main.go         # NewApp().Run + os.Exit
  app.go          # registry, dispatch, generated help, middleware chain
  command.go      # Command interface + Base, Output contract
  flags.go        # ONLY globals: --config, --license, --quiet/--json
  errors.go       # UsageError / DataError / exit codes, checkReadable
  deps.go         # service container; databaseConfig = the one gated adapter-config builder
  middleware.go   # recover → license; audit/resilience are wave 3.5
  compat.go       # v1 flat flags → v2 subcommands shim
  queryflags.go   # shared --where/--order-by/--limit/--offset/--fields bundle
  registry.go     # all Register() calls in one place
  <name>_cmd.go   # one file per command, thin over pkg/cli/commands
pkg/cli/commands/ # the engines BOTH binaries call
```

Binary name during transition: `tdtpcli_v2` (unambiguous in CI/logs).

**The engines live in `pkg/cli/commands`, not under either binary.** Until
2026-09-26 they were `cmd/tdtpcli/commands`, so the wave-4 step
`rm -rf cmd/tdtpcli` would have deleted the code v2 runs on. Moved with
`git mv`; the package name is unchanged (`commands`), only import paths
moved. Nothing in there calls `os.Exit` — keep it that way, it is a
library now.

## Status — 2026-09-26

**Ported** — the suite passes against `tdtpcli_v2` unchanged, or, where no
suite exists, output is proven identical to v1 by normalized comparison:
`validate` (new), `inspect`, `test`, `list` (+`--views` for
`--list-views`), `to-csv`, `to-xlsx`, `to-html`, `to-json` (new), `to-tdtp`,
`to-compact`, `export`, `import`, `export-xlsx`, `import-xlsx`, `from-xlsx`,
`pipeline`, `export-broker`, `import-broker`, `diff`, `merge`.

`tests/cli`: `test_sqlite.py` 122/122, `test_csv.py` 43/43,
`test_xlsx.py` 51/51 against both binaries. **Not yet run against v2:**
`test_postgres.py`, `test_mysql.py`, `test_mssql_msmq.py`, `test_kafka.py`,
`test_encryption.py`, `test_audit_database.py`. Each needs live
infrastructure, and several will fail today for the gaps listed below —
which is the point of running them: they are the checklist.

**Not ported:** `sync-incremental`, `map` (+`--listen`/`--drain`/
`--dry-run`), `listen`, `steps`, `inspect-table`, `process-request`,
`create-config-{pg,mssql,sqlite,mysql}`, `--version`.

## Remaining work, in order

The order is by what blocks what, not by size. Wave 3.5 comes first because
every command ported before it has to be revisited once it lands; the later
a middleware arrives, the more commands it has to be retrofitted into.

### Wave 3.5 — cross-cutting concerns (blocks shipping v2 to anyone)

v1 wraps every command in the same four things inside `main.go`. v2 has
none of them yet, and one of them is a hole rather than a missing feature.

1. **~~License — P0, a bypass~~ — done 2026-09-26.** `licenseMiddleware`
   resolves `tdtp.lic` exactly as v1 (`--license`, `TDTP_LICENSE`,
   `./tdtp.lic`, Community); a command asks for features through
   `FeatureGated` (`pipeline`: `enc`, `unsafe`; `export-broker`: `enc`);
   the adapter gate sits in `Deps.databaseConfig`, the only place that
   builds `adapters.Config` — `TestAdapterConfigBuiltOnlyInDeps` parses the
   package to hold that, and `TestLicense_DBCommandTableIsComplete` derives
   the DB commands from the source, so a new one cannot skip the refusal
   test. Same refusal texts (`commands.CheckFeature`/`CheckAdapter`), exit
   1 as in v1. Deliberate differences: the adapter is gated where a
   database is used, not whenever a config is loaded (v1 refused
   `--config pg.yaml --to-csv f.xml` on Community); the banner is a
   stderr notice, since v2's stdout is the data channel.

   **Found on the way, open in BOTH CLIs — product decisions, not bugs to
   patch quietly:**
   - `GateRowCount` is never called. The Community floor's 50 000-row
     cap (`license.Community`, and the header of `license_gate.go`) is
     enforced nowhere.
   - The `s3` feature is never gated: S3 input/output works on Community.
   - Pipeline **source** adapters are never gated (`pipeline` loads no
     database config), so a Community pipeline reads PostgreSQL. The
     `etl` feature and `PipelineLimit` are unused as well.
   - `--license /typo` silently falls back to Community (`license.Load`
     treats a missing file as "no license"). A downgrade, not a bypass,
     but it surfaces as a confusing "not licensed" later.

   Enforcing any of the first three changes what existing Community users
   can do today — decide per item, then land it in v2 first.
2. **Audit.** v1 sets an `audit.Operation` per branch and threads
   `commands.WithOpMetrics(ctx)` so engines report row counts back. As a
   middleware plus an optional `AuditOp() audit.Operation` on the command.
   `test_audit_database.py` is the acceptance suite.
3. **Resilience.** v1 runs each engine call through
   `prodFeatures.ExecuteWithResilience` (circuit breaker + retry from
   config). Middleware, config-driven, off unless configured — as in v1.
4. **A real `Deps`.** Started with the license work: `databaseConfig`
   is now the single adapter-config builder (gated, and it carries
   `database.strict_schema`, which the two old builders dropped). Still
   to come: parse the config once per run instead of per call;
   `StorageConfig()` for `s3://`; `Processors()` for mask/validate/
   normalize. File-only commands still never touch it.
5. **Build parity.** `drivers_s3.go` (`nos3` tag) is missing, so v2 has no
   S3 driver registered at all; the `production` tag (`pipeline_prod.go`)
   is untested for v2. CI runs v2's tests but `release.yml` does not ship
   the binary — decide when it starts to.

### Wave 3.6 — close the flag gaps in ported commands

Each of these is accepted by v1 and absent in v2. A v1 script using one
fails to parse under the shim — loudly, at least.

| Command | Missing in v2 | Needs |
|---|---|---|
| `export` | `--mask`, `--validate`, `--normalize`, `--enc`, `--enc13`, `--mercury-caller`, `s3://` output | 3.5 items 1, 4 |
| `export-broker` | `--mask`, `--validate`, `--normalize`, `--mercury-caller`, `--batch`, `--hash` | 3.5 item 4 |
| `export-xlsx` | `--translit`, `--mask`, `--validate`, `--normalize` | 3.5 item 4 |
| `import` | `--strict-schema`, `s3://` input | 3.5 item 4 |
| `to-csv`, `to-xlsx` | `--translit`; `s3://` input/output for `to-xlsx` | — / item 4 |
| `pipeline` | `--mask`, `--validate`, `--normalize` | 3.5 item 4 |

Processors are one bundle, like `queryFlags`: `addProcessorFlags(fs, &p)`
once, not the same three flags pasted into five commands.

### Wave 3.7 — the commands not yet ported

Lowest risk first; each lands with its suite run against v2.

| Command | Why here | Acceptance |
|---|---|---|
| `inspect-table` | read-only, one engine call | normalized v1 comparison |
| `version`, `init-config <db>` | trivial; the four `create-config-*` become one command with a positional | byte-identical sample files |
| `steps` | spawns sub-processes — must spawn **v2**, not whatever `tdtpcli` is on PATH | `pkg/workflow` tests + a v2 e2e |
| `map` (+`--dry-run`, `--drain`) | file/S3/broker input, its own target DSN | `map_test.go` engine tests + broker e2e (`TDTP_BROKER_TEST=1`) |
| `listen`, `map --listen` | long-running daemon: signals, NACK/requeue, graceful stop — test the shutdown path, not only the happy path | RabbitMQ e2e |
| `sync-incremental` | checkpoint file + broker target | `test_postgres.py` sync group |
| `process-request` | excluded from lint in `.golangci.yml` — read it before porting | — |

### Wave 4 — finale (every box must hold)

- [ ] 3.5, 3.6 and 3.7 done; every `tests/cli` suite green against
      `tdtpcli_v2` on live infrastructure.
- [ ] Migration guide: the deliberate differences from v1 in one list —
      exit codes (`validate` INVALID 1 → 3; `diff`/`merge` unreadable
      input → 1), `export` flag-over-config means "given", not
      "off-default", `merge` takes positionals, `--sort`/`to-json`/
      `validate` are new. `CHANGELOG_V2.md` records each; collect them.
- [ ] `rm -rf cmd/tdtpcli` (safe since the engines moved to `pkg/`),
      `git mv cmd/tdtpcli_v2 cmd/tdtpcli`, binary name `tdtpcli`.
- [ ] Update everything that names the binary or its path: `ci.yml`,
      `release.yml`, `deployments/docker/Dockerfile.worker`, the
      `tests/cli` defaults, `docs/`, `CLAUDE.md` (the `flagscope.go`
      section becomes history — the class of bug it guarded is gone).
- [ ] Merge `CHANGELOG_V2.md` into `CHANGELOG.md` as 2.0.0.
- [ ] **The compat shim stays one release after the rename**, not until
      it: the day `tdtpcli` becomes v2 is the day old scripts start going
      through it. Delete `compat.go` in 2.1.

## Porting rule

A command counts as ported when its `tests/cli/` suite passes against the
new binary unchanged (`TDTPCLI_BIN` swap). Where no suite exists: a
normalized comparison with v1 output (MessageID/Timestamp masked) plus
in-process tests through `App.Run`. The shim's resolved output must be
byte-identical to the native form.

**Test the streams, not only the exit code.** The first review of v2
found two framework promises broken while their tests passed: "a foreign
flag fails at parse time" (exit 2 — with an empty stderr) and "`--json`
carries errors in-band" (exit 1 — with an empty stdout). Both tests
checked the code alone.

## Shared-code rules

1. Any `pkg/` behaviour change must pass the suites of BOTH versions.
   v1 suites are the regression net — never weaken them.
2. New behaviour lands in v2 first; v1 gets backports only on demand.

## Changelog

`CHANGELOG_V2.md` in root, next to `CHANGELOG.md` (easy diff, same format,
`## [Unreleased]` → dated versions). Merged into `CHANGELOG.md` at wave 4.
The header note there explains why there are two.
