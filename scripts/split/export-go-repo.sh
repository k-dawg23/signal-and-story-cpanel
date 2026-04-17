#!/usr/bin/env bash
# Clone this monorepo and produce a Go/VPS–oriented tree (no apps/api-node).
# Usage: ./scripts/split/export-go-repo.sh /path/to/signal-and-story-go
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

echo "Removing apps/api-node ..."
git rm -rf apps/api-node 2>/dev/null || true
rm -rf apps/api-node

python3 "${ROOT}/scripts/split/transform_dev_sh.py" go "${DEST}/scripts/dev.sh"

# Summary line in dev.sh
if grep -q 'Go, or SAS_API=node' "${DEST}/scripts/dev.sh"; then
  sed -i 's/(Go, or SAS_API=node for api-node)/(Go API — see signal-and-story-cpanel for Node)/' "${DEST}/scripts/dev.sh"
fi

git add -A
if git diff --cached --quiet; then
  echo "No changes to commit (did you forget to commit apps/api-node / dev.sh markers in the monorepo first?)." >&2
else
  git -c user.email="split-export@local" -c user.name="Split export" \
    commit -m "Split export: Go/VPS line (remove apps/api-node)"
fi

echo "Done. Add GitHub remote and push, e.g.:"
echo "  cd \"${DEST}\" && git remote set-url origin https://github.com/YOU/signal-and-story.git && git push -u origin main"
