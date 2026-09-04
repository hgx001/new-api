#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'
umask 022

# Deploy the default frontend by pushing the current commit and building it on
# the production host. The API model names and backend container are untouched.

REMOTE_HOST="${DEPLOY_HOST:-119.29.253.97}"
REMOTE_PORT="${DEPLOY_PORT:-877}"
REMOTE_USER="${DEPLOY_USER:-ubuntu}"
GIT_REMOTE="${DEPLOY_GIT_REMOTE:-fork}"
SERVER_GIT_REMOTE="${DEPLOY_SERVER_GIT_REMOTE:-origin}"
BRANCH="${DEPLOY_BRANCH:-$(git branch --show-current 2>/dev/null || true)}"
REMOTE_APP_DIR="${DEPLOY_APP_DIR:-/home/ubuntu/new-api-src}"
REMOTE_FRONTEND_DIR="${DEPLOY_FRONTEND_DIR:-/opt/new-api/frontend/default}"
HEALTHCHECK_URL="${DEPLOY_HEALTHCHECK_URL:-https://api.heibaidao.cn}"
DRY_RUN=false
SKIP_PUSH=false
REF=""

usage() {
  cat <<'EOF'
Usage: deploy/deploy-frontend.sh [options]

Push the current committed branch to the configured Git remote, then make the
production server fetch that exact commit, build web/default, and atomically
publish its dist directory behind NGINX.

Options:
  --ref SHA|branch  Deploy an explicit commit SHA or branch (default: current HEAD)
  --no-push         Do not push; use --ref or the current HEAD already on the remote
  --dry-run         Validate and print the plan without changing Git or the server
  -h, --help        Show this help

Environment overrides:
  DEPLOY_GIT_REMOTE=fork
  DEPLOY_SERVER_GIT_REMOTE=fork  (remote name in the server checkout)
  DEPLOY_BRANCH=main
  DEPLOY_HOST=119.29.253.97 DEPLOY_PORT=877 DEPLOY_USER=ubuntu
  DEPLOY_APP_DIR=/opt/new-api
  DEPLOY_FRONTEND_DIR=/opt/new-api/frontend/default
  DEPLOY_HEALTHCHECK_URL=https://api.heibaidao.cn
EOF
}

log() {
  printf '[deploy] %s\n' "$*"
}

die() {
  printf '[deploy] ERROR: %s\n' "$*" >&2
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ref)
      [[ $# -ge 2 ]] || die '--ref requires a value'
      REF="$2"
      shift 2
      ;;
    --no-push)
      SKIP_PUSH=true
      shift
      ;;
    --dry-run)
      DRY_RUN=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown option: $1"
      ;;
  esac
done

command -v git >/dev/null 2>&1 || die 'git is required'
command -v ssh >/dev/null 2>&1 || die 'ssh is required'
git rev-parse --show-toplevel >/dev/null 2>&1 || die 'run this script inside the Git repository'

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

[[ -n "$BRANCH" ]] || die 'detached HEAD: set DEPLOY_BRANCH and use --ref'

if [[ -n "$(git status --porcelain)" ]]; then
  die 'working tree is not clean; commit or stash changes before deploying'
fi

if [[ -z "$REF" ]]; then
  REF="$(git rev-parse HEAD)"
fi

git rev-parse --verify "$REF^{commit}" >/dev/null 2>&1 || die "unknown local ref: $REF"
COMMIT_SHA="$(git rev-parse "$REF^{commit}")"

if [[ "$SKIP_PUSH" == false ]]; then
  if [[ "$DRY_RUN" == true ]]; then
    log "would push $COMMIT_SHA to $GIT_REMOTE ($BRANCH)"
  else
    log "pushing $COMMIT_SHA to $GIT_REMOTE ($BRANCH)"
    git push "$GIT_REMOTE" "$COMMIT_SHA:refs/heads/$BRANCH"
  fi
else
  log "skipping push; remote must already contain $COMMIT_SHA"
fi

SSH_OPTS=(-p "$REMOTE_PORT" -o BatchMode=yes -o StrictHostKeyChecking=yes -o ConnectTimeout=10 -o ServerAliveInterval=30 -o ServerAliveCountMax=3)
REMOTE="$REMOTE_USER@$REMOTE_HOST"

log "deploying $COMMIT_SHA to $REMOTE"
if [[ "$DRY_RUN" == true ]]; then
  log "would run remote deployment in $REMOTE_APP_DIR"
  log "would publish frontend under $REMOTE_FRONTEND_DIR and reload nginx"
  log "would verify $HEALTHCHECK_URL"
  exit 0
fi

REMOTE_COMMAND="COMMIT_SHA=$(printf '%q' "$COMMIT_SHA") REMOTE_APP_DIR=$(printf '%q' "$REMOTE_APP_DIR") REMOTE_FRONTEND_DIR=$(printf '%q' "$REMOTE_FRONTEND_DIR") SERVER_GIT_REMOTE=$(printf '%q' "$SERVER_GIT_REMOTE") BRANCH=$(printf '%q' "$BRANCH") HEALTHCHECK_URL=$(printf '%q' "$HEALTHCHECK_URL") bash -s"
ssh "${SSH_OPTS[@]}" "$REMOTE" "$REMOTE_COMMAND" <<'REMOTE_SCRIPT'
set -Eeuo pipefail

