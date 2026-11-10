"""真机验证 manwu（type=66，生产在售渠道 20/30）能读 metadata.ratio（2026-11-10）。

背景：
- 2026-11-10 第二轮审查发现 manwu 的 clientRequest **没有 Metadata 字段**，
  metadata.ratio 被完全丢弃 → 客户端传 9:16 出成默认比例。
- 修复：clientRequest 补 Metadata 字段；四条比例路径改走公共解析器
  relaycommon.ResolveTaskRatio。

本脚本用真实 API 调用验证 metadata.ratio 是否真的被承接。

用法：
    python scripts/_probe_manwu_metadata_ratio_real.py
"""
import json
import subprocess
import sys
import time

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
API = "https://api.heibaidao.cn"

# seedance-2-5（manwu 渠道 20 在售的视频模型，走标准 task 链路）。
MODEL = "seedance-2-5"
EXPECT_RATIO = "9:16"


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

    # 关键：比例放在 **metadata.ratio** —— 这是 manwu 之前完全不支持的路径。
    payload = {
        "model": MODEL,
        "prompt": "一只橘猫在窗台上打盹，午后阳光斜照",
        "metadata": {"ratio": EXPECT_RATIO},
    }
    print(f"[submit] payload = {json.dumps(payload, ensure_ascii=False)}")
    raw = subprocess.run(
        ["curl", "-s", "-X", "POST", f"{API}/v1/video/generations",
         "-H", f"Authorization: Bearer {token}",
         "-H", "Content-Type: application/json",
         "-d", json.dumps(payload, ensure_ascii=True)],
        capture_output=True, text=True,
    ).stdout.strip()
    print(f"[submit] {raw[:400]}\n")

    time.sleep(4)
    snap = sql(
        "SELECT task_id || '|' || payload::text FROM task_logs "
        "WHERE stage = 'submit' ORDER BY id DESC LIMIT 1"
    ).strip()
    if not snap:
        print("❌ 没查到提交快照，验证失败")
        return 1

    task_public_id, payload_raw = snap.split("|", 1)
    print(f"[snapshot] task_id = {task_public_id}")
    try:
        payload_obj = json.loads(payload_raw)
    except Exception as exc:
        print(f"❌ 快照解析失败：{exc}")
        return 1
    params = payload_obj.get("parameters") or {}
    print(f"[snapshot] channel_id = {payload_obj.get('channel_id')}")
    for k in ("ratio", "ratio_source", "ratio_metadata", "ratio_top_level", "ratio_invalid"):
        if k in params:
            print(f"   {k:22s} = {params[k]!r}")
    if not params:
        print(f"   [payload 原文] {payload_raw[:400]}")

    ok_ratio = params.get("ratio") == EXPECT_RATIO
    ok_source = params.get("ratio_source") == "metadata.ratio"
    print()
    print(f"{'✅' if ok_ratio else '❌'} 快照生效比例 = {params.get('ratio')!r}（期望 {EXPECT_RATIO!r}）")
    print(f"{'✅' if ok_source else '❌'} 比例来源 = {params.get('ratio_source')!r}（期望 'metadata.ratio'）")
    return 0 if (ok_ratio and ok_source) else 1


if __name__ == "__main__":
    sys.exit(main())
