# Changelog

All notable changes to tdtp-framework are documented in this file.

## [Unreleased]

### Eight hand-written copies of "decompress a TDTP packet" collapsed into one

Found while chasing the columnar bugs below: the same ~15-line sequence
(validate checksum, decompress, expand a columnar layout, verify
`RecordsInPart`) had been copied by hand into `pkg/etl`, the CLI's `--test`
and its export/import paths, both Python C-ABI readers, `pkg/xlsx`, and
`cmd/tdtp-svg` — eight independent rewrites of one another. Two had drifted
into real bugs: `pkg/xlsx/converter.go`'s `ToXLSX` and `cmd/tdtp-svg`'s
`decompressDataSection` never expanded a columnar layout at all, so
`--to-xlsx`/`--export-xlsx` or `tdtp-svg` on a `--columnar --compress` file
would have hit the same silent corruption fixed in the Python bindings
(previous entry) — column data returned as if it were rows, no error. A
third, `pkg/etl/loader.go`, hardcoded zstd regardless of what the packet
actually declared, so a pipeline reading a kanzi-compressed source would
have failed outright.

`processors.DecompressPacket` (new, `pkg/processors/decompress_packet.go`)
is now the one implementation; all eight call sites use it. Also merged
`DecompressDataForTdtpAlgo` into `DecompressDataForTdtpWithAlgo` — the two
were byte-for-byte identical.

### `--columnar` had no pipeline equivalent, and its compressed form couldn't be read back by one either

`output.tdtp.columnar` is new — the YAML config had no way to ask for the
column-major layout at all. Wired the same way `--export --columnar` already
does it: `SetColumnarLayout` before splitting into parts, and an explicit
`EnsureColumnar` before compression, since compression happens outside the
writer's own materialization step.

Two bugs surfaced while wiring it. `compressDataPacket` in `pkg/etl` replaced
`Data` with a fresh struct literal after compressing, dropping `Layout` along
with it — a columnar pipeline export would have written a compressed packet
with no `layout="columns"` attribute at all. And `pkg/etl/loader.go`'s own
decompression routine, a third hand-written copy of the same decompress-then-
verify logic already fixed twice elsewhere, never called `ExpandColumnarRows`
— a pipeline reading its own `--columnar --compress` output as a source
failed on the row-count check that follows.

Both fixed. `TestExporter_TDTP_Columnar` covers plain and compressed through
the real writer and the real `pkg/etl` reader; mutation-tested against all
three fixes.

### Changed — `pkg/etl`'s TDTP writer orders its steps from `pkg/transform` now, not a hand-coded chain

`exportToTDTP` used to be its own manually ordered `if` sequence, a second
copy of the ordering `cmd/tdtpcli/commands/export.go` already gets from
`transform.Plan`. No behavior change — same steps, same order, same
error-packet-on-integrity-failure path (a new test pins that one
specifically) — just one source of truth for the order instead of two that
had to be kept in sync by hand.

## [1.26.0] — 2026-08-31

### `LoadData` stops re-boxing a value the slot already holds

`convertValue` boxes a decoded value into `any` on every call. A reused slot now
skips the rebox when the value hasn't changed since the last row.

### PostgreSQL `ReadAllRows` stops going through `rows.Values()`

`rows.Values()` allocates a fresh `[]any` per row and boxes every decoded value
into it. `ReadAllRows` now scans into reused typed `pgtype.*` destinations
instead, converting each back to the same shape `Values()` used to produce so
the rest of the read pipeline is unchanged. Scoped to `ReadAllRows` only, where
the adapter builds its own query against a schema it just read; `ReadRowsWithSQL`
(caller-built queries) is unaffected. Excludes `REAL`/`FLOAT`/`DOUBLE`, and
`TEXT` columns carrying a Postgres-specific subtype (uuid/json/...).

### The workspace bypasses database/sql on both the write and the read

`LoadData` and `ExecuteSQL` now go straight to the driver — prepared
statements, `[]driver.NamedValue`, `driver.Rows` — instead of through
`database/sql`, removing bookkeeping and a per-cell type conversion. Falls back
to the generic path when a driver doesn't offer those interfaces; the fallback
is only safe before the first write or read.

### Two pipeline sources on one SQLite file could not open it

Two sources pointing at the same file, opened concurrently by per-source
goroutines, raced on WAL mode and failed with "database is locked". Fixed by
pooling one connection per (type, DSN, settings) and moving `busy_timeout` into
the DSN, since it can't be set as a pragma before the ping that triggers the
conflict.

### `--stream` produced parts larger than `--packet-size` asked for

The streaming partitioner measured a NULL as it arrives from the adapter rather
than as it's written (`[NULL]`), so streamed parts could run over the requested
size on NULL-heavy data. The estimator now measures a value as it will be
written; the buffered path was already correct.

### Opening a pipeline in tdtp-xray and saving it dropped ten of its thirteen output settings

tdtp-xray declared its own three-field output config instead of the real
thirteen-field one, so loading and re-saving a pipeline silently dropped most
of its settings. The local copy is now a type alias to the real struct.

### `--offset` did not work on SQL Server below compatibility level 110

