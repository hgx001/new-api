# 生产部署规范

**日期**: 2026-07-05  
**背景**: 之前前端通过 `//go:embed` 编译进 Go 二进制,每次改前端展示文本都需要完整构建 Docker 镜像。已通过 NGINX 反代 + 独立 dist 目录优化为前端独立部署。

## 1. 架构

### 当前架构 (Phase 1 完成)

```
用户 ──→ api.heibaidao.cn (443 HTTPS, Let's Encrypt)
          ↓
      NGINX (host, 1.24.0)
          │
          ├── /api/*  ──→ proxy_pass → 127.0.0.1:3000 (new-api 容器)
          ├── /mj/*   ──→ proxy_pass → 127.0.0.1:3000
          ├── /pg/*   ──→ proxy_pass → 127.0.0.1:3000
          │
          └── /*      ──→ root /opt/new-api/frontend/default/dist
                          (SPA: try_files $uri /index.html)
```

### 前端与服务分离

| 组件 | 部署位置 | 部署方式 | 更新方式 |
|---|---|---|---|
| 前端静态文件 (HTML/JS/CSS) | `/opt/new-api/frontend/{theme}/dist` | 仓库拉取后服务器构建 | `git push → deploy/deploy-frontend.sh → nginx reload` |
| Go 后端 (API) | Docker 容器 `new-api:3000` | Docker image | `docker build → docker load → 重启` |
| NGINX 反向代理 | host 原生包 | apt 安装 | `nginx -s reload` |

## 2. 目录结构

```
/opt/new-api/
├── frontend/
│   ├── default/dist/          # 默认主题前端
│   │   ├── index.html
│   │   ├── static/js/
│   │   └── static/css/
│   └── classic/dist/          # 经典主题前端(备用)
└── ...
```

## 3. 发布流程

### 3.1 前端更新 (仅改展示层)

适用于:修改 `i18n` 文案、展示逻辑、组件布局、模型展示名覆盖

```bash
# 工作区必须干净；默认 push 当前 HEAD 到 fork/main，然后 SSH 到生产机部署
deploy/deploy-frontend.sh

# 已经 push 的提交可跳过 push
deploy/deploy-frontend.sh --no-push --ref <commit-sha>
```

脚本在服务器执行以下步骤：

1. 使用远端部署锁，拒绝服务器 checkout 存在未提交修改。
2. `git fetch` 后 checkout 精确提交 SHA，避免服务器发生未预期 merge。
3. 在仓库根目录的 `web` workspace 执行 `bun install --frozen-lockfile`，再构建 `web/default`。
4. 将构建结果复制到 `/opt/new-api/frontend/default/releases/<timestamp>-<sha>`，通过临时符号链接原子切换 `dist`。
5. 执行 `nginx -t`、`systemctl reload nginx` 和 HTTPS 健康检查；失败时保留旧 release，不清空线上目录。

默认服务器源码目录为 `/opt/new-api`。如实际目录不同，使用 `DEPLOY_APP_DIR=/实际目录` 覆盖；服务器地址、端口、Git 远端、分支、前端目录和健康检查地址也都支持 `DEPLOY_*` 环境变量覆盖。完整参数见 `deploy/deploy-frontend.sh --help`。

**耗时**: 取决于服务器依赖安装和构建缓存；构建成功后只执行 NGINX reload，不重启后端容器。

### 3.2 后端更新 (Go 代码变更)

适用于:修改 API、middleware、relay 适配器、模型处理逻辑等

```bash
# Build Docker image
docker build -f Dockerfile.deploy -t new-api-custom:latest .
docker save -o new-api-custom.tar new-api-custom:latest

# 传输到生产
scp -P 877 new-api-custom.tar ubuntu@119.29.253.97:/tmp/

# 生产加载并重启
ssh -p 877 ubuntu@119.29.253.97 "
  docker load -i /tmp/new-api-custom.tar \
  && docker restart new-api"
```

**耗时**: ~2-3 min (build) + ~10s (传输 74MB) + ~5s (load + restart) ≈ 3 min

### 3.3 前后端同时更新

