# PureBasic + libtdtp.dll

`tdtp_example.pb` — reading a `.tdtp.xml` file from PureBasic through
`libtdtp.dll` (the same JSON-boundary C ABI the Python bindings drive via
`ctypes`, see `bindings/python/`). No PureBasic-specific build of the DLL is
needed — build it once per `bindings/python/DEVELOPER_GUIDE.md` (`make
build-lib-full` or the equivalent `go build -buildmode=c-shared` command) and
point `LoadTDTP()` at the resulting `libtdtp.dll`.

## Prerequisites

- PureBasic x64 (must match the DLL's architecture — a 32-bit PureBasic
  install will link against a 64-bit `libtdtp.dll` at the wrong ABI and
  either fail to resolve symbols or crash with undefined behavior).
- `libtdtp.dll` built with `-tags compress -buildmode=c-shared` (see
  `bindings/python/DEVELOPER_GUIDE.md`).

## Two gotchas this example gets right

Both were found by actually running the code, not by reading PureBasic's
docs — see the comment block at the top of `tdtp_example.pb` for the full
explanation:

1. **`ParseJSON(#JSON, Input$)` with `#PB_Any`** — the real handle comes back
   as the function's *return value*, not written back into the variable you
   passed in. Reusing that variable as if it were now valid crashes inside
   `JSONValue()`.
2. **Never call `CloseLibrary()` on `libtdtp.dll`.** Go's `c-shared` runtime
   keeps background goroutines (GC, sysmon) that don't support being
   unloaded — `FreeLibrary` crashes during `DLL_PROCESS_DETACH`. Leave the
   library loaded; the OS reclaims it at process exit, which is safe.

## What's not covered here

- **Static linking** (`-buildmode=c-archive` → `.a`): the archive links fine
  into a plain C program via gcc/MinGW, and PureBasic's own linker
  (`lld-link`) accepts the file format too — but the resulting PureBasic
  executable hangs on startup (Go runtime initialization appears
  incompatible with PureBasic's own runtime bootstrap on Windows). Dynamic
  loading (`OpenLibrary`/`GetFunction`, as in this example) is the
  confirmed-working path.
- **Row-level performance**: this example reads through the JSON boundary
  (`J_ReadFile`), which is adequate for typical files. For very large
  datasets from a native-C-ABI consumer, calling convention overhead is not
  the bottleneck it is for Python/ctypes — see `pkg/python/libtdtp`'s
  `D_*` (Direct/struct) API if you need to go lower-level, keeping in mind it
  is not currently PureBasic-verified.