MSSQL paging used `OFFSET/FETCH` (2012+ syntax), which fails below
compatibility level 110 and silently fell back to reading the whole table into
memory. Paging is now built on `ROW_NUMBER()`, which older compatibility levels
accept.

### `--offset` without `--order-by` now says the order is undefined

Paging without an explicit sort has no defined meaning across pages. The
export now prints a notice and `--help` recommends pairing the two.

### A NULL date in a pipeline result killed the whole query

`Workspace.ExecuteSQL` bound date columns to `*string` to dodge the driver's
own parsing, but a NULL in such a column failed the whole query — reproducible
through an ordinary `LEFT JOIN` with unmatched rows. Everything now scans into
`any`, and NULL becomes the empty string.

### Workspace load: multi-row INSERT

`LoadData` now batches rows into a multi-row `INSERT` instead of one `Exec` per
row, sized to roughly 60 placeholders per statement — a larger batch measured
worse.

### A flag that no command reads now says so

Several flags were accepted by commands that never read them (`--columnar` on
the query path, `--packet-size` on a file export, `--limit` on `--to-compact`,
`--fields` on `--to-html`/`--to-xlsx`), each exiting zero having silently done
something other than what was asked. A new check compares the flags the user
typed against a declared command→flags table and prints a notice for anything
the command doesn't claim.

### `--offset` without `--limit` fell back to reading the whole table

`OFFSET` without `LIMIT` is a syntax error in SQLite and MySQL, so the pushdown
failed and the export silently loaded the whole table into memory. The
generator now emits a sentinel `LIMIT` when only an offset is given; MSSQL
strips it since it expresses the same thing natively.

### `--to-html` and `--to-xlsx` ignored `--fields`

`--fields` worked reading from a table but was silently dropped by both
rendering commands when reading from a packet. Both now project through the
same helper `--to-tdtp` uses.

### `--limit -N` returned the first N rows from a database, not the last N

Tail mode worked correctly against a packet on disk but returned the first N
when reading from a database, because the query generator degraded silently
without an `ORDER BY`. A default sort key is now supplied when tail mode has
none, with a notice naming it.

### `--to-compact` ignored `--where`, `--limit` and `--offset` entirely

Unlike its siblings, `--to-compact` never applied the query at all. The filter
now runs before fixed-field detection, which has to see the filtered rows to
know what's actually constant.

### `--import` and `--map` take no row window, and now say so

Both accept `--limit` on the command line — flags are global — and have never
applied it, by design: they write the packet through as-is. Now documented in
`--help` and pinned by a test.

### `--packet-size` was accepted everywhere and honoured almost nowhere

It only worked for MSSQL exporting to a broker — the file path never applied
it, and the broker path worked only through a type assertion one adapter
satisfied. All adapters now implement the setter; one that doesn't is refused
rather than silently ignored.

### Known: the two partitioners disagree on NULL-bearing data

A streamed export can fit slightly more rows per part than a buffered one on
NULL-heavy tables, because the streaming path measures a row's size before
markers are applied. No data is affected — same rows, same order — but the two
paths aren't guaranteed to produce byte-identical files.

### Security — a configured field_masker exported unmasked data

A pipeline with `field_masker` (or `field_normalizer`/`field_validator`) in
`processors.pre_export` wrote the original values anyway: the processors ran
and produced correct output, but the output was discarded because the row
setter left an internal fast-path field pointing at the originals. Anyone
relying on `pre_export` masking should treat past exports as unmasked.

### Security — a v1.4 packet could lie about how many rows it carried

Nothing checked the declared row count against the actual rows from v1.4 on.
Now checked at every version, wherever the rows are readable; a v1.4+ packet
whose header disagrees with its rows is refused.

### Fixed — `--enc-dev` failed in the situation it exists for

The dev-mode key bypass required a second, undocumented environment variable
to actually take effect, so a production server enabled it, failed, and
degraded to an error packet. The exemption now travels with the flag alone.

### Fixed — `compress: true` was dropped when a pipeline was saved from tdtp-xray

`compress` and `compression` are two YAML spellings of one setting; tdtp-xray
read only the second, on load and on save. The loader now folds one into the
other, matching the exporter.

### Fixed — RecordsInPart lied after a filtering processor

A filter that removed rows left the header still declaring the original count.
Fixed by the same row-setter change above.

### Added — `--columnar`, a column-major Data layout

Puts a whole column in each row element instead of a row, which compresses
considerably better with zstd (kanzi gains little, since it already groups
values regardless of stream order). Off by default — the attribute changes
what a row element means, and a reader that doesn't know it will misread the
data. Readers in this version understand it and expand it transparently.

### Added — compression without the join copy

The old compression path joined every value into one big string first, purely
to hand it to the codec. A new entry point streams the pieces in directly
instead.

### Added — `pkg/transform`: the order of packet transformations is declared, not implied

The order transformations run in used to live in the order function calls
happened to be written, plus comments. It's now a declared registry with
explicit ordering and incompatibility rules, so the sequence can't drift by
moving a line, and an incompatible combination is refused up front instead of
corrupting data silently.

