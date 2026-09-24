# CLI v2 philosophy

One binary, many small commands. v1 (`cmd/tdtpcli`) grew a 1150-line
`main.go`, one flat `Flags` struct for everything, and `flagscope.go`
allowlists proving the system no longer knew which flags belonged to
which command. v2 inverts that:

- **A flag belongs to its command.** Each command owns a private
  `flag.FlagSet`. A foreign flag fails at parse time; there is nothing
  to allowlist.
- **Thin everything.** `main.go` is ~50 lines. Commands are wrappers
  over shared `pkg/` logic — never duplicated. A bug fixed in `pkg/`
  is fixed in 1.XX and 2.0 at once.
- **One lifecycle.** Parse globals → match command → `Validate` →
  middleware (recover; license/audit/timing join later) → `Run` →
  typed error → exit code. Adding a cross-cutting concern is one chain
  element, not edits in N branches.
- **Typed errors, stable codes.** `UsageError` → 2, `DataError` → 3
  ("the tool worked, the answer is no"), anything operational → 1.
- **Two audiences.** Humans read text; pipelines read `--json`.
  `--quiet` suppresses the human channel only.

## Adding a command (checklist)

1. New file `cmd/tdtpcli_v2/<name>_cmd.go`: struct embedding `Base`,
   `new<Name>Command()` filling name/aliases/help/flags, `Validate`
   (arity, required, mutual exclusion), `Run` (pure logic via `pkg/`,
   render via `out.Human`/`out.JSON`, typed errors out).
2. One line in `cmd/tdtpcli_v2/registry.go`.
3. Tests in `<name>_cmd_test.go` driving `App.Run` in-process (no
   subprocesses): happy path, `Validate` rejections, foreign-flag
   rejection is free from the framework.
4. Wave-1 rule for ports: the command counts as ported when its
   `tests/cli/` suite passes against `tdtpcli_v2` unchanged
   (`TDTPCLI_BIN` swap).
5. `CHANGELOG_V2.md` entry. Docs if the command deserves them.

## Compatibility during migration

v1 flat invocations (`--to-csv f.xml`) resolve through `compat.go` to
v2 subcommands with a deprecation notice on stderr; resolved output is
byte-identical to the native form. The shim lives one release.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | ok (including a VALID verdict) |
| 1 | operational failure: IO, DB, network, panic |
| 2 | usage: unknown command/flag, bad arity or combination |
| 3 | invalid data: the tool worked, the answer is no |
