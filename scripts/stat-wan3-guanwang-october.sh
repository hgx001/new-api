#!/usr/bin/env bash
# 10 月 wan3.0-video-官网 额度用量统计（北京时区；quota → 元：quota/500000*7.3）
# 日志类型：2=consume 扣费，4=system，5=error，6=refund
set -uo pipefail
SSH=(ssh -p 877 ubuntu@119.29.253.97)
PG='docker exec postgres psql -U root -d new-api -t -A -F"|"'
MODEL='wan3.0-video-官网'
OCT_START=1790784000   # 2026-10-01 00:00:00 +08:00（= 2026-09-30T16:00:00Z）
NOW=$(date +%s)

q() { "${SSH[@]}" "$PG -c \"$1\"" | tr -d '\r'; }
fmt() { node -e 'const q=Number(process.argv[1]||0); console.log((q/500000*7.3).toFixed(2));' "$1"; }
bj() { date -u -d "@$(( $1 + 28800 ))" '+%m-%d %H:%M'; }   # Git-bash 在 Windows 下不认 TZ，手动 +28800s

echo "窗口：$(bj $OCT_START) → $(bj $NOW)（北京时间）    模型：$MODEL"
echo

echo "=== ① 10 月扣费总量（type=2 consume）==="
RES=$(q "SELECT count(*) || '|' || COALESCE(sum(quota),0) FROM logs
         WHERE model_name='$MODEL' AND type=2 AND created_at >= $OCT_START AND created_at < $NOW")
N=${RES%%|*}; Q=${RES##*|}
echo "扣费条数=$N   合计额度=$Q   ≈ ¥$(fmt "$Q")"

ERR=$(q "SELECT count(*) FROM logs WHERE model_name='$MODEL' AND type=5 AND created_at >= $OCT_START AND created_at < $NOW")
REF=$(q "SELECT COALESCE(sum(quota),0) FROM logs WHERE model_name='$MODEL' AND type=6 AND created_at >= $OCT_START AND created_at < $NOW")
echo "错误日志条数=$ERR（额度 0，不计费）   退款额度=$REF ≈ ¥$(fmt "$REF")"

echo
echo "=== ② 逐条明细（北京时间 / 秒数 / 额度 / 状态）==="
q "SELECT created_at || '|' || id || '|' || COALESCE(sum(quota),0) || '|' || left(COALESCE(content,''),70) || '|' || left(COALESCE(request_id,''),16)
    FROM logs
   WHERE model_name='$MODEL' AND type=2 AND created_at >= $OCT_START AND created_at < $NOW
   GROUP BY created_at, id, content, request_id
   ORDER BY created_at" | while IFS='|' read -r ts id qc content rid; do
  echo "$(bj "$ts")  log#$id  $qc  ≈ ¥$(fmt "$qc")  req=$rid  | $content"
done

echo
echo "=== ③ 每日（北京时间）==="
q "SELECT d || '|' || n || '|' || s FROM (
      SELECT to_char(to_timestamp(created_at) AT TIME ZONE 'Asia/Shanghai','MM-DD') AS d,
             count(*) AS n, sum(quota) AS s
        FROM logs
       WHERE model_name='$MODEL' AND type=2 AND created_at >= $OCT_START AND created_at < $NOW
       GROUP BY 1) t ORDER BY d" | while IFS='|' read -r d n s; do
  echo "$d  条数=$n  额度=$s  ≈ ¥$(fmt "$s")"
done

echo
echo "=== ④ 渠道分布 ==="
q "SELECT c || '|' || n || '|' || s FROM (
      SELECT COALESCE(NULLIF(channel_name,''),'(null)') AS c, count(*) AS n, sum(quota) AS s
        FROM logs
       WHERE model_name='$MODEL' AND type=2 AND created_at >= $OCT_START AND created_at < $NOW
       GROUP BY 1) t ORDER BY s DESC" | while IFS='|' read -r c n s; do
  echo "$c  条数=$n  额度=$s  ≈ ¥$(fmt "$s")"
done

echo
echo "=== ⑤ 用户分布 ==="
q "SELECT u || '|' || tk || '|' || n || '|' || s FROM (
      SELECT username AS u, COALESCE(NULLIF(token_name,''),'-') AS tk, count(*) AS n, sum(quota) AS s
        FROM logs
       WHERE model_name='$MODEL' AND type=2 AND created_at >= $OCT_START AND created_at < $NOW
       GROUP BY 1,2) t ORDER BY s DESC" | while IFS='|' read -r u tk n s; do
  echo "$u / $tk  条数=$n  额度=$s  ≈ ¥$(fmt "$s")"
done

echo
echo "=== ⑥ tasks 表交叉核对（同期该渠道视频任务）==="
q "SELECT st || '|' || n || '|' || s FROM (
      SELECT status AS st, count(*) AS n, COALESCE(sum(quota),0) AS s
        FROM tasks
       WHERE created_at >= $OCT_START AND created_at < $NOW
         AND properties::text LIKE '%wan3.0-video-%'
       GROUP BY 1) t ORDER BY n DESC" | while IFS='|' read -r st n s; do
  echo "status=$st  条数=$n  任务额度=$s ≈ ¥$(fmt "$s")"
done

echo
echo "=== ⑦ 同期其它视频模型对比（10 月 consume）==="
q "SELECT m || '|' || n || '|' || s FROM (
      SELECT model_name AS m, count(*) AS n, sum(quota) AS s
        FROM logs
       WHERE model_name IN ('wan3.0-video-官网','wan3.0-smart','wan3.0-video-prime-1080p','wan2.7-r2v')
         AND type=2 AND created_at >= $OCT_START AND created_at < $NOW
       GROUP BY 1) t ORDER BY s DESC" | while IFS='|' read -r m n s; do
  echo "$m  条数=$n  额度=$s  ≈ ¥$(fmt "$s")"
done

echo
echo "=== ⑧ 该模型全历史（对照 9 月与更早）==="
q "SELECT m || '|' || n || '|' || s FROM (
      SELECT to_char(to_timestamp(created_at) AT TIME ZONE 'Asia/Shanghai','YYYY-MM') AS m,
             count(*) AS n, sum(quota) AS s
        FROM logs
       WHERE model_name='$MODEL' AND type=2
       GROUP BY 1) t ORDER BY m" | while IFS='|' read -r m n s; do
  echo "$m  条数=$n  额度=$s  ≈ ¥$(fmt "$s")"
done
