CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS orders (
  id UUID PRIMARY KEY,
  status VARCHAR(32) NOT NULL,
  amount_brl NUMERIC(18,2) NOT NULL,
  btc_amount NUMERIC(28,8) NOT NULL,
  fee_brl NUMERIC(18,2),
  payout_brl NUMERIC(18,2),
  address TEXT NOT NULL,
  asset VARCHAR(16) NOT NULL DEFAULT 'USDT',
  network VARCHAR(32) NOT NULL DEFAULT 'TRON',
  rate_locked NUMERIC(28,8) NOT NULL,
  rate_lock_expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  tx_hash TEXT,
  error TEXT,
  deposit_tx TEXT,
  deposit_amount NUMERIC(28,8),
  pix_cpf TEXT,
  pix_phone TEXT,
  derivation_index INT
);

CREATE TABLE IF NOT EXISTS order_events (
  id UUID PRIMARY KEY,
  order_id UUID REFERENCES orders(id),
  type VARCHAR(64) NOT NULL,
  payload JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS payouts (
  id UUID PRIMARY KEY,
  order_id UUID REFERENCES orders(id),
  pix_cpf TEXT,
  pix_key TEXT,
  status VARCHAR(32) NOT NULL,
  provider_response JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS onchain_cursor (
  id SERIAL PRIMARY KEY,
  network VARCHAR(32) NOT NULL UNIQUE,
  last_block BIGINT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sweeps (
  id UUID PRIMARY KEY,
  child_index INT NOT NULL,
  from_addr TEXT NOT NULL,
  to_addr TEXT NOT NULL,
  amount NUMERIC(28,8) NOT NULL,
  tx_hash TEXT,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  idempotency_key TEXT,
  amount_trx_fee NUMERIC(28,8),
  order_id UUID REFERENCES orders(id)
);

CREATE TABLE IF NOT EXISTS buy_orders (
  id UUID PRIMARY KEY,
  status VARCHAR(32) NOT NULL,
  amount_brl NUMERIC(18,2) NOT NULL,
  fee_brl NUMERIC(18,2),
  payout_brl NUMERIC(18,2),
  crypto_amount NUMERIC(28,8) NOT NULL,
  asset VARCHAR(16) NOT NULL DEFAULT 'USDT',
  dest_address TEXT NOT NULL,
  rate_locked NUMERIC(28,8) NOT NULL,
  rate_lock_expires_at TIMESTAMPTZ NOT NULL,
  pix_payload JSONB,
  tx_hash_out TEXT,
  error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS buy_order_events (
  id UUID PRIMARY KEY,
  buy_order_id UUID REFERENCES buy_orders(id),
  type VARCHAR(64) NOT NULL,
  payload JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_address ON orders(address);
CREATE INDEX IF NOT EXISTS idx_orders_pix_cpf_created ON orders(pix_cpf, created_at);
CREATE INDEX IF NOT EXISTS idx_orders_pix_phone_created ON orders(pix_phone, created_at);
CREATE INDEX IF NOT EXISTS idx_order_events_lookup ON order_events(order_id, type);
CREATE INDEX IF NOT EXISTS idx_buy_orders_status ON buy_orders(status);
CREATE INDEX IF NOT EXISTS idx_buy_order_events_lookup ON buy_order_events(buy_order_id, type);
CREATE INDEX IF NOT EXISTS idx_sweeps_status ON sweeps(status);
