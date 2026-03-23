# API Reference

Base URL: `http://localhost:3000` (configurable via `server.addr`)

All request and response bodies are JSON (`Content-Type: application/json`).

---

## GET /healthz

Liveness probe. Always returns 200 if the process is running.

**Response 200**
```json
{"status": "ok"}
```

---

## GET /readyz

Readiness probe. Pings both Redis instances.

**Response 200** — both instances healthy
```json
{
  "mercury_redis":  "ok",
  "pipeline_redis": "ok"
}
```

**Response 503** — one or both instances unreachable
```json
{
  "mercury_redis":  "ok",
  "pipeline_redis": "dial tcp 127.0.0.1:6380: connect: connection refused"
}
```

---

## POST /api/keys/bind

Generates a fresh AES-256 key, stores it in Mercury Redis with TTL, and
returns the key + an HMAC signature for client verification.

Before storing the key, xzmercury:
1. Checks LDAP/AD group membership for `caller` (cached 120 s in Pipeline Redis)
2. Atomically deducts quota credits via Lua script

### Request

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `package_uuid` | string | ✓ | UUID of the TDTP package being encrypted |
| `pipeline_name` | string | ✓ | Pipeline name (used for ACL + quota lookup) |
| `caller` | string | | AD service account (`sAMAccountName`). If omitted, LDAP check is skipped. |

```bash
curl -s -X POST http://localhost:3000/api/keys/bind \
  -H "Content-Type: application/json" \
  -d '{
    "package_uuid":  "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "pipeline_name": "salary-report",
    "caller":        "svc_tdtp"
  }'
```

### Response 200

| Field | Type | Description |
|-------|------|-------------|
| `request_id` | string | Opaque ID for lifecycle tracking (hex, 16 chars) |
| `key_b64` | string | AES-256 key, base64-encoded (32 bytes → 44 chars) |
| `hmac` | string | `HMAC-SHA256(package_uuid, SERVER_SECRET)` hex string |

```json
{
  "request_id": "35123e8d4dc76b47",
  "key_b64":    "zogAcS3YCx1RuiqnFtM+7k8pLvDhXn2bQe0wIaJsGcY=",
  "hmac":       "6a69880a96ca5fd6e3c12b4f89a3d701f8e2c5b..."
}
```

`tdtpcli` should verify the HMAC before using the key:
```go
ok := mercury.VerifyHMAC(packageUUID, binding.HMAC, os.Getenv("MERCURY_SERVER_SECRET"))
```

### Error responses

| Status | Condition |
|--------|-----------|
| 400 | Missing `package_uuid` or `pipeline_name` |
| 403 | `caller` is not a member of the required AD group |
| 429 | Hourly quota exceeded for the group |
| 500 | LDAP unreachable, quota Lua error, or Redis write failure |

```json
{"error": "caller is not a member of the required group"}
```

---

## POST /api/keys/retrieve

**Burn-on-read** retrieval. The key is returned and immediately deleted from
Mercury Redis via `GETDEL`. Any subsequent call for the same UUID returns 404.

Requires Redis 6.2+ (`GETDEL` command).

### Request

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `package_uuid` | string | ✓ | UUID of the package |
| `request_id` | string | | If provided, marks the request as `consumed` in Pipeline Redis |

```bash
curl -s -X POST http://localhost:3000/api/keys/retrieve \
  -H "Content-Type: application/json" \
  -d '{
    "package_uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "request_id":   "35123e8d4dc76b47"
  }'
```

### Response 200

```json
{
  "key_b64": "zogAcS3YCx1RuiqnFtM+7k8pLvDhXn2bQe0wIaJsGcY="
}
```

### Response 404

Returned when the key does not exist or has already been consumed.

```json
{"error": "key not found or already consumed (burn-on-read)"}
```

### Error responses

| Status | Condition |
|--------|-----------|
| 400 | Missing `package_uuid` |
| 404 | Key not found or already consumed |
| 500 | Redis error |

---

## GET /api/requests/{id}

Returns the stored state record for a request. Used by `tdtpcli` and any
web UI for observability.

```bash
curl -s http://localhost:3000/api/requests/35123e8d4dc76b47
```

### Response 200

```json
{
  "id":            "35123e8d4dc76b47",
  "package_uuid":  "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "pipeline_name": "salary-report",
  "caller":        "svc_tdtp",
  "state":         "consumed",
  "created_at":    "2026-02-26T11:12:57Z",
  "updated_at":    "2026-02-26T11:12:57Z"
}
```

### States

| State | Meaning |
|-------|---------|
| `approved` | Bind succeeded; key is in Mercury Redis |
| `rejected` | Bind failed (LDAP or quota); no key was generated |
| `consumed` | Key was retrieved (burned); pipeline can proceed |

Records expire after 24 hours (Pipeline Redis TTL).

### Response 404

```json
{"error": "request not found"}
```

---

## Pub/Sub events

Every state change publishes a JSON message to the Redis channel
`xzmercury:events` on Pipeline Redis.

```bash
redis-cli -p 6380 SUBSCRIBE xzmercury:events
```

### Event schema

```json
{
  "request_id":    "35123e8d4dc76b47",
  "package_uuid":  "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "pipeline_name": "salary-report",
  "state":         "consumed",
  "timestamp":     "2026-02-26T11:12:57.123456Z"
}
```

Events are best-effort — a Redis error during publish does not fail the
originating HTTP request.
