# Two GitHub repositories (Go line vs Node line)

**Published (k-dawg23):** [signal-and-story](https://github.com/k-dawg23/signal-and-story) (Go API on `main`) · [signal-and-story-cpanel](https://github.com/k-dawg23/signal-and-story-cpanel) (Node API on `main`). The monorepo snapshot with **both** APIs is tag **`monorepo-both-v1`** on `signal-and-story`.

This project can live in **two separate GitHub repos** so deployment and mental overhead stay clear:

| Repository | API | Typical hosting |
|------------|-----|-----------------|
| **`signal-and-story`** (or `signal-and-story-go`) | **Go** (`apps/api`) | VPS, systemd, reverse proxy — see [PRODUCTION.md](./PRODUCTION.md) §10 |
| **`signal-and-story-cpanel`** | **Node** (`apps/api-node`) | cPanel **Setup Node.js App** — see [PRODUCTION.md](./PRODUCTION.md) §10 (shared hosting alternative) |

Shared pieces in both: **`apps/auth`**, **`apps/storefront`**, **`apps/api/migrations/*.sql`**, **`infra/`**, **`scripts/`** (with small differences in `scripts/dev.sh` after export).

## Prerequisites

1. **Commit and push** the full monorepo (including `apps/api-node`) on the machine you use for the split. Export clones **committed** history only; uncommitted files are invisible to `git clone .`.
2. **Optional tag** on that monorepo so you can always get back to “both APIs in one tree”:

   ```bash
   git tag -a monorepo-both-v1 -m "Monorepo with Go API and api-node before GitHub split"
   git push origin monorepo-both-v1
   ```

3. **GitHub CLI** (`gh`) is optional. If `gh auth login` works, you can create the second repo from the shell; otherwise use **GitHub → New repository**.

## Create the two trees locally

From the **monorepo root** (`signal-and-story/`):

```bash
# Go/VPS-oriented copy (no apps/api-node)
./scripts/split/export-go-repo.sh ../signal-and-story-go-export

# Node/cPanel-oriented copy (Go source removed; migrations kept under apps/api/migrations)
./scripts/split/export-node-repo.sh ../signal-and-story-cpanel-export
```

Each script makes one commit on top of the clone. Inspect diffs, then publish (below).

## Publish to GitHub

Names below match a common layout; change `k-dawg23` / repo names to yours.

### Repo A — Go line

Point this at **`signal-and-story-go-export`** (or keep using your existing **`signal-and-story`** name if you want that repo to be Go-only).

```bash
cd ../signal-and-story-go-export
git remote rename origin monorepo-upstream 2>/dev/null || true
git remote add origin https://github.com/k-dawg23/signal-and-story.git
git push -u origin main
```

If **`signal-and-story`** already exists and you are **replacing** `main` with this Go-only history, coordinate with anyone else using the repo; consider renaming the old default branch or archiving first.

### Repo B — Node line

Create an **empty** repository on GitHub (no README), e.g. **`signal-and-story-cpanel`**, then:

```bash
cd ../signal-and-story-cpanel-export
git remote rename origin monorepo-upstream 2>/dev/null || true
git remote add origin https://github.com/k-dawg23/signal-and-story-cpanel.git
git push -u origin main
```

### Create the empty repo with `gh` (optional)

Re-authenticate if needed: `gh auth login -h github.com`.

```bash
gh repo create k-dawg23/signal-and-story-cpanel --private --description "Signal & Story — Node API for shared hosting" --source ../signal-and-story-cpanel-export --remote origin --push
```

Adjust `--private` / `--public` and flags to match your workflow.

## After the split

- **Bugfixes** that touch shared code (storefront, auth, SQL): cherry-pick or merge between remotes, or maintain a short-lived **monorepo** fork that runs both export scripts when you release.
- **API-only changes:** commit on the repo that owns that API (`signal-and-story` for Go, `signal-and-story-cpanel` for Node).
- **`scripts/dev.sh`:** The monorepo version contains `### EXPORT_OMIT_*` markers; do not remove them if you rely on the export scripts. Re-run exports after changing the API switch logic.

## Troubleshooting

- **`git rm apps/api-node` did nothing** — `apps/api-node` was never committed; commit it in the monorepo, then re-run the export.
- **Second repo push rejected** — ensure the GitHub repo is empty (no initial commit from the web UI README).
- **`gh` auth errors** — use the GitHub website to create the repo and `git remote add` + `git push` manually.
