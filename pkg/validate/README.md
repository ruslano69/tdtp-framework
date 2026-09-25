# tdtp-validate

Checks `.tdtp.xml` files for conformance to the protocol specification —
structure per `docs/tdtp.xsd`, semantics per the implementation.

```sh
go build -tags nokafka -o tdtp-validate ./cmd/tdtp-validate/

tdtp-validate file.tdtp.xml [more files...]
tdtp-validate -q file.tdtp.xml        # quiet: exit code only
tdtp-validate --max-mb 1024 big.xml   # raise the input size guard

tdtp-validate --stamp-integrity --output stamped.xml file.tdtp.xml
tdtp-validate --strip-integrity --output plain.xml file.tdtp.xml
```

Exit code is 0 when every file validates, 1 otherwise (including IO
errors). Example output:

```
orders.tdtp.xml: 1.4 table="orders" rows=8 cols=6 [integrity]
  VALID

broken.tdtp.xml: 1.0 table="orders" rows=0 cols=6
  x semantics: RecordsInPart mismatch: header declares 5 rows, <Data> contains 8
  INVALID: 1 error(s)
```

## What it checks

**Structure** — the real `docs/tdtp.xsd`, executed by a pure-Go XSD 1.0
engine (vendored fork of `github.com/jacoelho/xsd` v0.5.1 in
`third_party/xsd`: upstream needs Go ≥ 1.27, the fork is ported to the
repo's Go 1.25 — see its README). The schema is embedded via a generated
constant (`spec_xsd_gen.go`, refreshed by `go generate
./cmd/tdtp-validate/`); a test fails if the copy drifts from the file, so
the schema stays the single source of truth — nothing is transcribed by
hand. Presence, order, cardinality, attributes, enumerations, patterns and
unions all come from the file, with path/line/column diagnostics. A small
overlay adds the stricter-than-XSD rules (non-blank names, no structured
children under ciphertext, non-degenerate QueryContext).

**Semantics** (`validate.go`, what no XSD can express): row shape vs
Schema after decompression and compact/columnar expansion (in reverse
production order); RecordsInPart; duplicate field names; Dictionary
validity; checksum-without-compression; version-vs-features consistency
(compression → 1.2, compact → 1.3.1, integrity → 1.4, encryption → 1.5);
three-level xxh3 verification; QueryContext counter consistency and
OriginalQuery fields against the Schema. Encrypted (v1.5) sections are
reported, not judged — ciphertext is opaque.

**Deliberately not checked**: the pipe/backslash escaping inside `<R>`
beyond what row-shape counting exercises, hash canonicalization
byte-layouts, and pre-convention archives (compressed `version="1.0"`
files from before producers stamped versions fail the version check by
design — use `tdtpcli --test` if you only need those read, not judged).

Legacy whole-packet-encrypted (`.tdtp.enc`) files are opaque binary, not
XML — neither the XSD nor this tool can validate those.

## Mutations

Validation judges; two operations fix what a validator may fix without
touching user data. Both require exactly one input plus `--output`
(never in-place), are mutually exclusive, and refuse unsound input —
only the version-predates error is excused, since restoring the version
is the point; the result is strictly re-validated before writing.

- `--stamp-integrity`: three-level xxh3 via `packet.ComputeIntegrity`,
  version raised to 1.4. Local stamps only (no Mercury registration —
  same as `tdtpcli --integrity` without `--mercury-url`).
- `--strip-integrity`: all xxh3 stamps removed, version resolved down to
  the remaining features (compressed → 1.2, compact → 1.3.1, else 1.0).

Stamp order mirrors the export chain (integrity covers plaintext
row-major values): a compressed or columnar packet takes a round-trip
(decompress/expand → stamp → re-transpose/recompress, same algorithm at
its default level — blob bytes may differ, data is identical), reported
as `~` notes. Stamping an encrypted packet is refused: hashes must cover
plaintext, which is ciphertext there.
