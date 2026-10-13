# 部署体系梳理与改进设计

**日期**: 2026-10-13
**状态**: v2 勘误版，已实施（P0-B 已定：选项 1 fetch fallback；用户委托决策）
**范围**: 两个 P0 修复 + 脚本位置迁移 + 文档纠偏；不执行任何真实部署

## 1. 背景与问题清单

~~当前生产部署实际可用~~（v1 前提错误，实测推翻）。**实测结论（2026-10-13）：前后端两个部署脚本在当前生产服务器上均不能开箱跑通**，今天的两次发版均靠手工绕过完成。问题清单：

| # | 问题 | 后果 |
|---|---|---|
| **P0-A** | `Dockerfile.deploy` 被 `.git/info/exclude:17` 排除、从未入库；服务器 checkout 没有它，脚本 `docker build -f Dockerfile.deploy` 必然报 `no such dockerfile` | 后端脚本历史上从未完整成功过（服务器 prev 容器数今天 11:47 scp 前为 0）；今天发版靠手工 scp 补文件 |
| **P0-B** | `deploy-frontend.sh` L160 `git fetch --prune` 无 fallback，而生产机连不上 github.com:443，`set -Eeuo pipefail` 下直接终止 | 前端脚本同样跑不通；今天发版靠 bundle 直传绕过 |
| P1 | 后端部署脚本 `scripts/deploy-new-api.sh` 混在 80+ 个一次性探针/退款脚本中 | 按 `deploy/` 目录找部署入口只能找到前端一半 |
| P2 | 后端脚本需在服务器上执行（内部 `cd /home/ubuntu/new-api-src`），前端脚本在本地执行，方向相反但无任何文档并排说明 | 本地跑后端脚本报路径不存在 |
| P3 | spec 3.2 写 `docker compose up -d --force-recreate`，实际脚本是裸 `docker run` + env-file | 照 spec 操作走错路径 |
| P4 | spec 第 5 节回滚写 `docker load -i tar`，脚本从不导出 tar | 照文档操作发现 tar 不存在 |
| P5 | 部署规范分散在 AGENTS.md / CLAUDE.md / deploy/README.md / spec 四处，互有重复与矛盾 | 口径不一，需要人工拼碎片 |
| P6 | `scripts/` 无分类索引，危险脚本（会真实扣费的 probe/refund）与普通脚本无标识 | 误触风险 |
| P7 | 仓库 `docker-compose.yml` 是上游模板（公开 3000、示例密码），spec 却引用"生产 compose 文件" | 误以为生产走 compose |

## 2. 目标与原则

四个原则：

1. **入口对称** —— 前后端部署入口都在 `deploy/`，命名同构；
2. **单一事实源** —— `deploy/README.md` 是唯一权威文档，其余只引用不重复；
3. **文档与实现一致** —— 不写没做过的事，失实命令必须修掉；
4. **危险隔离** —— 部署脚本与付费探针脚本物理分家、分类标注。

**明确不做**（防过度工程）：不上 CI/CD、不合并前后端为统一框架脚本、不动 `deploy-frontend.sh` 的原子切换/发布逻辑（P0-B 仅在 fetch 处加 fallback，属修复非重构）、不做真实部署验证。

## 3. 改动明细

### 阶段〇：P0 修复（先于一切迁移，否则迁移得到的只是搬了家的坏脚本）

#### 3.0a P0-A：`Dockerfile.deploy` 入库

- `.git/info/exclude` 是**本机文件、不随仓库传播**，这是服务器永远缺该文件的根因；先删 exclude 第 17 行 `Dockerfile.deploy`（`Dockerfile.full` 维持排除，不在本方案范围）；
- `git add Dockerfile.deploy` 入库。入库前已核对：本地与服务器 11:47 手工 scp 版本 md5 一致（`23631a1db27f8573151e34717ba32631`），即入库的就是今天实测跑通的版本；
- 之后服务器 checkout（fetch 或 bundle 直传同步）天然带上该文件，无需再 scp。

#### 3.0b P0-B：前端 fetch fallback（已确认：选项 1）

用户委托决策，选**选项 1**：在 `deploy-frontend.sh` L160 加与后端脚本同款的 fallback（后端已用同一模式扛过同一问题，约 4 行）。理由：选项 2 每次发版都依赖人工记得 bundle 直传，违背「傻瓜式操作」；fallback 只在「fetch 失败但本地已有该 commit」时改变行为，失败路径依旧 fail-loud。

### 阶段一：入口收敛

#### 3.1 迁移后端脚本

```
git mv scripts/deploy-new-api.sh deploy/backend-remote.sh
```

- 逻辑改动仅限两处（SHA 断言、容器保留、健康检查实测有效；但**构建步骤依赖 3.0a 入库的 Dockerfile.deploy，历史上从未完整验证过**——今天 11:52 那次成功 = 脚本 + 手工 scp 的文件，不等于脚本自洽可跑），文件头加注释：

