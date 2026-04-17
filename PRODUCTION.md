# Production migration plan — `signal-and-story.k-dawg.uk`

Ordered steps for **shared hosting**: **cPanel**, **SSH**, **Setup Node.js App** (or **Application Manager**), **Node.js**, **PostgreSQL**, **no Docker**.

**Builds** (`npm run build`, `npx auth migrate`, `go build`) run on **your PC** (or CI). On the server you typically only run **`npm install`** / **Ensure dependencies** through cPanel—not arbitrary shell `npm` commands—unless your host allows SSH `npm`.

---

## 0. What runs where (decide URLs first)

| Piece | Runtime | Typical public URL | How it is usually hosted here |
|--------|---------|-------------------|--------------------------------|
| **Storefront** | Astro (hybrid) + **Node** adapter | `https://signal-and-story.k-dawg.uk` | **Setup Node.js App** (Passenger behind the scenes on many hosts) |
| **Auth** | **Node** (Better Auth / Express) | e.g. `https://auth.signal-and-story.k-dawg.uk` | Second **Setup Node.js App** entry |
| **API** | **Go** (compiled binary) | e.g. `https://api.signal-and-story.k-dawg.uk` | **Not** a standard Node app — see §10 |
| **PostgreSQL** | — | On-server (often `localhost:5432`) | cPanel PostgreSQL |

**Subdomains:** Auth and the API each need a **stable HTTPS origin** (`AUTH_BASE_URL`, `PUBLIC_AUTH_BASE`, `PUBLIC_API_BASE`). Separate subdomains are the straightforward choice.

**cPanel variants:**

- **Setup Node.js App** — Usually exposes **Node version**, **environment variables**, **application root**, **startup file**, and **Run NPM Install**. Prefer this when available.
- **Application Manager** — Some installs only let you **register** an app (name, domain, path, enable, ensure dependencies) with **no env UI**. Then use a **`.env` file in the application root** (same folder as `package.json`) for secrets and URLs; see §6.

**Go API:** Deployed as a **static `CGO_ENABLED=0` binary** plus your host’s method for a **public URL** and **long-lived process** — not the Node app wizard.

---

## 1. Pre-flight: confirm Go on the server (optional but recommended)

1. SSH: `uname -m` → **`GOARCH=amd64`** for `x86_64`, **`arm64`** for `aarch64`.
2. Build a tiny smoke binary on your PC with **`CGO_ENABLED=0`** so **old glibc** on the host does not cause `GLIBC_2.xx not found`:

   ```bash
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o go-smoke .
   ```

3. Upload, `chmod +x go-smoke`, run and `curl` locally on the server.

4. Real API (from **`apps/api`**, where **`go.mod`** lives):

   ```bash
   cd apps/api
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o api ./cmd/api
   ```

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
2. **Webhook** — Public URL on the **Go API**, e.g. `https://api.signal-and-story.k-dawg.uk/webhooks/stripe`, event **`checkout.session.completed`**. Set **`STRIPE_WEBHOOK_SECRET`** on the API host.
3. **`STRIPE_SUCCESS_URL` / `STRIPE_CANCEL_URL`** on the API — HTTPS storefront URLs, e.g. `https://signal-and-story.k-dawg.uk/checkout/success` and `…/checkout`.

---

## 5. Email (overview)

- **Auth (magic links):** Brevo and/or SMTP — exact variable names in **§6.1**.
- **API (order confirmation):** same Brevo/SMTP pattern on the **Go** process — **§6.2**.
- Do not use Mailpit-style localhost SMTP in production.

---

## 6. Environment variables — what goes where

Never commit secrets. In **cPanel’s environment variable fields**, enter **raw values** (no wrapping `'` / `"` unless those characters are literally part of the secret). That UI is **not** bash; **`!`** in passwords is fine without quotes.

### 6.1 Auth Node app (`apps/auth` — Setup Node.js App)

**Required:**

| Variable | Example / note |
|----------|----------------|
| **`DATABASE_URL`** | Same database as the API. |
| **`BETTER_AUTH_SECRET`** | Strong random secret (not dev). |
| **`AUTH_BASE_URL`** | Must include **`/api/auth`**, e.g. `https://auth.signal-and-story.k-dawg.uk/api/auth`. |

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

### 6.2 Go API (`apps/api`)

| Variable | Note |
|----------|------|
| **`DATABASE_URL`** | Same as auth. |
| **`API_ADDR`** | Listen address, e.g. `:8788` or host-required value. |
| **`AUTH_BASE_URL`** | Same value as auth’s public base (for session verification), e.g. `https://auth.signal-and-story.k-dawg.uk/api/auth`. |
| **`APP_BASE_URL`** | Storefront URL (CORS), e.g. `https://signal-and-story.k-dawg.uk`. |
| **`APP_ORIGIN_ALLOWLIST`** | Optional. |
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

SQL files: `apps/api/migrations/*.sql` in numeric order.

