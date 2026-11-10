"""真机验证「顶层 ratio / aspect_ratio」在生产链路上真的生效（2026-11-10）。

背景：
- 用户报「传 9:16 生成 1:1」，首轮修复（b13dc5bf4）只补了 youzanwan3 与 megabyai。
- 本轮（9f75182f6）抽出全站唯一解析器 relay/common/task_ratio.go，7 个适配器统一口径，
  并给提交快照（logs.other）加了 ratio_source 标记。
- 本脚本用**真实 API 调用**验证：顶层 ratio 传下去后，
  上游实收比例 = 9:16，且快照里 ratio_source = ratio（=来自顶层字段）。

⚠️ 关键约束（务必读）：
1. 比例路由配置（options.Seedance25TeHuiRoute30s.trigger_ratio）已按老板要求**关闭并回滚**，
   所以 9:16 **不会**改派到心笔/内链链路 —— 任务应**留在原链路（17=有赞）**。
   这正是期望状态：不是「改派成功」，而是「比例正确」。
2. 本脚本会真建单并扣费（2 秒 × ¥0.20/秒 ≈ ¥0.40）。

用法：
    python scripts/_probe_toplevel_ratio_real.py
"""
import json
import subprocess
import sys
import time

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
API = "https://api.heibaidao.cn"

SECONDS = 2
# 原链路（有赞 Wan3，type=63）。比例路由关闭时应落这里。
EXPECT_CHANNEL = "17"


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


def submit(token: str, payload: dict) -> str:
    body = json.dumps(payload, ensure_ascii=True)
    return subprocess.run(
        ["curl", "-s", "-X", "POST", f"{API}/v1/video/generations",
         "-H", f"Authorization: Bearer {token}",
         "-H", "Content-Type: application/json",
         "-d", body],
        capture_output=True, text=True,
    ).stdout.strip()


def main() -> int:
    token = get_token()

    policy = sql("SELECT value FROM options WHERE key = 'Seedance25TeHuiRoute30s'")
    has_ratio_rule = "trigger_ratio" in policy
    print(f"[policy] trigger_ratio 存在 = {has_ratio_rule}（应为 False，比例路由已按要求关闭）")
    print(f"[policy] {policy[:200]}\n")

    before = int(sql("SELECT COALESCE(max(id),0) FROM logs").strip() or 0)
    print(f"[baseline] 最大 logs id = {before}")

    # 关键：比例放在**顶层** ratio，而不是 metadata.ratio。
    # 这正是首轮漏修、生产上报「传了 9:16 却出 1:1」的那条路径。
    payload = {
        "model": "seedance2.5-特惠",
        "prompt": "一只橘猫在窗台上打盹，午后阳光斜照",
        "seconds": str(SECONDS),
        "ratio": "9:16",
    }
    print(f"[submit] payload = {json.dumps(payload, ensure_ascii=False)}")
    raw = submit(token, payload)
    print(f"[submit] {raw[:300]}\n")

    time.sleep(5)

    # 找本次提交对应的**提交快照**（task_logs，stage=submit）。
    # ⚠️ 注意：不是 logs.other —— 提交快照存在 task_logs.payload 里，
    # 且比例参数嵌在 payload.parameters 子对象中。
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
    for k in ("ratio", "ratio_source", "ratio_top_level", "ratio_metadata",
              "aspect_ratio_top_level", "ratio_invalid"):
        if k in params:
            print(f"   {k:22s} = {params[k]!r}")
    if not params:
        print(f"   [payload 原文] {payload_raw[:400]}")

    channel_id = str(payload_obj.get("channel_id") or "")

    ok_ratio = params.get("ratio") == "9:16"
    ok_source = params.get("ratio_source") == "ratio"
    ok_channel = channel_id == EXPECT_CHANNEL

    print()
    print(f"{'✅' if ok_ratio else '❌'} 快照实际生效比例 = {params.get('ratio')!r}（期望 '9:16'）")
    print(f"{'✅' if ok_source else '❌'} 比例来源 ratio_source = {params.get('ratio_source')!r}（期望 'ratio' = 顶层字段）")
    print(f"{'✅' if ok_channel else '⚠️ '} 落点 channel_id = {channel_id}（期望 {EXPECT_CHANNEL}，比例路由已关闭不应改派）")
    return 0 if (ok_ratio and ok_source) else 1


if __name__ == "__main__":
    sys.exit(main())
