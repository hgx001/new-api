"""把「特惠尽量走有赞、只有 <10 秒走 CapCut」落到生产配置（2026-11-16 老板口径）。

口径原文：
  1. 「2.5-特惠这个你要保证尽量用有赞的模型 只有接不住的时候才路由到capcut」
  2. 「2.5-活动这个就保持现在的配置」
  3. 「特惠的除了10以下的请求 路由 别的都走有赞」
  4. 「特惠30秒的请求要走有赞的prime的模型」（代码侧已实现）

本脚本只改**特惠**相关的闸门，活动版的覆盖条目保持原样（除对齐缺失字段）。

改了什么、为什么：
  · short_youzan_enabled = true
      规则六第二分支（有赞标准版 wan3.0-video）打开 —— 恰好 10 秒的请求走它。
      这是**全局**字段，但活动版有 short_rule_enabled=false，不受它影响。
  · source_overrides["seedance2.5-特惠"].seconds_rule_enabled = false
      关掉规则一（>20 秒 → 豆包 seedance-2-5）。豆包开关到点会自动恢复，
      恢复后特惠又会被改派到豆包 —— 与口径 3 冲突，必须用**源模型级**开关钉死。
  · source_overrides["seedance2.5-特惠"].ecommerce_rule_enabled = false
      关掉规则七（电商内容 → CapCut 2.5 Flash）。口径 1 要求电商流量也走有赞。
  · source_overrides["seedance2.5-活动"].short_rule_enabled = false
      新机缺这个字段（活动版规则六默认开启 → 短请求会被改派 CapCut）。
      老机已有，本次把新机对齐到老机 —— 双机配置必须一致，否则行为分叉。

不动的东西（逐字保留）：
  · 规则二（9:16 → 心笔内链）：特惠已是 ratio_rule_enabled=false。
  · 规则三（首尾帧 → wan3.0-720p-特惠）：**能力约束**，有赞上游 2026-10-28 起
    彻底不收首尾帧，只有 wan3 直连能跑；关掉它带首尾帧的请求直接 400。
  · 规则五（故障转移）：源模型是 seedance-2-5，与特惠无关。
  · 规则八（全量兜底）：保持关闭 —— 打开就是「全部到 2.5 Flash」，
    <10 秒就落不到 cp-seedance-2.0 满血档了。
  · short_capcut_enabled = true：保留，<10 秒正是它接（cp-seedance-2.0）。
  · short_link_enabled = false：保留，心笔是跨渠道链路，与「尽量用有赞」不符。
"""
import json
import subprocess
import time

HOSTS = [
    ("old", ["ssh", "-p", "877", "ubuntu@119.29.253.97"]),
    ("new", ["ssh", "newapi"]),
]
KEY = "Seedance25TeHuiRoute30s"
PSQL = "docker exec -i postgres psql -U root -d new-api -t -A"
TEHUI = "seedance2.5-特惠"
HUODONG = "seedance2.5-活动"


def psql(host, query: str) -> str:
    out = subprocess.run(
        host + [PSQL], input=query, capture_output=True, text=True,
    )
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed on {host[0]}: {out.stderr.strip()[:500]}")
    return out.stdout.strip()


def read_option(host) -> dict:
    raw = psql(host, f"SELECT value FROM options WHERE key = '{KEY}';")
    if not raw:
        raise RuntimeError(f"{host[0]}: option 不存在")
    return json.loads(raw)


def apply_policy(policy: dict) -> dict:
    """按口径改配置；只动该动的字段，其余原样保留。"""
    policy["short_youzan_enabled"] = True
    # 规则六三路互斥：心笔必须显式 false（三态指针 nil = 默认开启）。
    policy["short_link_enabled"] = False
    # CapCut 满血档保留：<10 秒正是它接。
    policy["short_capcut_enabled"] = True
    # 规则八保持关闭：它是「有赞彻底不可用」的逃生口，打开就全量改派 2.5 Flash。
    policy["total_fallback_enabled"] = False

    overrides = policy.setdefault("source_overrides", {})
    tehui = overrides.setdefault(TEHUI, {})
    # 规则一（>20 秒 → 豆包）关闭：特惠除 <10 秒外都走有赞。
    tehui["seconds_rule_enabled"] = False
    # 规则七（电商内容 → CapCut）关闭：电商流量也要走有赞。
    tehui["ecommerce_rule_enabled"] = False

    huodong = overrides.setdefault(HUODONG, {})
    # 活动版规则六关闭（对齐老机；活动版按次计价，不参与短时长分流）。
    huodong["short_rule_enabled"] = False

    policy["updated_at"] = int(time.time())
    return policy


def main():
    for label, host in HOSTS:
        print("=" * 72)
        print(f"{label} 机")
        print("=" * 72)
        before = read_option(host)
        after = apply_policy(json.loads(json.dumps(before)))

        print("改动前关键字段：")
        print("  short_youzan_enabled   =", before.get("short_youzan_enabled"))
        print("  short_link_enabled     =", before.get("short_link_enabled"))
        print("  short_capcut_enabled   =", before.get("short_capcut_enabled"))
        print("  total_fallback_enabled =", before.get("total_fallback_enabled"))
        print("  特惠 override          =",
              before.get("source_overrides", {}).get(TEHUI))
        print("  活动 override          =",
              before.get("source_overrides", {}).get(HUODONG))

        payload = json.dumps(after, ensure_ascii=False, separators=(",", ":"))
        psql(host, f"UPDATE options SET value = '{payload}' WHERE key = '{KEY}';")

        check = read_option(host)
        assert check["short_youzan_enabled"] is True
        assert check["short_link_enabled"] is False
        assert check["short_capcut_enabled"] is True
        assert check["total_fallback_enabled"] is False
        assert check["source_overrides"][TEHUI]["seconds_rule_enabled"] is False
        assert check["source_overrides"][TEHUI]["ecommerce_rule_enabled"] is False
        assert check["source_overrides"][HUODONG]["short_rule_enabled"] is False
        # 其余关键字段逐字保留
        for key in ("target_model", "trigger_seconds", "seconds_rule_ratio_gate",
                    "ratio_target_model", "trigger_ratio", "frames_target_model",
                    "short_target_model", "short_trigger_seconds",
                    "failover_source_model", "failover_target_model",
                    "failover_enabled", "failover_alt_enabled",
                    "ecommerce_enabled", "ecommerce_ratio_condition",
                    "ecommerce_trigger_seconds", "doubao_link_off_until"):
            assert check.get(key) == before.get(key), f"{key} 被改动了"
        assert check["source_overrides"][TEHUI].get("ratio_rule_enabled") is False
        print()
        print("改动后关键字段：")
        print("  short_youzan_enabled   =", check.get("short_youzan_enabled"))
        print("  short_link_enabled     =", check.get("short_link_enabled"))
        print("  short_capcut_enabled   =", check.get("short_capcut_enabled"))
        print("  total_fallback_enabled =", check.get("total_fallback_enabled"))
        print("  特惠 override          =", check["source_overrides"][TEHUI])
        print("  活动 override          =", check["source_overrides"][HUODONG])
        print()
        print("OK: 其余字段逐字保留")


if __name__ == "__main__":
    main()
