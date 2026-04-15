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
need_cmd curl

find_go() {
  if command -v go >/dev/null 2>&1; then
    echo "go"
    return 0
  fi
  if [[ -x "/usr/local/go/bin/go" ]]; then
    echo "/usr/local/go/bin/go"
    return 0
  fi
  if [[ -x "/usr/bin/go" ]]; then
    echo "/usr/bin/go"
    return 0
  fi
  if [[ -x "/snap/bin/go" ]]; then
    echo "/snap/bin/go"
    return 0
  fi
  return 1
}

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

echo "Ensuring Better Auth DB tables exist..."
# Better Auth CLI prompts for confirmation; auto-confirm for dev convenience.
(cd "${ROOT_DIR}/apps/auth" && printf 'y\n' | npx auth migrate --config ./src/auth.ts) >/dev/null 2>&1 || true

API_OK=0
if GO_CMD="$(find_go)"; then
  echo "Starting API on http://localhost:8788 ..."
  API_LOG="${ROOT_DIR}/tmp/api.log"
  mkdir -p "${ROOT_DIR}/tmp"
  (cd "${ROOT_DIR}/apps/api" && "${GO_CMD}" mod download) >/dev/null 2>&1 || true
  (cd "${ROOT_DIR}/apps/api" && "${GO_CMD}" run ./cmd/api) >"${API_LOG}" 2>&1 &
  API_PID="$!"
  PIDS+=("${API_PID}")

  echo "Waiting for API /healthz..."
  for _ in {1..40}; do
    if curl -fsS "http://127.0.0.1:8788/healthz" >/dev/null 2>&1; then
      API_OK=1
      break
    fi
    sleep 0.25
  done

  if [[ "${API_OK}" -ne 1 ]]; then
    echo "API did not become healthy. Storefront will run in offline-catalog mode." >&2
    echo "API log: ${API_LOG}" >&2
    echo "---- api.log (last 80 lines) ----" >&2
    tail -n 80 "${API_LOG}" >&2 || true
    echo "--------------------------------" >&2
  fi
else
  echo "Go is not installed (or not on PATH); skipping API start. The storefront will run in offline-catalog mode." >&2
fi

echo "Starting storefront on http://localhost:4321 ..."
(cd "${ROOT_DIR}/apps/storefront" && npm run dev) &
PIDS+=("$!")

echo
echo "Dev stack is running:"
echo "- Storefront: http://localhost:4321"
echo "- Auth:       http://localhost:${AUTH_PORT}"
echo "- API:        http://localhost:8788 (requires Go)"
echo "- Mailpit UI: http://localhost:8026"
echo
echo "Press Ctrl+C to stop."

wait