### Fixed — the CLI test suites were verifying a binary eleven versions behind

All suites defaulted to a fixed binary path and never checked it was current.
They now refuse a binary that isn't built from the current tree.

### Changed — the benchmark database generator

Rewritten to build the fixture in one transaction instead of many, and to stop
silently swallowing insertion errors. A seed option makes a run reproducible
byte for byte; the fixture now includes proper date-typed columns rather than
dates stored as text.

### Tests

CLI suites grew substantially this branch — SQLite gained coverage for date
types, `--packet-size`, and `--limit`/`--offset` correctness; PostgreSQL gained
coverage for its own date/numeric types and the columnar/transformation
matrix. `pkg/etl` gained regression tests for the NULL-date failure and
multi-row insert alignment. Remaining coverage gaps are tracked in
`TODO_NEXT.md`.

---

## [1.25.1] — 2026-08-22

Version handling put in order: three defects that each silently switched an
integrity check off instead of failing loudly.

### Fixed — protocol versions were compared as strings

Version comparison was lexical, which breaks past a two-digit component — a
1.10 packet would sort as older than 1.3.1 and be silently treated as pre-1.4,
skipping the integrity checks it requires. Now compared as numbers; the key
server carried the identical bug and is fixed the same way.

### Fixed — the version attribute was never validated

Any non-empty string passed, and garbage then sorted as "newer than any known
version," moving a packet onto the integrity-required path without saying so.
Malformed values are now refused; a well-formed but unknown version is still
accepted, since a reader is supposed to degrade gracefully on features it
doesn't recognize.

### Changed — a packet whose features outrun its version is read, with a warning

Compression never bumped the protocol version, so years of archives carry
compressed packets declaring `1.0`. They remain readable — the reader now
warns, naming the feature and the version that introduced it, rather than
refusing them.

### Fixed — a v1.4+ packet with no hashes was reported as integrity-verified

Verification only ran when hashes were present, so an unstamped v1.4+ packet
skipped the check entirely and was reported as OK. Such a packet is now
refused, since v1.4 has no feature other than those hashes.

---

## [1.25.0] — 2026-08-22

### Fixed — a large PostgreSQL `NUMERIC` exported in scientific notation

Large values were formatted in a style that switches to exponent notation, and
the scale check downstream then misread the exponent as a huge fractional part
and gave up, writing the exponent form to the packet untouched. Fixed by
formatting consistently in plain decimal; the same bug existed in SQLite for a
`DECIMAL` backed by `REAL` storage. MySQL and MSSQL were not affected — their
drivers hand back `DECIMAL` as text already.

### Fixed — SQLite export aborted on the first NULL date

A nullable date/datetime/timestamp column with any empty cell failed the whole
export, because those columns were bound to a type that can't hold NULL. Every
column now scans generically.

### Fixed — sub-second precision and DATE type lost on import

Export carried fractional seconds through; import threw them away again, and a
plain date column was written back as a timestamp at midnight. Both are now
preserved — for MySQL, import writes exactly as many fractional digits as the
column declares, since an unspecified-precision column rounds rather than
truncates what it can't store.

### Fixed — PostgreSQL `TIME` columns could not round-trip

Formatted with a pattern that dropped microseconds, and rendered through the
wrong branch once the value's subtype was ignored — import failed outright.
Now carries its subtype through and renders correctly.

### Fixed — PostgreSQL `infinity` never became a marker

An infinite date came back from the driver in a shape nothing handled, fell
through to a generic formatter, and printed in a spelling the marker detector
doesn't recognize. Both read and write now use the marker path.

### Fixed — MySQL `TIME` columns were rejected outright

Schema inspection refused any table with a TIME column. MySQL TIME is a signed
duration, not a time of day, so it now maps to text with a "time" subtype and
travels verbatim.

### Changed — PostgreSQL export skips the round-trip for numbers and dates

PostgreSQL used to pass an empty field descriptor into the value converter, so
it couldn't tell a DATE from a TIMESTAMP and formatted both the same way,
needing a corrective second pass. The field is now passed for real, producing
the canonical form directly — the same fast path every other adapter already
had via `database/sql`, which PostgreSQL doesn't go through.

### Changed — SQLite reads date columns as text

`ReadAllRows` now selects date columns through a text cast, sidestepping the
driver's own type-based parsing, and converts the raw text with a direct
string splice instead of a parse/format round trip.

### Changed — cheaper row splitting when values are escaped

Row splitting copies unescaped runs in bulk instead of byte by byte. Output is
unchanged; the old byte-at-a-time parser is kept as a reference implementation
for testing.

### Added — a row-splitting variant that reuses a caller-supplied slice

Useful for callers that consume and discard each row immediately instead of
retaining it.

### Added — hand-rolled MSSQL datetime formatters

Not yet wired into any adapter, since `database/sql` doesn't expose the wire
bytes needed to use them.

---

## [1.24.1] — 2026-08-10

### Fixed — pipeline TDTP output silently dropped compact-format fixed-field values

Any pipeline writing a compact-format file with fixed fields produced output
where every row but the first in each group had its fixed field silently
blanked, because the compact encoding ran before the packet was split into
parts, and the part actually written never carried the marker telling a reader
to expand it. Fixed by encoding per part, after the split. Any file produced by
an affected pipeline should be regenerated.

