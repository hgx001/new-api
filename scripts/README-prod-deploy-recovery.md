# 生产事故恢复与安全部署方案（2026-11-02）

## 一、事故回顾

| 项 | 内容 |
|---|---|
| 时间 | 2026-11-02 19:49 发起构建 → 20:22 未结束 → 20:27 起全服务无响应 |
| 主机 | `lhins-0dhgo2od`（腾讯云轻量应用服务器，Ubuntu-Lowf，ap-guangzhou-6） |
| 规格 | **4 核 4 GB** —— 内存是瓶颈 |
| 现象 | ping 正常（rtt 3.8ms）；TCP 80/443/877/500/8080 可建连；**应用层全部无响应**（HTTP 000、SSH banner 超时） |
| 根因推断 | 在生产机上执行 `Dockerfile` 全量构建：`bun install --frozen-lockfile` + 两份 `bun run build` + `go build`。4GB 内存下 bun 构建内存溢出 / CPU 打满，触发 OOM 或 IO 抖动，用户态进程（sshd、new-api）全部饿死 |
| 处置 | 腾讯云 Lighthouse API 强制重启实例（RequestId `0cc3ad7b-df06-479a-8ac0-e06af4db031b`） |

## 二、新部署策略（关键变更）

**原则：生产机永不编译前端。**

| 环节 | 旧做法（出事） | 新做法 |
|---|---|---|
| 前端构建 | 生产机 `docker build` 内跑 bun | **本机**（16 核 / 259GB 可用）跑 `bun run build`，产出 `web/default/dist`、`web/classic/dist` |
| 产物传输 | 无 | `scp` 上传 dist 到生产机仓库工作区 |
| 镜像构建 | `docker build -f Dockerfile`（含前端） | `docker build -f Dockerfile.deploy`（**跳过前端**，只编 Go） |
| 后端编译 | 生产机 | 生产机（Go 编译内存占用远低于 bun，可接受） |
| 资源限制 | 无 | `docker build --memory=2g --cpus=2` + `nice` 降优先级 |

### 为什么用 Dockerfile.deploy

该文件已内置硬校验：`web/default/dist/index.html` 与 `web/classic/dist/index.html` 缺失或 ≤200 字节即**构建失败**。
（起因是 2026-11-02 两次白屏上线 —— 用占位文件糊过，镜像前端只有 16 字节，页面全白。）

## 三、恢复后执行顺序

1. **立即**：停掉可能残留的 build 进程；查 OOM 证据
   ```bash
   pkill -f "docker build" ; pkill -f "bun" ; pkill -f "rsbuild"
   dmesg -T | grep -iE "out of memory|oom|killed process" | tail -20
   free -m ; docker system df
   ```
2. **确认基线**：当前线上容器必须仍健康、版本仍为旧版
   ```bash
   docker ps -a --format '{{.Names}}\t{{.Image}}\t{{.Status}}'
   curl -s http://127.0.0.1:3000/api/status
   ```
3. **上传前端产物**（本机 → 生产机）
   ```bash
   cd /c/work/new-api-src
   tar czf /tmp/webdist.tgz web/default/dist web/classic/dist
   scp -P 877 /tmp/webdist.tgz ubuntu@119.29.253.97:/tmp/
   ```
4. **生产机解包 + 资源受限构建**
   ```bash
   ssh -p 877 ubuntu@119.29.253.97 '
     cd ~/new-api-src && tar xzf /tmp/webdist.tgz \
       && ls -l web/default/dist/index.html web/classic/dist/index.html \
     && nice -n 15 docker build --memory=2g --cpus=2 -f Dockerfile.deploy -t new-api-custom:latest .
   '
   ```
5. **替换容器**（旧容器 rename 保留，可回滚）
   ```bash
   ssh -p 877 ubuntu@119.29.253.97 'cd ~/new-api && sudo ./replace-container.sh new-api-custom:latest'
   ```
6. **健康检查**：`curl -s http://127.0.0.1:3000/api/status` 必须 `"success": true`
7. **数据库变更**（顺序不可颠倒：代码先上线，再改库）
   ```bash
   ssh -p 877 ubuntu@119.29.253.97 'docker exec postgres psql -U root -d new-api -f /tmp/exec.sql'
   ```
8. **只读验证**：跑 `/tmp/verify.sql` 与 `verify-prod-seedance25-huodong.sh`

## 四、约束

- **全程不发起任何真实视频生成请求**（验证脚本已设计为只读）
- 每次远程操作前先确认前一步成功，不做连续盲跑
- 构建产物大小先在生产机核对 `index.html` 字节数，拒绝占位文件

## 五、待用户拍板（与本次事故无关）

规则三（首尾帧）成本倒挂：活动版 30 秒 + 首尾帧命中 `wan3.0-720p-特惠`（¥0.20/秒），
成本 ¥6.20 > 售价 ¥4.50，每单亏 ¥1.70（约 -38%）。当前代码完整继承四条规则，未做特殊处理。
