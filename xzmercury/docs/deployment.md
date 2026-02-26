# Deployment

## Prerequisites

| Component | Minimum version | Notes |
|-----------|----------------|-------|
| Go | 1.23 | For building |
| Redis (Mercury) | 6.2+ | `GETDEL` command required; **no persistence** |
| Redis (Pipeline) | 6.0+ | Quota, LDAP cache, request state |
| LDAP/AD | Any | OpenLDAP 2.4+, Windows Server 2012+ AD |
| OS | Linux (amd64/arm64) or Windows Server | |

## Build

```bash
cd xzmercury
go build -tags production -ldflags="-s -w" -o /usr/local/bin/xzmercury ./cmd/xzmercury/
```

`-tags production` disables the `DevClient` in `pkg/mercury` (compile-time guard).
`-ldflags="-s -w"` strips debug symbols for a smaller binary.

## Config file

```bash
cp configs/xzmercury.example.yaml /etc/xzmercury.yaml
chmod 400 /etc/xzmercury.yaml
chown svc_xzmercury /etc/xzmercury.yaml
```

Edit `/etc/xzmercury.yaml` — at minimum set:
- `mercury.addr`
- `pipeline.addr`
- `ldap.*`
- `quota.acl_file`

Do **not** put `server_secret` in the file — use `MERCURY_SERVER_SECRET` env var.

## Service account

```bash
useradd --system --shell /sbin/nologin --home /opt/xzmercury svc_xzmercury
mkdir -p /opt/xzmercury/configs
chown -R svc_xzmercury /opt/xzmercury
```

## systemd unit

`/etc/systemd/system/xzmercury.service`:

```ini
[Unit]
Description=xzmercury — TDTP key management service
After=network.target redis.service

[Service]
Type=simple
User=svc_xzmercury
Group=svc_xzmercury

ExecStart=/usr/local/bin/xzmercury --config /etc/xzmercury.yaml
Restart=on-failure
RestartSec=5s

# HMAC secret — keep out of config file
Environment="MERCURY_SERVER_SECRET=<secret-from-vault>"

# Hardening
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ReadWritePaths=/var/log/xzmercury
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload
systemctl enable xzmercury
systemctl start xzmercury
systemctl status xzmercury
```

## Docker

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY . .
RUN cd xzmercury && \
    go build -tags production -ldflags="-s -w" \
    -o /xzmercury ./cmd/xzmercury/

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /xzmercury /xzmercury
COPY xzmercury/configs/xzmercury.example.yaml /etc/xzmercury.yaml
EXPOSE 3000
ENTRYPOINT ["/xzmercury", "--config", "/etc/xzmercury.yaml"]
```

```bash
docker build -f xzmercury/Dockerfile -t xzmercury:latest .
docker run -d \
  -e MERCURY_SERVER_SECRET="$SECRET" \
  -v /etc/xzmercury.yaml:/etc/xzmercury.yaml:ro \
  -p 127.0.0.1:3000:3000 \
  xzmercury:latest
```

Note: the distroless image runs as `nonroot` (uid=65532) — the privilege guard
will pass without special configuration.

## Mercury Redis hardening

```bash
# /etc/redis/mercury.conf
save ""
appendonly no
bind 127.0.0.1
requirepass <strong-password>
maxmemory 256mb
maxmemory-policy allkeys-lru
```

```bash
redis-server /etc/redis/mercury.conf --port 6379 --daemonize yes
```

## Health checks

```bash
# Liveness
curl -sf http://localhost:3000/healthz && echo OK

# Readiness (checks both Redis instances)
curl -sf http://localhost:3000/readyz | jq .
```

## Production checklist

- [ ] Mercury Redis: `save ""`, `appendonly no`, no public bind
- [ ] Pipeline Redis: separate instance, AOF optional
- [ ] `SERVER_SECRET` stored in a secrets manager (Vault, AWS Secrets Manager, etc.)
- [ ] `bind_password` stored in a secrets manager, not in YAML
- [ ] Service runs as `svc_xzmercury` (uid != 0)
- [ ] systemd hardening: `NoNewPrivileges`, `ProtectSystem`, `PrivateTmp`
- [ ] Firewall: only tdtpcli hosts can reach `:3000`
- [ ] Firewall: only xzmercury host can reach Mercury Redis port
- [ ] `pipeline-acl.yaml` reviewed and signed off by security team
- [ ] LDAP `bind_dn` account is read-only
- [ ] Log aggregation configured (stdout → ELK / Loki / Graylog)
- [ ] `/readyz` wired into load balancer / k8s readiness probe

## Upgrading

xzmercury is stateless with respect to in-flight requests (keys in Mercury Redis
survive a restart). Rolling upgrade procedure:

1. Build new binary
2. Stop old instance (`systemctl stop xzmercury`) — in-flight bind calls complete within `write_timeout`
3. Start new instance
4. Any keys bound before the restart remain retrievable (they are in Redis, not in process memory)
