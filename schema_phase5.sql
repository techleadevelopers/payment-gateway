-- =============================================================================
-- Phase 5: Mobile Expansion — Incremental schema additions
-- Safe to run on top of existing schema.sql (all statements are idempotent).
-- =============================================================================

-- ─── Multi-Asset ─────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS assets (
  symbol           VARCHAR(16)    PRIMARY KEY,
  name             VARCHAR(64)    NOT NULL,
  network          VARCHAR(32)    NOT NULL DEFAULT 'BSC',
  contract_address TEXT,
  decimals         INT            NOT NULL DEFAULT 18,
  min_amount       NUMERIC(28,8)  NOT NULL DEFAULT 1,
  max_amount       NUMERIC(28,8)  NOT NULL DEFAULT 100000,
  daily_limit      NUMERIC(28,8)  NOT NULL DEFAULT 50000,
  monthly_limit    NUMERIC(28,8)  NOT NULL DEFAULT 500000,
  fee_bps          INT            NOT NULL DEFAULT 100,
  active           BOOLEAN        NOT NULL DEFAULT true,
  created_at       TIMESTAMPTZ    NOT NULL DEFAULT now()
);

-- Seed default assets
INSERT INTO assets (symbol, name, network, contract_address, decimals, min_amount, max_amount, fee_bps)
VALUES
  ('USDT',  'Tether USD',       'BSC', '0x55d398326f99059fF775485246999027B3197955', 18, 1,     100000, 100),
  ('USDC',  'USD Coin',         'BSC', '0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d', 18, 1,     100000, 100),
  ('BTCB',  'Bitcoin BEP20',    'BSC', '0x7130d2A12B9BCbFAe4f2634d864A1Ee1Ce3Ead9c', 18, 0.001,   10,   150),
  ('ETH',   'Ethereum BEP20',   'BSC', '0x2170Ed0880ac9A755fd29B2688956BD959F933F8', 18, 0.01,  1000,   150),
  ('BUSD',  'Binance USD',      'BSC', '0xe9e7CEA3DedcA5984780Bafc599bD69ADd087D56', 18, 1,     100000, 100),
  ('EURC',  'Euro Coin',        'BSC', NULL,                                           18, 1,     100000, 100)
ON CONFLICT (symbol) DO NOTHING;

