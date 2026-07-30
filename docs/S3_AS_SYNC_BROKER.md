# S3 as a synchronisation broker for TDTP

## Why

Classic message brokers — RabbitMQ, Kafka — are **push/subscribe**: the data
leaves the queue once it has been consumed. That is right for event streams and
wrong for moving **table snapshots** between isolated nodes, where there is no
guarantee of a second read, no storage to speak of, and routing between data
centres on closed networks is awkward.

S3-compatible storage (AWS S3, MinIO, **SeaweedFS**) offers the other model:
**store and forward**. The producer writes an object; the consumer reads it when
it is ready. The object lives as long as it needs to.

TDTP over S3 is asynchronous, decoupled, self-describing table transfer between
any two nodes.

---

## What S3 gives as a transport

### 1. Persist first, consume when ready

```
Node A (PostgreSQL)                         Node B (PostgreSQL, or any other DBMS)
──────────────────                          ──────────────────────────────────────
tdtpcli --export users                      tdtpcli --import s3://bucket/users_20260317.tdtp.xml
  → s3://bucket/users_20260317.tdtp.xml         → imports into its own table
```

Node A and node B know nothing about each other, and need no direct tunnel
between them. It is enough that both can reach the same S3 endpoint.

### 2. The object describes itself

Every uploaded object carries TDTP metadata in its S3 user metadata, which the
AWS SDK sends as `x-amz-meta-` headers:

| Key | Value | Set by |
|-----|-------|--------|
| `protocol` | `TDTP 1.0`, `TDTP 1.5`, or `TDTP-ENC 1.0` for a v1.3 encrypted blob | every upload |
| `table` | the table name from the packet header | every upload |
| `rows` | `RecordsInPart` — the number of **rows**, not packets | every upload |
| `checksum` | the packet checksum, when one is present | `--export` |
| `package_uuid` | the encryption package UUID | encrypted `--export` |
| `pipeline` | the pipeline name | `--pipeline` with an S3 destination |

So a consumer can `HEAD` the key and learn what is there without downloading it,
which is enough to build routing, filtering and orchestration on top of the
object store alone.

> Note the keys have no `tdtp-` prefix: the header is `x-amz-meta-table`, not
> `x-amz-meta-tdtp-table`. An earlier version of this document said otherwise.
> XLSX uploads (`--export-xlsx` to an `s3://` path) carry no metadata at all.

### 3. Compression at no extra cost

The data reaches S3 already compressed — zstd level 3, roughly 4×:

```
PostgreSQL, 100 rows → 24 932 bytes of TDTP XML → 6 024 bytes of zstd → S3
```

Bandwidth between data centres is expensive, and the 4× is built into the
protocol rather than bolted on.

### 4. Fault tolerance without an orchestrator

There is no single point of failure. If the consumer dies the object is still in
S3; when it restarts it reads the same key. Idempotency is TDTP's side of the
contract: the `replace`, `upsert` and `append` strategies.

---

## Topologies

### Hub and spoke — a central bucket

```
  DC-1 (PostgreSQL)                DC-2 (PostgreSQL)
       │  export                         │  import
       ▼                                 ▼
  s3://central-bucket/          s3://central-bucket/
       │                                 │
       └─────────── S3 broker ───────────┘
                         │
               DC-3 (MSSQL / SQLite)
                    import
```

Every node reads and writes one bucket. No direct connections between data
centres.

### Pipeline — ETL, then S3, then consumers

```
PostgreSQL (source A)  ─┐
PostgreSQL (source B)  ─┤─ ETL pipeline (join + transform) ──► s3://reports/daily_2026-03-17.tdtp.xml
                        │
                   (zstd 4×)
                        │
              ┌─────────┴──────────────┐
              ▼                        ▼
      Analytics DC                 Archive DC
  tdtpcli --import ...         tdtpcli --import ...
```

One object, several consumers. The data went through ETL — PII masking, joins,
aggregation — **before** it reached S3, not after.

### Edge to cloud

```
Industrial site (closed network)        Cloud or corporate DC
─────────────────────────────────       ──────────────────────────
SQLite / PostgreSQL on the edge node    Central PostgreSQL
tdtpcli --export sensors                tdtpcli --import s3://...
  → s3://seaweedfs-edge:8333/...    ──► reads the same SeaweedFS,
     (SeaweedFS running locally)         or AWS S3
```

