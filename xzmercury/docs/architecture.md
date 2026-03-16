# Architecture

## Overview

xzmercury is a key management microservice. Its sole responsibility is issuing
AES-256 keys for exactly one encryption operation each. Encryption itself is
performed by `FileEncryptor` — a processor inside `tdtpcli` (`pkg/processors`).

```
┌──────────────────────────────────────────────────────────────────────┐
│  TDTP Organisation                                                   │
│                                                                      │
│  ┌──────────────────────────────┐    HTTP/JSON                       │
│  │ tdtpcli (ETL pipeline)       │◄────────────────────────────────┐  │
│  │                              │                                 │  │
│  │  Processor.Execute()         │   ┌─────────────────────────┐  │  │
│  │  └─ Exporter                 │   │       xzmercury         │  │  │
│  │     └─ FileEncryptor         │   │                         │  │  │
│  │        1. BindKey ──────────────►│  ┌────────┐  ┌───────┐ │  │  │
│  │        2. VerifyHMAC         │   │  │ /bind  │  │ guard │ │  │  │
│  │        3. AES-256-GCM        │   │  │ /retr. │  │(T3.2) │ │  │  │
│  │           (data area only)   │   │  └────────┘  └───────┘ │  │  │
│  └──────────────────────────────┘   │       │                 │  │  │
│                                     │  ┌────▼──────────────┐ │  │  │
│  ┌──────────────────────────────┐   │  │   keystore (T3.1) │ │  │  │
│  │ recipient / pipeline exec    │   │  │ Bind / BurnOnRead │ │  │  │
│  │  1. retrieve key ───────────────►│  └────┬──────────┬───┘ │  │  │
│  │  2. AES-256-GCM decrypt      │   │       │          │     │  │  │
│  └──────────────────────────────┘   └───────┼──────────┼─────┘  │  │
│                                    Mercury   │     Pipeline      │  │
│                                    Redis◄────┘     Redis◄────────┘  │
│                                    (keys,          (quota, LDAP      │
│                                    RAM only,        cache, state)    │
│                                    no AOF)                           │
│                                                                      │
│                        ┌──────────────────────────────────────────┐ │
│                        │         LDAP / Active Directory          │ │
│                        └──────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────┘
```

## Two-Redis design

| Redis instance | Purpose | Persistence |
|---|---|---|
| **Mercury Redis** | AES key storage | **None** — `save ""`, no AOF, no RDB. Keys live in RAM only. |
| **Pipeline Redis** | Quota balances, LDAP membership cache, request state | AOF optional; losing it means quota resets — acceptable. |

Keeping them separate means:
- Mercury Redis can be configured with `maxmemory-policy allkeys-lru` + no
  persistence, giving maximum security guarantees.
- Pipeline Redis can be backed up and replicated without risk of key exposure.

## Data flow — bind (called by FileEncryptor inside tdtpcli)

`FileEncryptor.Encrypt()` calls `/api/keys/bind` transparently as part of
pipeline execution. The caller (`tdtpcli` user) does not invoke it manually.

```
FileEncryptor           xzmercury                Mercury Redis   Pipeline Redis   LDAP/AD
(inside tdtpcli)            │                         │               │              │
   │                        │                         │               │              │
   │─ POST /api/keys/bind ─►│                         │               │              │
   │  {uuid, pipeline,      │                         │               │              │
   │   caller}              │                         │               │              │
   │                        │─── GET ldap:member: ──────────────────►│◄─────────────│
   │                        │◄── cached / miss ──────────────────────│              │
   │                        │         (miss)           │               │              │
   │                        │─────────────────────────────────────────────────────► BIND
   │                        │◄──────────────────────────────────────────────────── ok
   │                        │─── SET ldap:member: ───────────────────►│              │
   │                        │                         │               │              │
   │                        │─── EVALSHA quota.lua ──────────────────►│              │
   │                        │◄── 1 (approved) ───────────────────────│              │
   │                        │                         │               │              │
   │                        │─ SET mercury:key:{uuid} ►│              │              │
   │                        │  value=keyB64 EX=300    │               │              │
   │                        │                         │               │              │
   │                        │─── SET request:{id} ──────────────────►│              │
   │                        │─── PUBLISH xzmercury:events ──────────►│              │
   │                        │                         │               │              │
   │◄── 200 {key_b64,        │                         │               │              │
   │        hmac, req_id} ──│                         │               │              │
   │                        │                         │               │              │
   │ verifyHMAC(uuid, hmac) │                         │               │              │
   │ AES-256-GCM encrypt    │                         │               │              │
   │ (data area only)       │                         │               │              │
   │ write blob to file     │                         │               │              │
```

