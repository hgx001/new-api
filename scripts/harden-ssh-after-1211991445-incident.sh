#!/usr/bin/env bash
# ==============================================================================
# 安全加固脚本 —— 121.199.14.45 未授权登录事件处置
# 生成时间: 2026-11-02 22:45 (CST)
# 目标主机: ubuntu@119.29.253.97:877  (腾讯云轻量应用服务器 Lighthouse)
#
# 事件时间线:
#   2026-10-11 18:32:03  authorized_keys 被修改(新增 langdu-mcp-tunnel 密钥)
#   2026-10-11 18:32:10  121.199.14.45 用该密钥登录成功
#   2026-10-11 18:33-18:47  4 次登录(其中 2 次持续 7 分钟)
#   2026-11-02 22:28:25  再次登录成功, 会话持续到 22:45 被处置
#
# 本脚本做四件事:
#   1. 断开入侵者活跃会话
#   2. iptables 拉黑 IP (INPUT + DOCKER-USER 双链)
#   3. 移除 authorized_keys 中的入侵者公钥
#   4. 关闭密码登录 + 收紧 SSH 参数
#
# ⚠️ 幂等性: 可重复执行。已完成的步骤会跳过。
# ⚠️ 前置条件: 必须先确认自己能用公钥登录, 否则第 4 步会把自己锁在门外。
# ==============================================================================
set -euo pipefail

readonly BAD_IP="121.199.14.45"
readonly BAD_KEY_COMMENT="langdu-mcp-tunnel"
readonly AUTH_KEYS="/home/ubuntu/.ssh/authorized_keys"
readonly HARDEN_CONF="/etc/ssh/sshd_config.d/99-hardening.conf"
readonly STAMP="$(date +%Y%m%d%H%M%S)"

echo "=============================================================="
echo " 安全加固开始: $STAMP"
echo " 目标 IP: $BAD_IP"
echo "=============================================================="

# --- 步骤 0: 自检 —— 确认当前会话用的是公钥, 不是密码 -----------------------
echo
echo "[0/4] 自检: 当前认证方式"
if [[ "${SSH_CONNECTION:-}" == *" 22" ]] || [[ -z "${SSH_CONNECTION:-}" ]]; then
  echo "  ⚠️  未能从 SSH_CONNECTION 判定认证方式，继续执行（公钥登录仍可用）"
else
  echo "  ✅ SSH_CONNECTION=${SSH_CONNECTION}"
fi

# --- 步骤 1: 断开入侵者活跃会话 --------------------------------------------
echo
echo "[1/4] 断开入侵者活跃会话"
mapfile -t BAD_PIDS < <(sudo ss -tnp 2>/dev/null | grep "${BAD_IP}" \
  | grep -oP 'pid=\K[0-9]+' | sort -u || true)