```bash
#!/usr/bin/env bash
# ⚠️ 在生产服务器上执行（内部固定 cd /home/ubuntu/new-api-src），勿在本地直接运行。
# 本地入口请用 deploy/deploy-backend.sh，它通过 SSH 管道把本脚本送到服务器执行。
# 用法（服务器上）: bash deploy/backend-remote.sh <short-sha>
```

- 唯一允许的逻辑微调（1 行）：硬编码路径改为环境变量缺省，与前端 `DEPLOY_APP_DIR` 同名同风格：

```bash
APP_DIR="${DEPLOY_APP_DIR:-/home/ubuntu/new-api-src}"
cd "$APP_DIR"
```

#### 3.2 新增本地 wrapper `deploy/deploy-backend.sh`

职责：本地一条命令完成后端部署。用 `ssh 'bash -s' < 本地脚本` 管道执行——脚本本身不需要预先存在于服务器 checkout，**顺带消除迁移后首次使用的"先有鸡先有蛋"问题**。

```bash
#!/usr/bin/env bash
# new-api 后端部署本地入口：SSH 到生产服务器，管道送入 backend-remote.sh 执行
# 用法: deploy/deploy-backend.sh <short-sha>
#       deploy/deploy-backend.sh --dry-run <short-sha>
set -euo pipefail

if [ "${1:-}" = "--dry-run" ]; then DRY=1; shift; fi
TARGET="${1:?usage: deploy-backend.sh [--dry-run] <short-sha>}"

# 边界校验：SHA 只允许十六进制，防止注入进远程命令串
if ! [[ "$TARGET" =~ ^[0-9a-f]{7,40}$ ]]; then
  echo "ABORT: '$TARGET' 不是合法的 git SHA 前缀" >&2; exit 1
fi

HOST="${DEPLOY_HOST:-ubuntu@119.29.253.97}"
PORT="${DEPLOY_PORT:-877}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

CMD="bash -s $TARGET"
if [ "${DRY:-0}" = 1 ]; then
  echo "DRY-RUN: ssh -p $PORT $HOST \"$CMD\" < $SCRIPT_DIR/backend-remote.sh"
  exit 0
fi
exec ssh -p "$PORT" "$HOST" "$CMD" < "$SCRIPT_DIR/backend-remote.sh"
```

要点：
- 远端退出码经 `exec ssh` 原样透传，失败即失败；
- `DEPLOY_HOST` / `DEPLOY_PORT` 环境变量与前端脚本同风格可覆盖。

#### 3.3 新增 `scripts/README.md` 分类索引

按前缀分组列表，四类：

| 类别 | 前缀/代表 | 说明 |
|---|---|---|
| deploy | （已迁走，指向 `deploy/`） | 部署入口统一在 `deploy/` |
| ⚠️ 危险·会真实扣费 | `probe-*`、`refund-*`、`run-erchun-*`、`smoke-*`、`cleanup-*` | 须用户明令才可跑；跑完按规写 `OPERATOR-COST-ACCOUNTING` 台账 |
| audit/只读 | `audit-*`、`coverage-*`、`stat-*`、`erchun-readonly-*`、`diag-*`、`verify-*` | 只读查询与核对 |
| one-off | 其余 dated SQL/脚本 | 历史操作留痕，勿重复执行 |

### 阶段二：文档单一事实源

#### 3.4 `deploy/README.md` 扩为部署总览（权威文档）

结构：前端部署（现内容保留）→ 后端部署（新增）→ 回滚（修正后）。

后端一章核心内容：

```
本地:  deploy/deploy-backend.sh <sha>          # 推荐，SSH 管道执行
服务器: bash deploy/backend-remote.sh <sha>    # 已在服务器时
机制:  SHA 前缀断言 → 服务器 docker build -f Dockerfile.deploy
       → 停旧容器改名 new-api-prev-<ts> → 裸 docker run 重建（env-file 复制旧容器环境）
       → /api/status 健康检查 → 保留最近 3 个旧容器
```

回滚一章用**修正后的正确序列**（旧容器直接 `docker start` 会因 3000 端口被新容器占用而失败）：

```bash
docker stop new-api && docker rm new-api
docker rename new-api-prev-<ts> new-api
docker start new-api
```

并注明：env 文件留在服务器 `/tmp/newapi-<ts>.env`（mode 600），回滚用不到它（旧容器自带环境）。

#### 3.5 spec 加头注 + 修两处失实命令

`docs/superpowers/specs/2026-07-05-production-deployment-spec.md`：

- 文件头加：`> ⚠️ 历史文档（2026-07-05）。现行部署流程以 deploy/README.md 为准，本文仅存档。`
- 修 3.2 节：`docker compose up -d --force-recreate` → 指向 `deploy/backend-remote.sh` 的真实流程（裸 `docker run` + env-file）；
- 修第 5 节：`docker load -i tar` 回滚 → 改为 3.4 的三步序列。

只修命令防照抄，不往 spec 堆新内容。

