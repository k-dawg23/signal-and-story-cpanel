# Signal & Story

Premium musician gear for sci‑fi & fantasy worlds.

## Dev setup

Prereqs:
- Docker + Docker Compose
- Go (for `apps/api`)
- Node.js (for `apps/storefront` and `apps/auth`)

Start infra:

```bash
docker compose -f infra/docker-compose.yml up -d
```

Apps will be wired up in subsequent steps.

