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

## Status — 2026-09-28

**Ported** — the suite passes against `tdtpcli_v2` unchanged, or, where no
suite exists, output is proven identical to v1 by normalized comparison:
`validate` (new), `inspect`, `test`, `list` (+`--views` for
`--list-views`), `to-csv`, `to-xlsx`, `to-html`, `to-json` (new), `to-tdtp`,
`to-compact`, `export`, `import`, `export-xlsx`, `import-xlsx`, `from-xlsx`,
`pipeline`, `export-broker`, `import-broker`, `diff`, `merge`,
`inspect-table`, `version`, `init-config`, `steps`, `map` (including
`--dry-run`, `--drain`, and `--listen`), `sync-incremental`, standalone Kafka
`listen`, and `process-request`.

`tests/cli`: `test_sqlite.py` 122/122, `test_csv.py` 43/43,
`test_xlsx.py` 51/51 against both binaries. Live runs 2026-09-27, also
green on both: `test_mssql_msmq.py` 23/23 (docker SQL 2022 + local MSMQ,
direct-auth fallback, dev license), `test_encryption.py` 15/15
(external xzMercury `--dev`, real HMAC), `test_audit_database.py` 8/8.
Live Docker runs against v2 on 2026-09-28: `test_postgres.py` 88/88,
`test_mysql.py` 58/58, `test_kafka.py` 13/13. The Kafka suite had one
transient empty-queue result on its first run; its second full run and the
standalone listener end-to-end test passed. The four legacy `create-config-*`
flags are replaced by `init-config <db>`; `--version` is supported.

### Scope check: porting the CLI is not the full 2.0 roadmap

The status above means the existing CLI commands have been ported. It does
not close the product work in [`ROADMAP.md`](ROADMAP.md) → **Next**:

- [ ] **Oracle adapter:** implemented for v2 with `go-ora/v2`; live 18c and 21c
      export/import, pipeline query and incremental tests pass. Verify 19c and
      production connection settings before declaring release support.
- [ ] **Streaming export/import:** `export --stream` is a beta, bounded-memory
      export to local files; it finalizes parts after the total is known.
      A live `TotalParts=0` CLI transport and `--import-stream` are absent.
- [ ] **Parallel import workers:** export parallelizes serialization, but
      multipart import still processes parts sequentially.
- [ ] **Schema migration:** `import --strict-schema` preserves selected source
      schema details; it does not detect all drift before import or provide
      explicit, audited additive `ALTER TABLE` application.
      Scope conflict to resolve: `README.md` lists add/drop/type changes,
      whereas `ROADMAP.md` allows automatic additive `ADD COLUMN` only.
- [ ] **Orchestrator scenario integrity:** admin approval by content SHA-256
      exists. Execution checks the loaded in-memory YAML, not the scenario
      file reread from disk, and jobs do not record the executed scenario hash.

[`TODO_NEXT.md`](TODO_NEXT.md) → **Behind the freeze — 2.0** also collects
candidate capabilities (pipeline database output, validation before import,
subtype enforcement, column constraints, failover, orchestrator retries,
and others). That section explicitly says they are not scheduled;
decide their release scope separately. Do not call 2.0 feature-complete from
the CLI port's test results alone.

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
   the DB commands from the source, so a new `--config` database command
   cannot skip the refusal test. `map` gets its target from mapping YAML;
   its separate adapter gate has a dedicated refusal test. Same refusal
   texts (`commands.CheckFeature`/`CheckAdapter`), exit
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

   **Preview decision 2026-09-28:** keep current Community behavior. Decide
   row, S3, and pipeline source/ETL enforcement separately before 2.0.
2. **~~Audit~~ — done 2026-09-27.** `auditMiddleware` + `Audited`
   (`AuditInfo(d, args)` with v1's operation + metadata; `test`,
   `inspect`, `validate` stay out like v1). Both sinks from one entry;
   DB opener shared as `pkg/audit.OpenDatabaseSink`.
   `test_audit_database.py` 8/8 on both binaries.
3. **~~Resilience~~ — done 2026-09-27.** `resilienceMiddleware`,
   last in the chain so audit records the post-retry outcome: breaker
   inside, retry outside, per-run instances from the config's
   `resilience:` section, off unless configured. Breaker transitions go
   to `out.Notice` (v1 prints them always). Attempt-count unit tests,
   `-race` clean, `test_sqlite.py` 122/122 on the passthrough.
