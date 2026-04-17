# Plan: PHP backend for shared hosting (Option B)

> **Prefer Node?** See **`PLAN_NODE_API_SHARED_HOSTING.md`** (Option A — Fastify/Express API, same cPanel deployment as auth).

This document plans a **PHP** implementation of the store **API** so it can run under **cPanel** (Apache, PHP-FPM, Composer) without a custom Go reverse proxy. It also covers **repository strategy** so the **current Go + Node stack** remains available for a **future VPS** deployment.

---

## 1. Goals

| Goal | Detail |
|------|--------|
| **Shared-hosting friendly** | Run as a normal PHP app (document root or subdirectory), **no** long-lived Go binary or reverse proxy to an arbitrary port. |
| **Frontend compatibility** | Preserve **HTTP paths**, **JSON shapes**, and **headers** (`Origin`, `X-User-Id`, `X-User-Email`) so the **Astro storefront** and **auth** service need minimal changes. |
| **Light and fast** | Prefer **Slim 4** or **Laravel** (or **Lumen**) with **PDO**/`pg_*` to PostgreSQL; avoid heavy frameworks unless you need them. |
| **Preserve current codebase** | Keep a **separate Git repository** with the existing **Go API + apps** intact for a possible **VPS deployment** later. |

---

## 2. Repository strategy: fork / duplicate for two lines of work

You want **two parallel histories**:

| Repository | Role |
|------------|------|
| **Original** (e.g. `signal-and-story`) | **Frozen or tagged** “Go stack” — `apps/api` (Go), `apps/auth`, `apps/storefront`; deployable on VPS when you choose. |
| **New** (e.g. `signal-and-story-php` or `signal-and-story-cpanel`) | **Active development** — PHP API + same auth + storefront; tuned for shared hosting. |

### 2.1 Recommended approach (clean separation)

1. **Tag the current repo** on `main` (or your release branch):

   ```bash
   git tag -a go-stack-v1 -m "Last unified Go API + Node auth/storefront before PHP API"
   git push origin go-stack-v1
   ```

2. **Create a new empty repository** on GitHub (do **not** use GitHub “Fork” if you want **no** automatic link to upstream PRs — a **new repo** is clearer). Name it e.g. **`signal-and-story-php`**.

3. **Push a copy of the tree** into the new repo:

   ```bash
   cd /path/to/signal-and-story
   git remote add php-origin https://github.com/YOU/signal-and-story-php.git
   git push php-origin main
   ```

4. In **`signal-and-story-php`**, default branch = **`main`**; **first commit** after the push can add `PLAN_PHP_SHARED_HOSTING.md` implementation notes and a new `apps/api-php/` (or `public/api/` — see §4) **without deleting** `apps/api` immediately — or remove `apps/api` in a dedicated commit once PHP parity exists.

### 2.2 Alternative: GitHub “Fork”

- **Fork** on GitHub creates a **fork** relationship (shows “forked from”) and keeps **easy PR** flow to upstream.
- Use **fork** if you might want to **upstream** patches; use **new repo + push** if you want **no** relationship to the original repo.

**Recommendation:** **New repository + initial push** from a tagged snapshot, plus **tag on original** for the Go stack — clearest for “two products”.

### 2.3 What stays in each repo

| Artifact | Original repo | New repo |
|----------|---------------|----------|
| `apps/api` (Go) | Optional: keep for VPS | **Remove** after PHP parity, or **archive** in a `legacy/` branch |
| `apps/auth` | Yes | **Keep** (unchanged deployment) |
| `apps/storefront` | Yes | **Keep**; only env / small URL tweaks |
| `apps/api-php` (new) | No | **Add** |
| PHP `composer.json` | No | Root or under `apps/api-php` |

---

## 3. API surface to reimplement (parity with Go)

Match these routes and behaviours (from `apps/api/internal/api/server.go`):

| Method | Path | Notes |
|--------|------|--------|
| `GET` | `/healthz` | Body `ok` |
| `GET` | `/api/_meta/build` | JSON metadata (email backend hints, etc.) |
| `POST` | `/webhooks/stripe` | Raw body, Stripe signature verification |
| `GET` | `/api/products` | Query: `q`, `collection` (if present in Go) |
| `GET` | `/api/products/{handle}` | |
| `POST` | `/api/checkout/session` | Headers: `X-User-Id`, `X-User-Email`) |
| `GET` | `/api/account/orders` | Auth headers |
| `GET` | `/api/admin/overview` | Admin gate |
| `GET` | `/api/admin/products` | … |
| `POST` | `/api/admin/products` | … |
| `PUT` | `/api/admin/products/{id}` | … |
| `DELETE` | `/api/admin/products/{id}` | … |
| `GET` | `/api/admin/orders` | … |
| `GET` | `/api/admin/orders/{id}` | … |
| `POST` | `/api/admin/orders/{id}/resend-confirmation` | … |
| `GET` | `/api/admin/collections` | … |
| `POST` | `/api/admin/collections` | … |
| `PUT` | `/api/admin/collections/{id}` | … |
| `DELETE` | `/api/admin/collections/{id}` | … |
| `PUT` | `/api/admin/collections/{id}/products` | … |

