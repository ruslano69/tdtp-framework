# Local prod reproduction (no Docker, no AD, no Redis daemon)

Production xZMercury needs a **real Redis** (two instances) and **Active Directory**.
On an air-gapped dev machine or CI without those, you can still exercise the full
prod code path using two helpers:

- **`tdtp-redis`** — in-memory Redis-compatible TCP server (two instances).
- **mock LDAP** — enabled by setting `ldap.mock_users_file` in the prod config.

What runs for real (identical to production):

- prod startup (`dev=false`): CA bootstrap, prod-mode HMAC, `caGuard` on `/api/keys/*`
- real Redis protocol over TCP: `GETDEL`, `SETNX`, `TTL`, the burn-marker Lua script
- enroll/authorize with `tdtp-ca` (TPM-stub challenge-response, /hello gate, seat-count)

What is substituted (local only):

- `tdtp-redis` instead of a Redis daemon — state is in memory, lost on exit
- mock LDAP instead of AD — driven by `ldap-users.dev.json`

> Never use this for production data. `tdtp-redis` persists nothing.

## Build the binaries

```bash
go build -o tdtp-redis.exe   ./xzmercury/cmd/tdtp-redis/
go build -o tdtp-ca.exe      ./xzmercury/cmd/tdtp-ca/
go build -o tdtp-certify.exe ./xzmercury/cmd/tdtp-certify/
go build -o xzmercury.exe    ./xzmercury/cmd/xzmercury/
```

## Launch sequence

### 1. Redis (two instances, with auth)

```bash
./tdtp-redis.exe --mercury 127.0.0.1:7379 --pipeline 127.0.0.1:7380 --password redispw
```

### 2. CA — keygen, start, issue a license

```bash
./tdtp-certify.exe keygen --out ca.priv
./tdtp-ca.exe --db ca.db --key ca.priv --addr :8466 &

./tdtp-certify.exe issue-license --db ca.db --licensee "ProdTest" \
    --permissions etl,enc,s3 --seat-limit 2 --expires 2027-06-01
# → prints a license key like TDTP-XXXXX-XXXXX-XXXXX-XXXXX
```

### 3. xZMercury in PROD mode (no --dev)

Paste the license key into `ca.license_key` (or export `TDTPCA_LICENSE_KEY`),
then point the config at the tdtp-redis instances and the CA:

```bash
# configs/xzmercury.prod-local.yaml is a ready template
TDTPCA_LICENSE_KEY=TDTP-XXXXX-XXXXX-XXXXX-XXXXX \
  ./xzmercury.exe --config xzmercury/configs/xzmercury.prod-local.yaml
```

Expected startup log:

```
WRN prod: using MOCK LDAP (mock_users_file is set) — local reproduction only
INF ca: enrolled successfully  cert_id=... permissions=["etl","enc","s3"]
INF CA authorization active — key operations enabled
INF xzmercury started  addr=:3088  dev=false
```

## Verify the prod path

```bash
# Status: prod mode, CA-authorized, licensed permissions
curl -s http://localhost:3088/status
# {"ca_authorized":true,"dev":false,"mode":"prod","permissions":["etl","enc","s3"]}

# Bind a key (stored in the real Redis via tdtp-redis)
curl -s -X POST http://localhost:3088/api/keys/bind \
  -H 'Content-Type: application/json' \
  -d '{"package_uuid":"u1","pipeline_name":"salary-pipeline"}'

# Retrieve once → 200 (burns key + writes burn marker)
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:3088/api/keys/retrieve \
  -H 'Content-Type: application/json' -d '{"package_uuid":"u1","caller":"svc_consumer"}'
# 200

# Retrieve again → 410 KEY_BURNED_BY_OTHER mode=prod
curl -s -X POST http://localhost:3088/api/keys/retrieve \
  -H 'Content-Type: application/json' -d '{"package_uuid":"u1","caller":"svc_other"}'
# {"code":"KEY_BURNED_BY_OTHER","mode":"prod","burned_at":"..."}
```

## CA admin while running

```bash
./tdtp-certify.exe list-active --db ca.db      # envs seen in last 24h
./tdtp-certify.exe list-licenses --db ca.db    # seat usage
```

## Moving to a real deployment

Swap two things, nothing else:

1. Replace `tdtp-redis` with a real Redis daemon (or cluster); keep `mercury.addr` /
   `pipeline.addr` / passwords pointing at it.
2. Clear `ldap.mock_users_file` and fill the real `ldap.*` settings → real AD.

The CA, license, HMAC mode, burn marker and caGuard behaviour are already exactly
what production runs.
