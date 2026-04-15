# Signal & Story

Premium musician gear for sci‑fi & fantasy worlds.

## Dev setup

Prereqs:
- Docker + Docker Compose
- Go (for `apps/api`)
- Node.js (for `apps/storefront` and `apps/auth`)

### 1) Infra (Postgres + Mailpit)

```bash
docker compose -f infra/docker-compose.yml up -d
```

Or run everything with the dev runner:

```bash
./scripts/dev.sh
```

Local services:
- **Postgres**: `localhost:5433` (db: `signal_and_story`, user: `signal`, pass: `story`)
- **Mailpit (SMTP)**: `localhost:1026`
- **Mailpit UI**: `http://localhost:8026`
- **Adminer**: `http://localhost:8081`

### 2) Database migrations + seed

```bash
./scripts/db-reset-and-seed.sh
```

### 3) Environment variables

Copy `.env.example` to `.env` at the repo root and fill values as needed:

- **Auth**: `BETTER_AUTH_SECRET`, `AUTH_BASE_URL`
- **Stripe**: `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, `STRIPE_SUCCESS_URL`, `STRIPE_CANCEL_URL`
- **Email**: for local dev, SMTP uses Mailpit; for prod, set `BREVO_API_KEY` and sender fields.

### 4) Auth service (Better Auth)

```bash
cd apps/auth
npm install
npm run dev
```

Auth runs on `http://localhost:8787` and exposes:
- Better Auth routes: `/api/auth/*`
- Session bridge for Go: `/internal/session`

> Note: Better Auth DB tables are created via the Better Auth CLI. If you need to run migrations:
>
> `cd apps/auth && npx auth migrate --config ./auth.ts`

### 5) API (Go)

```bash
cd apps/api
go run ./cmd/api
```

API runs on `http://localhost:8788`.
### 6) Storefront (Astro)

```bash
cd apps/storefront
npm install
npm run dev
```

Storefront runs on `http://localhost:4321`.

## Stripe setup notes (Checkout Session)

Stripe Checkout Sessions are created from the server using DB-priced line items (to prevent client-side price tampering).

- Set `STRIPE_SUCCESS_URL` to `http://localhost:4321/checkout/success` (the API appends `checkout_session_id` for display).
- Set `STRIPE_CANCEL_URL` to `http://localhost:4321/checkout`.
- Prices are treated as **VAT-inclusive**, and Stripe Checkout uses **automatic tax**.

### Shipping options

The storefront offers:
- `standard` (free)
- `express` (£2.99)
- `next-day` (£5.99)

## Webhooks

Point your Stripe webhook endpoint to:

- `POST http://<public-url>/webhooks/stripe`

Configure the webhook to send at least:
- `checkout.session.completed`

Set `STRIPE_WEBHOOK_SECRET` to the signing secret from the Stripe Dashboard.

