# Signal & Story (Node API)

Premium musician gear for sci‑fi and fantasy worlds — a full-stack demo store with accounts, checkout, and admin tools. This repo uses a **Fastify** API in **`apps/api-node`** for shared hosting; the **Go** reference API is **[signal-and-story](https://github.com/k-dawg23/signal-and-story)**.

## Stack

| Layer | Technology |
|--------|------------|
| **Storefront** | [Astro](https://astro.build/) — product catalog, cart, checkout, account, search, admin UI |
| **API** | [Fastify](https://fastify.dev/) (Node, `apps/api-node`) — same HTTP contract as the Go API |
| **Auth** | [Better Auth](https://www.better-auth.com/) (Node) — sessions, magic links; session bridge for the API |
| **Database** | PostgreSQL 16 (Docker) — products, collections, orders, line items |
| **Payments** | [Stripe](https://stripe.com/) Checkout — server-priced line items, automatic tax, VAT-inclusive pricing |
| **Email** | SMTP ([Mailpit](https://mailpit.axllent.org/) in dev) or [Brevo](https://www.brevo.com/) API for transactional mail |

Infra for local development: Docker Compose (Postgres, Mailpit, Adminer).

## Features

- **Catalog** — Products, collections, and collection membership; storefront browsing and product detail.
- **Search** — Server-backed search with live query handling.
- **Cart & checkout** — Cart persistence in the UI; Stripe Checkout redirect; success page with session reference.
- **Orders** — Order creation and updates via Stripe webhooks; order confirmation email with line items (Mailpit or Brevo), deduped per Checkout Session after migration **007**.
- **Shipping** — Checkout shipping options (e.g. standard / express / next-day) integrated with Stripe and order storage.
- **Accounts** — Sign-in via magic link; session shared with the API for protected actions.
- **Admin** — Admin area on the storefront for eligible users (`PUBLIC_ADMIN_EMAIL` / `ADMIN_EMAIL`): products, collections, orders; resend confirmation email where implemented.

## Documentation

- **Local setup, migrations, Stripe webhooks, email, and troubleshooting** → [DEVELOPMENT.md](./DEVELOPMENT.md)
- **Production (auth, API, storefront on cPanel)** → [PRODUCTION.md](./PRODUCTION.md)
- **Two-repo layout** → [REPOSITORIES.md](./REPOSITORIES.md)

## License

See repository root or project policy for license terms.
