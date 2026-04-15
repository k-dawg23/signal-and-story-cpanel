-- Ensure we only send one order confirmation email per order.

ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS confirmation_email_sent_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS orders_confirmation_email_sent_at_idx
  ON orders (confirmation_email_sent_at);

