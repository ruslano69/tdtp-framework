# `.tdtp` — Registration Dossier

**IANA (RFC 6838) media type and PRONOM (The National Archives, UK) file format registration**

| | |
|---|---|
| **Format** | Table Data Transfer Protocol (`.tdtp`) |
| **Media type** | `application/vnd.tdtp+xml` |
| **Base container** | XML 1.0 (UTF-8) |
| **Specification version** | v1.5 |
| **Maintainer / contact** | Ruslan Omelchenko — ruslano69@gmail.com |
| **Repository** | https://github.com/ruslano69/tdtp-framework |

---

## Section 1. IANA media type registration (RFC 6838)

Prepared against the RFC 6838 Section 5.6 template, for the vendor tree (`vnd.`)
with the `+xml` structured syntax suffix per RFC 7303.

### Type name

`application`

### Subtype name

`vnd.tdtp+xml`

### Required parameters

N/A

### Optional parameters

**`version`** — the TDTP specification version the packet conforms to (`1.5`,
`1.4`, `1.3.1`, `1.2`, `1.0`). When absent, the authoritative value is the
`version` attribute of the `<DataPacket>` root element, which is always present.
The parameter exists so that a transport can route on the version without
parsing the body.

**`charset`** — as defined for `application/xml` in RFC 7303, Section 3.2. The
reference implementation writes UTF-8 exclusively and declares it in the XML
prolog; the parameter is therefore redundant in practice and MAY be omitted.

### Encoding considerations

Same as `application/xml` (RFC 7303, Section 3.2); 8bit UTF-8 in practice.

The container is always well-formed XML. When zstd compression (v1.2 and later)
or section-level AES-256-GCM encryption (v1.5) is in use, the binary payload —
compressed block, or nonce concatenated with ciphertext — is Base64-encoded
inside the XML elements that carry it (`<R>`, `<Schema>`, `<Data>`,
`<QueryContext>`). A packet therefore remains transferable over any
XML-tolerant channel regardless of which features are enabled.

### Security considerations

**1. XML parsing.** TDTP is an XML-based format and inherits the risks
enumerated in RFC 7303 and RFC 3470. Implementations MUST disable DTD
processing and external entity resolution, and MUST bound entity expansion:
XML entity expansion attacks (Billion Laughs, quadratic blowup) and XXE apply
to this format exactly as they do to any other XML vocabulary.

**2. Integrity — what XXH3-128 does and does not provide.** From v1.4 a packet
may carry a three-level fingerprint computed with XXH3-128:

```
Schema.xxh3     = xxh3_128(MessageID || CanonicalSchemaXML)
Data.xxh3       = xxh3_128(MessageID || RawRowsPlaintext)   // before compression
DataPacket.xxh3 = xxh3_128(SchemaXXH3 || '|' || DataXXH3)
```

The packet's `MessageID` (a UUIDv4) is mixed into every digest so that a digest
computed for one packet cannot be replayed as the digest of another.

**XXH3 is a non-cryptographic hash, and this scheme is not a message
authentication code.** The `MessageID` is carried in the packet in the clear, so
an attacker who can modify a packet can recompute every digest in it. The
integrity scheme detects accidental corruption — truncation, transport damage,
an encoding fault — and casual tampering. It does **not**, on its own, resist a
deliberate attacker.

Resistance to a deliberate attacker comes from two other mechanisms, and an
implementation that needs it MUST enable at least one:

- the **xzMercury registry** (point 3), which holds an authoritative fingerprint
  out of band, so that a recomputed digest does not match the stored one;
- **AES-256-GCM section encryption** (point 4), whose authentication tag is a
  genuine MAC over the protected sections.

**3. Replay protection and the xzMercury registry.** The optional xzMercury
service registers a packet fingerprint with set-if-not-exists semantics and a
TTL. A replayed or substituted packet is refused with `409 Conflict`. Consumers
select a policy for registry unavailability: `FallbackBlock` (refuse the
import), `FallbackDegrade` (fall back to the local XXH3 check), or
`FallbackDowngrade`.

Note the ordering: the registry check necessarily runs **after** decompression,
because the data digest covers the plaintext rows. Any attack that acts during
parsing or decompression therefore fires before this layer can speak, which is
why the parser limits in point 5 are not redundant with it.

