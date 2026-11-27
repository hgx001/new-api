# 部署总览（唯一权威文档）

生产环境：`api.heibaidao.cn`。前端与后端是两条独立链路，入口都在本目录。

安全探针、误建任务处理与OAuth运维见 [生产运维补充](../docs/agent-guides/production-operations.md)；该指南不替代本文的发布与回滚流程。

后端 Gin 默认信任所有代理传来的客户端 IP 头不安全。应用默认仅信任 Docker bridge 网关 `172.18.0.1`（生产 nginx 容器连接后端时的代理地址），客户端 IP 只取该代理传递的 `X-Real-IP`/`X-Forwarded-For`（用于限流与日志）。若生产网络结构变更，必须通过容器环境变量 `TRUSTED_PROXIES` 配置实际代理的逗号分隔 IP/CIDR；应配置代理在后端容器看来使用的源地址，不要配置任意客户端网段或 `0.0.0.0/0`。

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

1. 按当前 `DEPLOY_MIN_FREE_MB` 阈值预检磁盘；SHA 前缀断言——checkout 后比对完整 SHA，防止用错误代码构建；
2. `git fetch origin main`——失败但本地已有该 commit 时继续（生产机连不上 github），否则报错退出；
3. 服务器上**带资源上限**地 `docker build -f Dockerfile.deploy`：默认 `CPUQuota=150%`、`MemoryMax=1536M`、`MemorySwapMax=1024M`（用 `systemd-run --scope` 套 cgroup），保留构建日志并处理磁盘不足；
   - 2026-11-27 事故：无限制的 Go 全量编译把 4 核/3.7G 的生产机 CPU 打满，SSH 与站点一起失联、整机重启。加限制后超限只会让**构建失败**，不会拖垮线上。
   - 可用 `DEPLOY_BUILD_CPU_QUOTA` / `DEPLOY_BUILD_MEMORY_MAX` / `DEPLOY_BUILD_MEMORY_SWAP_MAX` 调整；`DEPLOY_MIN_BUILD_FREE_MB`（默认 4096）是构建前的最低磁盘余量，不足时先回收构建缓存。
4. **停旧容器前**导出 `/tmp/newapi-<ts>.env`（mode 600）；达到脚本最小字节要求且包含 `SQL_DSN`、`SESSION_SECRET`、`CRYPTO_SECRET` 后，才把旧容器改名为 `new-api-prev-<ts>`；空/缺项快照直接中止；
5. 裸 `docker run` 按原参数重建 `new-api`（**生产不是 compose 管理**）；
6. `/api/status` 必须同时 `success:true`、`setup:true`，新容器仍有 `SQL_DSN` 且日志未回落 SQLite；任一不满足自动回滚，不能把空库的 success 当健康；
7. 只保留最近 3 个旧容器。

### 已知约束：服务器连不上 github.com:443

新提交常规走 bundle 直传，随后 fetch fallback 生效：

```bash
# 本地（Git Bash）：把待发 commit 打包（<base> 为服务器已有的基点）
git bundle create newapi-<sha>.bundle refs/heads/main ^<base>
scp -P 877 newapi-<sha>.bundle ubuntu@119.29.253.97:/tmp/

# 服务器：导入 bundle，之后正常执行部署脚本
cd /home/ubuntu/new-api-src && git fetch /tmp/newapi-<sha>.bundle refs/heads/main:refs/remotes/bundle/main
```

两点必须注意，否则会卡住：

- **bundle 的第一个参数必须是 ref 名，不能只给裸 SHA。** git 2.55 对
  `git bundle create x.bundle <sha> ^<base>` 会直接报
  `fatal: Refusing to create empty bundle`（2026-11-01 在 Windows 上实测）。
  写成 `refs/heads/main ^<base>` 就正常，增量包通常只有几 KB。
- **服务器侧 fetch 要显式写 ref 映射。** 不带 ref 时 git 会去找远端 `HEAD`，
  而 bundle 里没有 HEAD，报 `couldn't find remote ref HEAD`。目标用
  `refs/remotes/bundle/main` 而不是 `refs/heads/main`，避免动到 checkout 的分支。

