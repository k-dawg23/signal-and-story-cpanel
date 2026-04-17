-- BIGSERIAL avoids uuid-ossp (often unavailable without superuser on shared hosting).
CREATE TABLE IF NOT EXISTS checkout_sessions (
  id BIGSERIAL PRIMARY KEY,
  user_id TEXT,
  email TEXT,
  cart JSONB NOT NULL,
  shipping_method TEXT NOT NULL,
  shipping_cents INTEGER NOT NULL DEFAULT 0,
  subtotal_cents INTEGER NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT 'GBP',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS checkout_sessions_created_at_idx ON checkout_sessions (created_at DESC);

