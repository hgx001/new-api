#!/usr/bin/env bash
# 恢复被 AutoBan 的渠道：必须同时改 channels.status 与 abilities.enabled。
# 只改 status 是 2026-10-02 的真实事故根因（渠道看起来正常，模型全 503）。
# 用法: bash restore-channel.sh <channel_id> [原因]
set -euo pipefail
CID="${1:?need channel id}"
REASON="${2:-manual restore}"

echo "=== 恢复前 ==="
docker exec postgres psql -U root -d new-api -t -A -F' | ' -c \
  "SELECT id, name, status FROM channels WHERE id=$CID"
echo "--- abilities ---"
docker exec postgres psql -U root -d new-api -t -A -F' | ' -c \
  "SELECT model, enabled FROM abilities WHERE channel_id=$CID ORDER BY model"

docker exec postgres psql -U root -d new-api -c \
  "UPDATE channels SET status=1, other_info=NULL WHERE id=$CID" >/dev/null
docker exec postgres psql -U root -d new-api -c \
  "UPDATE abilities SET enabled=true WHERE channel_id=$CID" >/dev/null
echo "已恢复 status=1 + abilities.enabled=true（原因: $REASON）"

echo
echo "=== 恢复后 ==="
docker exec postgres psql -U root -d new-api -t -A -F' | ' -c \
  "SELECT id, name, status FROM channels WHERE id=$CID"
docker exec postgres psql -U root -d new-api -t -A -F' | ' -c \
  "SELECT model, enabled FROM abilities WHERE channel_id=$CID ORDER BY model"
echo "--- 全库 abilities 关闭数（应为 0）---"
docker exec postgres psql -U root -d new-api -t -A -c \
  "SELECT count(*) FROM abilities WHERE NOT enabled"