---

## [Unreleased] — refactor/orchestrator-route-groups

### Changed — the orchestrator database opens in WAL mode

Faster, and lets the health check run concurrently with writes. Two visible
side effects: WAL/SHM files now sit beside the database file (back them up
together), and durability on power loss relaxed slightly — a lost job row is
possible after an unclean shutdown, never a corrupt database.

### Changed — opening the orchestrator database no longer costs over a second

Startup ran its schema and migrations as many separate, individually
committed statements, each paying a full fsync. They now run in one
transaction.

### Changed — internal: route registration split by group

No behavior change. The router had grown into the package's one complexity
outlier as new endpoints kept landing in the same function; each route group
now registers itself separately.

---

## [1.24.0] — 2026-07-29

### Added — job log retention in the orchestrator

Job logs were kept forever and had become the majority of the database's
size. A retention flag clears old finished jobs' log text on a schedule; the
job rows themselves stay, since they're the only record of when a scenario
last ran. A companion flag reclaims the freed space at startup, sized off
actual database size rather than SQLite's freelist estimate — the estimate
badly undercounts for this data shape.

### Added — `--drain <duration>` for `--map`

A mode between "process one message and exit" and "run forever until
killed": consumes until the queue has been empty for the given window, then
exits with a total — a unit of work an orchestrator schedule can actually own.

### Fixed — audit entries lost when processes run in parallel

Audit IDs combined a timestamp with a per-process sequence counter, so several
processes launched together by one orchestrator step could produce colliding
IDs and lose entries to a primary-key conflict — precisely when the most work
is running in parallel. IDs now carry a per-process random component.

### Changed — `--quiet` now covers every path that moves rows

Previously reduced only incremental sync to one line per table; the other
paths, including broker export, printed banners and per-packet detail
regardless. All now report the same one-line shape.

### Changed — travel-agency runs its import under the orchestrator

The example's long-lived listener daemons are replaced by a scheduled
workflow, so the receiving half of the pipeline is now governed the same way
the sending half already was — with a job record, an approval, and a visible
failure.

---

## [1.23.0] — 2026-07-29

### Added — a config setting for the xZMercury URL

Previously flag-only, which meant repeating one URL across every workflow
step and listener — a single stale copy silently produces packets the other
side can't open. The flag still overrides it for a one-off run.

### Changed — travel-agency transports encrypted packets

Encryption was code-complete on both sides and simply not switched on in the
example; it now is. The packet header stays readable so a broker can still
route it and an operator can see which table a stuck packet belongs to without
holding a key.

### Fixed — a guard test for the version constant

The version file had been accidentally truncated to zero bytes by a bad edit,
twice, breaking the whole build both times. It's now under test.

---

## [1.22.2] — 2026-07-29

### Fixed — a schedule health metric reported every healthy schedule as never run

The metric was written at dispatch time with a status meaning "still
running," and nothing ever revisited it — so a schedule completing
successfully on every tick reported, permanently, that it had never run. Now
written only once a run reaches an outcome, from the same place the schedule's
own status is updated.

---

## [1.22.1] — 2026-07-29

### Fixed — the XLSX scanner accepted character references XML forbids

The byte-level XLSX scanner decoded character references that the standard
XML decoder — its own fallback — refuses outright, so the same file could
parse differently depending on which path happened to run. Both now share one
implementation of the XML character-validity rule.

---

## [1.22.0] — 2026-07-29

### Performance — byte-level scanner for XLSX cell data

XLSX reading and writing now use a hand-written scanner for the bulk cell data
instead of a third-party library, in the same style already used for TDTP's
own data section: the small structural parts stay with the standard decoder,
the bulk is scanned over raw bytes, and anything unrecognized falls back to
the full decoder rather than guessing.

### Changed — XLSX is written and read in-house; the excelize dependency is gone

Removes a large binary-size cost — a reflection-heavy formula engine had
forced the linker to stop pruning almost entirely — and one dependency with a
CVE history. Every behavioral trap the old code had documented is preserved.

---

## [1.21.0] — 2026-07-28

### Added — `--quiet`

Most of a captured run's output was preamble and banners. `--quiet` drops both
and leaves one line per operation — name, rows, elapsed — while never
suppressing warnings. A failing command still prints in full, since that's the
one case the log is actually read for.

---

## [1.20.4] — 2026-07-28

### Fixed — the SQLite adapter refused concurrent writers instead of waiting

WAL mode was enabled but a busy timeout was never set, so a second process
against the same file got an immediate refusal instead of waiting its turn —
the ordinary way this tool is used. The same gap had already been closed for
the audit sink.

### Fixed — a schedule's status never reached its outcome

The scheduler marked a job "running" at dispatch and never revisited it, so
the schedules endpoint reported every healthy schedule as permanently mid-run.
The terminal outcome is now carried back from the single method every job's
completion goes through.

### Fixed — disabling the Go workspace broke the integration suite

