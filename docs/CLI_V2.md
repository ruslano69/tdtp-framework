# CLI v2 philosophy

For scripts moving from v1, start with the [migration guide](CLI_V2_MIGRATION.md).

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
  middleware (recover → license → audit → resilience) → `Run` →
  typed error → exit code. Adding a cross-cutting concern is one chain
  element, not edits in N branches. Audit is init-on-entry / record-on-exit
  around `Run`, fed by an optional `Audited` method on the command.
- **Typed errors, stable codes.** `UsageError` → 2, `DataError` → 3
  ("the tool worked, the answer is no"), anything operational → 1.
- **Two audiences.** Humans read text; pipelines read `--json`.
  `--quiet` suppresses the human channel only. Notices that are neither
  data nor a result (the `License:` banner) go to stderr via
  `out.Notice`, so stdout stays clean for `to-json -o -`.
- **Globals:** `--config`, `--license`, `--quiet`, `--json` — before the
  command name. Everything else belongs to a command.

## Running the ported commands

Global options go before the command; command options go after it. The v1
flat-flag forms still work through the compatibility shim and print a
deprecation note to stderr.

### Help

`--help` lists every command, global option, examples and exit codes.
For one command, these forms show its synopsis, description, live flag
defaults and examples without opening a database:

```powershell
.\tdtpcli_v2.exe --help export
.\tdtpcli_v2.exe help export
.\tdtpcli_v2.exe export --help
```

Global options such as `--config` come before the command in examples.
An unknown help topic returns exit code 2.

### Oracle preview

`init-config oracle` creates a connection template for an Oracle service
such as `XEPDB1`. The v2 executable registers the Oracle adapter; it supports
`list`, `inspect-table`, `export`, `import`, incremental sync and pipeline
sources. Live integration tests pass on Oracle XE 18c and 21c. The intended
range is 18c–21c; 19c still needs its own live verification. See the
[adapter guide](../pkg/adapters/oracle/README.md) for config and limitations.

### Workflows

Save a workflow as `workflow.yaml`:

```yaml
name: check-packet
steps:
  - id: inspect
    command: inspect {{input}}
  - id: verify
    command: test {{input}}
    depends_on: [inspect]
```

Run it from the repository root:

```powershell
.\tdtpcli_v2.exe --quiet steps workflow.yaml @input=docs/samples/employees-plain.tdtp
```

The runner starts the same v2 executable for each step. Independent steps
run in parallel; `depends_on` controls ordering. Step commands may use v2
names or v1 flat flags during migration. `--quiet` reaches the children:
`inspect` still prints its schema report and `test` still prints its
integrity verdict. See [the workflow format](USER_GUIDE.md#--steps) for
`on_error`, retries, and more examples.

### Mappings

The mapping YAML owns the target connection. The input may be a local TDTP
file, an `s3://` object, or a `broker://` queue. Start with a local dry run:

```powershell
.\tdtpcli_v2.exe map mapping.yaml --input docs/samples/employees-plain.tdtp --dry-run
```

For a broker, one call without a mode flag processes one packet. `--drain`
processes packets until the queue has been idle for the given duration;
`--listen` keeps consuming until shutdown:

```powershell
.\tdtpcli_v2.exe --quiet map mapping.yaml --input broker://employees --drain 5s
.\tdtpcli_v2.exe map mapping.yaml --input broker://employees --listen
```

`--drain` and `--listen` require `input_source.broker` in the mapping YAML.
`--quiet` reports the target and total rows when the run ends. See
[the mapping guide](USER_GUIDE.md#--map) for the mapping format and mode
tradeoffs. `--mercury-url` overrides the URL from `--config` for encrypted
input.

### Output streams

| Mode | stdout | stderr |
|------|--------|--------|
| Default | Human progress and result | Notices and errors |
| `--quiet` | Results, including `inspect` and `test` reports and the `map` row total | Notices and errors |
| `--json` | One JSON verdict for `steps` or `map` | Progress, notices, and errors |

Use `--json` when another program needs to parse the result. A `map`
dry-run plan is written to stderr in that mode, leaving stdout as one JSON
document.

## Adding a command (checklist)

1. New file `cmd/tdtpcli_v2/<name>_cmd.go`: struct embedding `Base`,
   `new<Name>Command()` filling name/aliases/help/flags, `Validate`
   (arity, required, mutual exclusion), `Run` (pure logic via `pkg/`,
   render via `out.Human`/`out.JSON`, typed errors out).
   Database access through `--config` goes through `d.databaseConfig(name)` —
   the only CLI builder of `adapters.Config`, and the license adapter gate.
   `map` takes its target DSN from mapping YAML and checks that adapter
   before running the shared engine. A flag that
   needs a licensed feature (`--enc`, `--unsafe`) is declared by
   implementing `FeatureGated`. Tests enforce both.
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

Workflows use `tdtpcli_v2 steps workflow.yaml [@name=value...]` (or legacy
`--steps`). The runner starts this same v2 executable for every step. Existing
workflow YAML can keep legacy step commands such as `--test file.tdtp.xml`;
they pass through the compatibility shim. Native `sync-incremental`, standalone
Kafka `listen`, and `process-request` are available in the preview.

Mappings use `tdtpcli_v2 map mapping.yaml --input SOURCE`; the legacy
`--map` form is available through the shim. `--drain 5s` consumes a broker
queue until it stays idle for five seconds, while `--listen` runs until
shutdown. The target connection remains in the mapping YAML.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | ok (including a VALID verdict) |
| 1 | operational failure: IO, DB, network, panic |
| 2 | usage: unknown command/flag, bad arity or combination |
| 3 | invalid data: the tool worked, the answer is no |