```bash
psql -v ON_ERROR_STOP=1 "$DATABASE_URL" -f apps/api/migrations/001_init.sql
```

Repeat for each file. Use **single quotes** around `DATABASE_URL` in **bash** if the password contains **`!`**.

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

### Node version

Match **major** Node to cPanel (e.g. **22.20.0** on server → **22.x** locally). Avoid building on **Node 24** while the server runs **20/22**. Use **nvm** / **fnm** if needed.

### Where to upload (auth and storefront)

Upload into the **application root** you set in cPanel: that directory should contain **`package.json`**, **`package-lock.json`**, **`dist/`**, and optionally **`.env`**. The **startup file** is **relative to that root** (e.g. **`dist/server.js`** for auth).

`load-env` in auth reads **`.env` next to `package.json`** (flat deploy) — suitable if the panel has no env UI.

### 9.1 Auth

1. On your PC: `cd apps/auth && npm install && npm run build`.
2. Upload **`dist/`**, **`package.json`**, **`package-lock.json`** to the application root.
3. **Setup Node.js App:** set **Application URL** (e.g. `auth.signal-and-story.k-dawg.uk`), **Application startup file** `dist/server.js`, **environment variables** per **§6.1**.
4. Run **Run NPM Install** / ensure dependencies, then **Restart** the app.

**Smoke test:** `GET https://auth.signal-and-story.k-dawg.uk/healthz` should return **`ok`**.

**If you see a directory index** at the site root: the vhost is still serving **static files**; the Node app is not wired or not running. Fix **application root**, **startup file**, **Run NPM Install**, and **Restart** per host docs—not `/healthz` until that is resolved.

### 9.2 Storefront (Astro + `@astrojs/node`)

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

## 10. Go API — full deployment flow

The API is a **single Go binary** (`cmd/api`) that speaks HTTP, talks to **PostgreSQL**, calls **Stripe**, verifies sessions against **Better Auth**, and sends **order email** (Brevo or SMTP). cPanel’s **Setup Node.js App** does **not** run this; you **compile on your PC**, upload the binary, run it under SSH (or your host’s process manager), and put a **reverse proxy** (or equivalent) in front so **`https://api.…`** reaches it.

### Shared hosting alternative: Node API (`apps/api-node`)

To avoid a Go binary and reverse proxy on shared hosting, deploy **`apps/api-node`** with **Setup Node.js App** (same pattern as **`apps/auth`**): install dependencies, run **`npm run build`**, set the application **startup file** to **`dist/server.js`**, and configure **the same environment variables as §10.4** (see also **§6.2**). Point Stripe’s webhook URL and the storefront **`PUBLIC_API_BASE`** at this app’s public HTTPS URL. The Go steps below remain the right path for a VPS or any host where you can run a static Linux binary behind your own proxy.

### 10.1 Prerequisites (before this step)

1. **§7** — API SQL migrations applied to production Postgres.
2. **§8** — Better Auth tables exist (same `DATABASE_URL` user can reach from wherever the API runs).
3. **§4** — You know the public API base URL (e.g. `https://api.signal-and-story.k-dawg.uk`) for Stripe **webhook** configuration.
4. **Storefront** — `PUBLIC_API_BASE` in your **built** storefront (§6.3 / §9.2) must match that same HTTPS API URL.

### 10.2 Build the binary (on your PC)

From the repo, **`apps/api`** must contain **`go.mod`** (do not build from a folder without the module).

1. On the server (SSH): `uname -m` → **`amd64`** for `x86_64`, **`arm64`** for `aarch64`.

2. On your PC:

   ```bash
   cd apps/api
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o api ./cmd/api
   ```

   Use **`GOARCH=arm64`** on ARM hosts. **`CGO_ENABLED=0`** avoids **glibc** version errors on older shared-host Linux.

3. You get one file: **`api`** (Linux executable). No separate `node_modules`; you do **not** need to upload Go source for production.

### 10.3 Upload

Upload **`api`** to a directory on the server (e.g. `~/bin/signal-api/` or a path your host recommends). On SSH:

```bash
chmod +x ./api
```

Keep the binary **outside** world-readable web roots if possible.

### 10.4 Environment variables

Set these in the **shell** that starts the process, a **wrapper script**, **`systemd` `Environment=`**, or whatever your host documents — cPanel rarely has a GUI for Go.

**Required for a working site:**

| Variable | Purpose |
|----------|---------|
| **`DATABASE_URL`** | Postgres connection (same DB as auth; from server’s perspective often `localhost` / `127.0.0.1`). |
| **`AUTH_BASE_URL`** | Better Auth public base **including** `/api/auth`, e.g. `https://auth.signal-and-story.k-dawg.uk/api/auth` — used to verify sessions with the auth service. |
| **`APP_BASE_URL`** | Storefront origin for **CORS**, e.g. `https://signal-and-story.k-dawg.uk`. |
| **`ADMIN_EMAIL`** | Email allowed to use admin API routes. |
| **`STRIPE_SECRET_KEY`** | Stripe secret (`sk_live_…` / `sk_test_…`). |
| **`STRIPE_WEBHOOK_SECRET`** | Signing secret for **`checkout.session.completed`** webhook. |
| **`STRIPE_SUCCESS_URL`** / **`STRIPE_CANCEL_URL`** | HTTPS storefront checkout success / cancel URLs. |