One integration test starts a sibling module by path, which only resolves
with the workspace active; disabling it for CI broke that test alone. That job
now keeps the workspace; every other job still resolves independently.

---

## [1.20.3] — 2026-07-28

### Added — a limit parameter on the jobs listing endpoint

The listing was fixed at the last hundred, and each job carries a large output
blob. Callers can now ask for fewer.

### Added — a read-only operations dashboard for the travel-agency example

Joins the orchestrator's runs and schedules, the broker's queue depth, and the
audit trail on one page — aimed at the failure with no other symptom: a queue
with messages and no consumer, which looks identical to an idle queue unless
something is actually watching for it.

---

## [1.20.2] — 2026-07-28

### Fixed — timestamps lost their sub-second precision on export

TDTP's canonical timestamp format was whole-second, though every database the
framework talks to stores more. Beyond the precision loss itself, this broke
incremental sync outright: a watermark coarser than the data it's compared
against never converges, so an affected table resynced its newest row on
every run. The canonical form now keeps sub-second precision. Checkpoints
written before this fix hold truncated watermarks and will resend one row on
the first run after upgrading.

---

## [1.20.1] — 2026-07-28

### Fixed — the audit log's time filter dropped records, and retention deleted the wrong ones

Timestamps were bound into SQL as local time on a driver that stores them as
text, so comparisons were lexical across writers' different local zones —
records could sort out of chronological order. That silently dropped rows
from time-range queries and, worse, could make retention delete records newer
than its cutoff. Every timestamp entering SQL is now normalized to UTC first.
Existing SQLite audit databases keep their old, unreliable rows until
rewritten or aged out.

---

## [1.20.0] — 2026-07-28

### Added — incremental sync can send straight to a broker

Incremental sync could only write files; broker export could only send. This
connects them directly, so a watermark-tracked sync can feed a queue without
external state. The checkpoint advances only after the send succeeds.

### Added — an audit record per message in `--map --listen`

The daemon previously wrote one audit entry per run regardless of how many
messages it processed; each message now gets its own.

### Changed — the travel-agency example has no Python left in its data path

The example's two orchestration scripts are replaced by the framework doing
the same work directly: a long-lived listener per queue with proper
ACK-after-commit semantics, and the orchestrator standing in for what used to
be a hand-rolled coordinator script, which had its own quiet data-loss bug in
how it set its watermark.

### Fixed — incremental sync failed on every run after the first

The query builder emitted a comparison operator the query language has never
accepted in that spelling, so every run past the first — the first has no
watermark and builds no filter — failed outright.

### Fixed — "no changes" was reported as an error

An unchanged table returns one empty packet rather than zero packets, and a
guard checking for zero packets missed that shape.

---

## [1.19.2] — 2026-07-27

### Fixed — the fast parser's two ways of being more permissive than the reference

The fast XML parser is supposed to fall back to the full decoder on anything
unexpected, but two checks were too lenient in the direction that matters: a
schema-size scan stopped at the first near-miss element name instead of
skipping past it, silently disabling a read-size limit, and a
character-reference check accepted values XML itself forbids. Both now match
the reference parser's rules exactly.

---

## [1.19.1] — 2026-07-27

### Fixed — a carriage return in a cell value no longer changes on the way through the format

XML obliges every parser to fold raw line endings, so a value with Windows
line endings silently changed on a round trip, and the hand-rolled fast parser
didn't even fold consistently with the standard one. Carriage returns are now
escaped on write and restored on read.

### Fixed — a re-filter command refuses encrypted input instead of quietly decrypting it

It previously accepted an encrypted file and, with no explicit output, could
overwrite it with the plaintext — burning the encryption key and leaving a
readable file still named as encrypted. It now refuses encrypted input
outright, by content as well as by filename.

---

## [1.19.0] — 2026-07-27

### Added — re-filter or re-version an existing TDTP file without a DB round-trip

A new command sibling to the CSV/HTML/XLSX converters, except the output stays
TDTP XML — filtering, sorting, projecting, or changing protocol version
without re-running the original database query.

---

## [1.18.3] — 2026-07-22

### Added — a SQL sink for the audit logger

Audit entries can now be written to their own SQL database, deliberately a
separate connection from the pipeline's own — reusing the same credentials
would let the audited process rewrite its own trail.

### Fixed — two real bugs found while wiring this up

Audit ID generation could collide under tight, I/O-free loops (fixed with an
atomic counter); a failed batch write left the same poisoned entries queued to
fail forever instead of being cleared; and the SQLite audit sink hit the same
missing-busy-timeout concurrency bug as the main adapter.

---

## [1.18.2] — 2026-07-22

### Fixed — the Python-facing library silently returned garbage for v1.5 encrypted packets

It had no awareness that section-level encryption exists, so it "successfully"
parsed an encrypted packet's ciphertext as one opaque row of data, with no
error. All read entry points now detect an encrypted packet and refuse it with
a clear error, since decrypting requires the full CLI plus a reachable key
server this library was never meant to talk to.

---

## [1.18.1] — 2026-07-22

### Security — two dependency vulnerabilities

