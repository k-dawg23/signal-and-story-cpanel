#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing dependency: $1" >&2
    exit 1
  }
}

need_cmd docker
need_cmd npm

if [[ ! -f "${ROOT_DIR}/.env" ]]; then
  echo "No .env found. Copying from .env.example"
  cp "${ROOT_DIR}/.env.example" "${ROOT_DIR}/.env"
  echo "Created .env. Fill POLAR/BREVO secrets as needed."
fi

set -a
source "${ROOT_DIR}/.env"
set +a

echo "Starting infra..."
docker compose -f "${ROOT_DIR}/infra/docker-compose.yml" up -d

echo "Migrating + seeding DB..."
"${ROOT_DIR}/scripts/db-reset-and-seed.sh"

echo "Installing dependencies (if needed)..."
if [[ ! -d "${ROOT_DIR}/apps/auth/node_modules" ]]; then
  (cd "${ROOT_DIR}/apps/auth" && npm install)
fi
if [[ ! -d "${ROOT_DIR}/apps/storefront/node_modules" ]]; then
  (cd "${ROOT_DIR}/apps/storefront" && npm install)
fi

AUTH_PORT="${AUTH_PORT:-8787}"
API_ADDR="${API_ADDR:-:8788}"
PUBLIC_API_BASE="${PUBLIC_API_BASE:-http://localhost:8788}"
PUBLIC_AUTH_BASE="${PUBLIC_AUTH_BASE:-http://localhost:${AUTH_PORT}}"

export APP_BASE_URL="${APP_BASE_URL:-http://localhost:4321}"
export AUTH_BASE_URL="${AUTH_BASE_URL:-http://localhost:${AUTH_PORT}}"
export AUTH_PORT
export API_ADDR
export PUBLIC_API_BASE
export PUBLIC_AUTH_BASE

PIDS=()

cleanup() {
  echo
  echo "Shutting down..."
  for pid in "${PIDS[@]:-}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
  wait || true
}
trap cleanup INT TERM EXIT

echo "Starting auth service on http://localhost:${AUTH_PORT} ..."
(cd "${ROOT_DIR}/apps/auth" && npm run dev) &
PIDS+=("$!")

if command -v go >/dev/null 2>&1; then
  echo "Starting API on http://localhost:8788 ..."
  (cd "${ROOT_DIR}/apps/api" && go run ./cmd/api) &
  PIDS+=("$!")
else
  echo "Go is not installed; skipping API start. Install Go and rerun." >&2
fi

echo "Starting storefront on http://localhost:4321 ..."
(cd "${ROOT_DIR}/apps/storefront" && npm run dev) &
PIDS+=("$!")

echo
echo "Dev stack is running:"
echo "- Storefront: http://localhost:4321"
echo "- Auth:       http://localhost:${AUTH_PORT}"
echo "- API:        http://localhost:8788"
echo "- Mailpit UI: http://localhost:8026"
echo
echo "Press Ctrl+C to stop."

wait

