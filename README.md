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

Environment variables:

- `CHAWRTD_ADDR` (default `:8090`)
- `CHAWRTD_DEFAULT_TIMEOUT_SECONDS` (default `120`)

## API

### Health

```bash
curl -s http://127.0.0.1:8090/healthz
```

### FRPS

Deploy:

```bash
curl -s -X POST http://127.0.0.1:8090/v1/frps/deploy \
  -H 'Content-Type: application/json' \
  -d '{"port":7070,"token":"replace-with-random-token"}'
```

Status:

```bash
curl -s http://127.0.0.1:8090/v1/frps/status
```

Reset:

```bash
curl -s -X POST http://127.0.0.1:8090/v1/frps/reset
```

### WireGuard

Deploy:

```bash
curl -s -X POST http://127.0.0.1:8090/v1/wg/deploy \
  -H 'Content-Type: application/json' \
  -d '{"port":51820,"tunnelIp":"10.0.0.1/24"}'
```

Status:

```bash
curl -s http://127.0.0.1:8090/v1/wg/status
```

Reset:

```bash
curl -s -X POST http://127.0.0.1:8090/v1/wg/reset \
  -H 'Content-Type: application/json' \
  -d '{"interface":"wg0","removeKeys":true}'
```

Verify:

```bash
curl -s -X POST http://127.0.0.1:8090/v1/wg/verify \
  -H 'Content-Type: application/json' \
  -d '{"pingTargets":["10.0.0.2","10.0.0.3"]}'
```

## Next migration steps

- Add async job queue with `taskId` (`submit/get/cancel`).
- Move router fleet management flows from `manager.ts` into daemon-side adapters.
- Add transport adapter layer so Hermes and other platforms can reuse the same execution core.
