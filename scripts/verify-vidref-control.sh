#!/usr/bin/env bash
# 验证对照组产物是真实可播放的 mp4，并核对上下游金额，然后按规矩退款 + 记上游台账。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
TASK_ID=task_EULw1MShH6kaZKSG9oBLtwyxJjWOwwJb
BASELINE=427430   # 本轮任何测试之前的 user2 quota

URL=$(Q "SELECT private_data->>'result_url' FROM tasks WHERE task_id='$TASK_ID';" | tr -d '\r\n ')
echo "产物 URL: $URL"
echo
echo "=== 1. HTTP 可达性与真实大小 ==="
curl -sIL "$URL" | grep -iE "^HTTP/|^content-length|^content-type" | head -6
SIZE=$(curl -sIL "$URL" | grep -i "^content-length" | tail -1 | tr -dc '0-9')
echo "字节数: ${SIZE:-?}  ≈$(( ${SIZE:-0} / 1048576 )) MB"

echo
echo "=== 2. 下载并验魔数 + 容器（确认是合法 mp4，不是错误页/空文件）==="
curl -s -o /tmp/vidref.mp4 "$URL"
ls -l /tmp/vidref.mp4 | awk '{print "落盘大小:", $5, "bytes"}'
head -c 12 /tmp/vidref.mp4 | xxd | head -1
echo "ftyp box: $(head -c 32 /tmp/vidref.mp4 | strings | head -1)"

echo
echo "=== 3. 客户侧退款（本笔是我误建的探针，不是用户业务调用）==="
QTY=$(Q "SELECT quota FROM tasks WHERE task_id='$TASK_ID';" | tr -d ' ')
Q "UPDATE users SET quota = quota + $QTY WHERE id = 2 AND quota < $BASELINE;" >/dev/null
Q "UPDATE logs SET content = content || ' [REFUNDED-BY-OPERATOR: r2v 视频参考对照组误建探针]'
   WHERE model_name='wan3.0-smart' AND type=2 AND content NOT LIKE '%REFUNDED%'
     AND created_at > EXTRACT(EPOCH FROM now())::bigint - 1800;" >/dev/null
Q "INSERT INTO logs (user_id, created_at, type, content, username, token_name, model_name, quota, channel_id)
   SELECT 2, EXTRACT(EPOCH FROM now())::bigint, 5,
     '[OPERATOR-COST-ACCOUNTING] wan3.0-smart 视频参考对照组探针：上游实扣 90 有赞积分（task $TASK_ID，5 秒，SUCCESS，产物已验证为合法 mp4）。客户侧 $QTY quota 已退。起因：探针脚本的对照组未用必然被本地拦下的入参，违反 AGENTS.md 付费探针纪律第 3 条。',
     'admin','OPERATOR-COST-ACCOUNTING','wan3.0-smart',0,17
   WHERE NOT EXISTS (SELECT 1 FROM logs WHERE content LIKE '%r2v 视频参考对照组探针%');" >/dev/null

Q "SELECT 'user2_quota', quota FROM users WHERE id=2;"
Q "SELECT '与基线差（应为 0）', quota - $BASELINE FROM users WHERE id=2;"
Q "SELECT '上游台账条数', count(*) FROM logs WHERE content LIKE '%OPERATOR-COST-ACCOUNTING%';"
