# Development guide

How to run **Signal & Story** locally for development and integration testing (auth, API, storefront, Stripe, email).

## Prerequisites

- **Docker** and **Docker Compose** (Postgres, Mailpit, Adminer)
- **Node.js** (LTS recommended) for `apps/auth` and `apps/storefront`
- **Go** (for `apps/api`) **or Node** (for `apps/api-node`) — for the live API. Without either, the storefront can run in offline catalog mode. On shared hosting you typically deploy **`apps/api-node`** (see **`PLAN_NODE_API_SHARED_HOSTING.md`**).
- **curl**, **ss** (used by `scripts/dev.sh` for health checks and optional port cleanup)

## Quick start (recommended)

From the repository root (`signal-and-story/`):

```bash
./scripts/dev.sh
```

This script:

1. Creates `.env` from `.env.example` if missing
2. Starts infra via `infra/docker-compose.yml`
3. Runs database migrations (`scripts/db-reset-and-seed.sh`)
4. Optionally seeds sample products if `SAS_SEED_PRODUCTS=1` is set (in `.env` or the environment)
5. Runs Better Auth migrations (non-interactive confirm)
6. Starts **auth** (default port **8787**), **API** (**8788** — Go by default, or set **`SAS_API=node`** for **`apps/api-node`**), and **storefront** (**4321**)

Press **Ctrl+C** to stop the Node/Go processes (Docker containers keep running unless you stop them separately).

### Ports and URLs

| Service    | URL / port |
|------------|------------|
| Storefront | http://localhost:4321 |
| Auth       | http://localhost:8787 — Better Auth at `/api/auth/*`, session bridge at `/internal/session` |
| API        | http://localhost:8788 |
| Postgres   | `localhost:5433` — DB `signal_and_story`, user `signal`, password `story` |
| Mailpit SMTP | `localhost:1026` |
| Mailpit UI   | http://localhost:8026 |
| Adminer      | http://localhost:8081 |

### Environment variables

Copy `.env.example` to `.env` at the repo root and adjust:

- **Shared / URLs**: `APP_BASE_URL`, optional `APP_ORIGIN_ALLOWLIST` (comma-separated extra allowed origins, e.g. LAN or preview URLs)
- **Database**: `DATABASE_URL` (must match Docker Postgres)
- **Better Auth**: `BETTER_AUTH_SECRET`, `AUTH_BASE_URL` (typically `http://localhost:8787/api/auth`)
- **Stripe**: `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, `STRIPE_SUCCESS_URL`, `STRIPE_CANCEL_URL`
- **Email (dev)**: Mailpit via `SMTP_HOST`, `SMTP_PORT`, `SMTP_FROM`, etc.
- **Email (Brevo, optional)**: `BREVO_API_KEY`, `BREVO_SENDER_EMAIL`, `BREVO_SENDER_NAME` — when set, auth and order flows can use Brevo instead of SMTP
- **Admin**: `ADMIN_EMAIL` (API authorization), `PUBLIC_ADMIN_EMAIL` (storefront shows Admin link / magic-link behavior when the signed-in email matches)
- **Seed**: `SAS_SEED_PRODUCTS=1` to load sample products on migrate (does not wipe admin collection assignments)

### Database only (migrations + optional seed)

```bash
./scripts/db-reset-and-seed.sh
```

By default this **does not** seed products. To seed:

```bash
SAS_SEED_PRODUCTS=1 ./scripts/db-reset-and-seed.sh
```

To bring up infra without the dev runner:

```bash
docker compose -f infra/docker-compose.yml up -d
```

### Manual service startup (alternative to `dev.sh`)

1. **Auth**

   ```bash
   cd apps/auth
   npm install
   npm run dev
   ```

2. **API** (Go)

   ```bash
   cd apps/api
   go run ./cmd/api
   ```

   **API** (Node — same HTTP surface as Go, for cPanel / no Go):

   ```bash
   cd apps/api-node
   npm install
   npm run dev
   ```

   Or from repo root: **`SAS_API=node ./scripts/dev.sh`**

3. **Storefront**

   ```bash
   cd apps/storefront
   npm install
   npm run dev
   ```

Better Auth schema: if you need to run migrations by hand:

```bash
cd apps/auth && npx auth migrate --config ./src/auth.ts
```

## Dev script options

- **`SAS_SEED_PRODUCTS=1`**: seed sample catalog when migrations run (via `dev.sh` or `db-reset-and-seed.sh`).
- **`SAS_FREE_PORTS=1`** (default): try to free processes listening on the auth/API ports before start. Set to `0` to disable.
- **`AUTH_PORT`**, **`API_ADDR`**: override listen ports if needed.
- **`PUBLIC_API_BASE`**, **`PUBLIC_AUTH_BASE`**: URLs the storefront uses to reach the API and auth services.

Logs from auth/API when using `dev.sh`: `tmp/auth.log`, `tmp/api.log`.

## Stripe (test mode)

Checkout uses **Stripe Checkout Sessions** with server-side line items from the database (prices are not trusted from the client). VAT is treated as **inclusive**; Checkout uses **automatic tax**.

- Set `STRIPE_SUCCESS_URL` to `http://localhost:4321/checkout/success` (the API may append `checkout_session_id`).
- Set `STRIPE_CANCEL_URL` to `http://localhost:4321/checkout`.
- Configure a webhook endpoint that your running API can receive, e.g.:

  `POST http://<public-url>/webhooks/stripe`

  Subscribe at least to **`checkout.session.completed`**. Set `STRIPE_WEBHOOK_SECRET` to the signing secret from the Stripe Dashboard (or from `stripe listen` when using the Stripe CLI).

