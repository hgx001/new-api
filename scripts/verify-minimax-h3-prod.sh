#!/usr/bin/env bash
# 零成本验证：所有用例都在本地校验/能力门控阶段失败，绝不建单、绝不扣费。
# 断言：① 错误来自 hailuo 适配器自己的错误码 ② used_quota 前后差值为 0
#      ③ 渠道 21/20 保持启用 ④ ArcReel/上游零建单
set -u
q() { docker exec postgres psql -U root -d new-api -t -A -c "$1" 2>&1; }

# 选一个 default 分组、有余额的令牌（漫屋与 H3 都能用）
TK=$(q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id
        WHERE t.status=1 AND u.quota>100000 AND u.\"group\"='default' ORDER BY t.id LIMIT 1")
[ -z "$TK" ] && { echo "找不到可用令牌"; exit 1; }

Q0=$(q "SELECT COALESCE(SUM(used_quota),0) FROM tokens")
ARC0=$(grep -c "POST /api/v1/remote-generation/jobs" /home/ubuntu/arcreel/logs/arcreel.log 2>/dev/null || echo 0)
echo "起始 used_quota=$Q0  ArcReel建单计数=$ARC0"
echo

probe() { # $1=标签 $2=模型 $3=请求体
  local R=$(curl -s -o /tmp/v.json -w "%{http_code}" -X POST https://api.heibaidao.cn/v1/videos \
    -H "Authorization: Bearer $TK" -H "Content-Type: application/json" -d "$3")
  printf '%-34s HTTP=%s %s\n' "$1" "$R" "$(head -c 190 /tmp/v.json | tr -d '\n')"
}

echo "=== A. 漫屋四个模型（验证 abilities 修复后恢复路由）==="
# ⚠️ 本脚本**不得**使用能通过校验的 dola 入参：dola 的 n 字段不参与校验，
# 误用会真建单并扣费（已因此误建 2 次，均已取消+退款）。只用必然被本地拦下的输入。
probe "dola 非法 seconds=7"      dola-seedance-2.5      '{"model":"dola-seedance-2.5","prompt":"x","seconds":7}'
probe "manwu-image n 越界"            manwu-image            '{"model":"manwu-image","prompt":"x","n":99}'
probe "gemini-web-video 非法 seconds" gemini-web-video       '{"model":"gemini-web-video","prompt":"x","seconds":7}'
probe "jimeng-video-reverse 缺视频"   jimeng-video-reverse   '{"model":"jimeng-video-reverse"}'

echo
echo "=== B. MiniMax v2 能力门控（全部应在本地 400，不触上游）==="
probe "H3 时长越界(20s)"        MiniMax-H3             '{"model":"MiniMax-H3","prompt":"a cat","duration":20,"metadata":{"ratio":"16:9"}}'
probe "H3 分辨率越界(480P)"     MiniMax-H3             '{"model":"MiniMax-H3","prompt":"a cat","resolution":"480P","metadata":{"ratio":"16:9"}}'
probe "H3-Max 不支持 2K"        MiniMax-H3-Max         '{"model":"MiniMax-H3-Max","prompt":"a cat","resolution":"2K","metadata":{"ratio":"16:9"}}'
probe "H3-Max 时长越界(4s)"     MiniMax-H3-Max         '{"model":"MiniMax-H3-Max","prompt":"a cat","duration":4,"metadata":{"ratio":"16:9"}}'
probe "文生视频缺 ratio"        MiniMax-H3             '{"model":"MiniMax-H3","prompt":"a cat"}'
probe "首尾帧+参考素材混用"      MiniMax-H3             '{"model":"MiniMax-H3","prompt":"a","media":[{"type":"first_frame","url":"https://x.example/a.jpg"},{"type":"reference_image","url":"https://x.example/b.jpg"}],"metadata":{"ratio":"16:9"}}'
probe "参考图超 9 张"            MiniMax-H3             '{"model":"MiniMax-H3","prompt":"a","images":["https://x.example/1.jpg","https://x.example/2.jpg","https://x.example/3.jpg","https://x.example/4.jpg","https://x.example/5.jpg","https://x.example/6.jpg","https://x.example/7.jpg","https://x.example/8.jpg","https://x.example/9.jpg","https://x.example/10.jpg"],"metadata":{"ratio":"16:9"}}'
probe "非 http(s) 素材"         MiniMax-H3             '{"model":"MiniMax-H3","prompt":"a","images":["data:image/png;base64,AAA"],"metadata":{"ratio":"16:9"}}'
probe "Context-IR 缺视频 URL"    MiniMax-H3-Context-IR  '{"model":"MiniMax-H3-Context-IR","metadata":{"ratio":"16:9"}}'
probe "缺 prompt"                MiniMax-H3             '{"model":"MiniMax-H3","metadata":{"ratio":"16:9"}}'
probe "Context-IR 缺 prompt"     MiniMax-H3-Context-IR  '{"model":"MiniMax-H3-Context-IR","input_reference":"https://x.example/a.mp4","metadata":{"ratio":"16:9"}}'

echo
echo "=== C. 越界分辨率/时长是否真的被本端拦下（而非上游报错）==="
docker logs new-api --since 3m 2>&1 | grep -cE "api\.minimax\.io|video_generation" | sed 's/^/new-api 侧出现上游域名次数(应为0): /'

echo
echo "=== D. 计费与状态终检 ==="
Q1=$(q "SELECT COALESCE(SUM(used_quota),0) FROM tokens")
ARC1=$(grep -c "POST /api/v1/remote-generation/jobs" /home/ubuntu/arcreel/logs/arcreel.log 2>/dev/null || echo 0)
echo "used_quota: $Q0 -> $Q1  (差值 $((Q1-Q0))，应为 0)"
q "SELECT '未完成任务数（应 0）: ' || count(*) FROM tasks WHERE status NOT IN ('SUCCESS','FAILURE')"
q "SELECT 'channel ' || id || ' status=' || status FROM channels WHERE id IN (20,21) ORDER BY id"
q "SELECT 'abilities off: ' || count(*) FROM abilities WHERE NOT enabled"
echo
echo "=== E. 价格广场是否展示（Context-IR 故意隐藏）==="
curl -s https://api.heibaidao.cn/api/pricing | grep -o '"model_name":"MiniMax-H3[^"]*"' | sort -u | sed 's/^/  /'