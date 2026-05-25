# Benchmarks

Standalone programs used during framework development to answer specific
performance questions. Not part of the production build (`go build ./cmd/...`
excludes this directory).

| Program | Question answered |
|---------|------------------|
| `bench_direct` | Is `database/sql` mutex the throughput bottleneck? (No — direct access gives <5% gain) |
| `bench_duckdb` | Is DuckDB in-memory faster than modernc SQLite for read-heavy workloads? |
| `bench_dynamic` | What is the per-row overhead of the TDTP framework vs raw XML? |
| `bench_raw` | Baseline: raw SQLite → custom XML with no framework, minimum possible overhead |

## Build & Run

```bash
# Build individual benchmark
go build -o /tmp/bench_direct ./benchmarks/bench_direct/
go build -o /tmp/bench_raw    ./benchmarks/bench_raw/

# Run
/tmp/bench_direct path/to/db.sqlite
/tmp/bench_raw    path/to/db.sqlite output.xml
```

## Results (100k rows, Users table, SQLite)

| Mode | Time | Size |
|------|------|------|
| bench_raw (no framework) | ~380 ms | 9.9 MB |
| bench_direct (framework) | ~400 ms | 9.9 MB |
| framework + zstd level 3 | ~450 ms | 2.9 MB |
| framework + kanzi level 6 | ~700 ms | 1.5 MB |

Framework overhead vs raw: **~5%** — negligible.
