#!/bin/bash
# seedance2.5-活动 上架后的**只读**验证。
#
# ⚠️ 本脚本**不生成任何视频**、不发起任何模型请求 —— 只读数据库与
#    /api/status /api/pricing，验证配置已生效。
#
# 用法：ssh -p 877 ubuntu@119.29.253.97 'bash /tmp/verify-huodong.sh'
set -uo pipefail

PSQL="sudo docker exec postgres psql -U root -d new-api -t -A -F| "

echo "════════ 1. 渠道 17 模型清单 ════════"
${PSQL} -c "SELECT id, name, type, models FROM channels WHERE id = 17;"

echo ""
echo "════════ 2. 促销族 abilities（活动版应与母本完全同构）════════"
${PSQL} -c "SELECT \"group\", model, channel_id, enabled, priority, weight, coalesce(tag,'-') FROM abilities WHERE model IN ('seedance2.5-特惠','seedance2.5-活动') ORDER BY model;"

echo ""
echo "════════ 3. ModelPrice 关键键 ════════"
${PSQL} -c "SELECT value::jsonb ->> 'seedance2.5-特惠' AS tehui_yuan0_20_per_sec, value::jsonb ->> 'seedance2.5-活动' AS huodong_yuan4_5_per_call, value::jsonb ->> 'wan3.0-720p-按次' AS xinbi_yuan4_5_per_call FROM options WHERE key='ModelPrice';"

echo ""
echo "════════ 4. ModelPrice 键总数（不应比变更前少）════════"
${PSQL} -c "SELECT jsonb_object_length(value::jsonb) FROM options WHERE key='ModelPrice';"

echo ""
echo "════════ 5. 路由 option（source_models 应含两个模型）════════"
${PSQL} -c "SELECT value FROM options WHERE key='Seedance25TeHuiRoute30s';"

echo ""
echo "════════ 6. 容器版本（应为 e14a200fa）════════"
sudo docker inspect new-api --format '{{.Image}}'
sudo docker logs --tail 200 new-api 2>&1 | grep -o 'version=[^ ]*' | tail -2 || true
sudo docker exec new-api /new-api --version 2>/dev/null || true

echo ""
echo "════════ 7. 服务健康 ════════"
curl -fsS --max-time 8 http://127.0.0.1:3000/api/status | head -c 300
echo ""

echo ""
echo "════════ 8. 广场是否上架活动版（只读接口，不触发生成）════════"
curl -fsS --max-time 10 http://127.0.0.1:3000/api/pricing 2>/dev/null \
  | grep -o 'seedance2\.5-[^"]*' | sort -u | head -10
echo ""
echo "（上面出现 seedance2.5-活动 即表示广场已展示）"

echo ""
echo "════════ 9. 活动版是否出现在渠道可选模型里 ════════"
${PSQL} -c "SELECT count(*) AS huodong_in_abilities FROM abilities WHERE model='seedance2.5-活动';"

echo ""
echo "════════ 10. 旧容器保留情况（回滚用）════════"
sudo docker ps -a --filter "name=new-api-prev" --format '{{.Names}}  {{.Status}}' | head -5
