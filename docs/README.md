# TDTP Framework documentation

Protocol v1.5 · `tdtpcli` v1.24.0

Start at the [project README](../README.md) if you have not used the framework
before. This page is the map of everything else.

---

## I want to…

| Task | Document |
|------|----------|
| Install the framework and run something | [README.md](../README.md) |
| Use the CLI | [USER_GUIDE.md](./USER_GUIDE.md) |
| Write an ETL pipeline | [ETL_PIPELINE.md](./ETL_PIPELINE.md) |
| Understand the wire format | [SPECIFICATION.md](./SPECIFICATION.md) |
| Encrypt what leaves the system | [SPECIFICATION.md](./SPECIFICATION.md) · [ETL_PIPELINE.md](./ETL_PIPELINE.md) |
| Schedule work and govern it | [ORCHESTRATOR_SCENARIOS.md](./ORCHESTRATOR_SCENARIOS.md) |
| Decide what a scenario is allowed to do | [SCENARIO_TRUST.md](./SCENARIO_TRUST.md) |
| Deploy the whole thing | [DEPLOYMENT.md](./DEPLOYMENT.md) |
| Build against the framework | [DEVELOPER_GUIDE.md](./DEVELOPER_GUIDE.md) |
| Write a new database adapter | [DEVELOPER_GUIDE.md](./DEVELOPER_GUIDE.md) · [pkg/adapters/base](../pkg/adapters/base/README.md) |
| Use S3 instead of a broker | [S3_AS_SYNC_BROKER.md](./S3_AS_SYNC_BROKER.md) |
| Read or write Microsoft Access | [ACCESS_ADAPTER.md](./ACCESS_ADAPTER.md) |
| See a full working deployment | [examples/travel-agency](../examples/travel-agency/TRAVEL-AGENCY.md) |

---

## Core guides

**[USER_GUIDE.md](./USER_GUIDE.md)** — the `tdtpcli` reference. Every command
and flag: export and import, TDTQL filters, brokers, incremental sync,
encryption, XLSX and CSV conversion, multi-step workflows.

**[SPECIFICATION.md](./SPECIFICATION.md)** — the protocol. The XML format, the
type system, TDTQL, and how packets are exchanged. This is what you need to
write a reader or writer that is not this implementation.

**[ETL_PIPELINE.md](./ETL_PIPELINE.md)** — pipeline YAML: the configuration
reference and worked scenarios, including encrypted output through xZMercury and
what happens when the key server is unreachable.

**[DEVELOPER_GUIDE.md](./DEVELOPER_GUIDE.md)** — the architecture. Core modules,
adapters, brokers, the production features (circuit breaker, retry, audit,
processors), and how to add an adapter of your own.

**[DEPLOYMENT.md](./DEPLOYMENT.md)** — running it for real: service map and
dependencies, local development, production with Redis and TLS and LDAP, startup
order, air-gapped certificate handling, audit log formats.

---

## Orchestration

**[ORCHESTRATOR_SCENARIOS.md](./ORCHESTRATOR_SCENARIOS.md)** — turning a
pipeline into a scenario the orchestration server can run: one-off and
scheduled, plain and encrypted, the permission model, the client-submit and
admin-approve workflow, and job log retention.

**[SCENARIO_TRUST.md](./SCENARIO_TRUST.md)** — why a scenario is allowed to run
at all: the licence and Mercury permission intersection, checksum approval, and
what is refused when either side says no.

**[cmd/orchestrator/README.md](../cmd/orchestrator/README.md)** — the server
itself: HTTP API, tokens and roles, runners, metrics.

---

## Protocol and format

- **[SPECIFICATION.md](./SPECIFICATION.md)** — TDTP and TDTQL
- **[tdtp-protocol-schema.md](./tdtp-protocol-schema.md)** — schema reference
- **[tdtp-v14-protocol-schema.md](./tdtp-v14-protocol-schema.md)** — v1.4 integrity additions
- **[dictionary-as-dependency-manifest.md](./dictionary-as-dependency-manifest.md)** — the dictionary as a dependency manifest
- **[xzmercury/](../xzmercury/README.md)** — the key server: [architecture](../xzmercury/docs/architecture.md) · [API](../xzmercury/docs/api.md) · [security](../xzmercury/docs/security.md) · [configuration](../xzmercury/docs/configuration.md) · [deployment](../xzmercury/docs/deployment.md)

---

## Packages

Each production package documents itself.

**Reliability**
- [pkg/resilience](../pkg/resilience/README.md) — circuit breaker: three states, automatic recovery, concurrency limits, custom trip logic
- [pkg/retry](../pkg/retry/README.md) — exponential backoff, jitter, context-aware retry, dead letter queue
- [pkg/audit](../pkg/audit/README.md) — audit log: file, database and console sinks, three detail levels, sync and async

**Data**
- [pkg/processors](../pkg/processors/README.md) — masking, validation, normalisation, and chains of them
- [pkg/sync](../pkg/sync/README.md) — incremental sync: checkpoint tracking, timestamp and sequence modes, recovery
- [pkg/xlsx](../pkg/xlsx/README.md) — Excel in and out, with types preserved

**Adapters**
- [sqlite](../pkg/adapters/sqlite/README.md) · [postgres](../pkg/adapters/postgres/README.md) · [mysql](../pkg/adapters/mysql/README.md) · [mssql](../pkg/adapters/mssql/README.md) · [access](../pkg/adapters/access/README.md)
- [pkg/adapters/base](../pkg/adapters/base/README.md) — the shared helpers a new adapter builds on

**Other binaries**
- [cmd/tdtpserve](../cmd/tdtpserve/README.md) — HTTP service
- [cmd/tdtp-xray](../cmd/tdtp-xray/README.md) — packet inspector

---

## Examples

**[examples/README.md](../examples/README.md)** — the catalogue.

Worth reading first:

1. [01-basic-export](../examples/01-basic-export/) — the smallest thing that works
2. [travel-agency](../examples/travel-agency/TRAVEL-AGENCY.md) — a complete deployment: three databases, a broker, encryption, and an orchestrator governing both halves of the flow
3. [02-rabbitmq-mssql](../examples/02-rabbitmq-mssql/) — broker integration
4. [03-incremental-sync](../examples/03-incremental-sync/) — moving only what changed
5. [08-pipeline-encrypted](../examples/08-pipeline-encrypted/) — encrypted pipeline output

---

## Release history

**[CHANGELOG.md](../CHANGELOG.md)** is the record, and the only one kept current.
This page used to carry its own copy of the release notes; it fell five versions
behind, which is the usual fate of a second place to write the same thing.

**[ROADMAP.md](../ROADMAP.md)** — what is planned.
**[TODO_NEXT.md](../TODO_NEXT.md)** — what is being worked on now, in detail.

---

## A note on language

Documentation is being translated from Russian to English. Originals of
translated documents are kept under [ru-archive/](./ru-archive/) and are no
longer maintained — corrections belong in the English version.

Documents still in Russian are listed with their priority in
[TODO_NEXT.md](../TODO_NEXT.md) under *Internationalization*.

---

**Issues:** https://github.com/ruslano69/tdtp-framework/issues
**Contact:** ruslano69@gmail.com
