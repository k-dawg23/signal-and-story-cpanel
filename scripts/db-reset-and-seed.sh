#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/infra/docker-compose.yml"

echo "Starting infra..."
docker compose -f "${COMPOSE_FILE}" up -d db

echo "Waiting for Postgres..."
until docker exec "$(docker compose -f "${COMPOSE_FILE}" ps -q db)" pg_isready -U signal -d signal_and_story >/dev/null 2>&1; do
  sleep 1
done

DB_CID="$(docker compose -f "${COMPOSE_FILE}" ps -q db)"

echo "Applying migrations..."
for f in "${ROOT_DIR}/apps/api/migrations/"*.sql; do
  echo " - $(basename "$f")"
  docker exec -i "${DB_CID}" psql -U signal -d signal_and_story < "$f"
done

SAS_SEED_PRODUCTS="${SAS_SEED_PRODUCTS:-0}"
if [[ "${SAS_SEED_PRODUCTS}" == "1" ]]; then
  echo "Seeding products..."
  docker exec -i "${DB_CID}" psql -U signal -d signal_and_story < "${ROOT_DIR}/scripts/seed-products.sql"
else
  echo "Skipping product seed (set SAS_SEED_PRODUCTS=1 to seed)."
fi

echo "Done."

