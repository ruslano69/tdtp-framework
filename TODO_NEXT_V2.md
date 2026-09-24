# TODO_NEXT_V2 — CLI v2.0 (`cmd/tdtpcli_v2`)

Strangler-fig rebuild of the CLI. The old `cmd/tdtpcli` (1150-line `main.go`,
flat global `Flags`, `flagscope.go` allowlists) is frozen — new work goes here.
Shared domain logic stays in `pkg/` (imported by both): a bug fixed there is
fixed in 1.XX and 2.0 at once. New behaviour lands in v2 first, backported
to v1 only on demand.

## Architecture (the whole bet)

- **A flag belongs to its command.** Each command declares its own
  `flag.FlagSet`; there is no global flag soup. `flagscope.go` /
  `warnUnusedFlags` die as a class — a foreign flag simply does not parse.
- **Thin lifecycle in `app.go`:** parse globals → match command →
  `command.Validate` → middleware (license → audit → timing) → `Run` →
  typed error → exit code. `main.go` stays ~50 lines.
- **Typed errors:** `UsageError` → exit 2, `DataError` → exit 3,
  IO/DB/network → exit 1. Commands return them; `main` maps to code and
  format (text vs `--json`).
- **DI container (`deps.go`):** config, adapters, storage, Mercury, output —
  built once, lazily (file-only commands never touch a DB).
- **Output contract:** `--quiet` / `--json` global. Humans read text,
  pipelines read JSON (`{file, valid, errors[]}`-shaped per command).

## Layout

```
cmd/tdtpcli_v2/
  main.go         # App.New + os.Exit, ~50 lines
  app.go          # registry, dispatch, generated help, middleware chain
  command.go      # Command interface + BaseCommand
  flags.go        # ONLY globals: --config, --quiet/--json
  errors.go       # UsageError / DataError / exit codes
  deps.go         # lazy service container
  middleware.go   # recover (license/audit/timing join later)
  compat.go       # v1 flat flags → v2 subcommands shim (one release, then delete)
  validate_cmd.go # wave 0: thin wrapper, proves the framework
  registry.go     # all Register() calls in one place
```

Binary name during transition: `tdtpcli_v2` (unambiguous in CI/logs).
Finale (wave 4): `rm -rf cmd/tdtpcli`, `git mv cmd/tdtpcli_v2 cmd/tdtpcli`.

## Waves

| Wave | Content | Risk |
|------|---------|------|
| 0 | skeleton + `validate` wrapper + `docs/CLI_V2.md` | zero — proves the framework |
| 1 | `inspect`, `test`, `list`, `to-csv`, `to-xlsx`, `to-html` | read-only |
| 2 | `export`, `from-*`, `import` | writes; sqlite e2e each |
| 3 | `pipeline`, brokers, `sync`, `enc*` | needs Mercury/infra in CI |
| 4 | delete `cmd/tdtpcli`, rename, merge changelogs | finale |

Porting rule: a command counts as ported when its `tests/cli/` suite passes
against the new binary unchanged (`TDTPCLI_BIN` swap — the mechanism exists).
Compat shim (`--to-csv` → `tdtp to-csv` + deprecation warning) lives one
release; shim output must be byte-identical to the native form.

## Shared-code rules

1. Any `pkg/` behaviour change must pass the suites of BOTH versions.
   v1 suites are the regression net — never weaken them.
2. New behaviour lands in v2 first; v1 gets backports only on demand.

## Changelog

`CHANGELOG_V2.md` in root, next to `CHANGELOG.md` (easy diff, same format,
`## [Unreleased]` → dated versions). Merged into `CHANGELOG.md` at wave 4.
Header note below explains why there are two.

---
*Teams: skeleton+validate first; commands port independently after that.*
