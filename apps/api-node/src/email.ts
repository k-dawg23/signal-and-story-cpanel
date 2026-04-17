import type { Pool } from "pg";
import nodemailer from "nodemailer";
import { htmlEscape, moneyGBP } from "./util.js";

export async function sendOrderConfirmationEmail(
  pool: Pool,
  toEmail: string,
  orderID: number,
  orderRef: string
): Promise<void> {
  const subject = "Signal & Story — Order confirmed";

  const or = await pool.query(
    `SELECT status, currency, subtotal_cents, shipping_cents, tax_cents, total_cents
     FROM orders WHERE id=$1`,
    [orderID]
  );
  let subtotalCents = 0;
  let shippingCents = 0;
  let taxCents = 0;
  let totalCents = 0;
  let status = "";
  if (or.rows[0]) {
    status = or.rows[0].status as string;
    subtotalCents = Number(or.rows[0].subtotal_cents);
    shippingCents = Number(or.rows[0].shipping_cents);
    taxCents = Number(or.rows[0].tax_cents);
    totalCents = Number(or.rows[0].total_cents);
  }

  const ir = await pool.query(
    `SELECT product_name_snapshot, unit_price_cents, quantity FROM order_items
     WHERE order_id=$1 ORDER BY id ASC`,
    [orderID]
  );

  let lines = '<ul style="padding-left:18px;margin:10px 0">';
  if (ir.rows.length === 0) {
    lines += "<li>Items unavailable (local snapshot missing)</li>";
  } else {
    for (const r of ir.rows) {
      const name = r.product_name_snapshot as string;
      const unit = Number(r.unit_price_cents);
      const qty = Number(r.quantity);
      lines += `<li><strong>${htmlEscape(name)}</strong> &times; ${qty} — ${moneyGBP(unit * qty)}</li>`;
    }
  }
  lines += "</ul>";

  const html = `<div style="font-family: ui-sans-serif, system-ui; line-height: 1.5">
  <h2>Order confirmed</h2>
  <p><strong>Order:</strong> ${htmlEscape(orderRef)}</p>
  <p><strong>Status:</strong> ${htmlEscape(status)}</p>
  <h3 style="margin:14px 0 6px 0">Items</h3>
  ${lines}
  <div style="margin-top:12px;padding-top:12px;border-top:1px solid #eee">
    <div><strong>Subtotal:</strong> ${moneyGBP(subtotalCents)}</div>
    <div><strong>Shipping:</strong> ${moneyGBP(shippingCents)}</div>
    <div><strong>Tax:</strong> ${moneyGBP(taxCents)}</div>
    <div style="margin-top:8px;font-size:18px"><strong>Total:</strong> ${moneyGBP(totalCents)}</div>
  </div>
  <p style="margin-top:14px">If you have any questions, reply to this email.</p>
</div>`;

  const brevoKey = process.env.BREVO_API_KEY?.trim();
  if (brevoKey) {
    await sendBrevo(brevoKey, toEmail, subject, html);
    return;
  }
  await sendSMTP(toEmail, subject, html);
}

async function sendBrevo(apiKey: string, toEmail: string, subject: string, html: string): Promise<void> {
  const senderEmail = process.env.BREVO_SENDER_EMAIL?.trim() || "hello@signal-and-story.local";
  const senderName = process.env.BREVO_SENDER_NAME?.trim() || "Signal & Story";
  const payload = {
    sender: { email: senderEmail, name: senderName },
    to: [{ email: toEmail }],
    subject,
    htmlContent: html,
  };
  const res = await fetch("https://api.brevo.com/v3/smtp/email", {
    method: "POST",
    headers: {
      accept: "application/json",
      "content-type": "application/json",
      "api-key": apiKey,
    },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    const t = await res.text().catch(() => "");
    throw new Error(`brevo status ${res.status} ${t}`);
  }
}

async function sendSMTP(toEmail: string, subject: string, html: string): Promise<void> {
  const host = process.env.SMTP_HOST?.trim();
  if (!host) return;
  const port = Number(process.env.SMTP_PORT?.trim() || "1026");
  const from = process.env.SMTP_FROM?.trim() || "Signal & Story <hello@signal-and-story.local>";
  const user = process.env.SMTP_USER?.trim() || "";
  const pass = process.env.SMTP_PASS?.trim() || "";
  const transporter = nodemailer.createTransport({
    host,
    port,
    secure: false,
    auth: user || pass ? { user, pass } : undefined,
  });
  await transporter.sendMail({ from, to: toEmail, subject, html });
}
