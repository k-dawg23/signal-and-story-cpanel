# Plan: Node.js API for shared hosting (Option A)

This document plans replacing the **Go** service (`apps/api`) with a **Node.js** API so it deploys like **`apps/auth`**: **cPanel → Setup Node.js App**, **Passenger**, **environment variables in the panel**, **no** reverse proxy to a custom Go binary. It also covers **repository strategy** so the **current Go stack** stays available for a **future VPS**.

> If you previously read **`PLAN_PHP_SHARED_HOSTING.md`** (Option B), that path is **PHP**; **this** plan is the **Node** alternative (Option A).

---

## 1. Goals

| Goal | Detail |
|------|--------|
| **Shared-hosting friendly** | Same operational model as **auth**: one **Node** app per subdomain, **`npm run build`** locally, **`npm install`** (or Ensure dependencies) on server, **startup file** e.g. `dist/server.js`. |
| **Frontend compatibility** | Preserve **HTTP paths**, **JSON shapes**, and **headers** (`Origin`, `X-User-Id`, `X-User-Email`) so the **Astro storefront** needs only env / smoke-test updates. |
| **Light and fast** | Prefer **Fastify** (or **Express**) + **`pg`** (or `pg-pool`) + official **`stripe`** npm package; **TypeScript** compiled to `dist/`. |
| **Preserve Go codebase** | **Tag** and optionally **duplicate** the repo so **`apps/api` (Go)** remains for VPS deployment. |

---

## 2. Repository strategy: fork / duplicate for two lines of work

| Repository | Role |
|------------|------|
| **Original** (e.g. `signal-and-story`) | **Tagged** “Go stack” — `apps/api` (Go), `apps/auth`, `apps/storefront`; deployable on VPS when you choose. |
| **New** (e.g. `signal-and-story-node` or `signal-and-story-cpanel`) | **Active line** — Node API (`apps/api-node` or merge name) + auth + storefront; tuned for cPanel. |

### 2.1 Recommended approach

1. **Tag the current repo** before removing or replacing the Go API:

   ```bash
   git tag -a go-stack-v1 -m "Last Go API + Node auth/storefront before Node API migration"
   git push origin go-stack-v1
   ```

2. **Create a new empty repository** on GitHub (name e.g. **`signal-and-story-node`**). Use a **new repo + push** if you want independence from the original; use **Fork** only if you want GitHub’s upstream PR workflow.

3. **Push a copy** of the current tree:

   ```bash
   cd /path/to/signal-and-story
   git remote add node-origin https://github.com/YOU/signal-and-story-node.git
   git push node-origin main
   ```

4. In the **new repo**, add **`apps/api-node`** (or `apps/api` after renaming Go — see §5) and implement **route parity**; **remove** or **archive** `apps/api` (Go) in a dedicated commit once the Node API is verified.

### 2.2 What lives in each repo

| Artifact | Original repo | New repo |
|----------|---------------|----------|
| `apps/api` (Go) | **Keep** for VPS | **Remove** after Node parity (or move to branch `legacy/go-api`) |
| `apps/auth` | Yes | **Keep** (same deployment) |
| `apps/storefront` | Yes | **Keep**; rebuild **`PUBLIC_API_BASE`** when Node API URL is stable |
| **`apps/api-node`** (new) | No | **Add** — TypeScript Fastify/Express app |

---

## 3. API surface to reimplement (parity with Go)

Port behaviour from `apps/api/internal/api/server.go` (and related files). Match:

| Method | Path | Notes |
|--------|------|--------|
| `GET` | `/healthz` | Body `ok` |
| `GET` | `/api/_meta/build` | JSON (email backend hints, `SAS_BUILD_ID`, etc.) |
| `POST` | `/webhooks/stripe` | Raw body, Stripe signature verification |
| `GET` | `/api/products` | Query: `q`, `collection` as in Go |
| `GET` | `/api/products/{handle}` | |
| `POST` | `/api/checkout/session` | Headers: `X-User-Id`, `X-User-Email` |
| `GET` | `/api/account/orders` | |
| `GET` | `/api/admin/...` | All admin routes (products, orders, collections, resend email) |

**CORS:** Same `APP_BASE_URL` + `APP_ORIGIN_ALLOWLIST` logic as Go.

**Auth:** HTTP call to **`AUTH_BASE_URL`** / internal session (`/internal/session`) — mirror `attachUserFromAuthService` middleware.

**Stripe:** `stripe` npm package (same concepts as `stripe-go`).

**DB:** **`pg`** with **`DATABASE_URL`**; reuse **`apps/api/migrations/*.sql`** (no schema fork unless you fix something deliberately).

**Email:** Nodemailer + Brevo fetch or SMTP — same env vars as Go (`BREVO_*`, `SMTP_*`).

