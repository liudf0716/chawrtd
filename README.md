# chawrtd

`chawrtd` is the execution daemon for ChawRT operations.

It is designed to migrate long-running and host-level operations out of `openclaw-wrt/src/tool.ts`, while keeping `openclaw-wrt` focused on intent orchestration.

## What is migrated in this first step

- FRPS server operations:
  - deploy
  - status
  - reset
- WireGuard server operations:
  - deploy
  - status
  - reset
  - server-side verify with optional ping checks

## Why this helps

- Isolates risky host operations (`sudo`, `iptables`, `systemctl`) into one daemon.
- Makes command execution and timeout handling consistent.
- Prepares a clean API surface for future integration with `openclaw-wrt` and other platforms.

## Run

```bash
go run ./cmd/chawrtd
```

Show help/version:

```bash
go run ./cmd/chawrtd --help
go run ./cmd/chawrtd --version
```

Build binary with version metadata:

```bash
make build
./bin/chawrtd --version
```

By default, VERSION is derived from git describe (tags/commit), and commit/time are embedded automatically.

Override build metadata (optional):

```bash
make build VERSION=1.2.3
```

Environment variables:

- `CHAWRTD_ADDR` (default `:8001`)
- `CHAWRTD_DEFAULT_TIMEOUT_SECONDS` (default `120`)
- `CHAWRTD_CONFIG_FILE` (optional explicit TOML path)
- `CHAWRTD_TOKEN` (default `clawwrt`)
- `CHAWRTD_ALIAS_FILE` (default `device-aliases.json`)
- `CHAWRTD_TLS_CERT_FILE` (optional PEM certificate file; when set with key file, server serves HTTPS/WSS)
- `CHAWRTD_TLS_KEY_FILE` (optional PEM private key file; must be set together with cert file)

Device alias persistence:

- On first device connection, chawrtd auto-assigns aliases as `WiFi1`, `WiFi2`, `WiFi3`, ...
- Aliases are persisted in `CHAWRTD_ALIAS_FILE` as a device_id -> alias JSON map.
- Existing aliases are reused on reconnect.

Example with TLS enabled:

```bash
CHAWRTD_TLS_CERT_FILE=/etc/chawrtd/server.crt \
CHAWRTD_TLS_KEY_FILE=/etc/chawrtd/server.key \
go run ./cmd/chawrtd
```

## API

When TLS is enabled, use `https://` for the HTTP API and `wss://host:8001/ws/clawwrt` for router device connections.

### Health

```bash
curl -s http://127.0.0.1:8001/healthz
```

### FRPS

Deploy:

```bash
curl -s -X POST http://127.0.0.1:8001/v1/frps/deploy \
  -H 'Content-Type: application/json' \
  -d '{"port":7070,"token":"replace-with-random-token"}'
```

Status:

```bash
curl -s http://127.0.0.1:8001/v1/frps/status
```

Verify:

```bash
curl -s -X POST http://127.0.0.1:8001/v1/frps/verify \
  -H 'Content-Type: application/json' \
  -d '{"protocol":"tcp","port":7070}'
```

Reset:

```bash
curl -s -X POST http://127.0.0.1:8001/v1/frps/reset
```

### WireGuard

Deploy:

```bash
curl -s -X POST http://127.0.0.1:8001/v1/wg/deploy \
  -H 'Content-Type: application/json' \
  -d '{"port":51820,"tunnelIp":"10.0.0.1/24"}'
```

Status:

```bash
curl -s http://127.0.0.1:8001/v1/wg/status
```

Reset:

```bash
curl -s -X POST http://127.0.0.1:8001/v1/wg/reset \
  -H 'Content-Type: application/json' \
  -d '{"interface":"wg0","removeKeys":true}'
```

Verify:

```bash
curl -s -X POST http://127.0.0.1:8001/v1/wg/verify \
  -H 'Content-Type: application/json' \
  -d '{"pingTargets":["10.0.0.2","10.0.0.3"]}'
```

### Device Diagnose

Note: `diagnose/http` and `diagnose/https` target the router's apfree-wifidog captive portal authentication service by default (typically HTTP 2060 and HTTPS 8443), not arbitrary web services.

DHCP:

```bash
curl -s -X POST http://127.0.0.1:8001/v1/device/<device-id>/diagnose/dhcp \
  -H 'Content-Type: application/json' \
  -d '{"interface":"br-lan","probe_count":5}'
```

DNS:

```bash
curl -s -X POST http://127.0.0.1:8001/v1/device/<device-id>/diagnose/dns \
  -H 'Content-Type: application/json' \
  -d '{"dns_server":"127.0.0.1","domains":["captive.apple.com"],"probe_count":5}'
```

HTTP:

```bash
curl -s -X POST http://127.0.0.1:8001/v1/device/<device-id>/diagnose/http \
  -H 'Content-Type: application/json' \
  -d '{"host":"127.0.0.1","port":2060,"path":"/","probe_count":5}'
```

HTTPS:

```bash
curl -s -X POST http://127.0.0.1:8001/v1/device/<device-id>/diagnose/https \
  -H 'Content-Type: application/json' \
  -d '{"host":"127.0.0.1","port":8443,"path":"/","probe_count":5}'
```

## Next migration steps

- Add async job queue with `taskId` (`submit/get/cancel`).
- Move router fleet management flows from `manager.ts` into daemon-side adapters.
- Add transport adapter layer so Hermes and other platforms can reuse the same execution core.
