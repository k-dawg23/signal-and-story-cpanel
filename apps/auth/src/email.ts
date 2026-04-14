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

export async function sendEmail({ to, subject, html }: SendEmailInput) {
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

  await transporter.sendMail({
    from,
    to,
    subject,
    html,
  });
}