4. **~~A real `Deps`~~ — done 2026-09-27.** Started with the license
   work: `databaseConfig` is now the single adapter-config builder
   (gated, and it carries `database.strict_schema`, which the two old
   builders dropped). `StorageConfig()` for `s3://` (proven by T8 live
   against weed). Config parses once per run (`loadConfig` cached, same
   error texts). `Deps.processors()`: flags first, config-file
   `processors:` section as fallback per type (dead in v1 — parsed,
   never read), unknown rule types fail loud; five commands rewired.
   File-only commands still never touch the database half.
5. **Build parity.** `drivers_s3.go` (`nos3` tag) done 2026-09-27. The
   `production` tag verified for v2 2026-09-27 (suite green under
   `-tags "production nokafka"`, `--enc-dev` absent from prod help).
   `release.yml` now builds `tdtpcli_v2-preview-*` alongside v1 for the
   same five platforms starting with the next tag. Full switch remains at
   the wave-4 rename.

### Wave 3.6 — close the flag gaps in ported commands

Each of these is accepted by v1 and absent in v2. A v1 script using one
fails to parse under the shim — loudly, at least.

| Command | Missing in v2 | Needs |
|---|---|---|
| `export` | (~~`--mercury-caller` done 2026-09-27; `--enc` done, `--enc13` dropped, `s3://` output done~~) | 3.5 item 4 |
| `export-broker` | (~~`--mercury-caller` done 2026-09-27, honored for real; `--batch`, `--hash` done, v1-identical no-ops; `--integrity` done, live MSMQ+Mercury~~) | — |
| `export-xlsx` | (~~`--translit` done 2026-09-27, implemented for real — v1 ignores it~~) | — |
| `import` | (~~`--strict-schema` done 2026-09-28; `s3://` input done 2026-09-27~~) | 3.5 item 4 |
| `to-csv`, `to-xlsx` | (~~`--translit` done 2026-09-27, implemented for real; `s3://` for `to-xlsx` done, live against weed~~) | — / item 4 |

**~~Processors~~ — done 2026-09-26.** `--mask`/`--validate`/`--normalize`
are one bundle (`processorflags.go`) on `export`, `export-broker`,
`export-xlsx`, `import`, `import-xlsx`. Not on `pipeline`: its processors
live in the YAML, and v1's `--pipeline` accepted these flags and ignored
them. Porting them found three bugs in the shared chain, all fixed for v1
too — see `CHANGELOG.md`.

### Wave 3.7 — the commands not yet ported

Lowest risk first; each lands with its suite run against v2.

| Command | Why here | Acceptance |
|---|---|---|
| ~~`inspect-table` — done 2026-09-27~~ | read-only, one engine call | byte-identical reports on sqlite and mssql |
| ~~`version`, `init-config <db>` — done 2026-09-27~~ | trivial; the four `create-config-*` become one command with a positional | byte-identical sample files, unit-pinned |
| ~~`steps` — done 2026-09-27~~ | `os.Executable()` spawns v2; native and `--steps` shim | v2 e2e: v2-only `to-json` child, dependency, `skip`, quiet and JSON streams |
| ~~`map` (+`--dry-run`, `--drain`, `--listen`) — done 2026-09-27~~ | file/S3/broker input, its own target DSN | SQLite and live RabbitMQ e2e (`TDTP_BROKER_TEST=1`), per-message audit, shutdown/ACK/NACK tests |
| ~~`listen` (standalone) — done 2026-09-28~~ | long-running Kafka consumer: context cancellation, offset commit after import | live Kafka to SQLite e2e; RabbitMQ daemon is `map --listen` |
| ~~`sync-incremental` — done 2026-09-28~~ | checkpoint file + broker target | PostgreSQL suite and live Kafka sync-to-broker e2e |
| ~~`process-request` — done 2026-09-28~~ | recipient config and response packet | in-process roundtrip, recipient license gate and path validation |

### Wave 4 — finale (every box must hold)

- [ ] 3.5, 3.6 and 3.7 done; every `tests/cli` suite green against
      `tdtpcli_v2` on live infrastructure (PostgreSQL, MySQL, Kafka complete;
      rerun the remaining live suites before stable-name switch).
- [x] Migration guide: the deliberate differences from v1 in one list —
      exit codes (`validate` INVALID 1 → 3; `diff`/`merge` unreadable
      input → 1), `export` flag-over-config means "given", not
      "off-default", `merge` takes positionals, `--sort`/`to-json`/
      `validate` are new. See `docs/CLI_V2_MIGRATION.md`.
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
