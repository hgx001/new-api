#!/usr/bin/env bash
# ==============================================================================
# SSH 隧道专用密钥的受限加固脚本
# 生成时间: 2026-11-02 23:20 (CST)
# 目标主机: ubuntu@119.29.253.97:877  (腾讯云轻量应用服务器 Lighthouse)
# 对端主机: root@121.199.14.45       (阿里云 CentOS 7, lxg01)
#
# 事件背景:
#   2026-11-02 22:28:25  121.199.14.45 用 langdu-mcp-tunnel 密钥登录生产机，
#                         当初被误判为「入侵者」，做了 iptables DROP + 摘除公钥。
#   2026-11-02 23:16     取证确认: 该密钥是用户自己 10/11 部署的
#                         pwmcp-tunnel.service 专用密钥（私钥指纹 3/zGdGpw1gA
#                         与生产机 authorized_keys 完全一致），并非入侵。
#   2026-11-02 23:17     解除 DROP、恢复公钥，并用受限前缀替换，
#                         同时对 45 号机做 SSH 加固。
#
# 本脚本做四件事:
#   1. 解除对隧道对端 IP 的误伤 iptables DROP（若存在）
#   2. 以「受限前缀 + command 空命令」形式写入隧道专用公钥
#   3. 校验隧道功能可用（127.0.0.1:8931 应返回 HTTP 400）
#   4. 对隧道对端机做 SSH 加固（禁密码、限尝试次数）
#
# ⚠️ 幂等性: 可重复执行。已完成的步骤会跳过。
# ⚠️ 前置条件: 必须先确认自己能用公钥登录目标主机，否则会把自己锁在门外。
#
# 🔑 核心安全模型（受限公钥三要素）:
#   restrict                 禁止 pty / agent / X11 / user rc 转发
#   port-forwarding          只保留端口转发能力（隧道必需）
#   permitopen="host:port"   只允许转发到指定目标端口
#   command=""               强制执行空命令 → 拿不到任何 shell
#   ⇒ 私钥即使泄露，也只能拿到"到 8931 的单向隧道"，拿不到 root shell。
#      注意：生产机 ubuntu 用户有 NOPASSWD sudo，等效 root，所以这个限制很关键。
# ==============================================================================
set -euo pipefail

readonly PEER_IP="121.199.14.45"
readonly TUNNEL_KEY_COMMENT="langdu-mcp-tunnel"
readonly TUNNEL_PUBKEY='ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILD9cvbuHWJD/DsDIRJV2XGVSmZfM21nQ/6EmYz8/FDj'
readonly TUNNEL_TARGET_PORT="8931"
readonly AUTH_KEYS="/home/ubuntu/.ssh/authorized_keys"
readonly STAMP="$(date +%Y%m%d%H%M%S)"

# 受限前缀：restrict + port-forwarding + permitopen + 空命令
readonly RESTRICTED_PREFIX="restrict,port-forwarding,permitopen=\"127.0.0.1:${TUNNEL_TARGET_PORT}\",command=\"\""

echo "=============================================================="
echo " 隧道密钥受限加固: $STAMP"
echo " 目标 IP: $PEER_IP   隧道端口: ${TUNNEL_TARGET_PORT}"
echo "=============================================================="

# --- 步骤 1: 解除误伤 DROP --------------------------------------------------
echo
echo "[1/4] 解除对 ${PEER_IP} 的误伤 iptables DROP"
for chain in INPUT DOCKER-USER; do
  # ⚠️ iptables -D 不接受 -s 等匹配条件，只能按规则编号删除。
  #    规则编号要从 `-L --line-numbers` 提取（-S 输出带行号前缀，会解析错）。
  num="$(sudo iptables -L "${chain}" -n --line-numbers 2>/dev/null \
        | awk -v ip="${PEER_IP}" '$0 ~ ip {print $1; exit}')"
  if [[ -n "${num}" ]]; then
    sudo iptables -D "${chain}" "${num}"
    echo "  已从 ${chain} 删除规则号 ${num}"
  else
    echo "  ${chain} 中无 ${PEER_IP} 规则，跳过"
  fi
done
sudo netfilter-persistent save >/dev/null 2>&1 || \
  echo "  ⚠️  netfilter-persistent save 失败，请手动执行"
