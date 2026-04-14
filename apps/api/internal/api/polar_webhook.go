package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"strconv"
)

type polarWebhookEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (s *Server) handlePolarWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PolarWebhookSecret == "" {
		writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": "polar_webhook_not_configured"})
		return
	}

	webhookID := r.Header.Get("webhook-id")
	webhookTimestamp := r.Header.Get("webhook-timestamp")
	webhookSignature := r.Header.Get("webhook-signature")
	if webhookID == "" || webhookTimestamp == "" || webhookSignature == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_webhook_headers"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}

	if err := verifyPolarSignature(body, webhookID, webhookTimestamp, webhookSignature, s.cfg.PolarWebhookSecret); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_signature"})
		return
	}

	// Idempotency: record webhook-id.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err = s.db.Exec(ctx, `INSERT INTO processed_webhooks (webhook_id) VALUES ($1) ON CONFLICT DO NOTHING`, webhookID)
	if err != nil {
		// A DB issue shouldn't trigger retries; acknowledge.
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	var evt polarWebhookEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	if evt.Type == "order.paid" {
		_ = s.processPolarOrderPaid(ctx, evt.Data)
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func verifyPolarSignature(payload []byte, webhookID, webhookTimestamp, signatureHeader, secret string) error {
	// Reject very old timestamps to limit replay.
	ts, err := parseUnixTimestamp(webhookTimestamp)
	if err != nil {
		return err
	}
	if time.Since(ts) > 5*time.Minute || time.Until(ts) > 5*time.Minute {
		return errors.New("timestamp_out_of_range")
	}

	secretBytes, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return errors.New("invalid_secret_b64")
	}

	signed := fmt.Sprintf("%s.%s.%s", webhookID, webhookTimestamp, string(payload))
	mac := hmac.New(sha256.New, secretBytes)
	_, _ = mac.Write([]byte(signed))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	provided := extractV1Signature(signatureHeader)
	if provided == "" {
		return errors.New("missing_v1_signature")
	}
	if !hmac.Equal([]byte(provided), []byte(expected)) {
		return errors.New("signature_mismatch")
	}

	return nil
}

func parseUnixTimestamp(s string) (time.Time, error) {
	sec, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(sec, 0), nil
}

func extractV1Signature(header string) string {
	// Header is documented as "v1,<signature>" and may include multiple signatures separated by spaces or commas.
	parts := strings.FieldsFunc(header, func(r rune) bool { return r == ' ' || r == ',' })
	for i := 0; i < len(parts); i++ {
		if parts[i] == "v1" && i+1 < len(parts) {
			return strings.TrimSpace(parts[i+1])
		}
		if strings.HasPrefix(parts[i], "v1") && strings.Contains(parts[i], ",") {
			// unlikely with FieldsFunc, but keep for safety
			p := strings.SplitN(parts[i], ",", 2)
			if len(p) == 2 && p[0] == "v1" {
				return strings.TrimSpace(p[1])
			}
		}
		if strings.HasPrefix(parts[i], "v1") && len(parts[i]) > 3 && parts[i][2] == ':' {
			return strings.TrimSpace(parts[i][3:])
		}
	}
	// fallback for "v1,<sig>" in one token
	for _, token := range strings.Split(header, ",") {
		token = strings.TrimSpace(token)
		if strings.HasPrefix(token, "v1,") {
			return strings.TrimPrefix(token, "v1,")
		}
	}
	return ""
}

func (s *Server) processPolarOrderPaid(ctx context.Context, raw json.RawMessage) error {
	// Best-effort parsing. We rely on checkout_session_id metadata to rebuild line items.
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}

	orderID := getString(data, "id")
	if orderID == "" {
		return errors.New("missing_order_id")
	}
	status := getString(data, "status")
	if status == "" {
		status = "paid"
	}

	customer := getMap(data, "customer")
	email := getString(customer, "email")
	if email == "" {
		email = getString(data, "customer_email")
	}

	metadata := getMap(data, "metadata")
	checkoutSessionID := getString(metadata, "checkout_session_id")

	// Fetch our checkout session for cart reconstruction.
	type checkoutSession struct {
		UserID        *string
		Email         *string
		Cart          []byte
		Shipping      string
		ShippingCents int
		SubtotalCents int
		Currency      string
	}
	var cs checkoutSession
	if checkoutSessionID != "" {
		_ = s.db.QueryRow(ctx,
			`SELECT user_id, email, cart, shipping_method, shipping_cents, subtotal_cents, currency
			 FROM checkout_sessions WHERE id::text=$1`,
			checkoutSessionID,
		).Scan(&cs.UserID, &cs.Email, &cs.Cart, &cs.Shipping, &cs.ShippingCents, &cs.SubtotalCents, &cs.Currency)
	}

	if email == "" && cs.Email != nil {
		email = *cs.Email
	}
	if email == "" {
		return errors.New("missing_email")
	}

	totalCents := int64(getInt(data, "amount"))
	if totalCents == 0 {
		totalCents = int64(cs.SubtotalCents + cs.ShippingCents)
	}

	var orderDBID int64
	err := s.db.QueryRow(ctx,
		`INSERT INTO orders (polar_order_id, user_id, email, status, currency, subtotal_cents, shipping_cents, tax_cents, total_cents, shipping_method)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (polar_order_id) DO UPDATE SET status=EXCLUDED.status
		 RETURNING id`,
		orderID,
		cs.UserID,
		email,
		status,
		coalesceStr(cs.Currency, "GBP"),
		cs.SubtotalCents,
		cs.ShippingCents,
		0,
		totalCents,
		nullIfEmpty(cs.Shipping),
	).Scan(&orderDBID)
	if err != nil {
		return err
	}

	// Insert order items based on stored cart.
	if len(cs.Cart) > 0 {
		var items []struct {
			Handle        string `json:"handle"`
			Name          string `json:"name"`
			UnitPriceCents int   `json:"unit_price_cents"`
			Quantity      int    `json:"quantity"`
		}
		if err := json.Unmarshal(cs.Cart, &items); err == nil {
			for _, it := range items {
				_, _ = s.db.Exec(ctx,
					`INSERT INTO order_items (order_id, product_name_snapshot, unit_price_cents, quantity)
					 VALUES ($1,$2,$3,$4)`,
					orderDBID, it.Name, it.UnitPriceCents, it.Quantity,
				)
			}
		}
	}

	// Send confirmation email (best-effort, async follow-up can improve later).
	_ = s.sendOrderConfirmationEmail(email, orderID)
	return nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(m map[string]any, key string) int {
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		}
	}
	return 0
}

func getMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		if mm, ok := v.(map[string]any); ok {
			return mm
		}
	}
	return map[string]any{}
}

func coalesceStr(p *string, fallback string) string {
	if p != nil && strings.TrimSpace(*p) != "" {
		return *p
	}
	return fallback
}

