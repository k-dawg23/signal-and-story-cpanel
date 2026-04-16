package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

func moneyGBP(cents int64) string {
	// Prices are GBP-only right now.
	pounds := float64(cents) / 100.0
	return fmt.Sprintf("£%.2f", pounds)
}

func (s *Server) sendOrderConfirmationEmail(ctx context.Context, toEmail string, orderID int64, orderRef string) error {
	subject := "Signal & Story — Order confirmed"
	type item struct {
		Name           string
		UnitPriceCents int64
		Quantity       int
	}
	var (
		status        string
		currency      string
		subtotalCents int64
		shippingCents int64
		taxCents      int64
		totalCents    int64
	)
	_ = s.db.QueryRow(ctx, `
		SELECT status, currency, subtotal_cents, shipping_cents, tax_cents, total_cents
		FROM orders
		WHERE id=$1
	`, orderID).Scan(&status, &currency, &subtotalCents, &shippingCents, &taxCents, &totalCents)

	rows, err := s.db.Query(ctx, `
		SELECT product_name_snapshot, unit_price_cents, quantity
		FROM order_items
		WHERE order_id=$1
		ORDER BY id ASC
	`, orderID)
	items := []item{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var it item
			var unit int64
			if err := rows.Scan(&it.Name, &unit, &it.Quantity); err != nil {
				continue
			}
			it.UnitPriceCents = unit
			items = append(items, it)
		}
	}

	lines := bytes.NewBuffer(nil)
	lines.WriteString("<ul style=\"padding-left:18px;margin:10px 0\">")
	if len(items) == 0 {
		lines.WriteString("<li>Items unavailable (local snapshot missing)</li>")
	} else {
		for _, it := range items {
			lineTotal := it.UnitPriceCents * int64(it.Quantity)
			lines.WriteString(fmt.Sprintf(
				"<li><strong>%s</strong> &times; %d — %s</li>",
				htmlEscape(it.Name),
				it.Quantity,
				moneyGBP(lineTotal),
			))
		}
	}
	lines.WriteString("</ul>")

	_ = currency // currently not used in formatting; GBP assumed everywhere.

	html := fmt.Sprintf(`<div style="font-family: ui-sans-serif, system-ui; line-height: 1.5">
  <h2>Order confirmed</h2>
  <p><strong>Order:</strong> %s</p>
  <p><strong>Status:</strong> %s</p>
  <h3 style="margin:14px 0 6px 0">Items</h3>
  %s
  <div style="margin-top:12px;padding-top:12px;border-top:1px solid #eee">
    <div><strong>Subtotal:</strong> %s</div>
    <div><strong>Shipping:</strong> %s</div>
    <div><strong>Tax:</strong> %s</div>
    <div style="margin-top:8px;font-size:18px"><strong>Total:</strong> %s</div>
  </div>
  <p style="margin-top:14px">If you have any questions, reply to this email.</p>
</div>`,
		htmlEscape(orderRef),
		htmlEscape(status),
		lines.String(),
		moneyGBP(subtotalCents),
		moneyGBP(shippingCents),
		moneyGBP(taxCents),
		moneyGBP(totalCents),
	)

	if apiKey := strings.TrimSpace(os.Getenv("BREVO_API_KEY")); apiKey != "" {
		return sendBrevo(apiKey, toEmail, subject, html)
	}
	return sendSMTP(toEmail, subject, html)
}

func htmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(s)
}

func sendBrevo(apiKey, toEmail, subject, html string) error {
	senderEmail := strings.TrimSpace(os.Getenv("BREVO_SENDER_EMAIL"))
	if senderEmail == "" {
		senderEmail = "hello@signal-and-story.local"
	}
	senderName := strings.TrimSpace(os.Getenv("BREVO_SENDER_NAME"))
	if senderName == "" {
		senderName = "Signal & Story"
	}

	payload := map[string]any{
		"sender":      map[string]any{"email": senderEmail, "name": senderName},
		"to":          []map[string]any{{"email": toEmail}},
		"subject":     subject,
		"htmlContent": html,
	}
	b, _ := json.Marshal(payload)

	req, _ := http.NewRequest(http.MethodPost, "https://api.brevo.com/v3/smtp/email", bytes.NewReader(b))
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("api-key", apiKey)

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("brevo status %d", resp.StatusCode)
	}
	return nil
}

func sendSMTP(toEmail, subject, html string) error {
	host := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	if host == "" {
		return nil
	}
	portStr := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	if portStr == "" {
		portStr = "1026"
	}
	port, _ := strconv.Atoi(portStr)
	from := strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if from == "" {
		from = "Signal & Story <hello@signal-and-story.local>"
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))

	msg := bytes.NewBuffer(nil)
	fmt.Fprintf(msg, "From: %s\r\n", from)
	fmt.Fprintf(msg, "To: %s\r\n", toEmail)
	fmt.Fprintf(msg, "Subject: %s\r\n", subject)
	fmt.Fprintf(msg, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(msg, "Content-Type: text/html; charset=UTF-8\r\n")
	fmt.Fprintf(msg, "\r\n%s\r\n", html)

	return smtp.SendMail(addr, nil, extractEmail(from), []string{toEmail}, msg.Bytes())
}

func extractEmail(from string) string {
	// Best-effort extraction of email from "Name <email>".
	if i := strings.Index(from, "<"); i >= 0 {
		if j := strings.Index(from, ">"); j > i {
			return strings.TrimSpace(from[i+1 : j])
		}
	}
	return strings.TrimSpace(from)
}
