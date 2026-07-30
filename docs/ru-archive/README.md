# Russian documentation archive

The originals of documents that have been translated into English, kept at their
original paths under this directory.

They are here rather than deleted for two reasons. A translation can lose a
detail, and the original is the only way to tell whether something is missing or
was never there. And several of these were written while the feature was being
built, so they carry reasoning that never made it into the finished document.

**These files are not maintained.** Once a document is translated, the English
version is the one that gets corrected; the copy here is a snapshot of what was
translated from, frozen at that moment. Do not fix an error here — fix it in the
English document, which is the only one anybody reads.

## Layout

Paths mirror the repository, so `docs/README.md` archives as
`docs/ru-archive/docs/README.md`.

## Contents

| Original | Archived as | Translated |
|----------|-------------|------------|
| `docs/README.md` | `docs/README.md` | 2026-07-29 |
| `docs/ETL_PIPELINE.md` | `docs/ETL_PIPELINE.md` | 2026-07-29 |
| `docs/SPECIFICATION.md` | `docs/SPECIFICATION.md` | 2026-07-29 |
| `docs/USER_GUIDE.md` | `docs/USER_GUIDE.md` | 2026-07-29 |
| `docs/ACCESS_ADAPTER.md` | `docs/ACCESS_ADAPTER.md` | 2026-07-29 |
| `docs/S3_AS_SYNC_BROKER.md` | `docs/S3_AS_SYNC_BROKER.md` | 2026-07-29 |
| `docs/SCENARIO_TRUST.md` | `docs/SCENARIO_TRUST.md` | 2026-07-29 |
| `docs/tdtp-v14-protocol-schema.md` | `docs/tdtp-v14-protocol-schema.md` | 2026-07-29 |
| `docs/DEVELOPER_GUIDE.md` | `docs/DEVELOPER_GUIDE.md` | 2026-07-29 |
| `docs/xZMercury-TDTP-TZ-v1.2.md` | `docs/xZMercury-TDTP-TZ-v1.2.md` | never — see below |
| `CLAUDE.md` | `CLAUDE.md` | 2026-07-30 |
| `AGENTS.md` | `AGENTS.md` | 2026-07-30 |

## The xZMercury statement of work is archived, not translated

`xZMercury-TDTP-TZ-v1.2.md` is a different case from everything else here: it was
checked against the code on 2026-07-30 rather than translated, and the check
found it actively misleading rather than merely stale. It frames signing,
licensing and a certificate authority as a speculative future paid tier
("chiptdtp", separate from the free product) — but that tier shipped, under a
different shape, built directly into the product this document calls free:
`pkg/license` (three real tiers, Ed25519-signed), `xzmercury/internal/ca`
(enrollment and re-authorization), and the orchestrator's trust gate that
intersects the two. A reader would come away believing licensing was an
unbuilt idea.

Translating a wrong document produces a fluent wrong document, which is worse
than a Russian one nobody reads by mistake. The replacement is
[`docs/XZMERCURY_SERVICE.md`](../XZMERCURY_SERVICE.md), written from the code
rather than from this file. This one stays here as the historical record of
what was originally planned, in the language it was planned in.

## When a document is only partly stale

Some of these were out of date in Russian before they were translated — the
documentation index claimed v1.3 while the protocol was at v1.5 and the CLI at
1.24.0. Where that happened, the English version states the current facts rather
than reproducing the old ones, and the archived copy is the record of what the
document used to claim. Differences between the two are deliberate.