For local webhook forwarding, use the [Stripe CLI](https://stripe.com/docs/stripe-cli) `stripe listen --forward-to localhost:8788/webhooks/stripe` and paste the CLI webhook secret into `STRIPE_WEBHOOK_SECRET`.

## Email: Mailpit vs Brevo

- **Default dev**: transactional email goes to **Mailpit** over SMTP (`SMTP_*` in `.env`). Open http://localhost:8026 to read messages.
- **Brevo**: set `BREVO_API_KEY` and sender fields. When the API key is set, relevant paths can send via Brevo; otherwise SMTP/Mailpit is used.

## Admin UI

- Storefront **Admin** appears on `/account` when the signed-in user’s email matches `PUBLIC_ADMIN_EMAIL`.
- Admin routes live under `http://localhost:4321/admin`.
- The Go API checks `ADMIN_EMAIL` against the session email for admin endpoints.

## CORS and origins

The API enables CORS for the storefront origin(s) so browser calls from the Astro app (e.g. checkout) succeed. If you use a non-default host (e.g. `127.0.0.1` vs `localhost`), keep **`APP_BASE_URL`** and how you open the site consistent so session cookies and redirects align.

## Troubleshooting

- **Auth “invalid origin” or session missing**: ensure `APP_BASE_URL` / `trustedOrigins` match the URL you use in the browser; avoid mixing `localhost` and `127.0.0.1` for the same session.
- **Magic link lands on wrong page**: magic links should use an absolute `callbackURL` pointing at the storefront; see auth app configuration.
- **Checkout “failed to fetch”**: confirm the API is running and CORS allows your storefront origin.
- **Search returns no results**: the search page must not be fully prerendered without query params — use live data for `?q=`.
- **Port already in use**: run with `SAS_FREE_PORTS=1` (default) or stop the conflicting process; check `tmp/auth.log` / `tmp/api.log`.
- **API not starting**: install Go and ensure `DATABASE_URL` matches a healthy Postgres from Docker.

## Production testing

Production-like checks (real domains, HTTPS, live Stripe webhooks, Brevo) should use environment-specific `.env` values and the same variable names as in `.env.example`. Do not commit secrets; configure them in your host or secret store.
