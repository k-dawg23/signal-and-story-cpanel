#!/usr/bin/env bash
# Clone this monorepo and produce a shared-hosting / Node API tree (no Go source; keep SQL migrations).
# Usage: ./scripts/split/export-node-repo.sh /path/to/signal-and-story-cpanel
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEST="${1:?Pass destination directory (must not exist)}"

if [[ -e "${DEST}" ]]; then
  echo "Refusing to overwrite: ${DEST}" >&2
  exit 1
fi

echo "Cloning ${ROOT} → ${DEST} ..."
git clone "${ROOT}" "${DEST}"
cd "${DEST}"

echo "Removing Go API source (keeping apps/api/migrations) ..."
git rm -rf apps/api/cmd apps/api/internal apps/api/go.mod apps/api/go.sum 2>/dev/null || true
rm -rf apps/api/cmd apps/api/internal
rm -f apps/api/go.mod apps/api/go.sum apps/api/run-api.sh

# Remove stray binary if present (not usually tracked)
rm -f apps/api/api

git add -A

python3 "${ROOT}/scripts/split/transform_dev_sh.py" node "${DEST}/scripts/dev.sh"

if grep -q 'Go, or SAS_API=node' "${DEST}/scripts/dev.sh"; then
  sed -i 's/(Go, or SAS_API=node for api-node)/(Node API — see signal-and-story for Go)/' "${DEST}/scripts/dev.sh"
fi

git add -A
if git diff --cached --quiet; then
  echo "No changes to commit (did you forget to commit the monorepo first?)." >&2
else
  git -c user.email="split-export@local" -c user.name="Split export" \
    commit -m "Split export: Node/cPanel line (remove Go API; keep migrations)"
fi

echo "Done. Create an empty GitHub repo, then:"
echo "  cd \"${DEST}\" && git remote add origin https://github.com/YOU/signal-and-story-cpanel.git && git push -u origin main"
