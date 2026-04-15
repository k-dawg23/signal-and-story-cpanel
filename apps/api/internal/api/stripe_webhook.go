package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v78"
	"github.com/stripe/stripe-go/v78/webhook"
)

func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(s.cfg.StripeWebhookSecret) == "" || strings.TrimSpace(s.cfg.StripeSecretKey) == "" {
		writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": "stripe_webhook_not_configured"})
		return
	}

	sig := r.Header.Get("Stripe-Signature")
	if strings.TrimSpace(sig) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_signature"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}

	evt, err := webhook.ConstructEventWithOptions(
		body,
		sig,
		strings.TrimSpace(s.cfg.StripeWebhookSecret),
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true},
	)
	if err != nil {
		log.Printf("stripe webhook signature error: %v", err)
		if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "development") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_signature", "detail": err.Error()})
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_signature"})
		return
	}

	// Idempotency: record Stripe event ID (safe even if we don't handle the type).
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	_, _ = s.db.Exec(ctx, `INSERT INTO processed_webhooks (webhook_id) VALUES ($1) ON CONFLICT DO NOTHING`, evt.ID)

	switch evt.Type {
	case "checkout.session.completed":
		_ = s.processStripeCheckoutSessionCompleted(ctx, evt.Data.Raw)
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) processStripeCheckoutSessionCompleted(ctx context.Context, raw json.RawMessage) error {
	var cs stripe.CheckoutSession
	if err := json.Unmarshal(raw, &cs); err != nil {
		return err
	}
	if cs.ID == "" {
		return errors.New("missing_checkout_session_id")
	}
	if strings.ToLower(string(cs.PaymentStatus)) != "paid" {
		// Only treat paid sessions as orders.
		return nil
	}

	metadata := cs.Metadata
	checkoutSessionID := ""
	if metadata != nil {
		checkoutSessionID = strings.TrimSpace(metadata["checkout_session_id"])
	}

	email := strings.TrimSpace(cs.CustomerDetails.Email)
	if email == "" {
		email = strings.TrimSpace(cs.CustomerEmail)
	}

	// Fetch our checkout session for cart reconstruction and pricing snapshots.
	type checkoutSession struct {
		UserID        *string
		Email         *string
		Cart          []byte
		Shipping      string
		ShippingCents int
		SubtotalCents int
		Currency      string
	}
	var local checkoutSession
	if checkoutSessionID != "" {
		_ = s.db.QueryRow(ctx,
			`SELECT user_id, email, cart, shipping_method, shipping_cents, subtotal_cents, currency
			 FROM checkout_sessions WHERE id::text=$1`,
			checkoutSessionID,
		).Scan(&local.UserID, &local.Email, &local.Cart, &local.Shipping, &local.ShippingCents, &local.SubtotalCents, &local.Currency)
	}

	if email == "" && local.Email != nil {
		email = *local.Email
	}
	if email == "" {
		return errors.New("missing_email")
	}

	userID := (*string)(nil)
	if local.UserID != nil {
		userID = local.UserID
	} else if metadata != nil {
		if v := strings.TrimSpace(metadata["user_id"]); v != "" {
			userID = &v
		}
	}

	currency := strings.TrimSpace(local.Currency)
	if currency == "" {
		currency = "GBP"
	}

	// Stripe amounts are in the smallest currency unit (pence for GBP).
	subtotalCents := int64(local.SubtotalCents)
	shippingCents := int64(local.ShippingCents)
	taxCents := int64(0)
	totalCents := int64(0)

	if cs.AmountSubtotal > 0 {
		subtotalCents = cs.AmountSubtotal
	}
	if cs.ShippingCost != nil && cs.ShippingCost.AmountSubtotal > 0 {
		shippingCents = cs.ShippingCost.AmountSubtotal
	}
	if cs.TotalDetails != nil && cs.TotalDetails.AmountTax > 0 {
		taxCents = cs.TotalDetails.AmountTax
	}
	if cs.AmountTotal > 0 {
		totalCents = cs.AmountTotal
	} else {
		totalCents = subtotalCents + shippingCents + taxCents
	}

	shippingMethod := strings.TrimSpace(local.Shipping)
	if shippingMethod == "" && metadata != nil {
		shippingMethod = strings.TrimSpace(metadata["shipping_method"])
	}

	shippingAddress := map[string]any(nil)
	if cs.ShippingDetails != nil && cs.ShippingDetails.Address != nil {
		a := cs.ShippingDetails.Address
		shippingAddress = map[string]any{
			"line1":       a.Line1,
			"line2":       a.Line2,
			"city":        a.City,
			"state":       a.State,
			"postal_code": a.PostalCode,
			"country":     a.Country,
		}
	}

	paymentIntentID := ""
	if cs.PaymentIntent != nil {
		paymentIntentID = cs.PaymentIntent.ID
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var orderDBID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO orders (
			 stripe_payment_intent_id, stripe_checkout_session_id,
			 user_id, email, status, currency,
			 subtotal_cents, shipping_cents, tax_cents, total_cents,
			 shipping_method, shipping_address
		 )
		 VALUES ($1,$2,$3,$4,'paid',$5,$6,$7,$8,$9,$10,$11)
		 ON CONFLICT (stripe_checkout_session_id) DO NOTHING
		 RETURNING id`,
		nullIfEmpty(paymentIntentID),
		cs.ID,
		userID,
		email,
		currency,
		subtotalCents,
		shippingCents,
		taxCents,
		totalCents,
		nullIfEmpty(shippingMethod),
		mustJSON(shippingAddress),
	).Scan(&orderDBID)

	isNew := true
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			isNew = false
			if err := tx.QueryRow(ctx, `SELECT id FROM orders WHERE stripe_checkout_session_id=$1`, cs.ID).Scan(&orderDBID); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	if isNew && len(local.Cart) > 0 {
		var items []struct {
			Handle         string `json:"handle"`
			Name           string `json:"name"`
			UnitPriceCents int    `json:"unit_price_cents"`
			Quantity       int    `json:"quantity"`
		}
		if err := json.Unmarshal(local.Cart, &items); err == nil {
			for _, it := range items {
				_, _ = tx.Exec(ctx,
					`INSERT INTO order_items (order_id, product_name_snapshot, unit_price_cents, quantity)
					 VALUES ($1,$2,$3,$4)`,
					orderDBID, it.Name, it.UnitPriceCents, it.Quantity,
				)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if isNew {
		_ = s.sendOrderConfirmationEmail(email, cs.ID)
	}
	return nil
}
