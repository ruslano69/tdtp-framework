# Architecture

## Overview

xzmercury sits between `tdtpcli` (the pipeline client) and the pipeline
executor. Its sole responsibility is issuing AES-256 keys for exactly one
encryption operation each. Nothing else.

```
┌─────────────────────────────────────────────────────────────────┐
│  TDTP Organisation                                              │
│                                                                 │
│  ┌───────────┐    HTTP/JSON    ┌─────────────────────────────┐  │
│  │ tdtpcli   │◄──────────────►│        xzmercury            │  │
│  │           │                │                             │  │
│  │ 1. bind   │                │  ┌──────────┐  ┌────────┐  │  │
│  │ 2. encrypt│                │  │  /bind   │  │ guard  │  │  │
│  │ 3. send   │                │  │  /retr.  │  │(T3.2)  │  │  │
│  └───────────┘                │  └──────────┘  └────────┘  │  │
│                               │       │                     │  │
│                               │  ┌────▼──────────────────┐ │  │
│                               │  │    keystore (T3.1)    │ │  │
│                               │  │  Bind / BurnOnRead    │ │  │
│                               │  └────┬──────────┬───────┘ │  │
│                               │       │          │          │  │
│                        ┌──────┴───┐   │     ┌────┴──────┐  │  │
│                        │ Mercury  │◄──┘     │ Pipeline  │  │  │
│                        │  Redis   │         │  Redis    │  │  │
│                        │ (keys)   │         │ (quota,   │  │  │
│                        │ RAM only │         │  LDAP     │  │  │
│                        │ no AOF   │         │  cache,   │  │  │
│                        └──────────┘         │  requests)│  │  │
│                                             └─────┬─────┘  │  │
│                               │                   │         │  │
│                        ┌──────┴───────────────────┴──────┐ │  │
│                        │    LDAP / Active Directory      │ │  │
│                        │   (real in prod, mock in dev)   │ │  │
│                        └─────────────────────────────────┘ │  │
└─────────────────────────────────────────────────────────────────┘
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

## Data flow — bind

```
tdtpcli                 xzmercury                Mercury Redis   Pipeline Redis   LDAP/AD
   │                        │                         │               │              │
   │─ POST /api/keys/bind ─►│                         │               │              │
   │  {uuid, pipeline,       │                         │               │              │
   │   caller}               │                         │               │              │
   │                        │─── GET ldap:member: ───►│               │◄─────────────│
   │                        │◄── cached / miss ───────│               │              │
   │                        │         (miss)           │               │              │
   │                        │─────────────────────────────────────────────────────► BIND
   │                        │◄──────────────────────────────────────────────────── ok
   │                        │─── SET ldap:member: ───────────────────►│              │
   │                        │                         │               │              │
   │                        │─── EVALSHA quota.lua ──────────────────►│              │
   │                        │◄── 1 (approved) ───────────────────────│              │
   │                        │                         │               │              │
   │                        │─ SET mercury:key:{uuid} ►│               │              │
   │                        │  value=keyB64 EX=300    │               │              │
   │                        │                         │               │              │
   │                        │─── SET request:{id} ──────────────────►│              │
   │                        │─── PUBLISH xzmercury:events ──────────►│              │
   │                        │                         │               │              │
   │◄── 200 {key_b64,        │                         │               │              │
   │        hmac, req_id} ──│                         │               │              │
```

## Data flow — retrieve (burn-on-read)

```
tdtpcli                 xzmercury                Mercury Redis
   │                        │                         │
   │─ POST /api/keys/retrieve►│                        │
   │  {uuid, request_id}    │                         │
   │                        │─── GETDEL ─────────────►│
   │                        │◄── keyB64 (key deleted) │
   │                        │    (nil if already read) │
   │◄── 200 {key_b64} ──────│                         │
   │  (or 404 if burned)    │                         │
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
