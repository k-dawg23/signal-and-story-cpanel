CREATE TABLE IF NOT EXISTS processed_webhooks (
  webhook_id TEXT PRIMARY KEY,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

