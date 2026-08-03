# Security Policy

## Reporting a vulnerability

**Do not open a public issue for a security problem.**

Report it privately through GitHub: **Security → Advisories → Report a
vulnerability** on this repository
([direct link](https://github.com/ruslano69/tdtp-framework/security/advisories/new)).
That channel stays private between you and the maintainer until a fix is
published, and it needs no email address on either side.

Useful in a report, roughly in order of how much time each saves:

- the version (`tdtpcli --version`) and, where it matters, the OS and build tags
- what an attacker gains — read data, write data, run code, deny service
- the smallest input that triggers it: a `.tdtp.xml` packet, a pipeline YAML, a
  request. Attach the file rather than describing it
- whether it needs authentication, a licence tier, or a reachable xZMercury

## What to expect

This project has one maintainer. The honest version of a response timeline:

| | |
|---|---|
| Acknowledgement | within a few days |
| Assessment, with a severity and a plan | within two weeks |
| Fix for a confirmed high-severity issue | in the next release, or a patch release if it cannot wait |

Silence past that means the report was missed, not declined — ping the advisory
thread.

Credit in the advisory and the release notes unless you ask otherwise.

## Supported versions

Only the latest minor release gets security fixes.

| Version | Supported |
|---------|-----------|
| 1.24.x  | ✅ |
| < 1.24  | ❌ — upgrade |

The TDTP **protocol** version (`1.3` … `1.5`, in the packet header) is a
different number from the release version, and is not what this table is about.
Readers accept older protocol versions deliberately, so a security fix never
requires every producer to upgrade at once.

## Scope

In scope, in this repository:

- packet parsing and generation (`pkg/core/packet`) — anything reached by
  reading a packet from an untrusted source
- compression and decompression (`pkg/processors`)
- encryption, key handling and integrity (`pkg/crypto`, `pkg/security`,
  `pkg/mercury`)
- licence verification (`pkg/license`)
- the network-facing services: `cmd/orchestrator`, `cmd/tdtpserve`
- database adapters, in particular anything that builds SQL from input
  (`pkg/adapters`, `pkg/core/tdtql`)

xZMercury sits in the same repository but is a **separate Go module**
(`xzmercury/`, its own `go.mod`). Report those findings here as well, and say
so in the report.

## Known limits, by design

Documented properties rather than undiscovered bugs. Showing that a mitigation
is insufficient is a real finding and very welcome; restating the property
itself is not needed.

**The Mercury signature check runs after decompression, and cannot run anywhere
else.** The hash covers plain-text rows *before* compression, so verifying it
requires the decompressed rows first. Anything that breaks the parse therefore
fires before the signature can say anything — a decompression bomb, an
oversized schema, malformed XML. That window is closed by explicit limits
rather than by the signature:

| Limit | Value | Where |
|---|---|---|
| `MaxDecompressedBytes` | 256 MB | `pkg/processors/compression.go` |
| `MaxSchemaBytesRead` | 1 MB | `pkg/core/packet/parser.go` |
| `maxBufferedParse` | 64 MB | `pkg/core/packet/parser.go` |

A way past those is exactly the kind of report this policy is for.

**`--mercury-url` is optional.** Without it the integrity gate does not exist
at all. That is a deployment choice, not a defect.

**Read limits are deliberately looser than write limits.** `MaxSchemaBytes`
(200 KB, on write) against `MaxSchemaBytesRead` (1 MB, on read): the write
limit is a rule about the format and binds new packets only, while the same
limit on read would reject data written before the rule existed.

## Out of scope

- findings against a modified build, or against the modes that exist to make
  local development possible (`--no-auth`, `xzmercury --dev`)
- vulnerabilities in dependencies with no demonstrated path through this code.
  Report those upstream — and do tell us if the path does exist here
- missing hardening with no exploit behind it (an absent header, a scanner
  finding). Welcome as an issue, not as an advisory