if sudo iptables -S 2>/dev/null | grep -q "${PEER_IP}"; then
  echo "  ❌ 仍有残留 DROP 规则，请人工检查"
else
  echo "  ✅ 已无 ${PEER_IP} 的 DROP 规则"
fi

# --- 步骤 2: 写入受限版隧道公钥 ---------------------------------------------
echo
echo "[2/4] 写入受限版隧道公钥"
sudo cp -a "${AUTH_KEYS}" "${AUTH_KEYS}.bak-${STAMP}"
echo "  已备份: ${AUTH_KEYS}.bak-${STAMP}"

# 先移除旧的（可能是无限制版）条目，再写受限版
sudo grep -v "${TUNNEL_KEY_COMMENT}" "${AUTH_KEYS}" | sudo tee "${AUTH_KEYS}.new" >/dev/null
printf '%s %s %s\n' "${RESTRICTED_PREFIX}" "${TUNNEL_PUBKEY}" "${TUNNEL_KEY_COMMENT}" \
  | sudo tee -a "${AUTH_KEYS}" >/dev/null
sudo mv "${AUTH_KEYS}.new" "${AUTH_KEYS}"
sudo chmod 600 "${AUTH_KEYS}"
sudo chown ubuntu:ubuntu "${AUTH_KEYS}"

echo "  当前条目:"
sudo grep "${TUNNEL_KEY_COMMENT}" "${AUTH_KEYS}" | sed 's/^/    /'
echo
echo "  全部授权密钥指纹:"
sudo ssh-keygen -lf "${AUTH_KEYS}" | sed 's/^/    /'
echo "  总数: $(sudo grep -c . "${AUTH_KEYS}")"

# --- 步骤 3: 校验隧道 -------------------------------------------------------
echo
echo "[3/4] 校验隧道功能"
echo "  本机 8931（Playwright MCP 源）:"
code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 6 "http://127.0.0.1:${TUNNEL_TARGET_PORT}/mcp" || true)"
echo "    HTTP ${code}  （400 = MCP 对空 GET 的正常响应，视为通）"
if [[ "${code}" == "400" || "${code}" == "200" ]]; then
  echo "  ✅ Playwright MCP 源正常"
else
  echo "  ⚠️  本机 8931 无响应，需确认 Playwright MCP 是否在跑"
fi

# 从隧道对端验证（需对端可达）
echo "  从 ${PEER_IP} 侧验证隧道:"
ssh -i /root/.ssh/pwmcp_tunnel -p 877 -o StrictHostKeyChecking=no -o ConnectTimeout=8 -o BatchMode=yes \
  -N -L "127.0.0.1:${TUNNEL_TARGET_PORT}:127.0.0.1:${TUNNEL_TARGET_PORT}" \
  "ubuntu@119.29.253.97" >/dev/null 2>&1 &
tpid=$!
sleep 3
peer_code="$(ssh -i /root/.ssh/pwmcp_tunnel -p 877 -o StrictHostKeyChecking=no -o ConnectTimeout=8 -o BatchMode=yes \
  "ubuntu@119.29.253.97" 'true' 2>&1 && echo OK || echo FAIL)"
kill "${tpid}" 2>/dev/null || true
echo "    隧道对端密钥认证: ${peer_code}"

# --- 步骤 4: SSH 加固 -------------------------------------------------------
echo
echo "[4/4] SSH 加固（对端机 ${PEER_IP}）"
echo "  ⚠️ 跳过：对端加固由 harden-peer-ssh-45.sh 单独执行"
echo "      原因: 对端是 root 直登（PermitRootLogin yes 生效于第 38 行），"
echo "      改为 prohibit-password 才能保住密钥通道；写成 no 会锁死自己。"

echo
echo "=============================================================="
echo " 完成: $STAMP"
echo " 验证清单:"
echo "   1. 隧道: ssh -i /root/.ssh/pwmcp_tunnel -p 877 ubuntu@119.29.253.97 'echo OK'"
echo "      → 应无输出（command=\"\" 生效，shell 被拦截）"
echo "   2. 业务: curl -s -o /dev/null -w '%{http_code}' https://api.heibaidao.cn/api/status"
echo "      → 应为 200"
echo "=============================================================="
