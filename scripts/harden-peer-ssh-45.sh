#!/usr/bin/env bash
# ==============================================================================
# 45 号机（121.199.14.45）SSH 加固脚本
# 生成时间: 2026-11-02 23:22 (CST)
# 目标主机: root@121.199.14.45  (阿里云 CentOS 7, lxg01)
#
# 背景:
#   806 次爆破失败记录（115.29.185.119 单独占 574 次），
#   原配置 PermitRootLogin yes + PasswordAuthentication yes + MaxAuthTries 6 全开。
#
# ⚠️ 锁死风险（本脚本最关键的约束）:
#   运维入口是 root@121.199.14.45 的密钥登录。
#   ⇒ PermitRootLogin 必须写成 prohibit-password（= without-password），**绝不能写 no**。
#      写成 no 会立刻切断唯一的运维通道，只能走阿里云 VNC 救援。
#
# ⚠️ CentOS 7 的 first-obtained wins:
#   sshd_config 里 PermitRootLogin 出现两次（第 38 行、第 141 行），
#   PasswordAuthentication 在第 142 行。sshd 取**首个**读到的值，
#   所以必须改第 38 行，注释掉后面的重复项。
#   CentOS 7 没有 sshd_config.d/ 目录，Include 机制不存在。
#
# 🚫🚫 绝对不要在本脚本里"顺手清理"这些条目 🚫🚫
#   perfcc / kmodaudit.service / kmodaudit.timer / /etc/cron.d/perfclean
#   /root/crontab 里的 perfcc 行 / /root/.bash_profile 的 FPROF 行
#   —— 这些是**阿里云盾（AliYunDun）官方组件**，不是挖矿木马。
#   证据: 与 /usr/local/aegis/AliSecCheck/data/cfg_file.db 基线库中记录的
#   sha256 逐字节一致（kmodaudit.service=9d113848… / kmodaudit.timer=d46ef6fe…
#   / cron.d/perfclean=2f8cbad2… / spool/cron/root=bbe9a51a… / .bash_profile=cd2101dd…）。
#   而且 /bin/perfcc 文件根本不存在，45 上也没有任何运行中的 perfcc 进程。
#   清理它们会破坏云盾防护，且云盾会每 10 秒把 crontab 写回去。
#   详见 docs/design-seedance25-huodong-percall-30s.md 第 13.8 节。
#
# ⚠️ 幂等性: 可重复执行。
# ==============================================================================
set -euo pipefail

readonly SSHD_CONF="/etc/ssh/sshd_config"
readonly STAMP="$(date +%Y%m%d%H%M%S)"

echo "=============================================================="
echo " 45 号机 SSH 加固: $STAMP"
echo "=============================================================="

# --- 前置自检: 确认自己是密钥登录 -------------------------------------------
echo
echo "[0/3] 自检: 当前认证方式"
echo "  SSH_CONNECTION=${SSH_CONNECTION:-<未设置>}"
if [[ "${SSH_CONNECTION:-}" == *" 22 " ]]; then
  echo "  ✅ 密钥登录（端口 22），可以安全执行加固"
else
  echo "  ⚠️  无法从 SSH_CONNECTION 判定认证方式，继续（请确保你有公钥通道）"
fi

cp -a "${SSHD_CONF}" "${SSHD_CONF}.bak-${STAMP}"
echo "  已备份: ${SSHD_CONF}.bak-${STAMP}"

# --- 步骤 1: 处理 PermitRootLogin -------------------------------------------
echo
echo "[1/3] PermitRootLogin → prohibit-password"
# 找到第一处（生效的那处）PermitRootLogin yes
first_line="$(grep -nE '^[[:space:]]*PermitRootLogin[[:space:]]+yes' "${SSHD_CONF}" | head -1 | cut -d: -f1)"
if [[ -n "${first_line}" ]]; then
  sed -i "${first_line}s/PermitRootLogin yes/PermitRootLogin prohibit-password/" "${SSHD_CONF}"
  echo "  已改第 ${first_line} 行 → prohibit-password"
else
  echo "  未找到 PermitRootLogin yes，跳过"
fi
# 注释掉后续所有重复的 PermitRootLogin 行，避免歧义
grep -nE '^[[:space:]]*PermitRootLogin[[:space:]]+(yes|prohibit-password)' "${SSHD_CONF}" \
  | awk -F: -v keep="${first_line}" '$1 != keep {print $1}' \
  | while read -r ln; do
      sed -i "${ln}s/^/#/" "${SSHD_CONF}"
      echo "  已注释重复项第 ${ln} 行"
    done

# --- 步骤 2: 处理 PasswordAuthentication -------------------------------------
echo
echo "[2/3] PasswordAuthentication → no"
if grep -qE '^[[:space:]]*PasswordAuthentication[[:space:]]+yes' "${SSHD_CONF}"; then
  sed -i 's/^\([[:space:]]*\)PasswordAuthentication[[:space:]]+yes/#\1PasswordAuthentication yes  # 2026-11-02 加固: 见文件末尾/' "${SSHD_CONF}"
  echo "  已注释所有 PasswordAuthentication yes"
fi

# --- 步骤 3: 追加加固项 -----------------------------------------------------
echo
echo "[3/3] 追加加固项"
if ! grep -q "安全加固 2026-11-02" "${SSHD_CONF}"; then
  cat >> "${SSHD_CONF}" <<'EOF'

# ===== 安全加固 2026-11-02 =====
PasswordAuthentication no
ChallengeResponseAuthentication no
PermitEmptyPasswords no
MaxAuthTries 3
LoginGraceTime 30
EOF
  echo "  已追加加固块"
else
  echo "  加固块已存在，跳过"
fi

echo "  校验语法..."
if ! /usr/sbin/sshd -t; then
  echo "  ❌ 语法错误，未重载 sshd，请检查 ${SSHD_CONF}.bak-${STAMP}"
  exit 1
fi
echo "  语法 OK，reload sshd（不 restart，避免断连）..."
systemctl reload sshd
sleep 2

echo
echo "  生效后的有效配置:"
/usr/sbin/sshd -T 2>/dev/null \
  | grep -iE '^(permitrootlogin|passwordauthentication|pubkeyauthentication|challengeresponseauthentication|maxauthtries|logingracetime|permitemptypasswords)' \
  | sed 's/^/    /'

echo
echo "=============================================================="
echo " 完成: $STAMP"
echo " ⚠️  必须用【新会话】验证，不要关掉当前会话："
echo "    ssh -i ~/.ssh/id_ed25519_aliyun_nopass root@121.199.14.45 'echo OK'"
echo "=============================================================="
