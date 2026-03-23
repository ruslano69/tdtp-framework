# Configuration

xzmercury reads a YAML config file. Command-line flags and environment
variables override specific values.

## Startup flags

| Flag | Default | Description |
|------|---------|-------------|
| `--dev` | `false` | Dev mode: in-process miniredis + JSON mock LDAP |
| `--config` | `configs/xzmercury.yaml` | Path to config file |
| `--addr` | *(from config)* | Override `server.addr` |

## Environment variables

| Variable | Overrides | Description |
|----------|-----------|-------------|
| `MERCURY_SERVER_SECRET` | `security.server_secret` | HMAC secret. Required if not set in YAML. Env var takes precedence in production — keep the secret out of config files. |

## Full YAML reference

```yaml
# ── HTTP server ──────────────────────────────────────────────────────────────
server:
  addr: ":3000"          # listen address; ":3000" = all interfaces, port 3000
  read_timeout:  10s     # max time to read the full request body
  write_timeout: 10s     # max time to write the full response

# ── Mercury Redis — AES key storage ──────────────────────────────────────────
# Use a dedicated Redis instance with NO persistence (save "", no AOF).
# Configure maxmemory-policy allkeys-lru as a safety net.
mercury:
  addr:     "localhost:6379"
  password: ""    # leave empty for no-auth; use requirepass in redis.conf
  db:       0

# ── Key TTL ───────────────────────────────────────────────────────────────────
# A bound key auto-expires after key_ttl if tdtpcli never retrieves it.
# Keep short. The pipeline should retrieve within seconds of binding.
key_ttl: 5m

# ── Pipeline Redis — quota, LDAP cache, request state ────────────────────────
# Can be the same instance as Mercury in low-security environments,
# but a separate instance is strongly recommended.
pipeline:
  addr:     "localhost:6380"
  password: ""
  db:       0

# ── Security ──────────────────────────────────────────────────────────────────
security:
  # HMAC-SHA256 secret. Both xzmercury and tdtpcli must share this value.
  # Leave empty and set MERCURY_SERVER_SECRET env var instead.
  server_secret: ""

  # Simple per-IP rate limit (requests/second). 0 = disabled.
  # Full sliding-window Redis implementation is planned for v2.
  rate_limit: 100

# ── LDAP / Active Directory ───────────────────────────────────────────────────
ldap:
  # LDAP server address.
  addr: "dc.corp.local:389"

  # Service account DN used for the initial bind (read-only is sufficient).
  bind_dn:       "cn=svc_xzmercury,ou=service,dc=corp,dc=local"

  # Password for bind_dn. Use a secrets manager in production; never hard-code.
  bind_password: ""

  # Base DN for user searches.
  base_dn: "dc=corp,dc=local"

  # How long to cache membership results in Pipeline Redis.
  # A higher value reduces LDAP load; a lower value reacts faster to
  # group changes. 120 s is a good default.
  cache_ttl: 120s

  # Dev mode only: path to JSON file with mock users.
  # If empty in --dev mode, built-in defaults are used:
  #   svc_tdtp  → [tdtp-pipeline-users, tdtp-admins]
  #   analyst1  → [tdtp-pipeline-users]
  mock_users_file: ""

# ── Quota ─────────────────────────────────────────────────────────────────────
quota:
  # Credits granted to a group at the start of each UTC hour (on first use).
  # When the balance reaches zero, bind requests for that group return HTTP 429.
  default_hourly: 1000

  # Path to pipeline-acl.yaml. If empty, all pipelines use default_group
  # ("tdtp-pipeline-users") and cost=1.
  acl_file: "configs/pipeline-acl.yaml"
```

## Pipeline ACL file

`pipeline-acl.yaml` maps pipeline names to AD groups and quota costs.

```yaml
# Default policy for pipelines not explicitly listed.
default_group: "cn=tdtp-pipeline-users,ou=groups,dc=corp,dc=local"
default_cost:  1

pipelines:
  salary-report:
    group: "cn=tdtp-pipeline-users,ou=groups,dc=corp,dc=local"
    cost:  10

  sensitive-hr-data:
    group: "cn=tdtp-admins,ou=groups,dc=corp,dc=local"
    cost:  100
```

`group` is the full DN used in an LDAP `memberOf` filter with the
LDAP_MATCHING_RULE_IN_CHAIN OID (`1.2.840.113556.1.4.1941`) — so transitive
(nested) group membership works.

## Dev mock users file

`ldap-users.example.json` format:

```json
[
  {"username": "svc_tdtp",    "groups": ["tdtp-pipeline-users", "tdtp-admins"]},
  {"username": "analyst1",    "groups": ["tdtp-pipeline-users"]},
  {"username": "finance_svc", "groups": ["tdtp-pipeline-users", "tdtp-finance"]}
]
```

`username` matches `sAMAccountName`. `groups` are matched literally against the
`group` field from `pipeline-acl.yaml`.

> **Note**: in dev mode the `group` field in ACL is typically a short name
> (`tdtp-pipeline-users`), not a DN, because the mock client does string
> comparison rather than LDAP search.