## 老机（api.heibaidao.cn）：只 load 镜像，不构建（2026-11-27 起）

老机根分区 40G、业务数据已占 33G，**`docker build` 会把根分区写满**：2026-11-27 实测构建到 96%，
11:00:08 全机写失败停摆（`rsyslogd: No space left on device` 刷屏），22 分钟后才重启回来，
`new-api` 容器反复 `Exited (255)`，靠旧容器顶着 3000 端口才没对外中断。

构建统一放在**新机**（或本地），老机只负责 load 镜像 + 换容器：

```bash
# 1) 新机照常构建并上线（唯一构建机）
DEPLOY_HOST=newapi DEPLOY_PORT=22 deploy/deploy-backend.sh <sha>

# 2) 新机导出镜像（实测 250MB 镜像 → 76MB gz）
ssh newapi 'sudo docker save new-api-custom:latest | gzip -1' > /tmp/newapi-<sha>.tar.gz

# 3) 传到老机，两端 md5 一致再 load
scp -P 877 /tmp/newapi-<sha>.tar.gz ubuntu@119.29.253.97:/tmp/
ssh -p 877 ubuntu@119.29.253.97 'md5sum /tmp/newapi-<sha>.tar.gz; \
  sudo docker load -i /tmp/newapi-<sha>.tar.gz; \
  sudo docker images --format "{{.Repository}}:{{.Tag}} {{.ID}}" | grep new-api-custom'

# 4) 换容器：env 必须从**当前正在跑的那个**容器取
ssh -p 877 ubuntu@119.29.253.97 'bash -s' <<'EOS'
set -euo pipefail
cd /home/ubuntu/new-api-src && git checkout --detach <sha>
LIVE=$(sudo docker ps --format '{{.Names}}' | grep -E '^new-api' | head -1)
sudo docker inspect "$LIVE" --format '{{range .Config.Env}}{{println .}}{{end}}' > /tmp/newapi-swap.env
sudo chmod 600 /tmp/newapi-swap.env
TS=$(date +%Y%m%d%H%M%S)
sudo docker stop "$LIVE"; sudo docker rename "$LIVE" "new-api-prev-$TS"
sudo docker run -d --name new-api --restart always -p 127.0.0.1:3000:3000 \
  -v /home/ubuntu/new-api/data:/data -v /home/ubuntu/new-api/logs:/app/logs \
  --network new-api_new-api-network --env-file /tmp/newapi-swap.env \
  new-api-custom:latest --log-dir /app/logs
EOS

# 5) 验收：两台二进制 md5 必须一致（构建机不同也能证明同一个二进制）
sudo docker exec new-api md5sum /new-api      # 两台各跑一次
```

- **禁止**在老机跑 `docker build` / `docker compose build`，也禁止任何会把构建缓存写到根分区的操作。
  `/home/ubuntu/deploy-new-api.sh` 里那条 `docker build` 就是这次事故的直接原因，别再走它。
- **前端也一样**：`deploy/deploy-frontend.sh` 是在**目标机**上 `bun install + bun run build`（临时占用约 1G），
  老机发前端前同样要过 5G 门槛；空间不够就先清 `node_modules`/旧 release，或先在新机发完再单独传 `releases/<ts>-<sha>` 到老机的 `/opt/new-api/frontend/default/releases/` 后指软链。
- 老机换容器前先 `df -h /`，**可用空间 < 5G 不许动手**；不足就清构建残留
  （`docker builder prune`、旧 `new-api-prev-*`、旧 `new-api-custom:<sha>` tag），不要靠重启腾空间。
- 传镜像流程绕过了脚本的 SHA 断言，所以 **`git rev-parse --short HEAD` + 二进制 md5 两样都要人工核对**。
- 换容器后如健康检查不通过：`docker rm -f new-api` → `docker rename new-api-prev-<ts> new-api` → `docker start new-api`（见下方「后端回滚」）。

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