#### 3.6 全量替换引用 + 瘦身

精确 grep `deploy-new-api` 确认仅 **3 个文件**引用（v1 的"6 个文件"系检索模式混入 `deploy-frontend` 所致，已纠正）：AGENTS.md（2 处）、本 spec（5 处，重写时自然消解）、脚本自身（1 处）。**CLAUDE.md 无引用**。迁移后逐一替换：

- [AGENTS.md](../../../AGENTS.md)：两处 `scripts/deploy-new-api.sh <sha>` → `deploy/deploy-backend.sh <sha>`；Deployment Rules 补机制描述；
- [CLAUDE.md](../../../CLAUDE.md)：Deployment 小节补一行脚本名与 `deploy/README.md` 指引（属文档补全，非引用替换）。

#### 3.7 compose 模板加警示

`docker-compose.yml` 与 `docker-compose.dev.yml` 文件头各加一行：

```
# ⚠️ 上游模板，仅供本地体验。生产不使用本文件（生产为 deploy/backend-remote.sh 裸 docker run）。
```

### 阶段三：验证（不触生产）

| 项 | 命令/手段 | 通过标准 |
|---|---|---|
| P0-A 入库 | `git ls-files \| grep '^Dockerfile\.deploy$'`；`git check-ignore Dockerfile.deploy` | 前者非空；后者无输出 |
| 脚本语法 | `bash -n deploy/backend-remote.sh deploy/deploy-backend.sh` | 零报错 |
| wrapper 行为 | `deploy/deploy-backend.sh --dry-run <sha>` | 打印正确命令、退出码 0；非法 SHA（如 `foo;rm`）被拒 |
| 迁移纯净度 | `git diff` 后端脚本 | 仅头注释 + APP_DIR 两处改动 |
| 引用清零 | grep `deploy-new-api.sh` | 仓库内零残留 |
| 失实命令清零 | grep `force-recreate`、`docker load -i` | 零残留 |
| 前端 fallback（若选选项 1） | `bash -n deploy-frontend.sh` + 通读改动 | 语法通过；fallback 分支逻辑与后端脚本一致 |

⚠️ `deploy-frontend.sh --dry-run` 只在本地打印计划，**不覆盖服务器侧 fetch 路径**，不能作为 P0-B 修复的验证依据。

**生产侧验证项**（留待下一次真实发版，本方案不执行）：后端构建不再出现 `no such dockerfile`；前端 fetch 失败时 fallback 生效（或 bundle 约束生效）。

## 4. 风险与缓解

| 风险 | 缓解 |
|---|---|
| `.git/info/exclude` 仅本机生效、不随仓库传播 → 服务器 checkout 永远缺 `Dockerfile.deploy` | P0-A：文件入库；入库版本已与服务器实测跑通版 md5 核对一致 |
| 迁移后有人仍用旧路径 | 全量替换 + grep 清零验证；`scripts/README.md` 顶部指路 |
| wrapper 注入 | 本地十六进制白名单校验 SHA |
| 文档再漂移 | 单一事实源原则：其余文档只引用不复制流程；spec 头注声明存档地位 |
| 触碰生产 | 本方案全部改动不执行任何真实部署；验证仅 --dry-run 与静态检查 |

## 5. 实施顺序

1. 阶段〇：P0-A 入库 + P0-B（按确认结果二选一）——**不先修这两项，后面全是给坏脚本搬家**；
2. 阶段一（git mv + wrapper + scripts/README）→ 阶段三验证通过；
3. 阶段二文档修改（建议分三个 commit：P0 修复 / 脚本迁移 / 文档纠偏）；
4. 收尾 grep 复核；
5. 下一次真实发版时完成生产侧验证项。

## 附录：实测证据（2026-10-13）

| 事实 | 证据 |
|---|---|
| `Dockerfile.deploy` 未入库 | `.git/info/exclude:17` 命中；`git ls-files "*Dockerfile*"` 仅 Dockerfile、Dockerfile.dev；`git check-ignore -v Dockerfile.deploy` 指向 exclude:17 |
| 后端脚本首次完整成功发生在今天 | 服务器唯一 prev 容器 `new-api-prev-20261013115219`（11:52 创建），紧随 11:47 手工 scp `Dockerfile.deploy`（1245 字节）之后；此前 prev 容器数为 0，27 小时前那次发版系手工操作 |
| 本地与服务器文件一致 | 双侧 md5 = `23631a1db27f8573151e34717ba32631`，入库即实测跑通版 |
| 前端 fetch 无 fallback | `deploy-frontend.sh` L160 裸 `git fetch --prune`，L132 `set -Eeuo pipefail` 下失败即终止；后端脚本头注「github 偶尔连不上（本次就遇到）」从侧面佐证服务器 github:443 不通 |
| 引用数实为 3 文件 8 处 | 精确 grep `deploy-new-api`：AGENTS.md ×2（L246、L268）、本 spec ×5、脚本自身 ×1；CLAUDE.md ×0 |