**4. Section-level encryption (v1.5).** AES-256-GCM. `<Header>` (MessageID,
TableName, PartNumber) is deliberately left in the clear so that brokers can
route and reassemble multi-part packets without holding a key. `<QueryContext>`
(filter conditions and business logic), `<Schema>` (field names and types) and
`<Data>` (the rows) are each encrypted under the packet key with an independent
12-byte nonce, serialised as `Base64(nonce || ciphertext)`. Keys are held in
xzMercury's RAM store with burn-on-read semantics — the first successful read
removes the key.

**5. Resource exhaustion.** Three separate limits, easily confused with one
another:

| Limit | Value | What it bounds |
|---|---|---|
| `MaxDecompressedBytes` | 256 MB | The **decompression bomb defence**. zstd is bounded through the decoder's memory limit; kanzi through a length-checked limited reader. |
| `MaxSchemaBytes` / `MaxSchemaBytesRead` | 200 KB / 1 MB | The `<Schema>` section on write and on read. The read limit is deliberately looser: the write limit is a rule about the format and cannot be applied retroactively to packets that predate it. |
| `DefaultMaxMessageSize` | 3 800 000 | A **write-side** budget for splitting a payload into parts (counted in units twice the size of a UTF-8 byte, so roughly 1.9 MB of real XML). It is not a defence against anything and imposes no bound on a packet being read. |

The compression ratio has no upper bound, so a small packet can expand without
limit: 25 KB of ordinary repetitive data expands to 200 MB, a ratio of 8184×.
`MaxDecompressedBytes` is the only limit that closes this; the schema and part
limits do not reach it.

**6. Special values.** The special-value markers (`[NULL]`, `NaN`, `INF`,
`-INF`, `NoDate`) are typed at the schema level and passed to the database
through parameterised statements, so they cannot carry SQL injection.

### Interoperability considerations

The format is designed for exchange between database systems, programming
languages and office software.

**Field separator.** Pipe (`|`, ASCII 124). Pipe and backslash are escaped as
`\|` and `\\`.

**Database adapters.**

- *PostgreSQL* — native support for `NaN`, `INF`, `-INF` (`'NaN'::numeric`,
  `'infinity'::numeric`); `NoDate` (`0000-00-00`) converts to NULL.
- *Microsoft SQL Server* — no native `NaN`/`Inf`; the markers are converted to
  NULL rather than rejected.
- *MySQL* — under `STRICT_TRANS_TABLES`, `NoDate` becomes NULL; in non-strict
  mode it is preserved as a valid sentinel.
- *SQLite* — dates are stored as ISO text; `NaN`/`Inf` are stored as NULL.

**Excel / XLSX.**

- *The 1900 leap-year bug* — compensated for the fictitious 29 February 1900
  (serial number 60) that Lotus 1-2-3 introduced and Excel preserved.
- *Integers beyond 15 digits* — 64-bit integers with more than 15 digits are
  written as text cells, because Excel would otherwise round them through an
  IEEE-754 binary64.
- *Formula injection (CWE-1236)* — values beginning with `=`, `+`, `-` or `@`
  are written as escaped string cells.
- *`NaN` / `Inf`* — exported as blank cells, which avoids Excel's `#VALUE!`.

**Python (pandas).** `[NULL]` maps to `pd.NA` / `np.nan`; `NaN` and `INF` map to
native `float('nan')` / `float('inf')` before `astype()` is applied.

### Published specification

- Repository: https://github.com/ruslano69/tdtp-framework
- Documentation: https://github.com/ruslano69/tdtp-framework/tree/main/docs

The specification is published in full under the MIT licence.

### Applications which use this media type

- **tdtpcli** — command-line utility and ETL engine for export, import,
  validation and mapping.
- **Message brokers** — Apache Kafka, RabbitMQ, MSMQ.
- Master-data replication and MDM microservices.

### Fragment identifier considerations

None defined.

### Additional information

**Magic number(s).** A file begins with the XML prolog
`<?xml version="1.0" encoding="UTF-8"?>` (`3C 3F 78 6D 6C 20 …`), followed
within roughly 100 bytes by the root element `<DataPacket` carrying
`protocol="TDTP"`
(`70 72 6F 74 6F 63 6F 6C 3D 22 54 44 54 50 22`). `protocol` is the first
attribute of the root element in every packet the reference implementation
writes.

**File extension(s).** `.tdtp`, `.tdtp.xml`

**Macintosh file type code(s).** N/A

### Person & email address to contact for further information

