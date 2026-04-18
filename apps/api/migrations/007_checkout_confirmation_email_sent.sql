-- One confirmation email per Stripe Checkout Session (stronger than orders.confirmation_email_sent_at for webhook races / duplicate endpoints).
CREATE TABLE IF NOT EXISTS checkout_confirmation_email_sent (
  stripe_checkout_session_id TEXT PRIMARY KEY,
  sent_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
