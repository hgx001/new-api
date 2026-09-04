# Frontend deployment

`deploy-frontend.sh` is the production release entrypoint for the default
frontend. It implements the repository-pull flow:

```text
local clean commit -> push fork/main -> SSH -> fetch exact SHA ->
bun install --frozen-lockfile -> build -> atomic release switch -> nginx reload
```

## Prerequisites

The local machine needs Bash, Git, and OpenSSH. The server needs a checkout of
this repository at `DEPLOY_APP_DIR` (default `/home/ubuntu/new-api-src`), a Git remote that
can fetch the pushed repository, Bun, `flock`, `curl`, Git, NGINX, and systemd.
The deploy user also needs passwordless sudo for the narrowly scoped file and
NGINX commands used by the script. The production host key must already be in
the local SSH `known_hosts` file.

For a private repository, configure a read-only deploy key on the server. Do
not use SSH agent forwarding for the server's GitHub access.

## Usage

From the repository root:

```bash
# Preview without pushing or connecting to production
deploy/deploy-frontend.sh --dry-run

# Push the current clean HEAD and deploy it
deploy/deploy-frontend.sh

# Deploy an already-pushed commit without pushing again
deploy/deploy-frontend.sh --no-push --ref <full-commit-sha>
```

The default target is `ubuntu@119.29.253.97:877`. The local push remote defaults
to `fork`, while the server checkout remote defaults to `origin`. Override settings with
`DEPLOY_HOST`, `DEPLOY_PORT`, `DEPLOY_USER`, `DEPLOY_GIT_REMOTE`,
`DEPLOY_BRANCH`, `DEPLOY_SERVER_GIT_REMOTE`, `DEPLOY_APP_DIR`, `DEPLOY_FRONTEND_DIR`, and
`DEPLOY_HEALTHCHECK_URL`.

`DEPLOY_GIT_REMOTE` is the local remote to push. `DEPLOY_SERVER_GIT_REMOTE` is
the remote name inside the server checkout; set it separately when the local
remote is `fork` but the server checkout calls the same repository `origin`.

## Release layout and rollback

Builds are stored under `/opt/new-api/frontend/default/releases/<timestamp>-<sha>`
and the NGINX root remains `/opt/new-api/frontend/default/dist`. The script
never removes the live directory before a complete build is ready; it switches
the symlink atomically and retains previous releases for inspection.

To roll back manually, point `dist` at a known-good release using an atomic
temporary symlink, validate NGINX, and reload it:

```bash
cd /opt/new-api/frontend/default
sudo ln -s releases/<known-good-release> .dist-rollback
sudo mv -Tf .dist-rollback dist
sudo nginx -t && sudo systemctl reload nginx
```

Do not deploy with a dirty local or server checkout. The script deploys a
detached, exact commit SHA rather than running an unpinned `git pull`.