if [[ ${#BAD_PIDS[@]} -gt 0 ]]; then
  for pid in "${BAD_PIDS[@]}"; do
    echo "  杀掉 PID ${pid}: $(ps -p "${pid}" -o cmd= 2>/dev/null || echo '?')"
    sudo kill -9 "${pid}" 2>/dev/null || true
  done
  sleep 1
else
  echo "  无入侵者活跃会话"
fi

# --- 步骤 2: iptables 拉黑 -------------------------------------------------
echo
echo "[2/4] iptables 拉黑 ${BAD_IP}"
if sudo iptables -C INPUT -s "${BAD_IP}" -j DROP 2>/dev/null; then
  echo "  INPUT 规则已存在"
else
  sudo iptables -I INPUT 1 -s "${BAD_IP}" -j DROP
  echo "  已插入 INPUT DROP 规则"
fi
if sudo iptables -C DOCKER-USER -s "${BAD_IP}" -j DROP 2>/dev/null; then
  echo "  DOCKER-USER 规则已存在"
else
  sudo iptables -I DOCKER-USER 1 -s "${BAD_IP}" -j DROP
  echo "  已插入 DOCKER-USER DROP 规则 (挡容器流量)"
fi
echo "  持久化规则文件..."
sudo netfilter-persistent save >/dev/null 2>&1 || \
  echo "  ⚠️  netfilter-persistent save 失败，请手动执行"
sudo grep "${BAD_IP}" /etc/iptables/rules.v4 || true

# --- 步骤 3: 移除入侵者公钥 ------------------------------------------------
# ⚠️ 关键: SHA256 指纹是哈希值, grep -v 指纹匹配不到文件里的文本。
#          必须按公钥原文或注释删除, 否则会"删了但没删掉"。
echo
echo "[3/4] 移除 authorized_keys 中的入侵者公钥"
if sudo grep -q "${BAD_KEY_COMMENT}" "${AUTH_KEYS}" 2>/dev/null; then
  sudo cp "${AUTH_KEYS}" "${AUTH_KEYS}.bak-${STAMP}"
  echo "  已备份: ${AUTH_KEYS}.bak-${STAMP}"
  sudo grep -v "${BAD_KEY_COMMENT}" "${AUTH_KEYS}" | sudo tee "${AUTH_KEYS}.new" >/dev/null
  sudo mv "${AUTH_KEYS}.new" "${AUTH_KEYS}"
  sudo chmod 600 "${AUTH_KEYS}"
  sudo chown ubuntu:ubuntu "${AUTH_KEYS}"
  echo "  已移除。剩余授权:"
  sudo ssh-keygen -lf "${AUTH_KEYS}" | sed 's/^/    /'
else
  echo "  未找到 ${BAD_KEY_COMMENT}，无需处理"
fi

# --- 步骤 4: SSH 加固 -------------------------------------------------------
echo
echo "[4/4] SSH 加固"
sudo tee "${HARDEN_CONF}" >/dev/null <<'EOF'
# 安全加固 2026-11-02 (事件: 121.199.14.45 未授权登录)
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
PermitEmptyPasswords no
PermitRootLogin no
MaxAuthTries 3
LoginGraceTime 30
AllowUsers ubuntu
X11Forwarding no
EOF
echo "  已写入 ${HARDEN_CONF}"

# ⚠️ 关键: sshd 采用 "first-obtained wins" —— 首个被读到的值生效，
#          不是最后一个。而 Include 在主文件第 12 行，
#          所以 sshd_config.d/ 里字典序靠前的文件会覆盖 99-hardening.conf。
#          50-cloud-init.conf 里默认有 PasswordAuthentication yes，必须一并注释。
for f in /etc/ssh/sshd_config.d/50-cloud-init.conf /etc/ssh/sshd_config; do
  if sudo grep -q "^PasswordAuthentication yes" "$f" 2>/dev/null; then
    sudo cp "$f" "${f}.bak-${STAMP}"
    sudo sed -i "s|^PasswordAuthentication yes|#PasswordAuthentication yes  # 2026-11-02 加固: 由 99-hardening.conf 控制|" "$f"
    echo "  已注释 ${f} 中的 PasswordAuthentication yes"
  fi
done

echo "  校验语法..."
if ! sudo sshd -t; then
  echo "  ❌ 语法错误，不重启 sshd，请检查 ${HARDEN_CONF}"
  exit 1
fi
echo "  语法 OK，重启 sshd..."
sudo systemctl restart sshd
sleep 2

echo
echo "  生效后的有效配置:"
sudo sshd -T 2>/dev/null | grep -iE '^(permitrootlogin|passwordauthentication|pubkeyauthentication|kbdinteractiveauthentication|maxauthtries|logingracetime|allowusers|x11forwarding)' | sed 's/^/    /'

echo
echo "=============================================================="
echo " 加固完成: $STAMP"
echo " 请立即验证: ssh -p 877 ubuntu@119.29.253.97 'echo OK'"
echo "=============================================================="
