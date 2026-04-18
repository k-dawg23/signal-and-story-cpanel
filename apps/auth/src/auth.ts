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

function parseOriginList(raw: string | undefined): string[] {
  return (raw ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

function isProductionEnv(): boolean {
  return (
    process.env.APP_ENV === "production" ||
    process.env.NODE_ENV === "production"
  );
}

function defaultTrustedOrigins(): string[] {
  const base = process.env.APP_BASE_URL ?? "http://localhost:4321";
  return [
    base,
    "http://localhost:4321",
    "http://127.0.0.1:4321",
    ...parseOriginList(process.env.APP_ORIGIN_ALLOWLIST),
  ];
}

function normalizeAuthBaseURL(raw: string): string {
  // Our auth handler is mounted under `/api/auth/*` in `server.ts`.
  // Better Auth's `baseURL` must include that prefix so generated links work.
  const trimmed = raw.replace(/\/+$/, "");
  return trimmed.endsWith("/api/auth") ? trimmed : `${trimmed}/api/auth`;
}

/** Parent site domain (no scheme), e.g. `signal-and-story.k-dawg.uk`. When set, session cookies are scoped for all subdomains so the Node API can read the session (it forwards this cookie to `/internal/session`). */
const authCookieDomain = process.env.AUTH_COOKIE_DOMAIN?.trim() || "";

const pool = new Pool({
  connectionString: mustGetEnv("DATABASE_URL"),
});

export const auth = betterAuth({
  database: pool,
  secret: mustGetEnv("BETTER_AUTH_SECRET"),
  baseURL: normalizeAuthBaseURL(mustGetEnv("AUTH_BASE_URL")),
  advanced: {
    ...(authCookieDomain
      ? {
          crossSubDomainCookies: {
            enabled: true,
            domain: authCookieDomain,
          },
        }
      : {}),
  },
  trustedOrigins: (request) => {
    const origins = new Set(defaultTrustedOrigins());
    // Outside production, trust the browser Origin (LAN Network URL, alternate port, etc.).
    if (request && !isProductionEnv()) {
      const origin = request.headers.get("origin");
      if (origin) origins.add(origin);
    }
    return [...origins];
  },
  plugins: [
    magicLink({
      // Default is 1: corporate scanners / link previews often prefetch the verify URL once,
      // so the user's click hits ATTEMPTS_EXCEEDED. A small cushion fixes that without weakening much.
      allowedAttempts: 5,
      sendMagicLink: async ({ email, url }) => {
        try {
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
        } catch (err) {
          // eslint-disable-next-line no-console
          console.error("[auth] magic link email failed:", err);
          throw err;
        }
      },
      expiresIn: 60 * 10
    }),
  ],
});

export const authHandler = toNodeHandler(auth);

