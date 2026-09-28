# Migrating to the `tdtpcli_v2` preview

The release keeps `tdtpcli` as the stable binary and publishes
`tdtpcli_v2-preview-*` separately. Run the preview against a copy of your
scripts and compare the output before switching production jobs. The preview
executable is named `tdtpcli_v2` in the examples below.

## Command syntax

Global options precede the command; command options follow it:

```powershell
# v1
tdtpcli --config db.yaml --export orders --limit 5

# v2
tdtpcli_v2 --config db.yaml export orders --limit 5
```

The old flat forms are accepted by the compatibility shim with a deprecation
notice on stderr. The shim is planned to remain for one release after v2
replaces the stable binary. Use `tdtpcli_v2 help <command>` for the command's
own flags; an unrelated flag now fails with exit code 2.

Use `tdtpcli_v2 --help` for the full command list. `tdtpcli_v2 --help export`,
`tdtpcli_v2 help export`, and `tdtpcli_v2 export --help` show the same
command help, including flag defaults and examples.

| v1 | v2 |
|---|---|
| `--create-config-pg`, `--create-config-mssql`, `--create-config-mysql`, `--create-config-sqlite` | `init-config postgres`, `init-config mssql`, `init-config mysql`, `init-config sqlite` |
| `--sync-incremental orders --tracking-field updated_at` | `sync-incremental orders --tracking-field updated_at` |
| `--process-request request.xml` | `process-request request.xml` |
| `--listen --config broker.yaml` | `--config broker.yaml listen` |
| `--map mapping.yaml --input broker://queue --listen` | `map mapping.yaml --input broker://queue --listen` |

Standalone `listen` consumes Kafka streaming packets using the topic in the
broker config. RabbitMQ continuous mapping uses `map --listen`. The new
`process-request` writes a TDTP **response** packet and checks the recipient
database adapter against the active license. The new `import --strict-schema`
enables strict schema handling for that run; `database.strict_schema` in the
config also works.

## Deliberate behavior changes

- `--json` writes a machine-readable verdict to stdout. Progress and notices
  go to stderr. `--quiet` suppresses progress while retaining useful results.
- Exit codes are 0 for success, 1 for an operational error, 2 for usage, and
  3 for invalid data. In particular, `validate` returns 3 instead of 1 for
  INVALID. An unreadable `diff` or `merge` input returns 1, while a readable
  but invalid input returns 3.
- `merge` takes input files as positional arguments; `merge --sort fields`
  gives deterministic row ordering. `to-json` and `validate` are new commands.
- `export` honors an explicitly supplied compression flag over the config;
  an omitted flag leaves the config setting in effect.
- `--enc13` is absent. Use supported `--enc` and integrity options for new
  workflows. `--translit` on XLSX export and conversion is applied in v2;
  v1 accepted it without transforming field names.

The preview retains current Community licensing behavior. Decisions about
enforcing the currently unused row limit, S3 gate, and pipeline source/ETL
gates are deferred until before 2.0; do not treat preview behavior as a
promise for the final release.

See [CLI v2](CLI_V2.md) for the command model and
[the v2 changelog](../CHANGELOG_V2.md) for implementation details.
