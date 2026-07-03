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
- **Schema migration** — schema-drift *detection* by default (diff packet schema vs.
  live table, report only); auto-apply is a separate, explicit opt-in per source
  (mirrors ETL's `--unsafe` gate), additive-only (`ADD COLUMN`, never `DROP`/type
  narrowing), mandatory field-name sanitization on the DDL path, mandatory audit log.
  **Why so cautious:** the packet schema is producer-controlled — TDTP's own threat
  model already treats the producer as less trusted than the consumer (that's what the
  v1.4 integrity gate and Mercury notary exist for). Auto-applying DDL from an untrusted
  schema unconditionally would let a compromised producer silently mutate a consumer's
  database with no approval step and no visibility.
- **Orchestrator scenario integrity registration** — today `--scenarios/*.yaml` is
  trusted purely by filename, loaded once at startup, never re-read or hash-checked at
  run time (`TrustGate`/`GateScenario` only checks `scenario.permissions ⊆ (license ∩
  Mercury)`, i.e. declared capability strings — it never touches the file's own content).
  Register scenarios by `name + sha256(content)`, reject execution on mismatch, and
  record the executed scenario's hash on the job record (today only the *output
  artifact* gets a SHA-256, not the pipeline definition that produced it). This closes a
  different gap than schema-drift trust above: it protects the *instructions* (pipeline
  YAML) from tampering; the schema-migration guardrails protect against a legitimate,
  unmodified pipeline faithfully processing a malicious *payload*. Both are needed —
  neither substitutes for the other.
  Design draft for both, with a DBA-signed delegation model and a staged rollout:
  [docs/SCENARIO_TRUST.md](docs/SCENARIO_TRUST.md).

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