Upgraded a text-processing library carrying an infinite-loop denial-of-service
reachable through XLSX and the PostgreSQL driver, and fixed the workspace's
toolchain pin, which had been silently letting builds use an older, vulnerable
Go toolchain than declared.

---

## [1.18.0] — 2026-07-22

### Added — TDTP v1.5: section-level encryption

Encryption redesigned to mirror how compression already works: instead of
wrapping the whole packet in an opaque binary blob, the query context, schema,
and data sections are each individually replaced with ciphertext while the
header stays plain, so routing and multi-part reassembly need no key.
Standalone encryption now produces this format by default; the legacy
whole-blob format is still available for consumers not yet updated.

### Fixed — two real bugs found via live end-to-end testing

A caller that skipped compression could leave the original plaintext rows
sitting in an internal fast-path field the writer preferred over the encrypted
data — a packet that looked encrypted while leaking every row in the clear.
And an embedded dictionary entry was written after the integrity hash was
already stamped, so any packet combining integrity and encryption failed
verification on import. Both fixed; caught by live round-trips against a real
key server, not unit tests alone.

---

## [1.17.2] — 2026-07-02

> Rebuild the shared library before upgrading — this release changes a C
> struct's binary layout.

### Fixed — JSON casing drift, a boolean serialization bug, and a v1.4 gate bypass in the shared library

JSON field names had drifted from the documented convention; the pandas
bridge wrote booleans in a spelling the framework's own validator rejects; and
stamping a packet with real integrity hashes never bumped its declared
version, so it read back as pre-1.4 and silently skipped the whole v1.4
security gate.

### Added — four previously unreachable exports, plus network-verifying integrity

Several functions were compiled into the library but had no Python binding
wired up, and there was no way for a non-Go consumer to verify a packet's
integrity hash against the key server rather than only locally.

### Changed — row storage in the C-struct API is now a flat buffer (breaking change)

The old per-row layout cost one cross-language call per cell, which made the
supposedly faster low-level API slower than the JSON one on real workloads.
Replaced with a single flat buffer plus an offsets array — this specifically
fixes the interpreter-boundary overhead; a native C consumer with no such
boundary in the loop is better off with the old per-row layout.

### Fixed — a write path trusted a caller-supplied row count instead of deriving it

A hand-assembled multi-part packet could declare a row count inconsistent
with its actual data with no error at write time. The count is now always
derived from the payload.

### Security — SQL injection in the PostgreSQL driver

Upgraded past a placeholder-confusion vulnerability with dollar-quoted string
literals, which required bumping the minimum Go version.

---

## [1.17.1] — 2026-06-15

### Fixed — loop guard conflict in parallel step execution

The "already running" check scoped only to source/target system, so
independent mappings sharing a target database blocked each other even though
they had nothing to do with one another. Now scoped to the specific mapping.
Also recovers gracefully from a corrupted loop-guard log left by a prior crash.

---

## [1.17.0] — 2026-06-15

### Added — multi-step workflow orchestration

Runs a sequence of CLI sub-commands defined in a YAML file, replacing
hand-written shell scripts for multi-stage pipelines — DAG execution with
parallel waves, and per-step error policies (stop/skip/retry).

---

## [1.16.1] — 2026-06-15

### Fixed — RabbitMQ daemon resilience

Four separate reliability bugs in listener daemon mode, found by comparing
against a production broker bridge: a dropped connection sent the consumer
into an infinite fast error loop instead of reconnecting; reconnection had no
backoff; there was no flow-control limit on how many messages the broker
could push at once; and dead-connection detection relied on a long default
heartbeat.

---

## [1.16.0] — 2026-06-15

### Added — a standalone listener daemon mode

Turns any mapping config into a persistent daemon: one long-lived broker
connection, processing messages in a continuous loop with requeue-on-failure
and graceful shutdown — no external coordinator process required.

---

## [1.15.0] — 2026-06-15

### Added — sourcing the field-mapping command from a broker queue

Reads a packet directly from a queue, remaps its fields per a mapping config,
and upserts into the target database — no staging table, no separate merge
procedure.

### Changed — the example consumer migrated fully to the new pattern

All remaining entity handlers in the travel-agency example moved off a legacy
three-step flow (staging table plus a stored-procedure merge) onto the
single-call pattern above.

---

## [1.14.0] — 2026-06-13

### Added — sourcing the field-mapping command from S3

Reads its input packet from S3-compatible object storage instead of a local
file only.

---

## [Unreleased] — feature/sprint4-map

### Added — cross-system field mapping

A new command reads a packet, remaps fields and enum values per a mapping
config, and upserts into a target database — on-demand record sync between
systems without a hand-written importer. Supports compressed and
key-server-encrypted input, and a loop guard preventing a mapping from
re-triggering itself.

### Fixed — a "no date" marker wasn't decoded to NULL on PostgreSQL import

The adapter honored the NULL marker but not the sentinel used for
Navision/MSSQL zero-dates, so it reached a date column raw and failed.

### Fixed — Cyrillic double-encoding on PostgreSQL import

Row values were accumulated byte by byte into a string, which re-encodes any
byte above ASCII as if it were its own character — double-encoding every
multi-byte UTF-8 character. Affected all non-ASCII PostgreSQL imports.

