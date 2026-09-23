# third_party/xsd — vendored fork of github.com/jacoelho/xsd v0.5.1

Pure-Go XSD 1.0 validator (MIT, see LICENSE). Used by `cmd/tdtp-validate`
to execute `docs/tdtp.xsd` instead of transcribing it by hand.

## Why a fork, and why vendored

Upstream requires Go ≥ 1.27 (its `go.mod` says so); this repo builds on
Go 1.25 (CI matrix, `go.work`). The only 1.27-only language feature the
imported packages use is initialized promoted fields in composite
literals (`&compiler{simpleComponents: ...}` — one site), plus the
`errors.AsType` stdlib helper (~30 mechanical call sites). Both are
ported here; everything else is upstream verbatim.

No GitHub fork exists (no `gh`/tokens in this environment), so the fork
lives in-tree. Wired via `replace github.com/jacoelho/xsd =>
./third_party/xsd` in the root `go.mod` — imports keep the upstream path.

## What was taken, what was changed

Taken (runtime only, ~3 MB): root `*.go` (no tests), `xsderrors/`,
`internal/`, `LICENSE`. Left upstream: `tests/` (75 MB corpus), `docs/`,
`bin/`, `cmd/`, `.github/`, `Makefile`.

Changed vs v0.5.1 (all marked with `NOTE (fork)` comments):

- `go.mod`: `go 1.27.0` → `go 1.25.0` (`go.mod.upstream` keeps the original).
- `errors.AsType[T](err)` → `var x T` + `errors.As(err, &x)` (identical
  semantics for these shapes: first match in the chain).
- `internal/schema/compiler.go` (`newCompiler`): promoted-field literal
  spelled out per embedded struct — the only 1.27 language construct.

## Syncing with upstream

1. Copy the same file set from the new version over this directory
   (keep `go.mod` at 1.25.0 and this README).
2. Re-apply the `NOTE (fork)` patches above: `go build` under
   `GOTOOLCHAIN=go1.25.0` surfaces any new 1.27-isms as compile errors;
   fix them the same mechanical way.
3. Prove it: `GOTOOLCHAIN=go1.25.0 go test -tags nokafka -count=1
   ./cmd/tdtp-validate/ ./pkg/core/packet/` from the repo root — the
   validator suite asserts engine diagnostics verbatim, so behavior drift
   fails loudly.