-- ─── Multi-Country ────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS countries (
  code       VARCHAR(4)   PRIMARY KEY,
  name       VARCHAR(64)  NOT NULL,
  currency   VARCHAR(8)   NOT NULL,
  language   VARCHAR(8)   NOT NULL DEFAULT 'pt-BR',
  active     BOOLEAN      NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

INSERT INTO countries (code, name, currency, language)
VALUES
  ('BR', 'Brasil',         'BRL', 'pt-BR'),
  ('AR', 'Argentina',      'ARS', 'es-AR'),
  ('CL', 'Chile',          'CLP', 'es-CL'),
  ('CO', 'Colômbia',       'COP', 'es-CO'),
  ('MX', 'México',         'MXN', 'es-MX'),
  ('PE', 'Peru',           'PEN', 'es-PE'),
  ('US', 'United States',  'USD', 'en-US'),
  ('EU', 'European Union', 'EUR', 'en-EU')
ON CONFLICT (code) DO NOTHING;

-- ─── Multi-Rail ───────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS payment_rails (
  id           VARCHAR(32)  PRIMARY KEY,
  country_code VARCHAR(4)   REFERENCES countries(code),
  name         VARCHAR(64)  NOT NULL,
  currency     VARCHAR(8)   NOT NULL,
  active       BOOLEAN      NOT NULL DEFAULT true,
  metadata     JSONB,
  created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

INSERT INTO payment_rails (id, country_code, name, currency, metadata)
VALUES
  ('pix',    'BR', 'PIX',     'BRL', '{"supports_qrcode": true,  "instant": true}'),
  ('spei',   'MX', 'SPEI',    'MXN', '{"supports_clabe": true,   "instant": true}'),
  ('fednow', 'US', 'FedNow',  'USD', '{"instant": true}'),
  ('sepa',   'EU', 'SEPA',    'EUR', '{"instant": false, "days": 1}'),
  ('pse',    'CO', 'PSE',     'COP', '{"instant": false}'),
  ('khipu',  'CL', 'Khipu',   'CLP', '{"instant": true}')
ON CONFLICT (id) DO NOTHING;

-- ─── KYC Async ───────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS kyc_requests (
  id                   UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id              UUID          NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  level                INT           NOT NULL DEFAULT 1,
  status               VARCHAR(32)   NOT NULL DEFAULT 'pending',
  document_type        VARCHAR(32),
  document_url         TEXT,
  selfie_url           TEXT,
  proof_of_address_url TEXT,
  proof_of_income_url  TEXT,
  reviewer_notes       TEXT,
  submitted_at         TIMESTAMPTZ   NOT NULL DEFAULT now(),
  reviewed_at          TIMESTAMPTZ,
  created_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_kyc_requests_user    ON kyc_requests(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_kyc_requests_pending ON kyc_requests(status, submitted_at) WHERE status = 'pending';

-- ─── Swaps ────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS swaps (
  id                 UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id            UUID          NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  from_asset         VARCHAR(16)   NOT NULL,
  to_asset           VARCHAR(16)   NOT NULL,
  from_amount        NUMERIC(28,8) NOT NULL,
  to_amount          NUMERIC(28,8),
  rate               NUMERIC(28,8),
  fee_bps            INT           NOT NULL DEFAULT 50,
  slippage_tolerance NUMERIC(6,4)  NOT NULL DEFAULT 0.005,
  status             VARCHAR(32)   NOT NULL DEFAULT 'pending',
  tx_hash            TEXT,
  error              TEXT,
  created_at         TIMESTAMPTZ   NOT NULL DEFAULT now(),
  updated_at         TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_swaps_user   ON swaps(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_swaps_status ON swaps(status) WHERE status IN ('pending', 'executing');

-- ─── DCA Strategies (previously referenced in code but missing from schema) ──

CREATE TABLE IF NOT EXISTS dca_strategies (
  id             UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        UUID          NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_symbol   VARCHAR(16)   NOT NULL,
  amount_brl     NUMERIC(18,2) NOT NULL,
  frequency      VARCHAR(16)   NOT NULL DEFAULT 'weekly',
  active         BOOLEAN       NOT NULL DEFAULT true,
  total_invested NUMERIC(18,2) NOT NULL DEFAULT 0,
  total_tokens   NUMERIC(28,8) NOT NULL DEFAULT 0,
  next_execution TIMESTAMPTZ,
  created_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_dca_user   ON dca_strategies(user_id);
CREATE INDEX IF NOT EXISTS idx_dca_active ON dca_strategies(active, next_execution) WHERE active = true;

-- ─── Webhook Subscriptions ────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS webhook_subscriptions (
  id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID        REFERENCES users(id) ON DELETE CASCADE,
  target_url TEXT        NOT NULL,
  events     JSONB       NOT NULL DEFAULT '[]',
  secret     VARCHAR(80) NOT NULL,
  active     BOOLEAN     NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_webhook_subs_user   ON webhook_subscriptions(user_id);
CREATE INDEX IF NOT EXISTS idx_webhook_subs_active ON webhook_subscriptions(active) WHERE active = true;
-- GIN index enables the events @> $1::jsonb operator used in subscriptions-for-event queries
CREATE INDEX IF NOT EXISTS idx_webhook_subs_events ON webhook_subscriptions USING gin(events);

-- ─── Webhook Deliveries ───────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS webhook_deliveries (
  id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
  subscription_id UUID          NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
  event_type      VARCHAR(64)   NOT NULL,
  payload         JSONB         NOT NULL,
  status          VARCHAR(32)   NOT NULL DEFAULT 'pending',
  attempts        INT           NOT NULL DEFAULT 0,
  next_retry_at   TIMESTAMPTZ,
  response_status INT,
  response_body   TEXT,
  last_error      TEXT,
  created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_webhook_del_retry  ON webhook_deliveries(status, next_retry_at)
  WHERE status IN ('pending', 'retrying');
CREATE INDEX IF NOT EXISTS idx_webhook_del_sub    ON webhook_deliveries(subscription_id, created_at DESC);