---

## [1.13.0] — 2026-06-12

### Refactoring — the visual pipeline tool aligned with the framework core

It no longer duplicates its own SQL and in-memory logic; it now delegates to
the same adapter and workspace code the CLI uses.

### Added — deploy a pipeline straight to the orchestrator

Writes a generated pipeline directly into the orchestrator's scenarios
directory, where it's picked up automatically.

---

## [1.12.0] — 2026-06-03

Operational maturity: air-gap enrollment, seat policy, structured audit log,
per-job artifacts, orchestrator LDAP auth, metrics, and a Docker deployment
stack.

### Added — CA operational maturity

Offline certificate issuance for isolated networks, a one-license-per-
environment seat policy, a mock clock for testing certificate auto-renewal
without waiting on real time, and an admin tool for issuing capability
certificates for unsafe pipeline operations.

### Added — observability and SIEM

A structured audit log (text or JSON, optional syslog sink), per-job output
artifacts retrievable from the orchestrator with a checksum, and LDAP
authentication for its API.

### Added — monitoring

Metrics for job counts, durations, queue depth, and schedule health, plus an
extended health endpoint suitable for a readiness probe.

### Added — a Docker deployment stack

Minimal worker and key-server images, and separate dev/prod compose files —
dev brings up a metrics dashboard; prod adds the CA and separates cache
instances by role.

### Fixed

A missing schema migration, an offline certificate that could incorrectly be
accepted at the online enrollment endpoint, use of a deprecated cache method,
and a private-key loader that only understood one of the two key formats
actually in use.

---

## [1.11.0] — 2026-06-02

Full trust chain closed: a hardware-anchored CA authorizes the runtime
environment via the key server, an offline license file governs CLI
capabilities, and the orchestrator gates scenario execution on both.

### Added — CA wiring, admin tooling, offline licensing, and orchestrator trust gates

The key server now authorizes itself against a CA on startup, with automatic
renewal. A new admin CLI issues and revokes licenses and certificates. A new
offline, signed license file governs which adapters, features, and row limits
a CLI binary may use — entirely without network access. The orchestrator gates
every scenario run against the intersection of what the license and the CA
session both permit.

---

## [1.10.0] — 2026-06-01

### Fixed — tail mode on SQL Server

Getting `--limit -N` right on MSSQL needed several passes: plain syntax
translation wasn't enough, the semantics needed a proper subquery, and
combining it with `--fields` could reference a column outside the projection.

### Added — key-server security hardening

A retrieval audit trail; the server mode is now cryptographically bound into a
key's signature rather than self-reported; the server secret is mandatory
unless explicitly opted out for development; and burned/expired keys are now
distinguishable states.

### Added — a CA server and an orchestrator service

A standalone certificate authority with challenge-response enrollment and a
DDoS-resistant handshake gate. A new orchestrator wraps pipeline execution
behind an HTTP API with schedule storage, templated parameters, and a REST
interface for managing schedules and job history.

---

## [1.9.7] — 2026-05-30

Python library modernization: a plain-verb facade API, CLI parity in-process,
and an Arrow columnar bridge.

### Added

An Arrow read/write bridge; a facade offering plain verbs instead of the
low-level prefixed API; several CLI operations made available in-process; and
matching additions to the C# wrapper.

### Fixed

A test-only export helper was accidentally excluded from the compiled
library; an inspect call lost the compact-format flag; and sort ignored its
direction argument.

---

## [1.9.6] — 2026-05-30

### Added — CSV export

Configurable delimiter, code page, and BOM, with the same integrity gate as
other consumer commands.

### Added — standalone encryption for every command

Previously pipeline-only; now available on any standalone export/import/
convert command, with automatic extension handling and burn-on-read
semantics.

### Added — the v1.4 security gate on every import path

Previously covered only CSV export; now covers every command that writes data
to a database.

### Fixed

An XLSX-sourced packet was created without a message ID; `--limit` failed
outright on older SQL Server compatibility levels; and a pipeline's fast-path
row storage caused rows to silently vanish on some export paths.

### Performance

Regex-based filtering and schema field lookups in the in-memory query engine,
and the compact-format encoder/decoder, were both sped up by caching and
avoiding per-row allocation.

---

## [1.9.5] — 2026-05-25

### Added

CSV export; short flag aliases; and TDTP v1.4 integrity — hashes stamped into
the packet and optionally registered with a key-server notary for
consumer-side verification.

---

## [1.9.4] — 2026-05-20

### Added

A dictionary section in the schema for token-compressing repeated long
strings; a new streaming SVG-to-TDTP converter; and a row-limit safety valve
for the in-memory fallback path when SQL pushdown fails.

### Fixed

An MSSQL table name containing special characters broke query generation and
silently fell back to a full unbounded in-memory scan; a stray timezone
suffix broke MSSQL datetime literals; and the SQL-pushdown fallback now warns
instead of failing silently.

---

## [1.9.3] — 2026-05-08

### Added

Every pipeline-exported packet now carries the pipeline's name and the
variables used to produce it; a new flag lets an importer verify those
variables match expectations before touching the database; and pipeline
configs can now take parameters directly from the command line, substituted
into SQL and YAML fields with the existing SQL validator still guarding
against injection.