log() {
  printf '[remote-deploy] %s\n' "$*"
}

die() {
  printf '[remote-deploy] ERROR: %s\n' "$*" >&2
  exit 1
}

command -v git >/dev/null 2>&1 || die 'git is required on the server'
command -v bun >/dev/null 2>&1 || die 'bun is required on the server'
command -v flock >/dev/null 2>&1 || die 'flock is required on the server'
command -v curl >/dev/null 2>&1 || die 'curl is required on the server'

cd "$REMOTE_APP_DIR" || die "source checkout does not exist: $REMOTE_APP_DIR"
git rev-parse --show-toplevel >/dev/null 2>&1 || die "$REMOTE_APP_DIR is not a Git checkout"

exec 9>/tmp/new-api-frontend.deploy.lock
flock -n 9 || die 'another deployment is already running'

if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
  die 'server checkout has local changes; refusing to overwrite them'
fi

log "fetching $COMMIT_SHA from $SERVER_GIT_REMOTE"
git fetch --prune "$SERVER_GIT_REMOTE"
git cat-file -e "$COMMIT_SHA^{commit}" || die "commit is not available after fetch: $COMMIT_SHA"
git checkout --detach --quiet "$COMMIT_SHA"

[[ "$(git rev-parse HEAD)" == "$COMMIT_SHA" ]] || die 'checked out commit does not match requested commit'
[[ -f web/package.json && -f web/bun.lock ]] || die 'frontend workspace files are missing'
[[ -f VERSION ]] || die 'VERSION file is missing'

log 'installing frontend dependencies'
(cd web && bun install --frozen-lockfile)

log 'building default frontend'
(cd web/default && DISABLE_ESLINT_PLUGIN=true VITE_REACT_APP_VERSION="$(cat ../../VERSION)" bun run build)
[[ -f web/default/dist/index.html ]] || die 'frontend build did not produce dist/index.html'

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
short_sha="${COMMIT_SHA:0:12}"
release_dir="$REMOTE_FRONTEND_DIR/releases/${timestamp}-${short_sha}"
staging_dir="$REMOTE_FRONTEND_DIR/releases/.staging-${timestamp}-${short_sha}"
current_link="$REMOTE_FRONTEND_DIR/dist"

cleanup() {
  sudo -n rm -rf "$staging_dir"
}
trap cleanup EXIT

previous_target=""
legacy_dir=""
new_link=""
if [[ -L "$current_link" ]]; then
  previous_target="$(readlink "$current_link")"
fi

rollback() {
  if [[ -n "$previous_target" && -L "$current_link" ]]; then
    log 'deployment failed after publish; restoring previous frontend release'
    rollback_link="$REMOTE_FRONTEND_DIR/.dist-rollback-${timestamp}"
    sudo -n ln -s "$previous_target" "$rollback_link"
    sudo -n mv -Tf "$rollback_link" "$current_link"
    sudo -n nginx -t && sudo -n systemctl reload nginx || true
  elif [[ -n "$legacy_dir" && -d "$legacy_dir" ]]; then
    log 'deployment failed during first publish; restoring previous frontend directory'
    sudo -n rm -f "$current_link" "${new_link:-}"
    sudo -n mv "$legacy_dir" "$current_link"
    sudo -n nginx -t && sudo -n systemctl reload nginx || true
  fi
}
trap rollback ERR

log "staging release at $release_dir"
sudo -n install -d -m 0755 "$REMOTE_FRONTEND_DIR/releases"
sudo -n rm -rf "$staging_dir"
sudo -n install -d -m 0755 "$staging_dir"
sudo -n cp -a web/default/dist/. "$staging_dir/"
sudo -n mv "$staging_dir" "$release_dir"
trap - EXIT

if [[ -d "$current_link" && ! -L "$current_link" ]]; then
  legacy_dir="$REMOTE_FRONTEND_DIR/legacy-dist-${timestamp}"
  log "preserving existing dist directory at $legacy_dir"
  sudo -n mv "$current_link" "$legacy_dir"
fi

new_link="$REMOTE_FRONTEND_DIR/.dist-${timestamp}-${short_sha}"
sudo -n ln -s "$release_dir" "$new_link"
sudo -n mv -Tf "$new_link" "$current_link"

log 'validating nginx configuration'
sudo -n nginx -t
sudo -n systemctl reload nginx

log "checking $HEALTHCHECK_URL"
curl --fail --silent --show-error --location --max-time 20 "$HEALTHCHECK_URL" >/dev/null

trap - ERR

# Keep the last five releases. The current symlink and legacy migration copies
# are not matched by this cleanup pattern.
sudo -n find "$REMOTE_FRONTEND_DIR/releases" -mindepth 1 -maxdepth 1 -type d \
  -name '20??????T??????Z-*' -printf '%T@ %p\n' | sort -nr | tail -n +6 | cut -d' ' -f2- | xargs -r sudo -n rm -rf

log "deployment complete: $COMMIT_SHA"
REMOTE_SCRIPT

log 'deployment finished successfully'
