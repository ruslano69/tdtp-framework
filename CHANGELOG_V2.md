# Changelog — CLI v2

> Why two changelogs: v2 (`cmd/tdtpcli_v2`) is a strangler-fig rebuild
> alongside the frozen v1 (`cmd/tdtpcli`). Its history is tracked here
> until wave 4, when this file is merged into `CHANGELOG.md` and the old
> binary is deleted. Same format, same rules.

## [Unreleased]

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
- `docs/CLI_V2.md`: philosophy, command checklist, exit codes.
