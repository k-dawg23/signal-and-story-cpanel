package api

import (
	"bytes"
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

func (s *Server) sendOrderConfirmationEmail(toEmail, orderRef string) error {
	subject := "Signal & Story — Order confirmed"
	html := fmt.Sprintf(`<div style="font-family: ui-sans-serif, system-ui; line-height: 1.5">
  <h2>Gear for Every Universe</h2>
  <p>Your order is confirmed.</p>
  <p><strong>Order:</strong> %s</p>
  <p>If you have any questions, reply to this email.</p>
</div>`, orderRef)

	if apiKey := strings.TrimSpace(os.Getenv("BREVO_API_KEY")); apiKey != "" {
		return sendBrevo(apiKey, toEmail, subject, html)
	}
	return sendSMTP(toEmail, subject, html)
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