**CORS:** Replicate `APP_BASE_URL` + `APP_ORIGIN_ALLOWLIST` logic.

**Session / admin:** Replicate calls to **Better Auth** internal session (`AUTH_BASE_URL` + `/internal/session` or equivalent) as the Go `attachUserFromAuthService` middleware does.

**Stripe:** Use **Stripe PHP SDK**; webhook idempotency and order persistence must match DB schema from `apps/api/migrations/*.sql`.

**Email:** Brevo HTTP API or SMTP — same env vars as Go (`BREVO_*`, `SMTP_*`).

---

## 4. Suggested PHP stack (light)

| Layer | Choice |
|-------|--------|
| **Router** | **Slim 4** (small) or **Laravel** if you want batteries (optional) |
| **DB** | **PDO** with `pgsql` driver, or **Doctrine DBAL** if you prefer |
| **HTTP** | Native PHP; **Guzzle** for calling auth service if needed |
| **Stripe** | `stripe/stripe-php` |
| **Config** | `.env` via **vlucas/phpdotenv** (or Laravel’s env) |

**PHP version:** Match cPanel (often **8.1+**).

---

## 5. Project layout (in the new repo)

Example structure (adjust to taste):

```text
apps/
  auth/              # unchanged Node app
  storefront/        # unchanged Astro app
  api-php/           # PHP application
    public/          # web root (index.php front controller)
    src/
      routes.php
      Controllers/
      Services/
    composer.json
    .env.example
```

**Front controller:** `public/index.php` routes all requests to Slim; **cPanel document root** for `api.signal-and-story.k-dawg.uk` points at **`public/`**.

**Alternative:** Single **`public_html/api/`** subdirectory if the host only gives one domain root — then base path in Slim or rewrite rules.

---

## 6. Deployment on shared hosting (high level)

1. **Subdomain** `api.signal-and-story.k-dawg.uk` → document root = **`public/`** (or `api-php/public`).
2. **Composer:** Run **`composer install --no-dev --optimize-autoloader`** locally (or on host if SSH + Composer available), upload **`vendor/`** or run Composer on server.
3. **`.env`** next to the app (not `public/`): `DATABASE_URL`, Stripe, auth, mail, `APP_BASE_URL`, `APP_ORIGIN_ALLOWLIST`, `ADMIN_EMAIL`, `APP_ENV`.
4. **Apache:** `mod_rewrite` + Slim’s `.htaccess` so `index.php` receives all paths.
5. **HTTPS:** AutoSSL on the subdomain.

No Node proxy for the API — **PHP** is the native Apache handler.

---

## 7. Frontend changes (minimal)

| Change | Where |
|--------|--------|
| **`PUBLIC_API_BASE`** | Still `https://api.signal-and-story.k-dawg.uk` — **same** if the API hostname is unchanged. |
| **CORS** | Must allow storefront origin — PHP must replicate Go CORS headers. |
| **Stripe** | Success/cancel URLs unchanged if still configured in env for the PHP app. |

---

## 8. Phased implementation order

1. **Scaffold** Slim + health + `/api/_meta/build` + DB connection.
2. **Read paths:** `/api/products`, `/api/products/{handle}`, search/query params.
3. **Auth middleware:** Session verification vs auth service (mirror Go).
4. **Checkout session** + Stripe Checkout creation (server-side prices).
5. **Webhook** + order persistence + idempotency (critical).
6. **Email** — order confirmation.
7. **Account orders** + **admin** routes.
8. **Delete or archive** Go `apps/api` in the **PHP repo** once parity tested; **tag** the PHP repo `php-api-v1`.

---

## 9. Testing and rollback

| Step | Action |
|------|--------|
| **Contract tests** | Compare JSON responses Go vs PHP for key endpoints (or Postman collection). |
| **Stripe** | Test mode webhooks + one full checkout before live. |
| **Rollback** | Point `PUBLIC_API_BASE` at a **temporary** Go deployment on VPS, or **revert** DNS to a previous subdomain if you keep a staging API. |

---

## 10. Original repo (VPS later)

- Keep **`git tag go-stack-v1`** (or similar) on the **original** repository.
- **`PRODUCTION.md`** (VPS / Go path) remains valid for that line.
- **New repo** can add **`PRODUCTION_PHP.md`** with cPanel-only steps.

---

## 11. Risks

| Risk | Mitigation |
|------|------------|
| Subtle JSON or status-code differences | Automated tests + manual checkout + admin flows |
| Stripe webhook replay / idempotency bugs | Port logic carefully from `stripe_webhook.go` |
| Session header mismatch | Same `X-User-Id` / `X-User-Email` contract as Go |
| Composer not on server | Commit `vendor/` for deploy (not ideal but common on shared hosting) |

---

*This is a plan only. Implementation is tracked in the **new** repository after you split from the tagged Go snapshot.*
