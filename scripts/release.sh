#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

log()  { printf '[%s] [INFO] %s\n'  "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*"; }
warn() { printf '[%s] [WARN] %s\n'  "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*" >&2; }
err()  { printf '[%s] [ERROR] %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Runway Release CLI

Usage:
  ./scripts/release.sh <version> [options]

Examples:
  ./scripts/release.sh v0.1.0
  ./scripts/release.sh 0.1.0 --force -m "Initial release"

Options:
  -m, --message <msg>   Commit and tag annotation message
  -f, --force           Override existing local/remote tag and GitHub release
      --draft           Publish GitHub release as draft
      --prerelease      Mark GitHub release as prerelease
      --no-verify       Skip go vet / go test / script syntax checks
      --skip-commit     Do not auto-stage/commit pending workspace changes
  -h, --help            Show this help text
EOF
}

if [[ $# -eq 0 ]] || [[ "${1:-}" == "-h" ]] || [[ "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

RAW_VERSION="$1"
shift

MESSAGE=""
FORCE="false"
DRAFT="false"
PRERELEASE="false"
NO_VERIFY="false"
SKIP_COMMIT="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -m|--message)    MESSAGE="${2:-}"; shift 2 ;;
    -f|--force)      FORCE="true"; shift ;;
    --draft)         DRAFT="true"; shift ;;
    --prerelease)    PRERELEASE="true"; shift ;;
    --no-verify)     NO_VERIFY="true"; shift ;;
    --skip-commit)   SKIP_COMMIT="true"; shift ;;
    -h|--help)       usage; exit 0 ;;
    *)               err "Unknown option: $1" ;;
  esac
done

TAG="${RAW_VERSION}"
if [[ "${TAG}" != v* ]]; then
  TAG="v${TAG}"
fi

if [[ ! "${TAG}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?$ ]]; then
  err "Invalid semantic version '${RAW_VERSION}'. Expected e.g. v0.1.0 or 1.0.0."
fi

command -v git >/dev/null 2>&1 || err "git is required."
command -v gh >/dev/null 2>&1 || err "GitHub CLI (gh) is required."
gh auth status >/dev/null 2>&1 || err "gh CLI is not authenticated. Run 'gh auth login'."
git remote get-url origin >/dev/null 2>&1 || err "Git remote 'origin' is not configured."

if [[ "${NO_VERIFY}" != "true" ]]; then
  log "Running pre-release verification..."
  go vet ./cmd/... ./internal/...
  go test -race ./cmd/... ./internal/...
  bash -n scripts/deploy.sh scripts/release.sh
fi

if [[ "${SKIP_COMMIT}" != "true" ]]; then
  if [[ -n "$(git status --porcelain)" ]]; then
    local_msg="${MESSAGE:-chore(release): ${TAG}}"
    log "Staging and committing changes (${local_msg})..."
    git add -A
    git commit -m "${local_msg}"
  else
    log "Working tree clean; no release commit needed."
  fi
fi

# Check tag existence locally or remotely unless --force is set
if [[ "${FORCE}" != "true" ]]; then
  if git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null 2>&1; then
    err "Local tag ${TAG} already exists. Use --force to override."
  fi
  if git ls-remote --tags origin "refs/tags/${TAG}" | grep -q "${TAG}"; then
    err "Remote tag ${TAG} already exists on origin. Use --force to override."
  fi
fi

BRANCH="$(git rev-parse --abbrev-ref HEAD)"
log "Pushing branch '${BRANCH}' to origin..."
git push -u origin "${BRANCH}"

TAG_MSG="${MESSAGE:-Release ${TAG}}"
if [[ "${FORCE}" == "true" ]]; then
  log "Creating/overriding tag ${TAG}..."
  git tag -fa "${TAG}" -m "${TAG_MSG}"
  log "Force-pushing tag ${TAG} to origin..."
  git push --force origin "refs/tags/${TAG}"
else
  log "Creating tag ${TAG}..."
  git tag -a "${TAG}" -m "${TAG_MSG}"
  log "Pushing tag ${TAG} to origin..."
  git push origin "refs/tags/${TAG}"
fi

# Handle GitHub Release creation (replace existing release when --force)
if gh release view "${TAG}" >/dev/null 2>&1; then
  if [[ "${FORCE}" == "true" ]]; then
    warn "Deleting existing GitHub Release ${TAG} (--force)..."
    gh release delete "${TAG}" --yes
  else
    err "GitHub Release ${TAG} already exists. Use --force to replace it."
  fi
fi

GH_ARGS=(release create "${TAG}" --title "Runway ${TAG}" --generate-notes)
if [[ -n "${MESSAGE}" ]]; then
  GH_ARGS+=(--notes "${MESSAGE}")
fi
if [[ "${DRAFT}" == "true" ]]; then
  GH_ARGS+=(--draft)
fi
if [[ "${PRERELEASE}" == "true" ]]; then
  GH_ARGS+=(--prerelease)
fi

log "Publishing GitHub Release ${TAG}..."
gh "${GH_ARGS[@]}"
log "Release ${TAG} published successfully."
