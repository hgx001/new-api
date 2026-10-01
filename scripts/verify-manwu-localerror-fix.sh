#!/usr/bin/env bash
# 修复后回归验证：复现「零余额令牌提交反解」事故场景，确认不再误禁用渠道。
# 只做只读检查 + 一次必然失败的请求（预扣费阶段即 403，不建单、不计费）。
set -u

q() { docker exec postgres psql -U root -d new-api -t -A -c "$1"; }

TK_ZERO=$(q "SELECT key FROM tokens WHERE id=2")            # 属主 vvian quota=0
CH_STATUS_BEFORE=$(q "SELECT status FROM channels WHERE id=20")

echo "复现前渠道状态: ${CH_STATUS_BEFORE}  (1=启用 / 3=自动禁用)"

echo "--- 发出与事故完全相同的请求（零余额令牌提交反解，视频 URL 不可解析）---"
curl -s -o /tmp/fixcheck.json -w "HTTP=%{http_code}\n" -X POST https://api.heibaidao.cn/v1/videos \
  -H "Authorization: Bearer ${TK_ZERO}" -H "Content-Type: application/json" \
  -d '{"model":"jimeng-video-reverse","input_reference":"https://nonexistent-host-xyz.invalid/clip.mp4"}'
head -c 240 /tmp/fixcheck.json; echo
sleep 4

CH_STATUS_AFTER=$(q "SELECT status FROM channels WHERE id=20")
echo "复现后渠道状态: ${CH_STATUS_AFTER}"

BAN_LOGS=$(docker logs new-api --since 3m 2>&1 | grep -cE "发生错误，准备禁用" || true)
echo "本次窗口内『准备禁用』日志条数: ${BAN_LOGS}"

if [ "${CH_STATUS_AFTER}" = "1" ] && [ "${CH_STATUS_BEFORE}" = "1" ] && [ "${BAN_LOGS}" = "0" ]; then
  echo "结论: ✅ 修复生效 —— 本端额度错误不再禁用渠道"
else
  echo "结论: ❌ 未达预期（before=${CH_STATUS_BEFORE} after=${CH_STATUS_AFTER} bans=${BAN_LOGS}）"
fi

echo "--- 有余额令牌仍能正常路由到该渠道（证明渠道未被牵连）---"
TK_OK=$(q "SELECT t.key FROM tokens t JOIN users u ON u.id = t.user_id WHERE t.status = 1 AND u.quota > 100000 LIMIT 1")
if [ -z "${TK_OK}" ]; then
  echo "（没有找到有余额的令牌，跳过）"
else
  curl -s -X POST https://api.heibaidao.cn/v1/videos -H "Authorization: Bearer ${TK_OK}" \
    -H "Content-Type: application/json" -d '{"model":"manwu-image","prompt":"x","n":11}' | head -c 160
  echo
fi

echo "--- ArcReel 侧累计收到的建单 POST 数（预扣费失败不应到达上游）---"
grep -c "POST /api/v1/remote-generation/jobs" /home/ubuntu/arcreel/logs/arcreel.log || echo 0

echo "--- 渠道价格与模型清单复核 ---"
q "SELECT models FROM channels WHERE id=20"
q "SELECT e.key || ' = CNY ' || round((e.value::numeric)*7.3, 4) FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e WHERE o.key='ModelPrice' AND e.key IN ('gemini-web-video','manwu-image','jimeng-video-reverse') ORDER BY e.key"