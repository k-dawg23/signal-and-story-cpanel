import "dotenv/config";
import express from "express";
import cors from "cors";
import { auth, authHandler } from "./auth.js";

const app = express();

const allowedOrigin = process.env.APP_BASE_URL ?? "http://localhost:4321";

app.use(
  cors({
    origin: allowedOrigin,
    credentials: true,
  })
);

// Important: mount Better Auth before any body parsing middleware.
app.all("/api/auth/*", authHandler);

app.get("/healthz", (_req, res) => res.status(200).send("ok"));

// Server-to-server session verification for Go API.
app.get("/internal/session", async (req, res) => {
  try {
    const session = await auth.api.getSession({ headers: req.headers });
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