## Data flow — retrieve (burn-on-read, called by recipient)

The retrieve step is performed by the **recipient** (pipeline executor or
decryptor), not by the `tdtpcli` sender.

```
recipient               xzmercury                Mercury Redis
   │                        │                         │
   │─ POST /api/keys/retrieve►│                        │
   │  {uuid, request_id}    │                         │
   │                        │─── GETDEL ─────────────►│
   │                        │◄── keyB64 (key deleted) │
   │                        │    (nil if already read) │
   │◄── 200 {key_b64} ──────│                         │
   │  (or 404 if burned)    │                         │
   │                        │                         │
   │ AES-256-GCM decrypt    │                         │
   │ (data area)            │                         │
```

## Request lifecycle (T3.3)

```
             bind called
                  │
                  ▼
           [LDAP check]
          /            \
       fail            pass
        │                │
        ▼                ▼
    rejected          [quota]
    (terminal)       /      \
                  fail      pass
                   │          │
                   ▼          ▼
               rejected    approved ──► (stored in Pipeline Redis, TTL=24h)
               (terminal)      │
                               │  retrieve called
                               ▼
                           consumed ──► (Pub/Sub event published)
                           (terminal)
```

States are persisted in Pipeline Redis as JSON with 24 h TTL. State changes
are published to `xzmercury:events` (Redis Pub/Sub channel) for `tdtpcli` and
any web UI to consume.

## What gets encrypted

Only the **data area** is encrypted. The binary blob written by `FileEncryptor`
has a fixed-size plaintext header followed by the ciphertext:

```
┌────────────────────────────────────────────────────────────┐
│  Binary blob written to file                               │
├──────┬──────────┬────────────────────┬──────────┬─────────┤
│ ver  │  algo    │    package UUID     │  nonce   │  data   │
│ 2 B  │   1 B    │      16 B          │  12 B    │  area   │
│      │          │  (plaintext)       │          │(cipher) │
├──────┴──────────┴────────────────────┴──────────┼─────────┤
│              header  (31 bytes, plaintext)       │AES-GCM  │
│  allows format detection without a key          │ciphertxt│
└─────────────────────────────────────────────────┴─────────┘
```

The data area contains: AES-256-GCM sealed payload (serialised TDTP XML data)
+ 16-byte GCM authentication tag. The header (version, algorithm, UUID, nonce)
stays in plaintext to allow envelope inspection and key routing by UUID.

Encryption is performed in `pkg/processors/encryption.go` (`FileEncryptor`).
The crypto primitives are in `pkg/crypto/encryption.go`. xzmercury itself never
touches the plaintext or ciphertext — it only issues and stores the key.

## Internal packages

```
internal/
├── guard/      Reads OS privileges; blocks root/elevated start in production.
├── ldap/       Client interface → real (go-ldap) or mock (JSON file).
│               CachingClient wraps either with Pipeline Redis TTL.
├── keystore/   Bind: crypto/rand → base64 → SET mercury:key:{uuid} EX ttl
│               BurnOnRead: GETDEL → returns key or ErrKeyNotFound
├── quota/      Lua script: GET balance → check → SET balance-cost EX 3600
├── acl/        Loads pipeline-acl.yaml → Policy{Group, Cost} per pipeline
├── request/    Create/Reject/MarkConsumed stored in Pipeline Redis + PUBLISH
├── infra/      Config (YAML + env) + Setup (real or miniredis×2 + mock LDAP)
└── api/        chi router, zerolog middleware, keysHandler, request handler
```

## Dev mode (`--dev`)

```
┌────────────────────────────────────────┐
│  xzmercury process                     │
│                                        │
│  ┌──────────┐   ┌────────────────────┐ │
│  │ miniredis│   │ miniredis          │ │
│  │ :random  │   │ :random            │ │
│  │ (mercury)│   │ (pipeline)         │ │
│  └──────────┘   └────────────────────┘ │
│                                        │
│  MockLDAPClient (JSON file or built-in)│
│  guard: WARN instead of Fatal          │
└────────────────────────────────────────┘
```

Zero external dependencies. Single binary.
Suitable for local development and CI pipelines.
