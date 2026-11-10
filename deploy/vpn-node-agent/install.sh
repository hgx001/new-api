#!/usr/bin/env bash
# VPN 节点侧安装脚本（在节点上以 root 运行，可重复执行）
#
# 做的事：
#   1. 装 agent 到 /usr/local/bin/vpn-node-agent，写 600 权限的环境文件
#   2. 装 systemd service + timer（每分钟跑一轮）
#   3. 把 sing-box 日志级别从 warning 提到 info（不提升就采集不到连接源 IP）
#   4. 首次 dry-run，打印将要发生的配置变更，不落盘
#
# 用法：
#   bash install.sh --token <AGENT_TOKEN> [--api-base https://api.heibaidao.cn] \
#                   [--inbound-tag jp-vless-in] [--skip-log-level] [--force-env]
set -euo pipefail

API_BASE="https://api.heibaidao.cn"
INBOUND_TAG="jp-vless-in"
INBOUND_PORT="8443"
SERVICE="sing-box"
CONFIG="/etc/sing-box/config.json"
LOG_FILE="/var/log/sing-box/sing-box.log"
ENV_DIR="/etc/vpn-node-agent"
ENV_FILE="${ENV_DIR}/agent.env"
BIN="/usr/local/bin/vpn-node-agent"
SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOKEN=""
FORCE_ENV=0
SKIP_LOG_LEVEL=0

die() {
  echo "错误: $*" >&2
  exit 1
}

usage() {
  sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --token) TOKEN="$2"; shift 2 ;;
    --api-base) API_BASE="$2"; shift 2 ;;
    --inbound-tag) INBOUND_TAG="$2"; shift 2 ;;
    --inbound-port) INBOUND_PORT="$2"; shift 2 ;;
    --skip-log-level) SKIP_LOG_LEVEL=1; shift ;;
    --force-env) FORCE_ENV=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "未知参数 $1（--help 查看用法）" ;;
  esac
done

[[ "$(id -u)" -eq 0 ]] || die "需要 root 权限"
[[ -n "$TOKEN" ]] || die "--token 必填（与网站侧 VPN_NODE_AGENT_TOKEN 保持一致）"
command -v python3 >/dev/null || die "缺少 python3"
command -v sing-box >/dev/null || die "缺少 sing-box"

install -d -m 700 "$ENV_DIR"
install -d -m 700 /var/lib/vpn-node-agent

if [[ ! -f "$ENV_FILE" || "$FORCE_ENV" -eq 1 ]]; then
  umask 077
  cat > "$ENV_FILE" <<EOF
# 由 install.sh 生成，权限 600。改完执行 systemctl start vpn-node-agent.service 立即生效。
VPN_API_BASE=${API_BASE}
VPN_AGENT_TOKEN=${TOKEN}
VPN_INBOUND_TAG=${INBOUND_TAG}
VPN_INBOUND_PORT=${INBOUND_PORT}
VPN_SINGBOX_CONFIG=${CONFIG}
VPN_LOG_FILE=${LOG_FILE}
VPN_STATE_DIR=/var/lib/vpn-node-agent
EOF
  chmod 600 "$ENV_FILE"
  echo "[env] 已写入 $ENV_FILE"
else
  echo "[env] 保留已有 $ENV_FILE（如需覆盖加 --force-env）"
fi

install -m 755 "${SRC_DIR}/agent.py" "$BIN"
install -m 644 "${SRC_DIR}/vpn-node-agent.service" /etc/systemd/system/vpn-node-agent.service
install -m 644 "${SRC_DIR}/vpn-node-agent.timer" /etc/systemd/system/vpn-node-agent.timer
# 仓库在 Windows 上可能带 CRLF，systemd 不认带 \r 的 unit 行
sed -i 's/\r$//' /etc/systemd/system/vpn-node-agent.service /etc/systemd/system/vpn-node-agent.timer
systemctl daemon-reload
echo "[systemd] 已安装 vpn-node-agent.service / .timer"

if [[ "$SKIP_LOG_LEVEL" -eq 0 ]]; then
  python3 - "$CONFIG" "$SERVICE" <<'PY'
import json, os, shutil, subprocess, sys, time

path, service = sys.argv[1], sys.argv[2]
with open(path, "r", encoding="utf-8") as handle:
    config = json.load(handle)
log = config.setdefault("log", {})
level = str(log.get("level", "info"))
if level in ("info", "debug", "trace"):
    print("[log] 已经是 %s 级，无需调整" % level)
    sys.exit(0)

backup = path + ".bak-loglevel"
shutil.copy2(path, backup)
with open(path, "w", encoding="utf-8") as handle:
    json.dump(config, handle, indent=2, ensure_ascii=False)
    handle.write("\n")

check = subprocess.run(["sing-box", "check", "-c", path], capture_output=True, text=True)
if check.returncode != 0:
    shutil.copy2(backup, path)
    print("[log] 改完后 sing-box check 失败，已回滚:\n" + (check.stdout + check.stderr))
    sys.exit(1)

subprocess.run(["systemctl", "restart", service], check=True)
time.sleep(3)
active = subprocess.run(["systemctl", "is-active", "--quiet", service]).returncode == 0
print("[log] 日志级别 %s -> info，重启后服务 %s" % (level, "正常" if active else "异常"))
if not active:
    shutil.copy2(backup, path)
    subprocess.run(["systemctl", "restart", service])
    sys.exit(1)
PY
else
  echo "[log] 按要求跳过日志级别调整：连接源 IP 将采集不到"
fi

systemctl enable --now vpn-node-agent.timer
echo "[timer] 已启用，每 60 秒执行一轮"

echo "[dry-run] 下面是一次只读演练，不会改动 sing-box 配置："
"$BIN" --env-file "$ENV_FILE" --dry-run || true

echo
echo "安装完成。下一轮 timer（≤60s）会自动应用凭据清单。"
echo "查看日志: journalctl -u vpn-node-agent.service -n 50 --no-pager"
