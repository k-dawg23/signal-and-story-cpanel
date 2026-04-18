# Production migration plan — `signal-and-story.k-dawg.uk`

Ordered steps for **shared hosting**: **cPanel**, **SSH**, **Setup Node.js App** (or **Application Manager**), **Node.js** (auth, **API**, storefront), **PostgreSQL**, **no Docker**.

This repository is the **Node API** line (`apps/api-node` — Fastify). The **Go API** reference implementation lives in **[signal-and-story](https://github.com/k-dawg23/signal-and-story)**; this stack matches the same HTTP contract and environment variable names but deploys like **`apps/auth`** (no Go binary, no separate reverse-proxy process for the API).

**Builds** (`npm run build` for auth, API, and storefront; `npx auth migrate`) run on **your PC** (or CI). On the server you typically only run **`npm install`** / **Ensure dependencies** through cPanel—not arbitrary shell `npm` commands—unless your host allows SSH `npm`.

---

## 0. What runs where (decide URLs first)

| Piece | Runtime | Typical public URL | How it is usually hosted here |
|--------|---------|-------------------|--------------------------------|
| **Storefront** | Astro (hybrid) + **Node** adapter | `https://signal-and-story.k-dawg.uk` | **Setup Node.js App** (Passenger behind the scenes on many hosts) |
| **Auth** | **Node** (Better Auth / Express) | e.g. `https://auth.signal-and-story.k-dawg.uk` | Second **Setup Node.js App** entry |
| **API** | **Node** (Fastify in `apps/api-node`) | e.g. `https://api.signal-and-story.k-dawg.uk` | **Setup Node.js App** — see §9.2 |
| **PostgreSQL** | — | On-server (often `localhost:5432`) | cPanel PostgreSQL |

**Subdomains:** Auth and the API each need a **stable HTTPS origin** (`AUTH_BASE_URL`, `PUBLIC_AUTH_BASE`, `PUBLIC_API_BASE`). Separate subdomains are the straightforward choice.

**cPanel variants:**

- **Setup Node.js App** — Usually exposes **Node version**, **environment variables**, **application root**, **startup file**, and **Run NPM Install**. Prefer this when available.
- **Application Manager** — Some installs only let you **register** an app (name, domain, path, enable, ensure dependencies) with **no env UI**. Then use a **`.env` file in the application root** (same folder as `package.json`) for secrets and URLs; see §6.

**API:** Use the same **Setup Node.js App** workflow as auth (§9.2): **application root** = folder with **`package.json`**, **startup file** = **`dist/server.js`** after **`npm run build`** on your PC.

---

## 1. Node version (auth, API, storefront)

Match **major** Node on your PC to cPanel (e.g. **22.20.0** on server → **22.x** locally). Avoid building on **Node 24** while the server runs **20/22**. Use **nvm** / **fnm** if needed. Apply this to **`apps/auth`**, **`apps/api-node`**, and **`apps/storefront`** builds.

---

## 2. DNS and SSL

1. Create **`signal-and-story`**, **`auth`**, **`api`** (if used) under **cPanel → Domains** / DNS.
2. Enable **AutoSSL** / Let’s Encrypt for each hostname.
3. Production **Stripe** and cookies expect **HTTPS**.

---

## 3. PostgreSQL (cPanel)

1. **cPanel → PostgreSQL Databases:** create database and user; attach user to database; save password.
2. Build **`DATABASE_URL`** per host docs (`host`, **port** — often `5432` when phpPgAdmin shows `:5432`, **`sslmode`**).
3. **From your laptop:** `localhost` in `DATABASE_URL` points at **your PC**, not the hosting server. Use an **SSH tunnel** (§8) or a **remote** DB hostname if the host allows it.
4. If the API runs **off-server**, confirm **remote PostgreSQL** and firewall rules.

---

## 4. Stripe (before you rely on checkout)

Use **Test mode** first; repeat for **Live** when stable.

1. **API keys** — [Stripe Dashboard → API keys](https://dashboard.stripe.com/apikeys).
2. **Webhook** — Public URL on the **Node API**, e.g. `https://api.signal-and-story.k-dawg.uk/webhooks/stripe`, event **`checkout.session.completed`**. Set **`STRIPE_WEBHOOK_SECRET`** on the API app’s environment (§6.2 / §9.2).
3. **`STRIPE_SUCCESS_URL` / `STRIPE_CANCEL_URL`** on the API — HTTPS storefront URLs **without** extra query params, e.g. `https://signal-and-story.k-dawg.uk/checkout/success` and `…/checkout`. The API adds `session_id={CHECKOUT_SESSION_ID}` to the success URL so the storefront can load order details from Stripe after payment.

---

## 5. Email (overview)

- **Auth (magic links):** Brevo and/or SMTP — exact variable names in **§6.1**.
- **API (order confirmation):** same Brevo/SMTP pattern on the **Node API** — **§6.2**.
- Do not use Mailpit-style localhost SMTP in production.

---

## 6. Environment variables — what goes where

Never commit secrets. In **cPanel’s environment variable fields**, enter **raw values** (no wrapping `'` / `"` unless those characters are literally part of the secret). That UI is **not** bash; **`!`** in passwords is fine without quotes. **URLs must be exact:** a stray **space** in a hostname (e.g. `k dawg` instead of `k-dawg`) breaks `APP_BASE_URL` and causes **CORS** failures on checkout. The API also allows the origin derived from **`STRIPE_SUCCESS_URL`** and **`STRIPE_CANCEL_URL`** as a safeguard if **`APP_BASE_URL`** is wrong.

### 6.1 Auth Node app (`apps/auth` — Setup Node.js App)

**Required:**

| Variable | Example / note |
|----------|----------------|
| **`DATABASE_URL`** | Same database as the API. |
| **`BETTER_AUTH_SECRET`** | Strong random secret (not dev). |
| **`AUTH_BASE_URL`** | Must include **`/api/auth`**, e.g. `https://auth.signal-and-story.k-dawg.uk/api/auth`. |
| **`AUTH_COOKIE_DOMAIN`** | **Required** when storefront, auth, and API use different hosts under the same site (e.g. `signal-and-story.k-dawg.uk` **without** `https://`). Sets Better Auth **cross-subdomain** cookies so the API receives the session cookie on `fetch('/api/account/orders')`. After enabling, users should **sign in again** once. |

**URLs / CORS:**

| Variable | Example / note |
|----------|----------------|
| **`APP_BASE_URL`** | Storefront origin, e.g. `https://signal-and-story.k-dawg.uk`. |
| **`APP_ENV`** | Set **`production`** so trusted origins match production behaviour. |
| **`APP_ORIGIN_ALLOWLIST`** | Optional; comma-separated extra allowed origins. |

**Mail (magic links) — Brevo** (when `BREVO_API_KEY` is non-empty):

| Variable | Note |
|----------|------|
| **`BREVO_API_KEY`** | Required for Brevo path. |
| **`BREVO_SENDER_EMAIL`** | Verified sender in Brevo. |
| **`BREVO_SENDER_NAME`** | Optional display name. |
| **`SMTP_FROM`** | Still **required** by this codebase (used as fallback / parsing); use `"Name <email>"` or align with Brevo sender. |

**Mail — SMTP only** (leave `BREVO_API_KEY` empty):

| Variable | Note |
|----------|------|
| **`SMTP_HOST`**, **`SMTP_PORT`**, **`SMTP_FROM`** | **Required.** |
| **`SMTP_USER`**, **`SMTP_PASS`** | If your SMTP needs auth. |

**Listen port:**

| Variable | Note |
|----------|------|
| **`AUTH_PORT`** | Code defaults to **`8787`**. cPanel may inject **`PORT`** instead; if the app does not start or the proxy fails, align with host documentation (some maps require using their `PORT`). |

**Not used by the auth app:** `STRIPE_*`, `ADMIN_EMAIL`, `PUBLIC_ADMIN_EMAIL`, `SAS_SEED_PRODUCTS`.

### 6.2 Node API (`apps/api-node`)

| Variable | Note |
|----------|------|
| **`DATABASE_URL`** | Same as auth. |
| **`API_ADDR`** | Listen address, e.g. `:8788`. If unset, **`PORT`** from the host (e.g. cPanel) is used — see §9.2. |
| **`AUTH_BASE_URL`** | Same value as auth’s public base (for session verification), e.g. `https://auth.signal-and-story.k-dawg.uk/api/auth`. |
| **`APP_BASE_URL`** | Storefront origin for **CORS** (scheme + host, no path). Use the same host users open in the browser (e.g. `https://signal-and-story.k-dawg.uk`). If you serve both **apex** and **`www`**, add the other host in **`APP_ORIGIN_ALLOWLIST`**. Trailing slashes are normalized, but **`www` vs non-`www` are different origins**. |
| **`APP_ORIGIN_ALLOWLIST`** | Optional comma-separated extra storefront origins allowed to call the API with cookies (e.g. `https://www.signal-and-story.k-dawg.uk`). |
| **`STRIPE_SECRET_KEY`**, **`STRIPE_WEBHOOK_SECRET`**, **`STRIPE_SUCCESS_URL`**, **`STRIPE_CANCEL_URL`** | Stripe. |
| **`ADMIN_EMAIL`** | Admin API authorization. |
| **`BREVO_*` or `SMTP_*`** | Order emails (same semantics as auth mail; API has its own `email` code path). |

### 6.3 Storefront — **build time** on your PC (`npm run build`)

Baked into the built Astro app; changing them requires a **rebuild**:

| Variable | Example |
|----------|---------|
| **`PUBLIC_API_BASE`** | `https://api.signal-and-story.k-dawg.uk` |
| **`PUBLIC_AUTH_BASE`** | `https://auth.signal-and-story.k-dawg.uk` |
| **`PUBLIC_ADMIN_EMAIL`** | Same as **`ADMIN_EMAIL`**. |

---

## 7. Database schema — API migrations (from your PC)

SQL files live under **`apps/api/migrations/*.sql`** (legacy path — this repo keeps **only** those `.sql` files; there is no Go module here). Apply in numeric order.

```bash
psql -v ON_ERROR_STOP=1 "$DATABASE_URL" -f apps/api/migrations/001_init.sql
```

Repeat for each file through **`007_checkout_confirmation_email_sent.sql`** (required for idempotent order-confirmation email on webhooks). Use **single quotes** around `DATABASE_URL` in **bash** if the password contains **`!`**.

**Extensions:** `001_init.sql` may try `CREATE EXTENSION "uuid-ossp"`; on shared hosting that often **fails** (no superuser). Current **`003_checkout_sessions.sql`** uses **`BIGSERIAL`**, so **`uuid-ossp` is not required** for checkout sessions.

**Where to run `psql`:** If the DB is only reachable as **`localhost` on the server**, run `psql` **over SSH** on the host, or upload migration files with **`scp`** and run there. See earlier chat: tunnel from laptop if the host allows.

Optional seed: `scripts/seed-products.sql`.

---

## 8. Database schema — Better Auth (`npx auth migrate`)

`auth.ts` loads config at import time. You must provide at least **`DATABASE_URL`**, **`BETTER_AUTH_SECRET`**, and **`AUTH_BASE_URL`** when running:

```bash
cd apps/auth
npx auth migrate --config ./src/auth.ts
```

**Recommended:** **SSH tunnel** so your laptop’s `DATABASE_URL` uses `127.0.0.1` and a local port forwarded to the server’s Postgres:

```bash
ssh -N -L 15432:127.0.0.1:5432 YOURUSER@YOUR_HOST
# elsewhere:
export DATABASE_URL='postgres://USER:PASS@127.0.0.1:15432/DBNAME?sslmode=disable'
export BETTER_AUTH_SECRET='…'
export AUTH_BASE_URL='https://auth.signal-and-story.k-dawg.uk/api/auth'
npx auth migrate --config ./src/auth.ts
```

Use **Node matching your server major** (e.g. 22.x) when possible (`nvm`, `fnm`, etc.).

If you **cannot** run `npx` locally, you must use a machine that can (tunnel still applies). Copying **`apps/auth`** to the server only helps if the host allows **`npm install`** / **`npx`** from SSH (many do **not**).

---

## 9. Build Node apps locally; deploy via Setup Node.js App

### Where to upload (auth, API, storefront)

For **auth** and **`apps/api-node`**, the compiled entry is **`dist/server.js`**. For the **storefront**, the entry is under **`dist/server/`** (typically **`entry.mjs`**) — see §9.3.

Upload into each **application root** in cPanel: **`package.json`**, **`package-lock.json`**, **`dist/`**, and optionally **`.env`**. The **startup file** is **relative to that root**.

`load-env` in auth reads **`.env` next to `package.json`** (flat deploy) — suitable if the panel has no env UI. The API loads repo-root **`.env`** when present (see `apps/api-node` config).

### 9.1 Auth

1. On your PC: `cd apps/auth && npm install && npm run build`.
2. Upload **`dist/`**, **`package.json`**, **`package-lock.json`** to the application root.
3. **Setup Node.js App:** set **Application URL** (e.g. `auth.signal-and-story.k-dawg.uk`), **Application startup file** `dist/server.js`, **environment variables** per **§6.1**.
4. Run **Run NPM Install** / ensure dependencies, then **Restart** the app.

**Smoke test:** `GET https://auth.signal-and-story.k-dawg.uk/healthz` should return **`ok`**.

**If you see a directory index** at the site root: the vhost is still serving **static files**; the Node app is not wired or not running. Fix **application root**, **startup file**, **Run NPM Install**, and **Restart** per host docs—not `/healthz` until that is resolved.

### 9.2 API (`apps/api-node`)

1. On your PC: `cd apps/api-node && npm install && npm run build`.
2. Upload **`dist/`**, **`package.json`**, **`package-lock.json`** to the API application root.
3. **Setup Node.js App:** set **Application URL** (e.g. `api.signal-and-story.k-dawg.uk`), **Application startup file** **`dist/server.js`**, **environment variables** per **§6.2**.
4. Run **Run NPM Install** / ensure dependencies, then **Restart**.

**Listen address:** default **`:8788`**. If **`API_ADDR`** is unset, the API uses **`PORT`** when the host sets it (typical for cPanel / Passenger). Otherwise set **`API_ADDR`** explicitly per your provider (e.g. **`0.0.0.0:8788`** or the port they assign).

**Stripe webhook** (Dashboard): **`https://<your-api-host>/webhooks/stripe`** — mounted at the **root** of the API app (not under `/api`). **`STRIPE_WEBHOOK_SECRET`** must match this endpoint’s signing secret.

**Smoke tests:** `GET https://api.signal-and-story.k-dawg.uk/healthz` → plain **`ok`**; `GET …/api/_meta/build` → JSON.

**Prerequisites:** **§7** (SQL migrations) and **§8** (Better Auth tables) before checkout can succeed.

### 9.3 Storefront (Astro + `@astrojs/node`)

This repo uses **`output: "hybrid"`** and **`@astrojs/node`** in **`mode: "standalone"`** (`apps/storefront/astro.config.mjs`). The **Node server entry** is emitted under **`dist/server/`** after a production build.

#### 1) Build on your PC (with §6.3 env)

Set **`PUBLIC_API_BASE`**, **`PUBLIC_AUTH_BASE`**, and **`PUBLIC_ADMIN_EMAIL`** in the same shell (or in `apps/storefront/.env` if you use one for builds), then:

```bash
cd apps/storefront
npm install
npm run build
```

#### 2) Confirm the startup file path locally

After a successful build, list the server bundle:

```bash
ls -la dist/server/
```

For **`standalone`** mode you should see an **`entry.mjs`** (sometimes alongside `manifest_*.mjs` and a **`chunks/`** directory). The path you give cPanel is **relative to the application root**:

| Field | Typical value |
|--------|----------------|
| **Application startup file** | **`dist/server/entry.mjs`** |

If your Astro or adapter version uses a different filename, use whatever appears in **`dist/server/`** as the main entry (the file cPanel/Passenger should execute with Node).

#### 3) Upload to the server

Upload the whole **`dist/`** tree (preserve **`dist/server/`** structure), plus **`package.json`** and **`package-lock.json`**, into the **application root** for the storefront (the folder cPanel treats as the Node app root — same level as where **`package.json`** lands).

Do **not** upload only `dist/server/entry.mjs`; the server needs **`chunks/`**, manifests, and the rest of **`dist/`** as produced by the build.

#### 4) Setup Node.js App (storefront)

1. **Application URL** — e.g. `signal-and-story.k-dawg.uk` (or `www` — match what you put in **`PUBLIC_*`** and DNS).
2. **Application root** — directory containing **`package.json`** and **`dist/`**.
3. **Application startup file** — **`dist/server/entry.mjs`** (or the file you confirmed in step 2).
4. **Environment variables** — Usually **no** `PUBLIC_*` here if they were set **at build time** (they are compiled into the client/server bundle). Set **`NODE_ENV=production`** if your host recommends it. Add anything else only if Astro or your host docs require runtime env (this project’s public config is build-time).
5. Run **Ensure dependencies** / **Run NPM Install**, then **Restart**.

#### 5) Smoke test

Open **`https://signal-and-story.k-dawg.uk/`** (or your chosen host). You should get the storefront, not a directory index. If the API is up, product data should load (not a permanent “offline” catalog).

Reference: [Astro — Deploy your Astro Site to a Node server](https://docs.astro.build/en/guides/integrations-guide/node/).

---

## 11. End-to-end order (checklist)

1. DNS + SSL for storefront, auth, API.
2. PostgreSQL + **`DATABASE_URL`** (correct host from app’s perspective).
3. **API SQL migrations** (§7) and **Better Auth migrate** (§8).
4. Stripe test webhook + URLs (§4).
5. Deploy **auth** (§9.1); **`/healthz`** OK.
6. Deploy **API** (§9.2); **`/healthz`** and **`/api/_meta/build`** OK.
7. Deploy **storefront** (§9.3); **`PUBLIC_API_BASE`** matches the live API URL.
8. Smoke-test checkout, webhooks, mail, magic link, admin (§12).

---

## 12. Post-deploy verification (in this order)

**Checkout shows “Failed to fetch” in the browser:** open DevTools → **Network**, click the failing **`session`** request. If it is **(blocked:cors)** or missing **`access-control-allow-origin`**, fix **`APP_BASE_URL`** / **`APP_ORIGIN_ALLOWLIST`** on the API (§6.2). If the request URL is **`http://localhost:8788`** or the wrong host, rebuild the storefront with correct **`PUBLIC_API_BASE`** (§6.3). If the URL is correct but **(failed)** or **SSL** errors, fix DNS/HTTPS on the API host.

**Two order confirmation emails for one purchase:** apply migration **`007_checkout_confirmation_email_sent.sql`** (§7), redeploy the API, and ensure only **one** Stripe webhook endpoint targets your live API. If duplicates continue, check **Brevo** for an automation or template that also sends on the same trigger.

1. **TLS** on all three hosts.
2. **`GET /healthz`** on auth and API.
3. Storefront loads data from API.
4. Stripe test checkout + webhook **200**.
5. Order email + magic link mail.
6. Admin session for **`ADMIN_EMAIL`**.
7. Live Stripe only when test mode is stable.

---

## 13. Local vs production (quick reference)

| Topic | Local | Production |
|--------|--------|------------|
| DB | Docker / local | cPanel PostgreSQL; tunnel or on-server `psql` |
| Email | Mailpit | Brevo / SMTP |
| URLs | `http://localhost` | **HTTPS** |
| Webhooks | Stripe CLI | `https://api…/webhooks/stripe` |
| Node (auth, API, storefront) | `npm run dev` | **Local `npm run build`**; cPanel **`npm install`** + **Setup Node.js App** |
| Env UI | `.env` | cPanel fields **or** app-root **`.env`** |

---

## 14. Rollback

1. Backup Postgres before first migration.
2. Keep previous builds and Stripe test endpoints until go-live is proven.
3. Record env and DNS for quick revert.

---

*Host UIs differ. Use **Setup Node.js App** when it provides Node version and env vars; use **`.env` in the application root** when the panel does not. Rebuild the storefront if `PUBLIC_API_BASE` or `PUBLIC_AUTH_BASE` change.*
