"""验证「9:16 → wan3.0-720p-特惠」比例路由是否真的生效（2026-10-30）。

为什么不能用零成本探针（2026-10-30 实测结论，别再试）：
1. 客户端文案被 RedactUpstreamText 脱敏 —— "wan3" 和 "youzan" 都被替换成「上游」
   并收敛重复 → 两条链路的 400 文案**完全一样**，无法区分；
2. 素材校验失败是 LocalError，**不写 logs 表**，没有 channel_id 可查；
3. 用 metadata.media 传 bogus_type 想「在预扣前被拒」**不成立**：豆包路由的
   collectReferenceImages 不认 bogus_type → 静默忽略 → 变成一条没有素材的正常请求
   → **真建单扣费 ¥3.00**（task 874，上游 accepted 后 cancelled，已退款）。
→ 唯一可靠判据是**任务落在哪个渠道**，只能建一条真任务。

成本控制：2 秒 × ¥0.20/秒 = **¥0.40**（与老板之前批准的有赞 2 秒基线同价）。
wan3.0-720p-特惠 与 seedance2.5-特惠 都是 ¥0.20/秒，所以路由与否**同价**，
判据纯粹是 channel_id（12=DashScope wan3，17=有赞）。

用法：
    python scripts/_probe_seedance_ratio_real.py
"""
import json
import subprocess
import sys
import time

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
API = "https://api.heibaidao.cn"

SECONDS = 2


def sql(query: str) -> str:
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:400]}")
    return out.stdout.strip()


def get_token() -> str:
    row = sql(
        "SELECT t.key FROM tokens t JOIN users u ON u.id = t.user_id "
        "WHERE t.user_id = 28 AND t.status = 1 AND t.unlimited_quota = true "
        "AND u.deleted_at IS NULL ORDER BY t.id LIMIT 1"
    ).strip()
    if not row:
        raise RuntimeError("找不到 admin 的可用令牌")
    return row


def main() -> int:
    token = get_token()
    policy = sql("SELECT value FROM options WHERE key = 'Seedance25TeHuiRoute30s'")
    print(f"[policy] {policy}\n")

    before = int(sql("SELECT COALESCE(max(id),0) FROM tasks").strip() or 0)
    print(f"[baseline] 最大任务 id = {before}")

    payload = {
        "model": "seedance2.5-特惠",
        "prompt": "一只橘猫在窗台上打盹，午后阳光斜照",
        "seconds": str(SECONDS),
        "metadata": {"ratio": "9:16"},
    }
    body = json.dumps(payload, ensure_ascii=True)
    raw = subprocess.run(
        ["curl", "-s", "-X", "POST", f"{API}/v1/video/generations",
         "-H", f"Authorization: Bearer {token}",
         "-H", "Content-Type: application/json",
         "-d", body],
        capture_output=True, text=True,
    ).stdout.strip()
    print(f"[submit] {raw[:300]}")

    time.sleep(4)
    row = sql(
        "SELECT id || '|' || channel_id || '|' || status || '|' || quota || '|' || task_id "
        f"FROM tasks WHERE id > {before} ORDER BY id DESC LIMIT 1"
    ).strip()
    if not row:
        print("❌ 没建出任务，提交可能失败了")
        return 1

    task_id, channel_id, status, quota, public_id = row.split("|")
    cny = int(quota) / 500000 * 7.3
    print(f"\n[task] id={task_id} ch={channel_id} status={status} quota={quota} = ¥{cny:.4f}")
    print(f"       public_id={public_id}")

    routed = channel_id == "12"
    print(f"\n{'✅ 落在渠道 12（DashScope wan3）→ 比例规则生效' if routed else '➡️ 落在渠道 %s → 未按预期路由' % channel_id}")

    print("\n=== 任务属性 ===")
    print(sql(f"SELECT properties::text FROM tasks WHERE id = {task_id}")[:300])

    print("\n=== 等待出片（最长 10 分钟）===")
    deadline = time.time() + 600
    while time.time() < deadline:
        current = sql(f"SELECT status || '|' || COALESCE(progress,'-') FROM tasks WHERE id = {task_id}").strip()
        print(f"  [{time.strftime('%H:%M:%S')}] {current}")
        if current.startswith("SUCCESS") or current.startswith("FAILURE"):
            break
        time.sleep(20)

    final = sql(f"SELECT status || '|' || COALESCE(fail_reason,'-') FROM tasks WHERE id = {task_id}").strip()
    print(f"\n=== 最终状态 ===\n  {final}")
    return 0 if routed else 1


if __name__ == "__main__":
    sys.exit(main())
