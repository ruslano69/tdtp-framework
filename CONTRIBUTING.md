# Contributing

Bug reports, feature suggestions and pull requests are all welcome. This file
is the practical part: how to build it, how to test it, and what CI will
insist on before a pull request can be merged.

## Before a large change

Open an issue first for anything that changes the packet format, adds a CLI
flag or config key, or adds a dependency. Not for permission — to avoid the
case where a finished branch turns out to duplicate something already solved
elsewhere in the tree, which is the usual way effort gets wasted here.

Small fixes need no announcement. Send the pull request.

## One directory is not open source

The repository is MIT, **except `pkg/license/`**, which implements commercial
licence verification and is proprietary — see [`pkg/license/LICENSE`](pkg/license/LICENSE).

Patches to `pkg/license/` cannot be merged under the same terms as the rest of
the tree. If a change genuinely needs one, open an issue describing the
problem and it will be handled separately. Everything else in the repository
is ordinary MIT and needs no paperwork: no CLA, no sign-off.

## Setting up

Go **1.25** or newer (`go.mod` requires it; `jackc/pgx/v5` pulls in the
GO-2026-5004 fix). Nothing else is needed to build.

The repository is a Go workspace (`go.work`) spanning the main module and
`xzmercury/`. Two consequences worth knowing before something confuses you:

- CI builds and tests with `GOWORK=off`, so dependencies resolve from `go.mod`
  alone — the way a consumer importing this library sees them. If a build
  works locally and fails in CI, try `GOWORK=off` first.
- The one exception is the integration job, which needs the workspace active
  because it launches xZMercury from the sibling module.

**If module downloads fail** — a blocked proxy, a missing zip —
`proxy.golang.org` redirects to `storage.googleapis.com`, which some networks
block. Use a proxy that serves packages directly:

```bash
export GOPROXY=https://goproxy.io
export GONOSUMDB='*'
```

## Building

```bash
go build ./...
```

Two build tags exist for environments that cannot have the dependency:

| Tag | Excludes |
|---|---|
| `nokafka` | kafka-go and its dependency tree |
| `nosqlite` | `modernc.org/sqlite` |

CI builds with `-tags nokafka`, so a change must compile with and without it.

On Windows, build the binaries one at a time rather than through `./...` —
several `cmd/` targets produce executables that lock each other during a
combined build:

```bash
go build -tags nokafka -o tdtpcli.exe ./cmd/tdtpcli/
```

## Testing

What CI runs, and the fastest way to reproduce it:

```bash
go test -tags nokafka -short -race ./...
```

`-race` matters more here than in an average Go project: the parser splits
rows across goroutines above a threshold, and the brokers, orchestrator and
retention loops all run concurrent work. Run it on anything touching those.

Integration tests are behind a build tag and need real services (PostgreSQL,
RabbitMQ). They do not run on feature branches — only on pushes and pull
requests to `main`:

```bash
go test -tags "integration nokafka" -timeout 120s ./tests/integration/ ./pkg/adapters/postgres/...
```

**Test databases already exist — do not write new ones.** `scripts/` has
creation scripts for every supported engine (`create_postgres_test_db.py`,
`create_mysql_test_db.py`, `create_test_db.py` for SQLite, and the benchmark
variants). The PostgreSQL credentials CI uses are in `create_postgres_test_db.py`
and in `.github/workflows/ci.yml`; they match on purpose.

## What CI enforces

A pull request will not go green without all four:

| Check | Command |
|---|---|
| Formatting | `gofmt -s -l .` must print nothing |
| Vet | `go vet ./...` |
| Lint | `golangci-lint run` — config in `.golangci.yml` |
| Tests | Go 1.25 and `stable`, `-short -race -tags nokafka` |

The linter set is `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`,
`misspell`, `unconvert`, `gocritic`, `prealloc`, `nilerr`, `bodyclose`.
`unparam` and `revive` are off deliberately — too many false positives on
context parameters and on stutter. Test files and `examples/` are excluded.

Note `gofmt -s`, with simplification, not plain `gofmt`.

## Branches and commits

Name branches `feature/<something>` or `fix/<something>`. That is not
cosmetic: `ci.yml` and `lint.yml` trigger on push only for `main`, `master`,
`develop`, `feature/**` and `fix/**`. A branch named anything else gets CI
only once a pull request is opened.

Commit messages follow Conventional Commits — `feat(scope):`, `fix(scope):`,
`refactor(scope):`, `docs(scope):`, `perf(scope):` — and the body carries the
reasoning. The convention in this repository is that a commit body explains
*why*, with the measurement or the failing case that drove it, rather than
restating the diff. `git log` is the best guide to the expected shape.

## Changelog

Add an entry to [`CHANGELOG.md`](CHANGELOG.md) under `## [Unreleased]` for
anything a user or operator would notice: a flag, a default, a format change,
a fixed bug, an operational consequence. Pure internal refactoring can be
noted as such, briefly, or skipped.

The entries in that file are prose with numbers in them, not one-line
summaries. Match what is there.

## Before writing new code

[`docs/DEVELOPER_GUIDE.md`](docs/DEVELOPER_GUIDE.md) opens with a list of
problems this codebase has already solved — type conversion, XML parsing, SQL
generation, adapters, XLSX, encryption. Reimplementing one of those is the
most common way a pull request gets rejected on something other than its
merits.

Two more places worth a look before starting:

- [`ROADMAP.md`](ROADMAP.md) and `TODO_NEXT.md` — whether it is already
  planned, or already done
- [`docs/SPECIFICATION.md`](docs/SPECIFICATION.md) — the protocol, if the
  change touches the packet format at all

## Documentation language

Documentation is English. Some older files are still Russian and are being
translated; new documentation should be written in English regardless of what
sits next to it. Internal Go comments explaining a decision are the exception
— those stay in whatever language they were written in.

## Security

Do not open a public issue for a vulnerability. See
[`SECURITY.md`](SECURITY.md) for the private reporting channel, the scope, and
what to expect.

## Conduct

Participation is covered by the [Code of Conduct](CODE_OF_CONDUCT.md)
(Contributor Covenant 2.1).