Ruslan Omelchenko — ruslano69@gmail.com

### Intended usage

COMMON

### Restrictions on usage

None. The format is published under the MIT licence.

### Author

Ruslan Omelchenko / TDTP Framework open-source project

### Change controller

Ruslan Omelchenko

---

### Attack and defence summary

| Threat | Defence | Introduced in | Adversary resistance |
|---|---|---|---|
| Accidental corruption of rows in transit | `Data.xxh3` (XXH3-128) fails local verification | v1.4 | Corruption only |
| Accidental corruption of the schema | `Schema.xxh3` (XXH3-128) fails local verification | v1.4 | Corruption only |
| Deliberate modification with digests recomputed | xzMercury holds the authoritative fingerprint out of band | v1.5 | Yes, with the registry enabled |
| Interception and replay | Unique `MessageID` (UUIDv4) plus set-if-not-exists registration | v1.5 | Yes, with the registry enabled |
| Disclosure of packet contents in a broker | AES-256-GCM over `<Schema>`, `<Data>`, `<QueryContext>` | v1.5 | Yes |
| Undetected modification of encrypted sections | The AES-GCM authentication tag | v1.5 | Yes |
| Key disclosure after use | Burn-on-read: the key is deleted atomically on first read | v1.5 | Yes |
| Nonce reuse under AES-GCM | An independent 12-byte nonce per section | v1.5 | Yes |
| Decompression bomb | `MaxDecompressedBytes` = 256 MB | v1.2 | Yes |
| Oversized schema | `MaxSchemaBytes` 200 KB on write, 1 MB on read | v1.4 | Yes |
| XML entity expansion, XXE | DTD and external entity processing disabled | All versions | Yes |

The distinction in the last column is deliberate. Rows marked *corruption only*
rest on a non-cryptographic hash and are not, on their own, defences against a
deliberate attacker — see Security considerations, point 2.

---

## Section 2. PRONOM registration (The National Archives, UK)

PRONOM is the reference registry used by digital-preservation identification
tools (DROID, Siegfried, FITS).

| Field | Value |
|---|---|
| Format name | Table Data Transfer Protocol Packet |
| Format version | 1.5 (also covers 1.0, 1.2, 1.3, 1.3.1, 1.4) |
| Other names / acronym | TDTP, TDTP XML Data Packet |
| Format family | XML (Extensible Markup Language); tabular data exchange |
| Classification | Text (structured) / data interchange |
| Disclosure | Full public disclosure; open source (MIT licence) |
| Developer / maintainer | Ruslan Omelchenko / TDTP Framework open-source project |
| Primary file extensions | `.tdtp`, `.tdtp.xml` |
| Media type | `application/vnd.tdtp+xml` (RFC 6838) |
| Byte order | Not applicable (textual UTF-8 stream) |

### 2.1 Internal signatures for DROID / PRONOM

The signatures distinguish a `.tdtp` file from any other XML document by the
opening byte sequence and the closing root element.

| Anchor | Offset | Pattern |
|---|---|---|
| **BOF** | 0 – 512 bytes | `3C3F786D6C2076657273696F6E3D{1-2}312E30{1-2}{0-100}3C446174615061636B6574{0-100}70726F746F636F6C3D{1-2}54445450{1-2}` |
| **EOF** | 0 – 64 bytes from the end | `3C2F446174615061636B65743E` |

ASCII equivalents:

- BOF — `<?xml version="1.0"` … `<DataPacket` … `protocol="TDTP"`
- EOF — `</DataPacket>`

The `{1-2}` intervals accommodate either quotation mark: `"` (`22`) or `'`
(`27`).

### 2.2 Byte breakdown of the BOF signature

| Hex | ASCII | Element |
|---|---|---|
| `3C 3F 78 6D 6C 20` | `<?xml ` | Start of the standard XML prolog |
| `76 65 72 73 69 6F 6E 3D` | `version=` | XML version attribute |
| `22 31 2E 30 22` | `"1.0"` | Version value; a single quote (`27`) is equally valid |
| `{0-100}` | `encoding="UTF-8"` … | Variable interval: encoding declaration, whitespace, line breaks |
| `3C 44 61 74 61 50 61 63 6B 65 74` | `<DataPacket` | Root element of a TDTP packet |
| `{0-100}` | whitespace / attributes | Possible intervening attributes |
| `70 72 6F 74 6F 63 6F 6C 3D` | `protocol=` | Protocol marker |
| `22 54 44 54 50 22` | `"TDTP"` | Protocol identifier |

