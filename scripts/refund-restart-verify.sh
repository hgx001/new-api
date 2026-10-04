#!/usr/bin/env bash
# ① 退还误建 dola 任务的预扣费  ② 重启 new-api 刷新新渠道的能力缓存  ③ 零成本重验
set -u
q() { docker exec postgres psql -U root -d new-api -t -A -F' | ' -c "$1" 2>&1; }

echo "############ ① 退款（log 4286 / user 2 / 171232）############"
q "SELECT 'before: user_quota=' || quota FROM users WHERE id=2"
q "SELECT 'before: token1_used=' || used_quota FROM tokens WHERE id=1"
q "UPDATE users SET quota = quota + 171232 WHERE id=2" >/dev/null
q "UPDATE tokens SET used_quota = GREATEST(used_quota - 171232, 0) WHERE id=1" >/dev/null
q "UPDATE logs SET content = content || ' [REFUNDED: verification-only task cancelled, never reached upstream]'
   WHERE id=4286" >/dev/null
q "SELECT 'after: user_quota=' || quota FROM users WHERE id=2"
q "SELECT 'after: token1_used=' || used_quota FROM tokens WHERE id=1"
q "SELECT 'log 4286: ' || content FROM logs WHERE id=4286"

echo
echo "############ ② 重启 new-api（裸 SQL 建渠道不进内存能力缓存）############"
docker restart new-api >/dev/null && echo "restarted"
for i in $(seq 1 24); do
  wget -q -O - http://127.0.0.1:3000/api/status 2>/dev/null | grep -q '"success": *true' && { echo "HEALTH_OK ${i}x5s"; break; }
  [ "$i" = 24 ] && { docker logs new-api --tail 15; exit 1; }; sleep 5
done

echo
echo "############ ③ 零成本重验（探针全部在本地校验阶段失败，不建单）############"
TK=$(q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id
        WHERE t.status=1 AND u.quota>100000 AND u.\"group\"='default' ORDER BY t.id LIMIT 1")
Q0=$(q "SELECT COALESCE(SUM(used_quota),0) FROM tokens")
ARC0=$(grep -c "remote-generation/jobs" /home/ubuntu/arcreel/logs/arcreel.log 2>/dev/null || echo 0)
echo "起始 used_quota=$Q0"

probe() {
  local R=$(curl -s -o /tmp/v.json -w "%{http_code}" -X POST https://api.heibaidao.cn/v1/videos \
    -H "Authorization: Bearer $TK" -H "Content-Type: application/json" -d "$3")
  printf '%-30s HTTP=%s %s\n' "$1" "$R" "$(head -c 175 /tmp/v.json | tr -d '\n')"
}

echo "--- 漫屋四模型（用会失败的输入，验证路由通；seedance-2.0 用非法 seconds）---"
probe "seedance-2.0 非法 seconds=20" seedance-2.0      '{"model":"seedance-2.0","prompt":"x","seconds":20}'
probe "seedance-2.5 非法 seconds=15" seedance-2.5      '{"model":"seedance-2.5","prompt":"x","seconds":15}'
probe "Nano Banana Pro n 越界"    "Nano Banana Pro"     '{"model":"Nano Banana Pro","prompt":"x","n":99}'
probe "gemini-web-video 非法"     gemini-web-video      '{"model":"gemini-web-video","prompt":"x","seconds":7}'
probe "jimeng-video-reverse 缺参" jimeng-video-reverse  '{"model":"jimeng-video-reverse"}'

echo "--- MiniMax v2 能力门控 ---"
probe "H3 时长越界"        MiniMax-H3            '{"model":"MiniMax-H3","prompt":"a cat","duration":20,"metadata":{"ratio":"16:9"}}'
probe "H3 分辨率越界"      MiniMax-H3            '{"model":"MiniMax-H3","prompt":"a cat","resolution":"480P","metadata":{"ratio":"16:9"}}'
probe "H3-Max 不支持 2K"  MiniMax-H3-Max        '{"model":"MiniMax-H3-Max","prompt":"a cat","resolution":"2K","metadata":{"ratio":"16:9"}}'
probe "H3-Max 时长越界"   MiniMax-H3-Max        '{"model":"MiniMax-H3-Max","prompt":"a cat","duration":4,"metadata":{"ratio":"16:9"}}'
probe "文生视频缺 ratio"   MiniMax-H3            '{"model":"MiniMax-H3","prompt":"a cat"}'
probe "首尾帧+参考混用"    MiniMax-H3            '{"model":"MiniMax-H3","prompt":"a","media":[{"type":"first_frame","url":"https://x.example/a.jpg"},{"type":"reference_image","url":"https://x.example/b.jpg"}],"metadata":{"ratio":"16:9"}}'
probe "Context-IR 缺视频"  MiniMax-H3-Context-IR '{"model":"MiniMax-H3-Context-IR","metadata":{"ratio":"16:9"}}'

echo
echo "--- 终检 ---"
Q1=$(q "SELECT COALESCE(SUM(used_quota),0) FROM tokens")
ARC1=$(grep -c "remote-generation/jobs" /home/ubuntu/arcreel/logs/arcreel.log 2>/dev/null || echo 0)
echo "used_quota: $Q0 -> $Q1  差值 $((Q1-Q0))（应 0）"
echo "ArcReel 建单: $ARC0 -> $ARC1  差值 $((ARC1-ARC0))（应 0）"
echo "上游域名出现在日志次数（应 0）:"; docker logs new-api --since 3m 2>&1 | grep -c "api.minimax.io" || true
q "SELECT 'channel ' || id || ' status=' || status FROM channels WHERE id IN (20,21) ORDER BY id"
q "SELECT 'abilities off: ' || count(*) FROM abilities WHERE NOT enabled"
q "SELECT '未完成任务数: ' || count(*) FROM tasks WHERE status NOT IN ('SUCCESS','FAILURE')"