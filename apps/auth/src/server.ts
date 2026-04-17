import "./load-env.js";
import type { IncomingHttpHeaders } from "node:http";
import express from "express";
import cors from "cors";
import { auth, authHandler } from "./auth.js";

/** Better Auth expects Fetch `Headers`; Express uses `IncomingHttpHeaders`. */
function toWebHeaders(headers: IncomingHttpHeaders): Headers {
  const h = new Headers();
  for (const [key, raw] of Object.entries(headers)) {
    if (raw === undefined) continue;
    const values = Array.isArray(raw) ? raw : [raw];
    for (const v of values) {
      if (v !== undefined && v !== "") h.append(key, v);
    }
  }
  return h;
}

const app = express();

function isProductionEnv(): boolean {
  return (
    process.env.APP_ENV === "production" ||
    process.env.NODE_ENV === "production"
  );
}

const base = process.env.APP_BASE_URL ?? "http://localhost:4321";
const allowlist = new Set<string>(
  [base, "http://localhost:4321", "http://127.0.0.1:4321"]
    .concat(
      (process.env.APP_ORIGIN_ALLOWLIST ?? "")
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
    )
    .filter(Boolean)
);

app.use(
  cors({
    origin: (origin, callback) => {
      if (!origin) return callback(null, true);
      if (allowlist.has(origin)) return callback(null, true);
      // Match Better Auth: non-production accepts the browser Origin (e.g. Astro Network URL).
      if (!isProductionEnv()) return callback(null, true);
      callback(null, false);
    },
    credentials: true,
  })
);

// Important: mount Better Auth before any body parsing middleware.
app.all("/api/auth/*", authHandler);

app.get("/healthz", (_req, res) => res.status(200).send("ok"));

// Debug helper in dev to verify cookie + session.
app.get("/debug/session", async (req, res) => {
  try {
    const session = await auth.api.getSession({ headers: toWebHeaders(req.headers) });
    return res.status(200).json({
      hasCookieHeader: Boolean(req.headers.cookie),
      cookieHeader: req.headers.cookie ?? null,
      session: session ?? null,
    });
  } catch (err) {
    return res.status(500).json({ error: "debug_session_failed" });
  }
});

// Server-to-server session verification for Go API.
app.get("/internal/session", async (req, res) => {
  try {
    const session = await auth.api.getSession({ headers: toWebHeaders(req.headers) });
    if (!session) return res.status(200).json({ session: null });
    return res.status(200).json({ session });
  } catch (err) {
    return res.status(500).json({ error: "session_lookup_failed" });
  }
});

const port = Number(process.env.AUTH_PORT ?? "8787");
app.listen(port, () => {
  // eslint-disable-next-line no-console
  console.log(`auth listening on http://localhost:${port}`);
});