The edge node runs **SeaweedFS locally** — a complete S3 with no internet. When
connectivity returns the object is replicated, or read centrally.

---

## S3 against a classic message broker

| | RabbitMQ / Kafka | S3 / SeaweedFS |
|---|---|---|
| Model | push / subscribe | store and forward |
| Kept after reading | no | yes |
| Re-readable | Kafka yes, RabbitMQ no | always |
| Routing | exchanges and topics | buckets and prefixes |
| Reachable without a direct network | awkward | over any HTTP |
| Message size | megabytes | gigabytes |
| Schema or format | no standard | TDTP metadata in the headers |
| Offline nodes | a problem | they read when they connect |
| Self-hosted without a cloud | yes | yes, SeaweedFS |

**This does not replace an event broker.** It is a different niche: batch
snapshots between nodes with different network availability and different
schedules.

---

## SeaweedFS as a self-hosted S3 broker

What it offers over AWS S3 and MinIO in this context:

1. **One binary** — `weed server` brings up master, volume, filer and the S3 gateway
2. **No cloud dependency** — works on isolated networks, at the edge, in air-gapped data centres
3. **IAM from a local JSON file** — no AWS IAM, no Vault, no external identity provider
4. **The same AWS SDK** — set `ForcePathStyle: true` and any S3 client works unchanged
5. **Replication built in** — volume replication across SeaweedFS nodes

Starting it, with the flags a sandbox or private network needs:
```bash
/tmp/weed server \
    -ip=127.0.0.1 \
    -ip.bind=127.0.0.1 \
    -dir=/data/seaweedfs \
    -filer \
    -s3 \
    -s3.port=8333 \
    -s3.config=/etc/seaweedfs/iam.json
```

> **On Windows, and on SeaweedFS 4.17 in particular, this single-command form
> has not worked reliably.** Start the four components separately and give the
> S3 gateway its own config — `weed s3 -config=./s3.json` — and bind everything
> to `127.0.0.1` explicitly. Check against your own version before assuming the
> combined form works.

The IAM configuration (`/etc/seaweedfs/iam.json`):
```json
{
  "identities": [
    {
      "name": "tdtp-node",
      "credentials": [{"accessKey": "...", "secretKey": "..."}],
      "actions": ["Read", "Write", "List", "Admin"]
    }
  ]
}
```

> **This matters:** the key is `identities`, not `accounts`. SeaweedFS differs
> from MinIO's documentation here, and the error it gives is unhelpful.

---

## Using S3 from the tool

### Exporting a table to an S3 URI

```bash
tdtpcli --config config.yaml \
        --export users \
        --output "s3://tdtp-sync/snapshots/users_$(date +%Y%m%d).tdtp.xml" \
        --compress
```

`config.yaml`:
```yaml
storage:
  type: s3
  s3:
    endpoint: "http://seaweedfs-node:8333"
    region: "us-east-1"
    bucket: "tdtp-sync"
    access_key: "testkey"
    secret_key: "testsecret"
```

### Importing from an S3 URI

```bash
tdtpcli --config config.yaml \
        --import "s3://tdtp-sync/snapshots/users_20260317.tdtp.xml" \
        --table users \
        --strategy replace
```

A multi-part export is discovered automatically: naming the base key is enough,
and every `_part_N_of_M` object is found and reassembled.

### An ETL pipeline writing to S3

```yaml
output:
  type: tdtp
  tdtp:
    format: xml
    compression: true
    destination: "s3://tdtp-sync/etl/report_20260317.tdtp.xml"
    s3:
      endpoint: "http://seaweedfs-node:8333"
      region: "us-east-1"
      access_key: "testkey"
      secret_key: "testsecret"
```

---

## What this changes

Before the S3 integration TDTP was point to point: export to a file, move the
file somehow, import it.

With it, any node that can reach an S3 endpoint can be a producer or a consumer,
with no direct link to the source. That turns the tool from an ETL utility into
a synchronisation protocol between:

- geographically separated data centres
- cloud and on-premise nodes
- edge devices and central storage
- databases of any kind running on unrelated schedules

The bucket becomes the meeting point: producer and consumer meet in storage
rather than on the network.
