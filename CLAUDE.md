# CLAUDE.md — Project Conventions for new-api

@AGENTS.md

## Claude Code

- Follow the shared project instructions imported from `AGENTS.md`.

## Deployment

### 生产环境

- **域名**: `api.heibaidao.cn` (HTTPS, Let's Encrypt)
- **SSH**: `ssh -p 877 ubuntu@119.29.253.97`
- **NGINX**: Host 原生 1.24.0,配置在 `/etc/nginx/sites-available/api.heibaidao.cn.conf`
- **Go 后端**: Docker 容器 `new-api`,监听 `127.0.0.1:3000`
- **前端文件(默认)**: `/opt/new-api/frontend/default/dist` (NGINX 直接读取)
- **前端文件(经典)**: `/opt/new-api/frontend/classic/dist` (备用)
- **SSL 证书**: Let's Encrypt,覆盖 `api.heibaidao.cn`(API)、`www.heibaidao.cn`(H5商城)

### 前端展示层更新

用于 i18n 文案、模型展示名覆盖、UI 组件改动。采用“本地 push，服务器按提交拉取并构建”的发布流程，不需要重启后端：

```bash
# 工作区必须干净；脚本默认推送当前提交到 fork/main
deploy/deploy-frontend.sh

# 只检查部署计划，不 push、不连接服务器
deploy/deploy-frontend.sh --dry-run

# 指定已经推送到远端的提交，跳过脚本 push
deploy/deploy-frontend.sh --no-push --ref <commit-sha>
```

脚本会在服务器 `/home/ubuntu/new-api-src` 拉取精确提交，在服务器执行 `bun install --frozen-lockfile` 和 `bun run build`，将产物发布到带时间戳的 release 目录，原子切换 `/opt/new-api/frontend/default/dist`，执行 `nginx -t`、reload 和 HTTPS 健康检查。服务器源码目录、分支、远程和域名可通过 `DEPLOY_*` 环境变量覆盖；具体实现见 `deploy/deploy-frontend.sh`。

### Go 后端更新 (远程构建 Docker)

后端发布默认通过 SSH 在生产服务器上构建 Docker 镜像，再重建 `new-api` 容器；本地不需要启动 Docker Desktop，也不需要在本地执行 `docker build` 或 `docker save`。发布脚本必须将源码精确固定到待发布 commit，完成远程构建、容器重建、健康检查和业务接口验证。

`deploy/deploy-frontend.sh` 仅用于前端静态文件发布，不能代替后端发布流程。后端变更不得按“只发布前端”处理。

### 模型广场展示名覆盖

在 `web/default/src/features/pricing/lib/model-helpers.ts` 中维护:

```typescript
const MODEL_DISPLAY_NAME_OVERRIDES: Record<string, string> = {
  'kimi-for-coding': 'kimi-k2.7',
}
```

key = 数据库 channels.models 中的模型标识符 (API 实际名称),value = UI 展示名。
改完后执行前端部署流程即可,不需要动数据库和 Docker 镜像。

### 完整文档

`docs/superpowers/specs/2026-07-05-production-deployment-spec.md`