先走 3.1 更新前端,再走 3.2 更新后端。前端更新不需要重启容器,零停机。

## 4. NGINX 配置

### `api.heibaidao.cn.conf` (当前生效)

```nginx
server {
    server_name api.heibaidao.cn;
    charset utf-8;
    client_max_body_size 100M;

    access_log /var/log/nginx/api_heibaidao_access.log;
    error_log /var/log/nginx/api_heibaidao_error.log;

    # 前端 SPA
    location / {
        root /opt/new-api/frontend/default/dist;
        try_files $uri $uri/ /index.html;
        # 静态资源长缓存
        location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff2?|ttf|eot|webp)$ {
            expires 1y;
            add_header Cache-Control "public, immutable";
        }
    }

    # API 代理
    location /api/ {
        proxy_pass http://127.0.0.1:3000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /mj/ {
        proxy_pass http://127.0.0.1:3000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /pg/ {
        proxy_pass http://127.0.0.1:3000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    listen 443 ssl;
    ssl_certificate /etc/letsencrypt/live/api.heibaidao.cn/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/api.heibaidao.cn/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;
}

server {
    if ($host = api.heibaidao.cn) { return 301 https://$host$request_uri; }
    listen 80;
    server_name api.heibaidao.cn;
    return 404;
}
```

### 配置管理

- NGINX 配置文件的更新应通过 SSH 手动执行
- 每次修改前备份: `cp conf conf.bak.$(date +%Y%m%d-%H%M%S)`
- 修改后必须 `nginx -t` 验证语法
- 确认无误后 `systemctl reload nginx` (零停机)

## 5. 回滚方案

### 前端回滚

```bash
# 查看已有 release
ls -la /opt/new-api/frontend/default/releases

# 原子切换到已知正常的 release
cd /opt/new-api/frontend/default
sudo ln -s releases/<known-good-release> .dist-rollback
sudo mv -Tf .dist-rollback dist
sudo nginx -t && sudo systemctl reload nginx
```

仓库式部署的完整用法、环境变量和前置条件见 `deploy/README.md`。回滚不需要重新构建或重新拉取代码。

### 后端回滚

```bash
# 恢复到上一个版本的 Docker 镜像
docker load -i /tmp/new-api-custom.previous.tar
docker restart new-api
```

若无保留旧 tar,可从 Docker 历史缓存恢复:
```bash
docker images | grep new-api-custom
docker tag <previous-image-id> new-api-custom:latest
docker restart new-api
```

## 6. 模型展示名覆盖配置

维护在 `web/default/src/features/pricing/lib/model-helpers.ts`:

```typescript
const MODEL_DISPLAY_NAME_OVERRIDES: Record<string, string> = {
  'kimi-for-coding': 'kimi-k2.7',
}
```

**规则**:
- key = 数据库 `channels.models` 中的模型标识符 (API 调用的实际名称)
- value = 模型广场 UI 展示名
- 名称变更只需改这个 map → 重新部署前端 (40 秒)
- 不需要动数据库、后端、Docker 镜像

## 7. 安全与性能

- **HTTPS**: Let's Encrypt 自动续期(Certbot),已在 NGINX 配置中正确加载
- **静态缓存**: JS/CSS/字体等文件设置 `expires 1y; Cache-Control: public, immutable`
- **文件大小上限**: `client_max_body_size 100M` (已有,用于大文件上传)
- **CORS**: 由 Go 后端处理,NGINX 不需要额外配置

## 8. 未来可改进方向

1. **CI/CD**: GitHub Actions 自动构建前端、推送到 CDN 或直传服务器
2. **CDN**: 前端静态文件走 CDN (如 Cloudflare),减轻源站压力
3. **多版本管理**: `/opt/new-api/frontend/default/releases/v1, v2...` 方便快速切换版本
4. **SSL 优化**: 配置 HTTP/2,OCSP Stapling,HSTS
5. **监控**: NGINX 请求日志接入告警;500 错误率告警
6. **本地审批流**: 前端构建前后在本地走 `verification-before-completion` skill 验证
