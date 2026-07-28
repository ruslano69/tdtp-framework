# Travel Agency: Event-Driven Data Synchronization

A reference example of a distributed data synchronization system built on **TDTP Framework**.  
Three independent nodes — **Central**, **Branch**, **Airline** — exchange data through RabbitMQ  
using **Event-Driven Architecture (EDA)**: database changes trigger high-throughput sync pipelines.

---

## Architecture

Three nodes, each with its own PostgreSQL database:

| Node | Port | Role |
|------|------|------|
| **Central Office** | 5432 | Master catalogs (tours, countries, guides); aggregates sales from all branches |
| **Branch Office** | 5433 | Regional office: manages clients, processes sales, receives catalog updates from Central |
| **Airline Partner** | 5434 | External supplier: pushes flight and booking data to Central |

### Data Flow

```mermaid
graph TD
    A[activity.py] -- "1. DB Change & Event" --> MQ[RabbitMQ Exchange: travel]
    MQ -- "2. Event Notification" --> CO[coordinator.py]
    CO -- "3. tdtpcli --export-broker" --> Q[RabbitMQ Named Queues]
    CO -- "4. Signal" --> R[Redis Pub/Sub]
    MQ2 -- "5. Queue" --> CS[tdtpcli --map --listen]
    CS -- "6. tdtpcli --import-broker" --> STG[(Staging Tables)]
    STG -- "7. SQL Merge" --> DB[(Destination DB)]
    CS -- "8. Log" --> S3[MinIO / S3 Audit]
```

### Sync Map

| Direction | Entities | Sync Type |
|-----------|----------|-----------|
| **Airline → Central** | Flights, Bookings | Incremental (`last_updated`) |
| **Central → Branch** | Countries, Tours, Guides, Schedule | Mixed (Full / Incremental) |
| **Branch → Central** | Clients, Sales | Incremental |

---

## Components

### `activity.py` — Traffic Simulator
Emulates real user activity across all nodes:
- Registers new clients and sales in **Branch**
- Updates catalogs (prices, guide statuses) in **Central**
- Changes flight statuses and creates bookings in **Airline**
- After each DB write: publishes a short JSON event to RabbitMQ exchange `travel`  
  with a routing key (e.g. `branch.sales.created`)

### `coordinator.py` — Export Coordinator
Bridge between events and data:
- Listens to RabbitMQ exchange `travel`
- On event: determines which data to transfer (via `ROUTE_MAP`)
- Runs `tdtpcli --export-broker` — reads changed records (using incremental fields like `last_updated`),  
  compresses and sends to the target RabbitMQ queue
- Publishes a readiness signal to Redis Pub/Sub

### `listeners.ps1` — Import Listeners

No Python here: `tdtpcli` listens to the queue itself.

    tdtpcli --map <mapping> --input broker://<queue> --listen

One long-lived process per queue, started by `listeners.ps1 -Node <node>`.
The message is ACKed only after the upsert succeeds, so a process that dies
mid-write returns the message to the queue instead of losing it.

This replaced `consumer.py`, which subscribed to a Redis channel and launched
`tdtpcli --map` per notification — a new process and a new broker connection
each time, with no acknowledgement. The Redis notification went with it: the
queue is the signal, and announcing "there is something in the queue" only
made sense while nothing was listening to the queue.

Its S3 audit marker went too, replaced by a real audit record per message
(`audit:` in the node config) written to a database kept separate from the one
being written to.

---

## Quick Start

### Step 1: Infrastructure

Ensure the following are running:

```
PostgreSQL   — 3 instances on ports 5432, 5433, 5434
RabbitMQ     — credentials: tdtp / tdtp
Redis        — port 6379
MinIO        — S3 on port 8333
```

### Step 2: Initialize Databases

Run SQL scripts from `setup/` in order:

```bash
psql -p 5432 -f setup/setup_database_postgres.sql    # Central
psql -p 5433 -f setup/setup_branch_postgres.sql      # Branch
psql -p 5434 -f setup/setup_airline_postgres.sql     # Airline
psql -p 5432 -f setup/setup_central_additions.sql    # Central additions
psql -p 5432 -f setup/setup_staging_central.sql      # Central staging tables
psql -p 5433 -f setup/setup_staging_branch.sql       # Branch staging tables
psql -p 5432 -f setup/seed_central_postgres.sql      # Seed reference data
```

Or use the populate scripts:

```bash
python setup/populate_data_postgres.py
```

### Step 3: Start Services

Run each in a separate terminal:

```bash
# Export coordinator
python coordinator.py

# Import listeners (one tdtpcli process per queue)
./listeners.ps1 -Node central
./listeners.ps1 -Node branch

# Traffic simulators
python activity.py --node airline --interval 5
python activity.py --node branch  --interval 3
python activity.py --node central --interval 10
```

---

## Configuration

All TDTP settings (compression, retries, circuit breaker) are in `configs/`:

| File pattern | Used by | Purpose |
|---|---|---|
| `configs/config_central.yaml` | `listeners.ps1` | Central DB connection + audit sink |
| `configs/config_branch.yaml` | `listeners.ps1` | Branch DB connection + audit sink |
| `configs/config_src_tdtp_sync_*.yaml` | `coordinator.py` | Source configs per entity |
| `mappings/sync_*.yaml` | `listeners.ps1` | Queue + target table per entity |
| `configs/config_broker_*.yaml` | both | RabbitMQ broker settings |

Default settings:
- Compression: `compress: true`, level 3 (zstd)
- Resilience: exponential retry on broker or DB failure

---

## Notes

### Staging Tables and Data Types

**Rule:** column type in the staging table must match the source type.  
TDTP stores NULL as marker `[NULL]` in the packet body and restores it to `nil` on import —  
but only if the destination column type allows it (not `TEXT`).

```sql
-- Correct: TIMESTAMP NULL — TDTP handles [NULL] → NULL automatically
cancellation_date  TIMESTAMP NULL,

-- Wrong: TEXT causes pgx error "unable to encode time.Time into text"
-- cancellation_date  TEXT,
```

The merge procedure receives `NULL` directly — no additional casting needed:

```sql
-- Correct (after fix):
cancellation_date,

-- Not needed (old workaround):
-- NULLIF(NULLIF(cancellation_date, ''), '[NULL]')::TIMESTAMP
```

### Pipeline YAMLs

The `pipelines/` directory contains `--pipeline` configs for multi-source ETL:
- `extract_*.yaml` — pull data from MSSQL source into S3
- `load_*.yaml` — load from S3 into PostgreSQL destination

These use the `--pipeline` command and are independent of the broker-based sync above.
