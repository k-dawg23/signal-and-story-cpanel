import "./load-env.js";
import { betterAuth } from "better-auth";
import { toNodeHandler } from "better-auth/node";
import { magicLink } from "better-auth/plugins";
import { Pool } from "pg";
import { sendEmail } from "./email.js";

function mustGetEnv(name: string): string {
  const v = process.env[name];
  if (!v) throw new Error(`Missing env: ${name}`);
  return v;
}

const pool = new Pool({
  connectionString: mustGetEnv("DATABASE_URL"),
});

export const auth = betterAuth({
  database: pool,
  secret: mustGetEnv("BETTER_AUTH_SECRET"),
  baseURL: mustGetEnv("AUTH_BASE_URL"),
  plugins: [
    magicLink({
      sendMagicLink: async ({ email, url }) => {
        await sendEmail({
          to: email,
          subject: "Your Signal & Story magic link",
          html: `<div style="font-family: ui-sans-serif, system-ui; line-height: 1.5">
  <h2>Sign in to Signal &amp; Story</h2>
  <p>Click to sign in:</p>
  <p><a href="${url}">${url}</a></p>
  <p style="color:#666">If you didn’t request this, you can ignore this email.</p>
</div>`,
        });
      },
      expiresIn: 60 * 10
    }),
  ],
});

export const authHandler = toNodeHandler(auth);

