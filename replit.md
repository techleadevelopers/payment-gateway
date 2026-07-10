# ChainFX Payment Gateway

Backend Go para orquestração instantânea de settlement fiat ↔ USDT/cripto.

## Stack

- **Language**: Go 1.25.5
- **Database**: PostgreSQL
- **Blockchain**: BSC (BEP-20) via go-ethereum
- **Architecture**: HTTP API + background workers + mobile API layer

## How to run

```bash
# Requires DATABASE_URL env var (see .env.example in README)
go run ./cmd/api
```

The API starts on port **8080** (configurable via `PORT` env var).

## Project structure

| Path | Purpose |
|------|---------|
| `cmd/api` | Main API server entrypoint |
| `internal/server` | Web API routes (existing) |
| `internal/mobile` | `/api/mobile/*` routes for React Native app |
| `internal/workers` | Background workers (price, payout, buysend, onchain, sweep, KYC, swap, push notifications, webhooks) |
| `internal/database` | PostgreSQL queries |
| `internal/config` | Environment variable config |
| `internal/models` | Shared data models |
| `schema.sql` | Core DB schema |
| `schema_phase5.sql` | Phase 5 additions (must be applied to DB separately) |
| `signer/` | Isolated crypto signer service |
| `contracts/` | BSC smart contracts |

## Mobile API base path

All mobile endpoints live under `/api/mobile/` and are handled by `internal/mobile/Server`. They are additive — the existing web API at all other paths is untouched.

### Phase 5 mobile endpoints

| Feature | Endpoints |
|---------|-----------|
| Multi-Asset | `GET /api/mobile/assets`, `/assets/{symbol}`, `/assets/{symbol}/rate` |
| Multi-Country | `GET /api/mobile/countries`, `/countries/detect`, `/countries/{code}/rails` |
| KYC (async, non-blocking) | `POST /api/mobile/kyc/submit`, `GET /kyc/status`, `/kyc/history`, `/kyc/limits` |
| Swap (crypto→crypto) | `POST /api/mobile/swap/quote`, `/swap/execute`, `GET /swap/{id}`, `/swaps` |
| Webhooks (n8n/Zapier/Make) | `POST /api/mobile/webhooks/subscribe`, `GET /webhooks`, `DELETE /webhooks/{id}`, `PUT /webhooks/{id}/toggle` |

## Environment variables

See `README.md` for full list. Key vars:

```env
DATABASE_URL=postgres://...
MOBILE_JWT_SECRET=...
FCM_SERVER_KEY=...          # For push notifications
PIX_WEBHOOK_SECRET=...
SIGNER_URL=...
```

## Phase 5 DB migration

After setting `DATABASE_URL`, apply the Phase 5 schema:

```bash
psql $DATABASE_URL -f schema.sql
psql $DATABASE_URL -f schema_phase5.sql
```

## User preferences

- Keep mobile API isolated under `/api/mobile/` — never break existing web API routes
- Phase 5 is mobile-only; web API additions belong in `internal/server/`
