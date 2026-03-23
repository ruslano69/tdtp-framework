# Security Model

## Threat model

xzmercury defends against:

| Threat | Mitigation |
|--------|-----------|
| Key exfiltration via Redis dump | Mercury Redis runs with **no persistence** — keys exist only in RAM |
| Replay attack (key reuse) | **Burn-on-read** (`GETDEL`) — each key can be retrieved at most once |
| Man-in-the-middle substitution | **HMAC-SHA256** on every bind response; client verifies before use |
| Unauthorized pipeline execution | **LDAP group check** + **pipeline-acl.yaml** per pipeline |
| Quota abuse / DoS | **Lua-atomic hourly quota** per AD group |
| Running with OS privileges | **Privilege guard** rejects start as root / elevated Administrator |
| Key lingering after TTL | **Redis `EX` TTL** auto-deletes unread keys (default 5 min) |

## Privilege guard (T3.2)

xzmercury must run under a dedicated, unprivileged service account
(`svc_xzmercury` or equivalent).

**Linux**: checks `os.Getuid() == 0`. Startup fails if root.

**Windows**: reads `TokenElevation` via `GetTokenInformation`. Startup fails
if the process token is elevated (UAC-elevated Administrator).

In `--dev` mode the guard emits a `WARN` instead of aborting, because
containers and CI environments commonly run as root.

Recommended Linux setup:
```bash
useradd --system --shell /sbin/nologin --home /opt/xzmercury svc_xzmercury
chown svc_xzmercury /opt/xzmercury /etc/xzmercury.yaml
chmod 400 /etc/xzmercury.yaml
```

## Burn-on-read

Key retrieval uses Redis `GETDEL` (atomic get + delete, Redis 6.2+):

```
SET mercury:key:{uuid}  keyB64  EX 300    ← bind
GETDEL mercury:key:{uuid}                 ← retrieve → key deleted immediately
GETDEL mercury:key:{uuid}                 ← 2nd call → nil → HTTP 404
```

If the connection drops between the GET and DEL in a non-atomic implementation,
the key could be read twice. `GETDEL` eliminates this race condition.

## HMAC verification

Every bind response includes:
```
hmac = HMAC-SHA256(package_uuid, SERVER_SECRET)
```

`tdtpcli` must verify this before trusting the key:
```go
if !mercury.VerifyHMAC(packageUUID, binding.HMAC, os.Getenv("MERCURY_SERVER_SECRET")) {
    return nil, mercury.ErrHMACVerificationFailed
}
```

Both sides share `SERVER_SECRET` out-of-band (environment variable or secrets
manager). The HMAC ensures that:
- The key came from a genuine xzmercury instance (not an impostor)
- The key is bound to the correct package UUID

## LDAP membership cache

Raw LDAP searches run on every bind — against an AD controller — can be slow
and would hammer the DC under load. Results are cached in Pipeline Redis:

```
Key:   ldap:member:{sAMAccountName}:{group_dn}
Value: "1" (member) or "0" (not member)
TTL:   120 s (configurable via ldap.cache_ttl)
```

A cache miss falls through to a live LDAP search. If the Redis cache itself is
unavailable, the code falls through to live LDAP (degraded mode, not fatal).

Implication: group membership changes take up to `cache_ttl` seconds to
propagate to xzmercury. For immediate revocation, flush the relevant cache
keys:
```bash
redis-cli -p 6380 DEL "ldap:member:svc_tdtp:cn=tdtp-pipeline-users,..."
```

## Mercury Redis hardening checklist

```conf
# /etc/redis/mercury.conf
save ""                       # disable RDB snapshots
appendonly no                 # disable AOF
bind 127.0.0.1                # localhost only; use TLS tunnel for remote
requirepass <strong-password>
maxmemory 256mb
maxmemory-policy allkeys-lru  # evict LRU if full (safety net)
```

Firewall: only xzmercury's process should be able to reach Mercury Redis.
Never expose it on a public interface.

## Key entropy

Each AES-256 key is generated with `crypto/rand.Read(key[:32])` — the OS CSPRNG
(`/dev/urandom` on Linux, `BCryptGenRandom` on Windows). This provides 256 bits
of entropy per key.

## What xzmercury does NOT protect against

- A local administrator with `CAP_SYS_PTRACE` or kernel-level access can still
  read the key from process memory between bind and encrypt. The privilege guard
  raises the bar; it is not an absolute guarantee.
- If Mercury Redis is compromised while a key has not yet been retrieved, the
  attacker can read (and burn) the key. Keep Redis on localhost or a private
  network.
- xzmercury does not authenticate the HTTP caller (no mTLS, no API key). Add a
  reverse proxy with mTLS or network-level controls (firewall, VPC) in
  production.
