import nodemailer from "nodemailer";

export type SendEmailInput = {
  to: string;
  subject: string;
  html: string;
};

function mustGetEnv(name: string): string {
  const v = process.env[name];
  if (!v) throw new Error(`Missing env: ${name}`);
  return v;
}

function parseFrom(fromRaw: string): { name: string; email: string } {
  // Accept "Name <email>" or plain email.
  const m = fromRaw.match(/^(.*)<([^>]+)>\s*$/);
  if (m) {
    const name = (m[1] ?? "").trim().replace(/^"|"$/g, "");
    const email = (m[2] ?? "").trim();
    return { name: name || "Signal & Story", email };
  }
  return { name: "Signal & Story", email: fromRaw.trim() };
}

async function sendBrevo({ to, subject, html }: SendEmailInput) {
  const apiKey = mustGetEnv("BREVO_API_KEY");

  const senderEmail = (process.env.BREVO_SENDER_EMAIL ?? "").trim();
  const senderName = (process.env.BREVO_SENDER_NAME ?? "").trim();
  const fallbackFrom = mustGetEnv("SMTP_FROM");
  const from = parseFrom(fallbackFrom);

  const payload = {
    sender: {
      email: senderEmail || from.email,
      name: senderName || from.name,
    },
    to: [{ email: to }],
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
    const text = await res.text().catch(() => "");
    throw new Error(`brevo_error status=${res.status} body=${text}`);
  }
}

async function sendSMTP({ to, subject, html }: SendEmailInput) {
  const host = mustGetEnv("SMTP_HOST");
  const port = Number(mustGetEnv("SMTP_PORT"));
  const user = process.env.SMTP_USER ?? "";
  const pass = process.env.SMTP_PASS ?? "";
  const from = mustGetEnv("SMTP_FROM");

  const transporter = nodemailer.createTransport({
    host,
    port,
    secure: false,
    auth: user || pass ? { user, pass } : undefined,
  });

  await transporter.sendMail({ from, to, subject, html });
}

export async function sendEmail(input: SendEmailInput) {
  // Prefer Brevo if configured.
  if ((process.env.BREVO_API_KEY ?? "").trim() !== "") {
    return sendBrevo(input);
  }
  return sendSMTP(input);
}