---

## 4. Suggested Node stack

| Layer | Choice |
|-------|--------|
| **HTTP** | **Fastify** (lean) or **Express** (familiar) |
| **Language** | **TypeScript** → `tsc` → `dist/` |
| **DB** | **`pg`** `Pool` |
| **Stripe** | **`stripe`** (official) |
| **Config** | **`dotenv`** for local dev; production = cPanel env vars |

**Node version:** Match **cPanel** (e.g. **22.x**) for local builds — same as §9 in **`PRODUCTION.md`**.

---

## 5. Project layout (in the new repo)

```text
apps/
  auth/                 # unchanged
  storefront/           # unchanged
  api-node/             # new Node API (name may be apps/api after Go removal)
    package.json
    tsconfig.json
    src/
      server.ts         # Fastify listen + register routes
      routes/           # or handlers mirroring Go packages
      middleware/
      services/
    dist/               # produced by npm run build
```

**Startup:** Same pattern as auth: **`node dist/server.js`** (or `dist/index.js` — **one** entry file).

**Env:** Reuse **`§6.2`** variable names from **`PRODUCTION.md`** (`DATABASE_URL`, `API_ADDR`, `STRIPE_*`, `AUTH_BASE_URL`, `APP_BASE_URL`, mail, `ADMIN_EMAIL`, etc.). **`API_ADDR`** defaults to `:8788` if you keep parity with Go.

---

## 6. Deployment on shared hosting (high level)

Same as **auth** (see **`PRODUCTION.md` §9.1**):

1. **Subdomain** `api.signal-and-story.k-dawg.uk` → **Setup Node.js App**.
2. **Application root** = folder with **`package.json`**, **`dist/`**, **`package-lock.json`**.
3. **Startup file** = e.g. **`dist/server.js`**.
4. **Environment variables** in cPanel (or **`.env`** in app root with a wrapper — see auth).
5. **Ensure dependencies** / Run NPM Install, **Restart**.

**No** Go binary, **no** reverse proxy to a random port — **HTTPS** is handled like your other Node apps.

---

## 7. Frontend changes

| Item | Action |
|------|--------|
| **`PUBLIC_API_BASE`** | Still `https://api.signal-and-story.k-dawg.uk` if the hostname is unchanged — **rebuild** storefront after switching API. |
| **Contract** | If JSON matches Go, **no** Astro code changes beyond env. |

---

## 8. Phased implementation order

1. Scaffold **Fastify** + **`GET /healthz`** + **`GET /api/_meta/build`** + DB pool.
2. **Catalog:** `GET /api/products`, `GET /api/products/:handle`.
3. **Auth middleware** (session from auth service).
4. **`POST /api/checkout/session`** + Stripe Checkout Session.
5. **`POST /webhooks/stripe`** + orders + idempotency.
6. **Order email** + **account orders**.
7. **Admin** routes.
8. Remove or archive **Go `apps/api`** in the new repo; **tag** `node-api-v1`.

---

## 9. Testing and rollback

| Step | Action |
|------|--------|
| **Parity** | Spot-check JSON vs Go for products, checkout, orders (or small test suite). |
| **Stripe** | Test mode webhooks + full checkout before live. |
| **Rollback** | Original repo still has **Go** at tag **`go-stack-v1`**; deploy VPS API and point **`PUBLIC_API_BASE`** there if needed. |

---

## 10. Original repo (VPS later)

- **`go-stack-v1`** tag on **`signal-and-story`** preserves the Go API line.
- **`PRODUCTION.md`** Go sections remain valid for VPS + reverse proxy if you return to Go.

---

## 11. Risks

| Risk | Mitigation |
|------|------------|
| Stripe / webhook subtle differences | Port `stripe_webhook.go` logic carefully; test idempotency |
| TypeScript / ESM vs CJS | Match **`apps/auth`** module style (`"type": "module"`) for consistency |
| Two Node apps memory | Shared hosting limits — monitor; Fastify helps vs heavy stacks |

---

## 12. Status (this repo)

**Implemented:** `apps/api-node` — Fastify + `pg` + Stripe Node SDK, same routes and env contract as `apps/api` (Go). Deploy with cPanel **Setup Node.js App**; local dev: `SAS_API=node ./scripts/dev.sh` or `cd apps/api-node && npm run dev`.

**Two GitHub repos:** See **[REPOSITORIES.md](./REPOSITORIES.md)** for `export-go-repo.sh` / `export-node-repo.sh` and push steps (`signal-and-story` vs `signal-and-story-cpanel`).

To keep a **Go-only** line for VPS, tag the monorepo (`go-stack-v1`) before deleting `apps/api` if you ever remove it.

---

*Original plan text above; implementation lives in **`apps/api-node`**.*