In practice `protocol` is the first attribute of `<DataPacket>`, so the second
`{0-100}` interval is almost always empty; it is specified for tolerance rather
than because implementations use it.

### 2.3 Sample files to accompany the submission

Generated with `tdtpcli` from a SQLite source and checked with `tdtpcli --test`;
they are in [`samples/`](samples/).

| File | Declares | Distinguishing markup |
|---|---|---|
| `employees-plain.tdtp` | `version="1.0"` | none — the baseline shape |
| `employees-zstd.tdtp` | `version="1.0"` | `<Data compression="zstd" checksum="…">` |
| `timesheet-compact.tdtp` | `version="1.3.1"` | `<Data compact="true">` |
| `employees-integrity.tdtp` | `version="1.4"` | `xxh3` on `<DataPacket>`, `<Schema>` and `<Data>` |

**The `version` attribute records the features in use, not the release the
packet was written by.** Compression does not raise it — a zstd packet still
declares `1.0` and announces the compression on `<Data>` instead. The compact
format raises it to `1.3.1`, integrity hashes to `1.4`, section encryption to
`1.5`. An identification tool must therefore key on `protocol="TDTP"` and treat
`version` as informational, which is what the signatures in 2.1 do.

A fifth sample, section-encrypted v1.5, is not included here: `--enc` is gated
behind a licensed feature, so producing one requires a `tdtp.lic` signed with
the vendor key. It matches the same BOF and EOF signatures as the other four —
that is the point of leaving `<Header>` and the root element unencrypted — and
should be added to the submission when a licensed build is used.

All four files match the BOF and EOF signatures in 2.1, verified byte for byte.

---

## Section 3. shared-mime-info definition (freedesktop.org)

Install as `/usr/share/mime/packages/vnd.tdtp+xml.xml` so that Linux desktops,
`file`, and `xdg-mime` recognise the format.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<mime-info xmlns="http://www.freedesktop.org/standards/shared-mime-info">
  <mime-type type="application/vnd.tdtp+xml">
    <comment>TDTP Data Packet</comment>
    <comment xml:lang="ru">Пакет данных TDTP</comment>
    <sub-class-of type="application/xml"/>
    <generic-icon name="text-xml"/>
    <glob pattern="*.tdtp" weight="70"/>
    <glob pattern="*.tdtp.xml" weight="80"/>
    <magic priority="70">
      <match type="string" offset="0:256" value="&lt;DataPacket">
        <match type="string" offset="1:128" value="protocol=&quot;TDTP&quot;"/>
      </match>
      <match type="string" offset="0:64" value="&lt;?xml">
        <match type="string" offset="0:512" value="&lt;DataPacket">
          <match type="string" offset="1:128" value="protocol=&quot;TDTP&quot;"/>
        </match>
      </match>
    </magic>
  </mime-type>
</mime-info>
```

`<sub-class-of type="text/plain"/>` is omitted deliberately:
`application/xml` is already a subclass of `text/plain` in the freedesktop
database, and declaring both produces a redundant edge.

---

## Section 4. Submission and verification

### 4.1 IANA (IETF media types registrar)

1. Use the web form at https://www.iana.org/form/media-types.
2. Fill each field from Section 1 above.
3. Alternatively send the completed RFC 6838 template to `media-types@iana.org`.
4. Review by the IESG-designated expert normally takes two to four weeks.
   Vendor-tree registrations do not require IETF consensus, but the expert does
   review the security considerations — which is why point 2 states plainly what
   XXH3 does and does not provide.

### 4.2 PRONOM (The National Archives)

1. Download the *Outline File Format Signature Submission Form* from
   https://www.nationalarchives.gov.uk/PRONOM.
2. Transfer the metadata, the BOF/EOF sequences and the offsets from Section 2.
3. Attach the three sample files listed in 2.3.
4. Send to `pronom@nationalarchives.gov.uk`.

### 4.3 Local installation and testing on Linux

```bash
sudo cp vnd.tdtp+xml.xml /usr/share/mime/packages/
```

```bash
sudo update-mime-database /usr/share/mime
```

```bash
xdg-mime query filetype example.tdtp
```

Expected output: `application/vnd.tdtp+xml`

---

*Table Data Transfer Protocol Framework v1.5 — open specification.*
