-- Migrate orders table for Stripe (keep Polar columns for history).

ALTER TABLE orders
  ALTER COLUMN polar_order_id DROP NOT NULL;

ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS stripe_payment_intent_id TEXT,
  ADD COLUMN IF NOT EXISTS stripe_checkout_session_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS orders_stripe_payment_intent_id_uidx
  ON orders (stripe_payment_intent_id);

CREATE UNIQUE INDEX IF NOT EXISTS orders_stripe_checkout_session_id_uidx
  ON orders (stripe_checkout_session_id);