### Fixed

The NULL marker wasn't recognized before parsing a timestamp value on
PostgreSQL or MSSQL import, so a literal marker reached the database driver
and failed.

---

## [1.9.2] — 2026-04-21

### Added

Full CLI integration test coverage for the MySQL adapter, matching the
structure already established for SQLite and PostgreSQL.

---

## [1.9.1] — 2026-04-07

### Fixed

PostgreSQL's time-of-day type failed to export — it was mapped to a subtype
the converter didn't actually handle. Fixed, along with making the test
fixtures' random data deterministic.

---

## [1.9.0] — 2026-04-06

### Message broker — production release

Kafka export/import moves from beta to production-ready: packets are
compressed and serialized in parallel, sent in a single batched write instead
of one round trip per packet, and import decompresses in parallel while
committing a single offset that covers everything before it. A new raw
pass-through mode saves broker messages verbatim, useful for recovery.

### Changed

Multi-part broker imports are now atomic by default; an opt-in streaming mode
trades that atomicity for constant memory on very large batches.

### Fixed

Field names containing spaces or commas weren't parseable in `--fields`, and
the same names produced invalid SQL in a projection — both now go through the
same bracket-quoting the WHERE clause already supported.

---

## [1.8.2] — 2026-04-05

### Performance — meaningfully faster import

Parts are now processed one at a time instead of all buffered in memory
first, keeping memory constant regardless of part count; row splitting for
the unescaped common case avoids per-field allocation; stateless helper
structs stopped being reallocated on every row; and the SQLite batch insert is
now prepared once and reused.

---

## [1.8.1-beta] — 2026-04-02

### Added

A field-name sanitizer for importing into databases that reject the source
system's column names — transliteration plus a symbol-replacement map,
applied only on import so export always preserves the original names; and
bracket-quoted identifier syntax in the query language for names containing
spaces or special characters.

---

## [1.8.0-beta] — 2026-03-31

### Added

S3-compatible object storage as a source and destination; a dry-run integrity
check command requiring no database connection; a config-file default for the
compression algorithm; and full CLI integration test suites for the SQLite
and PostgreSQL adapters.

---

## [1.7.1-beta] — 2025 Q4

### Added

The TDTP compact format, column projection, a metadata-inspection command, a
streaming consumer daemon, repeatable and IN-capable filtering, tail-mode
limits, special-value markers for NULL/NaN/infinity/zero-date, RabbitMQ and
MSMQ broker support, the key-server encryption layer, a standalone HTTP
viewer for encrypted data, and initial Python and C# bindings.

### Fixed

A cluster of row-count and fast-path regressions from the same period.

---

## [1.7.0] — 2025 Q4

### Added

kanzi compression, column projection on export, MSMQ broker support, and a
configurable maximum packet size.

---

## [1.6.0] — 2025 Q3

### Added

SQL-style filtering; the ETL pipeline framework; an MS Access adapter; kanzi
compression; checksums; pagination; and HTML/XLSX export and import.

---

## Version History Summary

| Version | Highlights |
|---------|-----------|
| 1.12.0 | Air-gap offline cert, seat policy, structured audit log, per-job artifacts, orchestrator LDAP auth, metrics, Docker stack |
| 1.11.0 | Full trust chain: CA → key server → license file → orchestrator dual gate; admin CLI; offline license engine |
| 1.10.0 | Tail-mode fix on MSSQL, burn marker, mode-bound HMAC, mandatory server secret, CA server, orchestrator with schedule DB |
| 1.9.7 | Arrow columnar bridge, facade API, CLI parity in-process, C# wrapper parity |
| 1.9.6 | CSV export, MSSQL tail-mode fix, XLSX message-ID fix, row-loss fix, faster filtering and compact format, standalone encryption, v1.4 security gate everywhere |
| 1.9.5 | Shorthand flags, v1.4 integrity + key-server notary |
| 1.9.4 | Schema dictionary, SVG converter, MSSQL full-scan fix, fallback row limit |
| 1.9.3 | Pipeline context in packet header, variable-expectation check, pipeline variables |
| 1.9.2 | MySQL adapter — full CLI test coverage |
| 1.9.1 | PostgreSQL time type fix, deterministic test data |
| 1.9.0 | Kafka production-ready, parallel compress/decompress, batched sends, raw pass-through import, streaming import mode |
| 1.8.2 | Faster import, streaming import, prepared-statement reuse, embedded help files |
| 1.8.1-beta | Field-name sanitization, bracket-quoted identifiers |
| 1.8.0-beta | S3 object storage, dry-run integrity check, compression config default, SQLite+PostgreSQL CLI test suites |
| 1.7.1-beta | Compact format, inspect command, listener daemon, special-value markers, key-server pipeline encryption |
| 1.7.0 | kanzi compression, field projection, MSMQ, configurable packet size |
| 1.6.0 | Query-language filtering, ETL pipeline, Access adapter, checksums, XLSX/HTML viewer |
| 1.3.1 | TDTP protocol v1.3.1 — compact format specification |
