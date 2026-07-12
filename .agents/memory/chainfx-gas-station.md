---
name: ChainFX Gas Station + PSP layer
description: Architecture notes for Gas Station (paymaster), Auto-Sweeper, and PSP abstraction — completed 2026-07.
---

## Gas Station (Paymaster)

Package `internal/paymaster/` with:
- `oracle.go` — gas price oracle, queries BSC `eth_gasPrice` via rpc.Pool
- `idempotency.go` — sig_hash-based dedup backed by `gas_relay_requests` table
- `retry.go` — exponential backoff up to 4 attempts, then DLQ
- `estimator.go` — weiToGwei uses `big.Float.Quo` (not integer div) for precision; weiToGwei signature is `func weiToGwei(wei *big.Int) *big.Float`
- `batcher.go` — batch collector, 50 ms window, max 10 per batch, sends to signer
- `paymaster.go` — top-level `Service`; imports `math/big` (required for weiToGwei)

**Why:** Gasless relay for end-users; hot wallet sponsors BNB gas, charges USDT fee deducted from relay.

## Database pattern

`database.DB` struct: `SQL *sql.DB` field (NOT embedded). All queries in `gas_station.go` use:
- `db.SQL.QueryRowContext(...)`
- `db.SQL.ExecContext(...)`
- `db.SQL.QueryContext(...)`

**Why:** DB wraps sql.DB for privacy codec and config access — it does NOT embed it, so direct method calls fail at compile time.

## Worker Manager

`NewWorkerManager(db, cfg, mailer, pool *rpc.Pool)` — 4-argument signature. Pool is nil-safe; Gas Station + Auto-Sweeper self-disable gracefully when pool is nil.

`wg.Add(9)` — 9 workers total including Auto-Sweeper + Paymaster relay loop.

`workerMgr.PaymasterService` — exported field, wired to HTTP server via `api.WithPaymaster(...)` in `cmd/api/main.go`.

## HTTP routes

6 routes under `/v1/gas/`:
- `GET  /v1/gas/status`       — public, enabled flag + oracle stats
- `POST /v1/gas/quote`        — public, fee estimate
- `POST /v1/gas/relay`        — admin auth, submit relay
- `GET  /v1/gas/relay/{id}`   — public, relay status
- `GET  /v1/gas/relays`       — admin auth, relay list with stats
- `GET  /v1/gas/sweeper/runs` — admin auth, last 50 sweeper runs

Rate limiter: `AllowN(key, max)` — 3-VU burst per address (not `AllowWithLimit` which doesn't exist).
`authorizeAdmin` returns 3 values: `(*AdminUser, chainFXAuth, bool)`.

## PSP Abstraction

`internal/psp/provider.go` — `PixProvider` interface + `Router`
`internal/psp/efi_adapter.go` — Efí (Gerencianet) adapter

Not yet wired to main PIX handlers — integration is optional next step.

## Migration

`migrations/005_gas_station.sql` — tables: `gas_relay_requests`, `auto_sweeper_runs`

Apply with:
```bash
psql $DATABASE_URL -f migrations/005_gas_station.sql
```

## k6 Stress Test

`tests/paymaster_stress.js` — 4 scenarios, custom metrics (`relay_errors`, `quote_duration_ms`, `relay_duration_ms`), p95 thresholds: status<500ms, quote<800ms, relay<2000ms, GET<400ms.
