# TDTP Specification

**Table Data Transfer Protocol** — the format for exchanging tabular data
through message brokers.

**Version:** 1.5 (base protocol v1.0; extensions: v1.2 compression, v1.3
encryption, v1.3.1 compact format / fixed fields / special values, v1.4
integrity xxh3_128 hashes + xzMercury, v1.5 section-level encryption)
**Date:** 2026-07-22
**Status:** Production ready

---

## Contents

1. [Introduction](#introduction)
2. [Architecture](#architecture)
3. [Format identity](#format-identity)
   - [File extension](#file-extension)
   - [Media type](#media-type)
   - [Identifying a packet](#identifying-a-packet)
   - [Registration status](#registration-status)
4. [Packet format](#packet-format)
   - [Header](#header)
   - [Schema](#schema)
   - [Data](#data)
   - [Integrity](#integrity)
   - [Query (TDTQL)](#query-tdtql)
   - [QueryContext](#querycontext)
5. [Data types](#data-types)
6. [TDTQL query language](#tdtql-query-language)
7. [Compact format v1.3.1](#compact-format-v131)
8. [Examples](#examples)
9. [Adapter-specific behaviour of SpecialValues](#adapter-specific-behaviour-of-specialvalues)
10. [Versioning](#versioning)

---

## Introduction

### Purpose

TDTP (Table Data Transfer Protocol) is a protocol for exchanging tabular data
between systems through message brokers — RabbitMQ, MSMQ, Kafka. It was designed
for:

- **synchronising reference data** between information systems
- **replicating data** between databases of different kinds (SQLite, PostgreSQL, MS SQL)
- **exchanging data** through message queues
- **statistical extracts** with filtering and sorting

### Key properties

- **Self-describing** — every packet carries its full schema
- **Stateless** — every message is independent and carries its whole context
- **Validated** — strict typing, checked at the schema level
- **Paginated** — large tables are split into parts automatically (up to 3.8 MB)
- **Filterable** — TDTQL, a built-in query language
- **Portable** — works with any database and any message broker
- **Compressible** — optional zstd compression for large packets (v1.2+)
- **Encrypted** — AES-256-GCM with UUID binding through xZMercury (v1.3+)
- **Integrity-checked** — XXH3-128 hashes over Schema, Data and Packet, optionally registered with xzMercury (v1.4)

### Data format

- **Container:** XML (UTF-8)
- **Field separator:** pipe `|` (ASCII 124)
- **Maximum packet size:** 3.8 MB (configurable)
- **Encoding:** UTF-8

---

## Architecture

### Packet structure

```
DataPacket
├── Header              (required)
│   ├── Type            (reference|delta|request|response|alarm|error)
│   ├── TableName
│   ├── MessageID       (UUID)
│   ├── Timestamp       (ISO 8601)
│   └── Pagination      (PartNumber/TotalParts)
│
├── Schema              (required for data packets)
│   └── Field[]         (field descriptions)
│       ├── Name
│       ├── Type        (INTEGER|TEXT|DECIMAL|...)
│       ├── Length/Precision/Scale
│       └── Attributes  (key, nullable, timezone, subtype)
│
├── Data                (required for data packets)
│   ├── compression     (optional attribute: "zstd")     v1.2
│   └── Row[]           (pipe-delimited, or compressed)
│
├── Query               (optional, for request/response)
│   ├── Filters         (TDTQL conditions)
│   ├── OrderBy
│   └── Limit/Offset
│
└── QueryContext        (optional, for response)
    └── ExecutionResults
```

### Packet types

| Type | Purpose | Required elements |
|------|---------|-------------------|
| **reference** | Full synchronisation of a reference table | Header, Schema, Data |
| **delta** | Incremental update | Header, Schema, Data, Query |
| **request** | Request for data | Header, Query, Sender, Recipient |
| **response** | Answer to a request | Header, Schema, Data, QueryContext |
| **alarm** | Monitoring notification | Header, AlarmDetails (Severity, Code, Message) |
| **error** | Handled ETL pipeline failure | Header, Schema, Data (a row in `tdtp_errors`) |

> **alarm versus error:** `alarm` uses a non-standard `<AlarmDetails>` block and
> is meant for monitoring systems — it is not compatible with an ETL pipeline.
> `error` is an ordinary `DataPacket` with Schema and Data, written into a
> `tdtp_errors` table, and any downstream consumer can read it. It is produced
> automatically when xZMercury degrades.

---

## Format identity

This section defines what a TDTP packet is called on disk, how it is labelled in
transit, and how software that has never heard of TDTP can recognise one.

### File extension

`.tdtp` is the extension of a TDTP packet.

`.tdtp.xml` is equally valid and is what the reference implementation writes
today; a reader MUST accept both. The compound form is the older convention and
carries a practical benefit — editors, `xmllint` and generic XML tooling
recognise the file without configuration — while `.tdtp` names the format
itself, which matters because the family already contains a member that is not
XML at all: `.tdtp.enc`, the whole-packet binary envelope of the legacy v1.3
encryption.

Which of the two is written by default is an implementation choice, not a
requirement of this specification.

### Media type

```
application/vnd.tdtp+xml
```

A vendor-tree name under RFC 6838, carrying the `+xml` structured syntax suffix
defined in RFC 7303. The name is claimed by this specification and not yet
assigned by IANA — see [Registration status](#registration-status) below.

The suffix is not decoration: it is what allows a generic XML processor to
handle a TDTP packet correctly without knowing anything about TDTP, in the same
way `image/svg+xml` and `application/atom+xml` work.

**Parameters.** `version` (optional) carries the specification version so that a
transport can route without parsing the body; the authoritative value is always
the `version` attribute of `<DataPacket>`. `charset` is permitted as for
`application/xml`, but is redundant — the container is UTF-8 and declares it in
the prolog.

**Compression is not part of the media type.** A compressed packet is still
`application/vnd.tdtp+xml`; the compression is declared separately:

```
Content-Type:     application/vnd.tdtp+xml
Content-Encoding: zstd
```

The `+suffix` convention in RFC 6838 denotes a *structured syntax* — `+xml`,
`+json`, `+cbor` — and not a content coding, so a type of the form
`application/xml+zstd` is not a valid construction and MUST NOT be used.

> **Known deviation.** The Kafka and RabbitMQ transports in this repository
> currently send `application/xml`, and Kafka sends `application/xml+zstd` for
> compressed payloads (`pkg/brokers/kafka.go`). Both predate this section and do
> not conform to it. Consumers should continue to accept them until the
> transports are updated.

### Identifying a packet

A TDTP packet is recognisable from its opening bytes without parsing:

1. the XML prolog, `<?xml version="1.0" encoding="UTF-8"?>`;
2. within roughly 100 bytes, the root element `<DataPacket`;
3. carrying the attribute `protocol="TDTP"`, which is the first attribute of the
   root element in every packet the reference implementation writes.

The file ends with `</DataPacket>`.

**Identify on `protocol`, not on `version`.** The `version` attribute records
which features the packet uses, not which release wrote it: compression leaves
it at `1.0` and is announced by `<Data compression="…">`, the compact format
raises it to `1.3.1`, integrity hashes to `1.4`, and section encryption to
`1.5`. `protocol="TDTP"` is the invariant.

This holds for every variant of the format, including compressed and encrypted
packets: compression touches only the contents of `<Data>`, and v1.5 section
encryption deliberately leaves `<Header>` and the root element in the clear so
that routing, reassembly and identification work without a key.

Byte-level signatures for DROID and PRONOM, with the hexadecimal sequences and
offsets, are in
[`REGISTRATION_DOSSIER.md`](REGISTRATION_DOSSIER.md).

### Registration status

The media type and the PRONOM format entry are **prepared but not yet
registered**. The complete submission — the RFC 6838 template, the PRONOM
metadata and signatures, and a `shared-mime-info` manifest for Linux desktops —
is in [`REGISTRATION_DOSSIER.md`](REGISTRATION_DOSSIER.md).

Until registration completes, `application/vnd.tdtp+xml` should be treated as
the intended type rather than an assigned one.

---

## Packet format

### Header

Metadata about the message.

**XML:**
```xml
<Header>
  <Type>reference</Type>
  <TableName>users</TableName>
  <MessageID>REF-2025-a1b2c3d4-P1</MessageID>
  <PartNumber>1</PartNumber>
  <TotalParts>3</TotalParts>
  <RecordsInPart>1000</RecordsInPart>
  <Timestamp>2025-11-16T12:00:00Z</Timestamp>
  <Sender>SourceSystem</Sender>
  <Recipient>TargetSystem</Recipient>
  <InReplyTo>REQ-2025-xyz123</InReplyTo>
</Header>
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| Type | enum | yes | reference, delta, request, response, alarm, error |
| TableName | string | yes | Table or reference-list name |
| MessageID | UUID | yes | Unique message identifier |
| PartNumber | int | yes | Part number, for pagination |
| TotalParts | int | yes | Total number of parts |
| RecordsInPart | int | no | Rows in this part |
| Timestamp | ISO 8601 | yes | When the packet was created |
| Sender | string | no | Sending system |
| Recipient | string | no | Receiving system |
| InReplyTo | string | no | Request ID, for a response |

### Schema

The structure of the table and the types of its fields.

**XML:**
```xml
<Schema>
  <Field name="id" type="INTEGER" key="true"></Field>
  <Field name="username" type="TEXT" length="100"></Field>
  <Field name="email" type="TEXT" length="255"></Field>
  <Field name="balance" type="DECIMAL" precision="12" scale="2"></Field>
  <Field name="created_at" type="TIMESTAMP" timezone="UTC"></Field>
  <Field name="user_id" type="TEXT" length="-1" subtype="uuid"></Field>
  <Field name="metadata" type="TEXT" length="-1" subtype="jsonb"></Field>
</Schema>
```

**XML with `fixed` and `SpecialValues` (v1.3.1):**
```xml
<Schema>
  <!-- fixed=true: the value does not change within the packet (compact optimisation) -->
  <Field name="dept_id"   type="INTEGER"           fixed="true"></Field>
  <Field name="dept_name" type="TEXT" length="100" fixed="true"></Field>
  <Field name="emp_id"    type="INTEGER"></Field>

  <!-- SpecialValues: markers for NULL, Infinity, NaN, NoDate -->
  <Field name="notes" type="TEXT" length="500">
    <SpecialValues>
      <Null marker="[NULL]"/>
    </SpecialValues>
  </Field>

  <Field name="sensor_value" type="REAL">
    <SpecialValues>
      <Infinity    marker="INF"/>
      <NegInfinity marker="-INF"/>
      <NaN         marker="NaN"/>
    </SpecialValues>
  </Field>

  <Field name="graduation_date" type="DATE">
    <SpecialValues>
      <NoDate marker="1900-01-01"/>
    </SpecialValues>
  </Field>
</Schema>
```

**Field attributes:**

| Attribute | Type | Applies to | Default | Description |
|-----------|------|------------|---------|-------------|
| name | string | all | — | Field name (required) |
| type | enum | all | — | TDTP data type (required) |
| length | int | TEXT, BLOB | — | Maximum length (-1 = unlimited) |
| precision | int | DECIMAL | — | Total number of digits |
| scale | int | DECIMAL | — | Digits after the decimal point |
| timezone | string | TIMESTAMP, TIME | UTC | Time zone (UTC, Local, +03:00) |
| key | bool | any | false | Primary key |
| subtype | string | any | — | Subtype (uuid, jsonb, inet, array) |
| **fixed** | bool | any | false | v1.3.1: the value does not change within the packet |

**Child element `<SpecialValues>`** (v1.3.1)

Declares string markers for values that cannot be expressed directly:

| Element | Attribute | Applies to | Meaning |
|---------|-----------|------------|---------|
| `<Null>` | `marker` | TEXT | NULL, as distinct from the empty string `""` |
| `<Infinity>` | `marker` | REAL, DECIMAL | Positive infinity |
| `<NegInfinity>` | `marker` | REAL, DECIMAL | Negative infinity |
| `<NaN>` | `marker` | REAL | Not a Number (0/0, sqrt(-1)) |
| `<NoDate>` | `marker` | DATE, TIMESTAMP | Absence of a date — not the same as NULL |

**Decoder rules for SpecialValues:**
- If a value equals a marker, apply the corresponding special value
- For TEXT: an empty field `||` is `""`, an empty string that is stored; the `[NULL]` marker is NULL, which is not
- For DATE: the NoDate marker is a sentinel meaning "no date", distinct from NULL

### Data

Rows are pipe-delimited.

**XML, uncompressed:**
```xml
<Data>
  <R>1|john_doe|john@example.com|1500.50|2025-01-15 10:30:00</R>
  <R>2|jane_smith|jane@example.com|2300.00|2025-01-16 14:20:00</R>
</Data>
```

**XML, zstd-compressed (v1.2):**
```xml
<Data compression="zstd">
  <R>KLUv/WBgVKEAAYsBAHNvbWUtY29tcHJlc3NlZC1kYXRhLWhlcmU=</R>
</Data>
```

**Data attributes:**

| Attribute | Type | Values | Description |
|-----------|------|--------|-------------|
| compression | string | `"zstd"` | Compression algorithm (optional, v1.2+) |
| checksum | string | hex | XXH3 hash of the compressed data (v1.2+) |
| **compact** | bool | `"true"` | v1.3.1: fixed fields are written only when they change |

**Compression (v1.2+):**

With `compression="zstd"` set:
- every data row is concatenated and compressed with zstd
- the compressed bytes are base64-encoded
- the result goes into a single `<R>` element
- on decompression the original pipe-delimited rows are restored

**When to compress:**
- packets over 1 KB (configurable)
- large tables with many rows
- to save bandwidth over a message broker
- typical ratio: 50–80%

**Formatting rules:**

- **Separator:** pipe `|` (ASCII 124)
- **Empty value:** nothing between two separators — `field1||field3`
- **NULL:** an absent value is NULL
- **Escaping the separator:** backslash escaping for a pipe inside a value
  - `|` → `\|`
  - `\` → `\\`
- **XML entities:** XML special characters are escaped automatically
  - `<` → `&lt;`
  - `>` → `&gt;`
  - `&` → `&amp;`
  - `"` → `&quot;`
  - `'` → `&apos;`

**Escaping examples:**
```xml
<!-- Plain values -->
<R>value1|value2|value3</R>

<!-- Pipe inside the first value -->
<R>path\|to\|file|value2|value3</R>
<!-- decodes to: ["path|to|file", "value2", "value3"] -->

<!-- Backslash inside a value -->
<R>C:\\Windows\\System32|value2</R>
<!-- decodes to: ["C:\Windows\System32", "value2"] -->

<!-- Both -->
<R>C:\\path\|to\|file|value2</R>
<!-- decodes to: ["C:\path|to|file", "value2"] -->
```

### Integrity

> **v1.4+** — the packet must carry `version="1.4"` on its root element.

#### The hash model

TDTP v1.4 introduces a three-level integrity scheme built on **XXH3-128**
(Cyan4973's algorithm; 128-bit, non-cryptographic, deterministic for identical
input).

| Level | XML field | What is hashed | Salt |
|-------|-----------|----------------|------|
| Schema | `<Schema xxh3="...">` | Canonical schema XML, excluding the schema's own `xxh3` attribute | MessageID |
| Data | `<Data xxh3="...">` | Raw rows before compression: `row₀\nrow₁\n…rowN\n` | MessageID |
| Packet | `<DataPacket … xxh3="...">` | `SchemaXXH3 + "|" + DataXXH3` | — (already carried by the components) |

**The salt** is the packet's `MessageID` (a UUID), prepended to each hash input.
It prevents a captured hash from being reused against a different packet. The
salt is not a secret — it sits in plain text in the `<Header>`.

All three values are 32-character lowercase hex strings (128 bits = 16 bytes).

#### XML (v1.4)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.4"
            xxh3="c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6">
  <Header>
    <Type>reference</Type>
    <TableName>users</TableName>
    <MessageID>550e8400-e29b-41d4-a716-446655440000</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>3</RecordsInPart>
    <Timestamp>2026-05-26T10:00:00Z</Timestamp>
  </Header>
  <Schema xxh3="a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6">
    <Field name="id"   type="INTEGER" key="true"></Field>
    <Field name="name" type="TEXT" length="100"></Field>
  </Schema>
  <Data xxh3="f8e7d6c5b4a3928171605040302010ff">
    <R>1|Alice</R>
    <R>2|Bob</R>
    <R>3|Carol</R>
  </Data>
</DataPacket>
```

With compression (`--compress --integrity`) the hashes are computed over the
**uncompressed** rows and stored before compression runs. The receiver
decompresses first and verifies second.

```xml
<Data compression="zstd" xxh3="f8e7d6c5b4a3928171605040302010ff">
  <R>KLUv/WBgVKEA...</R>
</Data>
```

> **`Data.checksum` versus `Data.xxh3`:**
> - `checksum` (v1.2) is an XXH3-64 hash of the **compressed** blob, kept for backward compatibility
> - `xxh3` (v1.4) is an XXH3-128 hash of the **uncompressed** rows, and part of the three-level scheme

#### CLI: exporting with integrity

```
--integrity             Compute the xxh3_128 hashes (Schema + Data + Packet),
                        set the attributes, set version="1.4".
                        Hashes are computed BEFORE compression.
--mercury-url <url>     Register the packet fingerprint with xzMercury
                        (for example http://mercury:3000).
                        Omitted: local hashes only.
--mercury-caller <name> Service identifier for xzMercury (default "tdtpcli").
```

Examples:

```bash
# Local hashes only
tdtpcli --export users --compress --integrity --output users_v14.tdtp.xml

# Hashes plus registration with Mercury
tdtpcli --export orders --compress --integrity \
        --mercury-url http://mercury:3000 --mercury-caller svc-exporter \
        --output orders_v14.tdtp.xml
```

The export prints the first eight hex characters of each hash:

```
  → Integrity: schema=a1b2c3d4… data=f8e7d6c5… packet=c3d4e5f6…
```

#### Verification

Verification runs automatically in any command that reads a packet: `--import`,
`--to-csv`, `--to-xlsx`, `--to-html`, `--test`.

1. **Mercury pre-flight**, when the packet carries `@MRC` in its dictionary: `GET /api/hashes/{uuid}/{part}?xxh3=...`
2. **Local xxh3 check**: recompute the three hashes and compare them with the XML attributes
3. **Dictionary expansion**: replace `@` tokens in the data

If Mercury is unreachable the system falls back to **FallbackDegrade** and runs
step 2 only.

On a hash mismatch:
- `--import` aborts **before** anything is written to the database
- `--to-csv`, `--to-xlsx`, `--to-html` return an error and create no file
- `--test` prints the error and exits non-zero

A packet with no `xxh3` attribute — anything produced before v1.4 — skips
verification silently.

#### The xzMercury hash registry

xzMercury is an optional fingerprint registry. It stores one record per
`(uuid, part_number)`:

```
POST /api/hashes
  { "uuid": "550e...", "part": 1, "xxh3": "c3d4...", "table": "users",
    "caller": "svc-exporter", "version": "1.4" }

GET /api/hashes/{uuid}/{part}?xxh3=c3d4...
  → 200 OK        hash matches
  → 409 Conflict  registered under a different hash — possible tampering
  → 404 Not Found packet not registered
```

On registration the Mercury address is embedded in the packet dictionary as the
token `@MRC`, so a consumer can find the registry without being configured for
it.

### Query (TDTQL)

The filter structure.

**XML:**
```xml
<Query language="TDTQL" version="1.0">
  <Filters>
    <And>
      <Filter field="balance" operator="gte" value="1000"></Filter>
      <Filter field="is_active" operator="eq" value="1"></Filter>
    </And>
  </Filters>
  <OrderBy field="balance" direction="DESC"></OrderBy>
  <Limit>100</Limit>
  <Offset>0</Offset>
</Query>
```

See [TDTQL query language](#tdtql-query-language).

### QueryContext

Execution context, present only in a response.

**XML:**
```xml
<QueryContext>
  <OriginalQuery language="TDTQL" version="1.0">
    <!-- copy of the original request -->
  </OriginalQuery>
  <ExecutionResults>
    <TotalRecordsInTable>10000</TotalRecordsInTable>
    <RecordsAfterFilters>150</RecordsAfterFilters>
    <RecordsReturned>100</RecordsReturned>
    <MoreDataAvailable>true</MoreDataAvailable>
  </ExecutionResults>
</QueryContext>
```

---

## Data types

### Base types

| TDTP type | Description | SQL equivalents | Representation in Data |
|-----------|-------------|-----------------|------------------------|
| **INTEGER** | Whole number | INT, BIGINT, SERIAL | `123`, `-456` |
| **REAL** | Floating point | FLOAT, DOUBLE | `123.45`, `-0.001` |
| **DECIMAL** | Exact number | DECIMAL(p,s), NUMERIC | `1234.56` |
| **TEXT** | String | VARCHAR, TEXT, NVARCHAR | `Hello World` |
| **BLOB** | Binary | BLOB, BYTEA, VARBINARY | Base64 |
| **BOOLEAN** | Boolean | BOOLEAN, BIT | `0` (false), `1` (true) |
| **DATE** | Date | DATE | `2025-01-15` (ISO 8601) |
| **TIME** | Time | TIME | `14:30:00` (ISO 8601) |
| **TIMESTAMP** | Date and time | TIMESTAMP, DATETIME | `2025-01-15 14:30:00` |

### Type attributes

**LENGTH** (TEXT, BLOB):
- a positive number is the maximum length
- `-1` means unlimited (TEXT, JSONB, UUID)

**PRECISION and SCALE** (DECIMAL):
- `precision` — total significant digits
- `scale` — digits after the decimal point
- `DECIMAL(12,2)` → `precision="12" scale="2"` → `9999999999.99`

**TIMEZONE** (TIMESTAMP, TIME):
- `UTC`
- `Local` — the system's local time
- `+03:00`, `-05:00` — a specific offset

**KEY**:
- `true` — the field is part of the primary key
- `false` or absent — an ordinary field

**SUBTYPE**:
- `uuid` — UUID/GUID (`TEXT length="-1" subtype="uuid"`)
- `jsonb` — JSON binary (`TEXT length="-1" subtype="jsonb"`)
- `json` — JSON text (`TEXT length="-1" subtype="json"`)
- `inet` — IP address (`TEXT subtype="inet"`)
- `array` — array (`TEXT subtype="array"`)
- `timestamptz` — timestamp with time zone (`TIMESTAMP timezone="UTC" subtype="timestamptz"`)

### Special types through subtype

**UUID:**
```xml
<Field name="user_id" type="TEXT" length="-1" subtype="uuid"></Field>
<R>e5f1c2a3-8d7b-4c9e-a1f0-2b3c4d5e6f7a</R>
```

**JSONB:**
```xml
<Field name="metadata" type="TEXT" length="-1" subtype="jsonb"></Field>
<R>{&quot;key&quot;:&quot;value&quot;,&quot;count&quot;:42}</R>
```

**INET:**
```xml
<Field name="ip_address" type="TEXT" length="-1" subtype="inet"></Field>
<R>192.168.1.100</R>
```

**ARRAY:**
```xml
<Field name="tags" type="TEXT" length="-1" subtype="array"></Field>
<R>{tag1,tag2,tag3}</R>
```

---

## Compact format v1.3.1

### The problem

In one-to-many join patterns — views, reports — many columns repeat the same
value on every row. In the base v1.0 format that duplication costs 50–70%
overhead.

**Duplication in v1.0:**
```xml
<Data>
  <R>10|Sales|Moscow|101|Ivan Petrov|45000</R>
  <R>10|Sales|Moscow|102|Anna Sidorova|52000</R>
  <R>10|Sales|Moscow|103|Boris Kozlov|48000</R>
</Data>
```

`dept_id`, `dept_name` and `location` repeat on every row.

### The solution

Three complementary mechanisms in v1.3.1:

1. **`fixed="true"`** on a Field — declares that the field does not change within a group
2. **`compact="true"`** on Data — fixed values are written only when they change
3. **`<SpecialValues>`** on a Field — markers for NULL, Infinity, NaN, NoDate

### Fixed fields

`fixed="true"` on a `<Field>` tells the processor that the value is constant
across a run of rows.

```xml
<Schema>
  <Field name="dept_id"   type="INTEGER" fixed="true"></Field>   <!-- constant -->
  <Field name="dept_name" type="TEXT"    fixed="true"></Field>   <!-- constant -->
  <Field name="emp_id"    type="INTEGER"></Field>                <!-- varies -->
  <Field name="emp_name"  type="TEXT"></Field>                   <!-- varies -->
</Schema>
```

**Convention for SQL views (`_prefix`):**

Prefix a view's column with `_` to mark it fixed. `tdtpcli` detects those
automatically, strips the `_` from the name, and sets `fixed="true"`:

```sql
CREATE VIEW dept_employees_report AS
SELECT
  d.dept_id   AS _dept_id,     -- becomes: name="dept_id" fixed="true"
  d.dept_name AS _dept_name,   -- becomes: name="dept_name" fixed="true"
  d.location  AS _location,    -- becomes: name="location" fixed="true"
  e.emp_id,                    -- varies
  e.full_name
FROM employees e
JOIN departments d ON e.dept_id = d.dept_id
ORDER BY dept_id, emp_id;
```

### The compact format

With `compact="true"` on `<Data>`, a fixed field's value is written only:
- on the first row, which is the group's **header row**
- when the value changes, which starts a new group

Everywhere else in the group, the fixed field's position holds an empty string
(`||`).

**Example — three departments, five employees each:**
```xml
<Data compact="true">
  <!-- dept 10 — header row: every value present -->
  <R>10|Sales|Moscow|101|Ivan Petrov|45000</R>
  <!-- carry-forward: dept_id/dept_name/location from the row above -->
  <R>|||102|Anna Sidorova|52000</R>
  <R>|||103|Boris Kozlov|48000</R>
  <R>|||104|Elena Novikova|55000</R>
  <R>|||105|Dmitry Smirnov|49500</R>
  <!-- dept 20 — new group: every value again -->
  <R>20|Engineering|Saint Petersburg|201|Alice Volkov|72000</R>
  <R>|||202|Charlie Morozov|65000</R>
  <R>|||203|Diana Popova|69000</R>
  <R>|||204|Egor Lebedev|61000</R>
  <R>|||205|Fiona Kuznetsova|78000</R>
</Data>
```

**Decoder algorithm (carry-forward):**

```
currentFixed = []

for each row:
  for each position i:
    if field[i].fixed == true:
      if values[i] != "":
        currentFixed[i] = values[i]   // new value → update the carry
      else:
        values[i] = currentFixed[i]   // gap → take from the carry
```

**Note:** the decoder does not verify that `fixed="true"` is correct. That is
the sender's responsibility.

### Processing order

**Encoding:**
```
1. Determine the fixed fields from the Schema (or by _prefix, or from --fixed-fields)
2. For each row:
   - value equals the previous one → write ""
   - otherwise → write it explicitly
3. Set compact="true" on <Data>
4. Set version="1.3.1" on the packet
5. Optionally compress: compression="zstd"
```

**Decoding:**
```
1. Decompress zstd, if compression="zstd"
2. If compact="true": expand the carry-forward into normalised rows
3. Apply the <SpecialValues> markers
4. Import as an ordinary set of rows
```

### Combining with compression

The two are compatible:

```xml
<Data compression="zstd" compact="true">
  <R>KLUv/WBgVKEAAesEA...base64-compressed-compact-data...</R>
</Data>
```

### Size saved

| Case | v1.0 | v1.3.1 compact | Saved |
|------|------|----------------|-------|
| 3 fixed fields × 15 rows | 100% | ~30% | ~70% |
| plus zstd compression | 100% | ~10–15% | ~85–90% |

---

## TDTQL query language

**TDTQL** (Table Data Transfer Query Language) filters and sorts tabular data.

### Query structure

```xml
<Query language="TDTQL" version="1.0">
  <Filters>
    <!-- conditions -->
  </Filters>
  <OrderBy>
    <!-- sorting -->
  </OrderBy>
  <Limit>100</Limit>
  <Offset>0</Offset>
</Query>
```

### Comparison operators

| Operator | Meaning | SQL | Example |
|----------|---------|-----|---------|
| `eq` | Equal | `=` | `<Filter field="age" operator="eq" value="25"/>` |
| `ne` | Not equal | `!=`, `<>` | `<Filter field="status" operator="ne" value="deleted"/>` |
| `gt` | Greater than | `>` | `<Filter field="balance" operator="gt" value="1000"/>` |
| `gte` | Greater or equal | `>=` | `<Filter field="age" operator="gte" value="18"/>` |
| `lt` | Less than | `<` | `<Filter field="price" operator="lt" value="100"/>` |
| `lte` | Less or equal | `<=` | `<Filter field="quantity" operator="lte" value="10"/>` |

### Range and list operators

| Operator | Meaning | SQL | Example |
|----------|---------|-----|---------|
| `between` | Within a range | `BETWEEN` | `<Filter field="age" operator="between" value="18" value2="65"/>` |
| `in` | In a list | `IN` | `<Filter field="city" operator="in" value="Moscow,SPb,Kazan"/>` |
| `not_in` | Not in a list | `NOT IN` | `<Filter field="status" operator="not_in" value="deleted,archived"/>` |

### Pattern operators

| Operator | Meaning | SQL | Example |
|----------|---------|-----|---------|
| `like` | Matches a pattern | `LIKE` | `<Filter field="email" operator="like" value="%@example.com"/>` |
| `not_like` | Does not match | `NOT LIKE` | `<Filter field="username" operator="not_like" value="test%"/>` |

Wildcards:
- `%` — any number of characters
- `_` — exactly one character

### NULL operators

| Operator | Meaning | SQL | Example |
|----------|---------|-----|---------|
| `is_null` | Is NULL | `IS NULL` | `<Filter field="deleted_at" operator="is_null"/>` |
| `is_not_null` | Is not NULL | `IS NOT NULL` | `<Filter field="email" operator="is_not_null"/>` |

### Logical operators

**AND:**
```xml
<Filters>
  <And>
    <Filter field="age" operator="gte" value="18"/>
    <Filter field="is_active" operator="eq" value="1"/>
  </And>
</Filters>
```

**OR:**
```xml
<Filters>
  <Or>
    <Filter field="city" operator="eq" value="Moscow"/>
    <Filter field="city" operator="eq" value="SPb"/>
  </Or>
</Filters>
```

**Nested groups:**
```xml
<Filters>
  <And>
    <Filter field="is_active" operator="eq" value="1"/>
    <Or>
      <Filter field="city" operator="eq" value="Moscow"/>
      <Filter field="city" operator="eq" value="SPb"/>
    </Or>
  </And>
</Filters>
```

SQL equivalent:
```sql
WHERE is_active = 1 AND (city = 'Moscow' OR city = 'SPb')
```

### Sorting

**Single field:**
```xml
<OrderBy field="balance" direction="DESC"></OrderBy>
```

**Several fields:**
```xml
<OrderBy>
  <Fields>
    <OrderField name="balance" direction="DESC"/>
    <OrderField name="created_at" direction="ASC"/>
  </Fields>
</OrderBy>
```

**Direction:**
- `ASC` — ascending (default)
- `DESC` — descending

### Pagination

```xml
<Limit>100</Limit>
<Offset>200</Offset>
```

- **Limit** — maximum rows returned
- **Offset** — rows to skip

SQL equivalent:
```sql
LIMIT 100 OFFSET 200
```

### A complete TDTQL example

**In words:**
```
Find active users over 18 with a balance of at least 1000, in Moscow or
Saint Petersburg, sorted by balance descending, first 50 rows.
```

**TDTQL:**
```xml
<Query language="TDTQL" version="1.0">
  <Filters>
    <And>
      <Filter field="is_active" operator="eq" value="1"/>
      <Filter field="age" operator="gte" value="18"/>
      <Filter field="balance" operator="gte" value="1000"/>
      <Or>
        <Filter field="city" operator="eq" value="Moscow"/>
        <Filter field="city" operator="eq" value="SPb"/>
      </Or>
    </And>
  </Filters>
  <OrderBy field="balance" direction="DESC"></OrderBy>
  <Limit>50</Limit>
  <Offset>0</Offset>
</Query>
```

**SQL equivalent:**
```sql
SELECT * FROM users
WHERE is_active = 1
  AND age >= 18
  AND balance >= 1000
  AND (city = 'Moscow' OR city = 'SPb')
ORDER BY balance DESC
LIMIT 50 OFFSET 0
```

---

## Examples

### Reference packet — a full reference table

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>reference</Type>
    <TableName>users</TableName>
    <MessageID>REF-2025-abc123-P1</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>3</RecordsInPart>
    <Timestamp>2025-11-16T12:00:00Z</Timestamp>
  </Header>
  <Schema>
    <Field name="id" type="INTEGER" key="true"></Field>
    <Field name="username" type="TEXT" length="100"></Field>
    <Field name="email" type="TEXT" length="255"></Field>
    <Field name="balance" type="DECIMAL" precision="12" scale="2"></Field>
    <Field name="is_active" type="BOOLEAN"></Field>
    <Field name="created_at" type="TIMESTAMP" timezone="UTC"></Field>
  </Schema>
  <Data>
    <R>1|john_doe|john@example.com|1500.50|1|2025-01-15 10:30:00</R>
    <R>2|jane_smith|jane@example.com|2300.00|1|2025-01-16 14:20:00</R>
    <R>3|bob_jones|bob@example.com|750.25|0|2025-01-17 09:15:00</R>
  </Data>
</DataPacket>
```

### Request packet

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>request</Type>
    <TableName>users</TableName>
    <MessageID>REQ-2025-xyz789</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <Timestamp>2025-11-16T12:00:00Z</Timestamp>
    <Sender>ClientApp</Sender>
    <Recipient>ServerDB</Recipient>
  </Header>
  <Query language="TDTQL" version="1.0">
    <Filters>
      <And>
        <Filter field="balance" operator="gte" value="1000"></Filter>
        <Filter field="is_active" operator="eq" value="1"></Filter>
      </And>
    </Filters>
    <OrderBy field="balance" direction="DESC"></OrderBy>
    <Limit>100</Limit>
  </Query>
</DataPacket>
```

### Response packet

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>response</Type>
    <TableName>users</TableName>
    <MessageID>RESP-2025-def456-P1</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>2</RecordsInPart>
    <Timestamp>2025-11-16T12:00:01Z</Timestamp>
    <Sender>ServerDB</Sender>
    <Recipient>ClientApp</Recipient>
    <InReplyTo>REQ-2025-xyz789</InReplyTo>
  </Header>
  <QueryContext>
    <OriginalQuery language="TDTQL" version="1.0">
      <Filters>
        <And>
          <Filter field="balance" operator="gte" value="1000"></Filter>
          <Filter field="is_active" operator="eq" value="1"></Filter>
        </And>
      </Filters>
      <OrderBy field="balance" direction="DESC"></OrderBy>
      <Limit>100</Limit>
    </OriginalQuery>
    <ExecutionResults>
      <TotalRecordsInTable>1000</TotalRecordsInTable>
      <RecordsAfterFilters>2</RecordsAfterFilters>
      <RecordsReturned>2</RecordsReturned>
      <MoreDataAvailable>false</MoreDataAvailable>
    </ExecutionResults>
  </QueryContext>
  <Schema>
    <Field name="id" type="INTEGER" key="true"></Field>
    <Field name="username" type="TEXT" length="100"></Field>
    <Field name="balance" type="DECIMAL" precision="12" scale="2"></Field>
    <Field name="is_active" type="BOOLEAN"></Field>
  </Schema>
  <Data>
    <R>2|jane_smith|2300.00|1</R>
    <R>1|john_doe|1500.50|1</R>
  </Data>
</DataPacket>
```

### Delta packet — incremental update

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>delta</Type>
    <TableName>users</TableName>
    <MessageID>DELTA-2025-ghi012</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>1</RecordsInPart>
    <Timestamp>2025-11-16T12:05:00Z</Timestamp>
  </Header>
  <Query language="TDTQL" version="1.0">
    <Filters>
      <And>
        <Filter field="updated_at" operator="gte" value="2025-11-16 12:00:00"></Filter>
      </And>
    </Filters>
  </Query>
  <Schema>
    <Field name="id" type="INTEGER" key="true"></Field>
    <Field name="username" type="TEXT" length="100"></Field>
    <Field name="balance" type="DECIMAL" precision="12" scale="2"></Field>
    <Field name="updated_at" type="TIMESTAMP" timezone="UTC"></Field>
  </Schema>
  <Data>
    <R>1|john_doe|1600.00|2025-11-16 12:03:00</R>
  </Data>
</DataPacket>
```

### Alarm packet — monitoring notification

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>alarm</Type>
    <TableName>users</TableName>
    <MessageID>ALARM-2025-err404</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <Timestamp>2025-11-16T12:00:00Z</Timestamp>
    <Sender>ServerDB</Sender>
    <Recipient>MonitoringSystem</Recipient>
  </Header>
  <Alarm>
    <Severity>error</Severity>
    <Code>DB_CONNECTION_FAILED</Code>
    <Message>Failed to connect to PostgreSQL database: connection timeout</Message>
    <AffectedRecords>0</AffectedRecords>
  </Alarm>
</DataPacket>
```

### Error packet — handled ETL failure (v1.3+)

Produced automatically by the ETL pipeline when xZMercury degrades — encryption
is enabled and Mercury is unreachable. It is written to the output file in place
of unencrypted data, and the pipeline exits 0.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>error</Type>
    <TableName>tdtp_errors</TableName>
    <MessageID>ERR-2026-a1b2c3d4-P1</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>1</RecordsInPart>
    <Timestamp>2026-02-26T10:00:00Z</Timestamp>
  </Header>
  <Schema>
    <Field name="package_uuid"  type="TEXT" length="36" key="true"></Field>
    <Field name="pipeline"      type="TEXT" length="255"></Field>
    <Field name="error_code"    type="TEXT" length="64"></Field>
    <Field name="error_message" type="TEXT" length="1000"></Field>
    <Field name="created_at"    type="TIMESTAMP" timezone="UTC"></Field>
  </Schema>
  <Data>
    <R>550e8400-e29b-41d4-a716-446655440000|employee-dept-report|MERCURY_UNAVAILABLE|connect: connection refused|2026-02-26T10:00:00Z</R>
  </Data>
</DataPacket>
```

**Error codes:**

| Code | Cause |
|------|-------|
| `MERCURY_UNAVAILABLE` | xZMercury unreachable (timeout, connection refused) |
| `MERCURY_ERROR` | xZMercury returned HTTP 5xx |
| `HMAC_VERIFICATION_FAILED` | The key signature did not verify |
| `KEY_BIND_REJECTED` | xZMercury refused the request (HTTP 403/429) |

---

### Reference packet in compact format (v1.3.1+)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.3.1">
  <Header>
    <Type>reference</Type>
    <TableName>dept_employees_report</TableName>
    <MessageID>REF-2026-compact-001-P1</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>10</RecordsInPart>
    <Timestamp>2026-03-10T10:00:00Z</Timestamp>
  </Header>
  <Schema>
    <!-- Three fixed fields (_prefix in the SQL view → stripped, fixed=true) -->
    <Field name="dept_id"   type="INTEGER"            fixed="true"></Field>
    <Field name="dept_name" type="TEXT" length="100"  fixed="true"></Field>
    <Field name="location"  type="TEXT" length="100"  fixed="true"></Field>
    <!-- Varying fields -->
    <Field name="emp_id"    type="INTEGER"></Field>
    <Field name="full_name" type="TEXT" length="100"></Field>
    <Field name="salary"    type="DECIMAL" precision="10" scale="2"></Field>
    <!-- SpecialValues: NULL in a TEXT field -->
    <Field name="notes" type="TEXT" length="500">
      <SpecialValues>
        <Null marker="[NULL]"/>
      </SpecialValues>
    </Field>
  </Schema>
  <Data compact="true">
    <!-- dept 10 — header row: all seven values -->
    <R>10|Sales|Moscow|101|Ivan Petrov|45000.00|on probation</R>
    <!-- carry-forward: dept_id/dept_name/location from the row above -->
    <R>|||102|Anna Sidorova|52000.00|[NULL]</R>
    <R>|||103|Boris Kozlov|48000.00|[NULL]</R>
    <R>|||104|Elena Novikova|55000.00|team lead</R>
    <R>|||105|Dmitry Smirnov|49500.00|[NULL]</R>
    <!-- dept 20 — new group: all values again -->
    <R>20|Engineering|Saint Petersburg|201|Alice Volkov|72000.00|[NULL]</R>
    <R>|||202|Charlie Morozov|65000.00|[NULL]</R>
    <R>|||203|Diana Popova|69000.00|[NULL]</R>
    <R>|||204|Egor Lebedev|61000.00|[NULL]</R>
    <R>|||205|Fiona Kuznetsova|78000.00|[NULL]</R>
  </Data>
</DataPacket>
```

**Decoded:**

| dept_id | dept_name | location | emp_id | full_name | salary | notes |
|---------|-----------|----------|--------|-----------|--------|-------|
| 10 | Sales | Moscow | 101 | Ivan Petrov | 45000.00 | on probation |
| 10 | Sales | Moscow | 102 | Anna Sidorova | 52000.00 | NULL |
| 10 | Sales | Moscow | 103 | Boris Kozlov | 48000.00 | NULL |
| … | … | … | … | … | … | … |
| 20 | Engineering | Saint Petersburg | 201 | Alice Volkov | 72000.00 | NULL |

---

### Reference packet with compression (v1.2+)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>reference</Type>
    <TableName>orders</TableName>
    <MessageID>REF-2025-compressed-001</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>1000</RecordsInPart>
    <Timestamp>2025-12-08T10:00:00Z</Timestamp>
  </Header>
  <Schema>
    <Field name="id" type="INTEGER" key="true"></Field>
    <Field name="customer_id" type="INTEGER"></Field>
    <Field name="product_name" type="TEXT" length="200"></Field>
    <Field name="quantity" type="INTEGER"></Field>
    <Field name="price" type="DECIMAL" precision="10" scale="2"></Field>
    <Field name="order_date" type="TIMESTAMP" timezone="UTC"></Field>
  </Schema>
  <Data compression="zstd">
    <R>KLUv/WBgUKEAAesEABWsAgBZCwIIbGFy...base64-encoded-compressed-data...</R>
  </Data>
</DataPacket>
```

**Notes on the compressed form:**

1. `compression="zstd"` states that the data is zstd-compressed
2. A **single `<R>` element** holds all of it, instead of many rows
3. Base64 keeps binary data safe inside XML
4. `RecordsInPart=1000` is the real row count after decompression
5. Decompressing yields 1000 ordinary pipe-delimited rows

**Benefits:**
- 50–80% smaller packets
- less bandwidth over a message broker
- handled automatically by the framework (v1.2+)

**When it applies:**
- automatically for packets over 1 KB (configurable)
- most effective on large tables
- recommended over slow links

---

## Adapter-specific behaviour of SpecialValues

SpecialValues markers (v1.3.1) have one meaning at the protocol level, but every
adapter meets the limits of its target system. This section records what each
one actually does.

### PostgreSQL

| Marker | Field type | On import |
|--------|-----------|-----------|
| `[NULL]` | TEXT | `NULL`, not an empty string |
| `NaN` | REAL / NUMERIC | `'NaN'::numeric` — PostgreSQL supports NaN natively |
| `INF` | REAL / NUMERIC | `'infinity'::numeric` |
| `-INF` | REAL / NUMERIC | `'-infinity'::numeric` |
| `0000-00-00` | DATE | `NULL` (NoDate sentinel) |
| `infinity` / `-infinity` | DATE, TIMESTAMP | `'infinity'::timestamp` / `'-infinity'::timestamp` (PostgreSQL-specific) |

> PostgreSQL is the only adapter that stores `NaN`, `INF` and `-INF` as
> **numeric values** rather than NULL. Exporting from PostgreSQL encodes those
> values back into SpecialValues markers automatically.

### MS SQL Server

| Marker | Field type | On import |
|--------|-----------|-----------|
| `[NULL]` | TEXT | `NULL` |
| `NaN`, `INF`, `-INF` | FLOAT | `NULL` — MSSQL has no NaN or Inf in its numeric types |
| `0000-00-00` | DATE / DATETIME | `NULL` |

> On MSSQL, importing `NaN`/`INF`/`-INF` into a numeric column stores `NULL`.
> That is a loss of meaning: if the business logic distinguishes "no data" from
> "mathematically undefined", carry a separate flag column.

### MySQL

| Marker | Field type | On import |
|--------|-----------|-----------|
| `[NULL]` | TEXT | `NULL` |
| `NaN`, `INF`, `-INF` | FLOAT / DOUBLE | `NULL` (strict SQL mode) |
| `0000-00-00` | DATE | `'0000-00-00'` when `NO_ZERO_DATE` is unset; otherwise `NULL` |

> MySQL in strict mode (`sql_mode=STRICT_TRANS_TABLES`) rejects `0000-00-00` as
> an invalid date. In non-strict mode the NoDate sentinel is stored as
> `0000-00-00`. The framework's `NoDateSentinels` configuration controls this.

### SQLite

| Marker | Field type | On import |
|--------|-----------|-----------|
| `[NULL]` | TEXT | `NULL` |
| `NaN`, `INF`, `-INF` | REAL | `NULL` — SQLite stores numbers as float64 and does not distinguish NaN from Inf |
| `0000-00-00` | TEXT (date as string) | Stored literally as `"0000-00-00"` |

> SQLite has no dedicated DATE type — dates live in TEXT or INTEGER columns, so
> the NoDate sentinel is stored verbatim as a string.

### XLSX (Excel)

Excel is the most constrained target. Use this table when designing a pipeline
whose output is a spreadsheet.

**Export (TDTP → XLSX):**

| Value | Field type | Written to the cell | Cell type |
|-------|-----------|---------------------|-----------|
| `[NULL]` | any | empty cell | Blank |
| `NaN` | REAL | empty cell | Blank |
| `INF` | REAL | empty cell | Blank |
| `-INF` | REAL | empty cell | Blank |
| int64 ≤ 15 digits | INTEGER | number | Number |
| int64 > 15 digits | INTEGER | string `"1234567890123456789"` | Text |
| date ≥ 1900-01-01 | DATE | float serial plus a date format | Date |
| date < 1900-01-01 | DATE | string `"1899-10-12"` | Text |
| strings starting `=`, `+`, `-`, `@` | TEXT | string (written with `SetCellStr`) | Text |

> **Why NaN and Inf become blank rather than text?**
> The literal string `"NaN"` in a numeric column breaks Excel formulas — `=SUM()`
> returns `#VALUE!`. An empty cell is the safe canonical NULL for a business user.
>
> **Why BIGINT over 15 digits becomes a string?**
> Excel stores every number as IEEE-754 float64, giving 15 significant digits.
> Written as a number, `1234567890123456789` becomes `1234567890123456800` —
> silent data loss.
>
> **The 1900 leap-year bug:** Excel wrongly treats 1900 as a leap year (serial 60
> is 29 February 1900, a date that never existed). The framework compensates for
> the shift on import by inverting the serial calculation with a correction for
> the phantom day.

**Import (XLSX → TDTP):**

| Excel cell type | What is read | What is written to TDTP |
|-----------------|--------------|-------------------------|
| Number, date-styled | serial float | ISO date/datetime via epoch arithmetic |
| Number, ordinary | raw decimal string | the value as-is |
| String | trimmed string | the value as-is |
| Error (`#N/A`, `#DIV/0!`, `#NUM!`, …) | the error code | `""` → NULL |
| Blank | `""` | `""` → NULL (for non-TEXT types) |
| Boolean | `TRUE`/`FALSE`/`1`/`0` | `"1"` / `"0"` |

### Python (pandas)

| Marker | pandas column type | Behaviour |
|--------|--------------------|-----------|
| `[NULL]` | any | `None` → `NaN` in float columns, `pd.NA` in nullable int/string |
| `NaN` | float64 | `float('nan')` — native NaN, `pd.isna()` is True |
| `INF` | float64 | `float('inf')` — native +∞ |
| `-INF` | float64 | `float('-inf')` — native −∞ |
| `0000-00-00` | datetime / object | `None` → `NaT` on datetime conversion |

> Markers are applied **before** `astype()` is called. That prevents a
> `ValueError` when converting the string `"NaN"` to a numeric type. `"INF"` is
> converted through the strings `"inf"`/`"-inf"`, which `pandas.to_numeric()`
> understands natively.

---

## Versioning

**Current version:** 1.5

### Changelog

**v1.5** (2026-07-22)

- **Section-level encryption** replaces v1.3's opaque whole packet with
  selective encryption of sections, mirroring how compression works in v1.2.
  `<Header>` stays as plain XML — routing, deduplication and multi-part
  reassembly need no key — while `<QueryContext>`, `<Schema>` and `<Data>` each
  become opaque ciphertext carrying `encryption="aes-256-gcm"`.
  - Section format: `BASE64(nonce || ciphertext)`, with a distinct nonce per
    section under one key (AES-256-GCM forbids reusing a nonce with the same key)
  - The key is bound to the packet's `Header.MessageID` (`POST /api/keys/bind`)
    rather than to a separately generated UUID as in v1.3, so a receiver can
    learn which UUID to `RetrieveKey` for by reading only the plain Header,
    without decrypting anything
  - Multi-part packets need no special handling: each part already carries its
    own unique `MessageID` (`{base}-P{n}`), so `BindKey` is simply called once
    per part, in the same place as before
  - The order of transformations is fixed and not configurable: hash (v1.4) →
    compress → encrypt when writing; decrypt → decompress → verify when reading
  - **v1.4 integration is mandatory:** `ComputeIntegrity` and `RegisterHash` now
    run before every v1.5 encryption even without an explicit `--integrity`.
    Otherwise the consumer's pre-flight (`VerifyAndPrepare`) blocks the packet
    with `HASH_NOT_REGISTERED`, because that pre-flight already applies to any
    packet of version ≥1.4 whenever `--mercury-url` is set — which v1.5
    decryption always requires
- **CLI:** `--enc` now produces v1.5 by default, where it previously produced
  v1.3. The new `--enc13` explicitly requests the legacy whole-blob format for
  consumers not yet updated. Available on `--export`, `--export-broker` and
  `--pipeline` (`output.tdtp.encryption`)
- **Compatibility:** `--import`, `--map` and `--import-broker` detect the format
  from the bytes themselves — a v1.0–v1.4 binary header versus a v1.5
  `encryption` XML attribute — so previously encrypted packets keep decrypting
  unchanged
- **Backward compatibility:** a v1.4-or-earlier reader does not understand v1.5.
  It will not attempt to decrypt it; it sees an opaque `<Schema encryption="…">`
  and `<Data encryption="…">` with no readable fields, and simply cannot read
  the data without being updated
- Full schema, producer and consumer diagrams, the threat analysis and the
  resolved design questions are in
  [`docs/tdtp-protocol-schema.md`](tdtp-protocol-schema.md), section "v1.5"

**v1.4** (2026-05-26)

- **Integrity xxh3_128** — three-level packet integrity
  - `xxh3` on `<DataPacket>` — PacketXXH3 = xxh3_128(SchemaXXH3 + "|" + DataXXH3)
  - `xxh3` on `<Schema>` — xxh3_128(salt + canonical Schema XML)
  - `xxh3` on `<Data>` — xxh3_128(salt + raw rows before compression)
  - The salt is the packet's MessageID (UUID), which prevents replay
  - Hashes are computed **before** compression; the receiver decompresses first and verifies second
  - CLI: `--integrity` on export; verification is automatic in `--import`, `--to-csv`, `--to-xlsx`, `--to-html` and `--test`
- **xzMercury hash registry** — optional fingerprint registration
  - `--mercury-url` — the registry address; on registration it is embedded in the packet dictionary as the `@MRC` token
  - `--mercury-caller` — the sending service's identifier (default "tdtpcli")
  - FallbackDegrade: with Mercury unreachable, only the local xxh3 check runs
- **Backward compatibility:** a v1.3.1-or-earlier reader reads v1.4 packets, ignoring the `xxh3` attributes

**v1.3.1** (2026-03-10)

- **Fixed fields** — `fixed="true"` on `<Field>`
  - Declares the field constant across a run of rows
  - The `_fieldname` convention in a SQL view is auto-detected: `tdtpcli` strips the `_` and sets `fixed=true`
- **Compact format** — `compact="true"` on `<Data>`
  - Fixed values are written only on a group's first row, its header row
  - Other rows in the group leave `||` at the fixed positions
  - A change of value starts a new group and resets the carry-forward
  - Compatible with `compression="zstd"` (order: decode zstd → expand compact)
  - Saves 50–70% on repeated values, up to 85–90% combined with zstd
- **Special values** — the `<SpecialValues>` child of `<Field>`
  - `<Null marker="…"/>` — for TEXT: distinguishes NULL from `""`
  - `<Infinity marker="…"/>`, `<NegInfinity marker="…"/>`, `<NaN marker="…"/>` — for REAL and DECIMAL
  - `<NoDate marker="…"/>` — for DATE and TIMESTAMP: a "no date" sentinel distinct from NULL
- **Backward compatibility:** a v1.0 reader reads v1.3.1 packets, ignoring compact, fixed and SpecialValues
- **Forward compatibility:** a v1.3.1 reader reads v1.0 packets unchanged

**v1.3** (2026-02-26)

- **The `error` packet type** — an ordinary DataPacket recording an ETL failure
  - Table `tdtp_errors` with `package_uuid`, `pipeline`, `error_code`, `error_message`, `created_at`
  - Generated automatically when xZMercury degrades
  - Readable by every downstream consumer, unlike `alarm`
- **AES-256-GCM encryption** through xZMercury (UUID-binding flow)
  - Binary header: `[2B version][1B algo][16B package_uuid][12B nonce][ciphertext]`
  - The key comes from xZMercury and is never passed on the command line
  - HMAC-SHA256 verification of the key (`MERCURY_SERVER_SECRET`)
  - With Mercury unreachable, an error packet is written instead of the data, exit 0
- **pkg/mercury** — HTTP client for the xZMercury UUID-binding and burn-on-read flow
- **pkg/crypto** — AES-256-GCM encryption and decryption
- **cmd/xzmercury-mock** — standalone mock server for end-to-end testing
- **ETL CLI** — `--enc` (override encryption) and `--enc-dev` (local key, non-production builds)
- **ResultLog** — the `completed_with_errors` status and the `package_uuid` field

**v1.2** (2025-12-08)

- **zstd compression**
  - `compression="zstd"` on the Data element
  - Compressed data base64-encoded
  - Automatic for packets over 1 KB
  - Ratio 50–80%
- Production features: circuit breaker, retry, audit, incremental sync
- Data processors: compression, masking, validation, normalisation
- XLSX converter (database ↔ Excel)
- Kafka broker integration
- MySQL adapter

**v1.0** (2025-11-16)

- First production release
- Core modules complete: Packet, Schema, TDTQL
- Adapters: SQLite, PostgreSQL, MS SQL Server
- Message brokers: RabbitMQ, MSMQ
- The `tdtpcli` command-line tool
- Maximum packet size 3.8 MB
- Subtypes: UUID, JSONB, INET, ARRAY

---

## Licence

MIT License

Copyright (c) 2025 TDTP Framework

---

## Contact

- **GitHub:** https://github.com/ruslano69/tdtp-framework
- **Email:** ruslano69@gmail.com
- **Documentation:** https://github.com/ruslano69/tdtp-framework/tree/main/docs

---

*Last updated: 2026-08-22*
