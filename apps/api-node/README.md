# Signal & Story API (Node)

TypeScript **Fastify** service that mirrors **`apps/api` (Go)** for shared hosting and local dev without Go.

- **Run (dev):** `npm install && npm run dev` (loads repo-root `.env` via `src/config.ts`).
- **Run (prod build):** `npm run build && node dist/server.js` — use **Setup Node.js App** like `apps/auth`; startup **`dist/server.js`** (or run compiled `server.js` entry: check `dist/` after build; entry is **`dist/server.js`**).

Environment variables match **`PRODUCTION.md` §6.2** (same as Go API).

**Local stack:** from repo root, `SAS_API=node ./scripts/dev.sh` starts this API on port **8788** instead of Go.