**Recommended:**

| Variable | Purpose |
|----------|---------|
| **`API_ADDR`** | Listen address. Default **`:8788`**. If a reverse proxy on the same machine forwards to a fixed port, set e.g. **`127.0.0.1:8788`**. |
| **`APP_ENV`** | Set **`production`** once you are live (some debug-only behaviour is gated on this). |
| **`APP_ORIGIN_ALLOWLIST`** | Optional comma-separated extra CORS origins (`www`, previews). |

**Order confirmation email** (same idea as auth — Brevo if key set, else SMTP):

- **`BREVO_API_KEY`**, **`BREVO_SENDER_EMAIL`**, **`BREVO_SENDER_NAME`**, or  
- **`SMTP_HOST`**, **`SMTP_PORT`**, **`SMTP_USER`**, **`SMTP_PASS`**, **`SMTP_FROM`**

Optional: **`SAS_BUILD_ID`** (shown on `GET /api/_meta/build` for sanity checks).

Full cross-reference: **§6.2**.

### 10.5 Listen address and reverse proxy

- The process listens on **`API_ADDR`** (default **all interfaces `:8788`**). For shared hosting, binding **`127.0.0.1:8788`** is often safer so only the local web server can reach the API.
- **HTTPS** is usually terminated by **Apache/Nginx** in front of you. Configure the host so:

  - **`https://api.signal-and-story.k-dawg.uk`** proxies to **`http://127.0.0.1:8788`** (or whatever port you chose).

- **Stripe webhook URL** in the Dashboard must be exactly:

  **`https://<your-api-host>/webhooks/stripe`**

  This route is mounted at the **root** of the API (not under `/api`). Example:  
  `https://api.signal-and-story.k-dawg.uk/webhooks/stripe`

### 10.6 Run the process

**Smoke test (SSH):**

```bash
export DATABASE_URL='…'
export AUTH_BASE_URL='https://auth.signal-and-story.k-dawg.uk/api/auth'
export APP_BASE_URL='https://signal-and-story.k-dawg.uk'
# … all other vars from §10.4 …
./api
```

In another session (or browser):

- **`GET https://api.signal-and-story.k-dawg.uk/healthz`** → **`ok`**
- **`GET https://api.signal-and-story.k-dawg.uk/api/_meta/build`** → JSON (email backend hints; no secrets)

For **production**, avoid tying the API to an interactive SSH session. Use whatever your provider supports: **`nohup`**, **`screen`**, **`tmux`**, **`systemd` user service**, or their documented **daemon** / **application** wrapper. If the host **cannot** keep a long-lived Go process or **cannot** proxy to it, run the API on **another** machine (VPS/PaaS) and point **`PUBLIC_API_BASE`** and Stripe there — only if Postgres and policy allow remote access.

### 10.7 After deploy

1. Confirm **CORS**: storefront origin matches **`APP_BASE_URL`** / allowlist (browser checkout calls the API).
2. **Stripe** — send a test event or complete a test checkout; Dashboard → Webhooks should show **200** for `checkout.session.completed`.
3. **Order email** — confirm a message arrives after a test order.

---

## 11. End-to-end order (checklist)

1. DNS + SSL for storefront, auth, API.
2. PostgreSQL + **`DATABASE_URL`** (correct host from app’s perspective).
3. **API SQL migrations** (§7) and **Better Auth migrate** (§8).
4. Stripe test webhook + URLs (§4).
5. Deploy **auth** (§9.1); **`/healthz`** OK.
6. Deploy **storefront** (§9.2).
7. Deploy **Go `api`** (§10); public **`/healthz`** OK.
8. Smoke-test checkout, webhooks, mail, magic link, admin (§12).

---

## 12. Post-deploy verification (in this order)

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
| Node | `npm run dev` | **Local `npm run build`**; cPanel **`npm install`** + **Setup Node.js App** |
| Go | `go run` | **`CGO_ENABLED=0` binary** |
| Env UI | `.env` | cPanel fields **or** app-root **`.env`** |

---

## 14. Rollback

1. Backup Postgres before first migration.
2. Keep previous builds and Stripe test endpoints until go-live is proven.
3. Record env and DNS for quick revert.

---

*Host UIs differ. Use **Setup Node.js App** when it provides Node version and env vars; use **`.env` in the application root** when the panel does not. Rebuild the storefront if `PUBLIC_API_BASE` or `PUBLIC_AUTH_BASE` change.*
