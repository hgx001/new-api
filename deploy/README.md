# 部署总览（唯一权威文档）

生产环境：`api.heibaidao.cn`。前端与后端是两条独立链路，入口都在本目录：

| | 前端 | 后端 |
|---|---|---|
| 本地入口 | `deploy/deploy-frontend.sh` | `deploy/deploy-backend.sh <sha>` |
| 服务器侧执行体 | 脚本内嵌远端段 | `deploy/backend-remote.sh`（SSH 管道送入，勿在本地直接运行） |
| 产物 | 静态 dist 原子切换 + nginx reload | Docker 镜像 + 容器重建 |
| 中断 | 零停机，不重启后端 | 容器重启数秒 |

## 前端部署

`deploy-frontend.sh` is the production release entrypoint for the default
frontend. It implements the repository-pull flow:

```text
local clean commit -> push fork/main -> SSH -> fetch exact SHA ->
bun install --frozen-lockfile -> build -> atomic release switch -> nginx reload
```

### Prerequisites

The local machine needs Bash, Git, and OpenSSH. The server needs a checkout of
this repository at `DEPLOY_APP_DIR` (default `/home/ubuntu/new-api-src`), a Git remote that
can fetch the pushed repository, Bun, `flock`, `curl`, Git, NGINX, and systemd.
The deploy user also needs passwordless sudo for the narrowly scoped file and
NGINX commands used by the script. The production host key must already be in
the local SSH `known_hosts` file.

For a private repository, configure a read-only deploy key on the server. Do
not use SSH agent forwarding for the server's GitHub access.

### Usage

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

## 后端部署

本地一条命令（推荐）：

```bash
deploy/deploy-backend.sh <short-sha>        # SSH 到生产服务器执行 backend-remote.sh
deploy/deploy-backend.sh --dry-run <sha>    # 只打印将执行的命令
```

已在服务器上时等价手工：`bash deploy/backend-remote.sh <short-sha>`

机制（`backend-remote.sh`）：

1. SHA 前缀断言——checkout 后比对完整 SHA，防止用错误代码构建；
2. `git fetch origin main`——失败但本地已有该 commit 时继续（生产机连不上 github），否则报错退出；
3. 服务器上 `docker build -f Dockerfile.deploy`（**该文件已入库，是构建的必要前提**）；
4. 停旧容器改名 `new-api-prev-<ts>`，env 备份到 `/tmp/newapi-<ts>.env`（mode 600）；
5. 裸 `docker run` 按原参数重建 `new-api`（**生产不是 compose 管理**）；
6. `/api/status` 健康检查，24×5s；
7. 只保留最近 3 个旧容器。

### 已知约束：服务器连不上 github.com:443

新提交常规走 bundle 直传，随后 fetch fallback 生效：

```bash
# 本地（Git Bash）：把待发 commit 打包（<base> 为服务器已有的基点，可省略打全量）
git bundle create newapi-<sha>.bundle <full-sha> ^<base>
scp -P 877 newapi-<sha>.bundle ubuntu@119.29.253.97:/tmp/

# 服务器：导入 bundle，之后正常执行部署脚本
cd /home/ubuntu/new-api-src && git fetch /tmp/newapi-<sha>.bundle
```

## 回滚

### 前端回滚

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

The deployer keeps the five newest successful releases. The server checkout is
owned by the deploy user, while the frontend release directory is writable by
that user in the current production setup. If ownership differs, grant only
the exact file operations used by the script plus `nginx -t` and
`systemctl reload nginx`; do not grant an unrestricted sudo rule for a shared
deployment account.

### 后端回滚

旧容器 `new-api-prev-<ts>` 保留了旧镜像与环境，**不能直接 `docker start`**（3000 端口被新容器占用），按此序列：

```bash
docker stop new-api && docker rm new-api
docker rename new-api-prev-<ts> new-api
docker start new-api
```

## 通用约束

- 本地与服务器 checkout 都必须干净；脚本部署的是 detached 精确 SHA，不是 unpinned `git pull`。
- 两个脚本均支持 `DEPLOY_HOST` / `DEPLOY_PORT` / `DEPLOY_APP_DIR` 环境变量覆盖（同风格）。
- 历史规范存档：`docs/superpowers/specs/2026-07-05-production-deployment-spec.md`（以本文件为准）。
