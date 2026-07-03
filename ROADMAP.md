# TDTP Framework — Roadmap

> For what has already shipped, see [CHANGELOG.md](CHANGELOG.md).
> For what the framework does today, see [README.md](README.md).
> This file only tracks forward-looking plans.

---

## Next

- **Streaming export/import** (`TotalParts=0`, "TCP for tables") — core is ready
  (`pkg/core/packet/streaming.go`, channel-based `StreamingGenerator`), not yet wired to
  the CLI (`--export-stream` / `--import-stream`)
- **Parallel import workers** — concurrent multi-part import (export already parallelizes
  compress+serialize; import is still sequential per part)
- **Schema migration** — `ALTER TABLE` support (add/drop columns, type changes) across
  adapters

## Later

- **Additional adapters** — Oracle, MongoDB, Redis, Cassandra, Snowflake
- **Additional object storage backends** — GCS, Azure Blob (S3-compatible already covered)
- **Distributed tracing** — OpenTelemetry spans across pipeline/orchestrator/adapters
- **Kubernetes** — operator + Helm charts for the orchestrator/xZMercury stack

## Exploratory (not committed)

- Web-based pipeline builder / monitoring dashboard (today: `tdtp-xray` desktop GUI +
  Grafana dashboards, no browser-based control plane)
- Data quality monitoring / anomaly detection on pipeline runs
- Schema version control and data lineage tracking
- CLI plugin system / custom processor SDK for third-party processors

---

Have a request that isn't listed? Open an issue:
https://github.com/ruslano69/tdtp-framework/issues
