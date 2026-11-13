#!/usr/bin/env bash
# 万相 wan2.7 免灵感值双模型（图生 / 编辑）生产零成本路由探针 —— T3。
#
# ⚠️⚠️ 2026-10-16 实测坑（务必读）：
# 本脚本最初用 bash 变量 + curl 直接发中文模型名，**全线 503 model_not_found**。
# 根因不是路由，而是 **Git Bash 把 UTF-8 的中文按 GBK(cp936) 交给了 curl**：
#   磁盘上脚本是正确的 UTF-8（`wan2.7-图生` = e5 9b be e7 94 9f），
#   但 curl 实际送出的是 GBK 字节（cd bc c9 fa），服务器按 UTF-8 解析成
#   `wan2.7-ͼ\ufffd\ufffd` → 分发层找不到渠道 → 503。
# → **凡是要把中文模型名送进 HTTP 请求，必须用 Python/Node 显式 UTF-8 编码。**
# 故本脚本只做壳，真正的探针在 _wan27_e2e.py（urllib + json.dumps(ensure_ascii=False)
# + .encode('utf-8')）。
#
# 纪律同 probe-manwu-dola-dual.sh：只用**必然被拦下的入参**，一条都不许真建单。
# 断言：used_quota / 非终态任务 / ArcReel 建单日志三项零变化。
set -uo pipefail
cd "$(dirname "$0")"
PY=$(command -v python || command -v python3)
exec "$PY" _wan27_e2e.py t3
