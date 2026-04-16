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
need_cmd ss

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

pids_listening_on_port() {
  local port="$1"
  # Example ss line:
  # users:(("node",pid=12345,fd=19))
  ss -lptn "sport = :${port}" 2>/dev/null \
    | grep -o 'pid=[0-9][0-9]*' \
    | cut -d= -f2 \
    | sort -u
}

free_port() {
  local port="$1"
  local label="$2"
  local pid
  for pid in $(pids_listening_on_port "${port}"); do
    echo "Port ${port} (${label}) is in use by PID ${pid}; stopping it so dev services can start..." >&2
    kill "${pid}" >/dev/null 2>&1 || true
  done
  sleep 0.2
}

if [[ ! -f "${ROOT_DIR}/.env" ]]; then
  echo "No .env found. Copying from .env.example"
  cp "${ROOT_DIR}/.env.example" "${ROOT_DIR}/.env"
  echo "Created .env. Fill Stripe/BREVO secrets as needed."
fi

set -a
source "${ROOT_DIR}/.env"
set +a

echo "Starting infra..."
docker compose -f "${ROOT_DIR}/infra/docker-compose.yml" up -d

echo "Migrating DB..."
SAS_SEED_PRODUCTS="${SAS_SEED_PRODUCTS:-0}" "${ROOT_DIR}/scripts/db-reset-and-seed.sh"

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
export AUTH_BASE_URL="${AUTH_BASE_URL:-http://localhost:${AUTH_PORT}/api/auth}"
export AUTH_PORT
export API_ADDR
export PUBLIC_API_BASE
export PUBLIC_AUTH_BASE
export SAS_BUILD_ID="${SAS_BUILD_ID:-local-$(date +%s)}"

API_LISTEN_PORT="${API_ADDR##*:}"
if [[ "${API_LISTEN_PORT}" == "${API_ADDR}" ]]; then
  API_LISTEN_PORT="8788"
fi

SAS_FREE_PORTS="${SAS_FREE_PORTS:-1}"
if [[ "${SAS_FREE_PORTS}" == "1" ]]; then
  free_port "${AUTH_PORT}" "auth"
  free_port "${API_LISTEN_PORT}" "api"
fi

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

echo "Ensuring Better Auth DB tables exist..."
# Better Auth CLI prompts for confirmation; auto-confirm for dev convenience.
(cd "${ROOT_DIR}/apps/auth" && printf 'y\n' | npx auth migrate --config ./src/auth.ts) >/dev/null 2>&1 || true

echo "Starting auth service on http://localhost:${AUTH_PORT} ..."
AUTH_LOG="${ROOT_DIR}/tmp/auth.log"
mkdir -p "${ROOT_DIR}/tmp"
(cd "${ROOT_DIR}/apps/auth" && npm run dev) >"${AUTH_LOG}" 2>&1 &
PIDS+=("$!")

echo "Waiting for Auth /healthz..."
AUTH_OK=0
for _ in {1..40}; do
  if curl -fsS "http://127.0.0.1:${AUTH_PORT}/healthz" >/dev/null 2>&1; then
    AUTH_OK=1
    break
  fi
  sleep 0.25
done
if [[ "${AUTH_OK}" -ne 1 ]]; then
  echo "Auth did not become healthy on port ${AUTH_PORT}." >&2
  echo "Auth log: ${AUTH_LOG}" >&2
  echo "---- auth.log (last 80 lines) ----" >&2
  tail -n 80 "${AUTH_LOG}" >&2 || true
  echo "--------------------------------" >&2
  echo "Tip: another process may still be bound to :${AUTH_PORT}. Try: SAS_FREE_PORTS=1 ./scripts/dev.sh" >&2
fi

API_OK=0
if GO_CMD="$(find_go)"; then
  echo "Starting API on http://localhost:${API_LISTEN_PORT} ..."
  API_LOG="${ROOT_DIR}/tmp/api.log"
  mkdir -p "${ROOT_DIR}/tmp"
  (cd "${ROOT_DIR}/apps/api" && "${GO_CMD}" mod download) >/dev/null 2>&1 || true
  (cd "${ROOT_DIR}/apps/api" && "${GO_CMD}" run ./cmd/api) >"${API_LOG}" 2>&1 &
  API_PID="$!"
  PIDS+=("${API_PID}")

  echo "Waiting for API /healthz..."
  for _ in {1..40}; do
    if curl -fsS "http://127.0.0.1:${API_LISTEN_PORT}/healthz" >/dev/null 2>&1; then
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
echo "- API:        http://localhost:${API_LISTEN_PORT} (requires Go)"
echo "- Mailpit UI: http://localhost:8026"
echo
echo "Press Ctrl+C to stop."

wait

