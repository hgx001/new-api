"""查生产定价现状：ModelPrice / 模型卡片描述 / 渠道价目表（2026-10-30）。

老板反馈：「wan3.0-video-prime 描述和定价不一致，要以真实定价为准」。
本脚本一次性把三处口径拉出来比对：
  1. options.ModelPrice —— 站内实际扣费依据
  2. models 表的卡片描述 —— 用户在定价页看到的文案
  3. YouzanWan3Pricing —— 有赞同步的「上游成本 + 加价」策略
"""
import json
import subprocess

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"

WATCH = ("prime", "720p", "wan3", "video")


def sql(query: str) -> str:
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:400]}\n{query}")
    return out.stdout.strip()


def main():
    print("=" * 70)
    print("1) options.ModelPrice（站内实际扣费依据，quota/秒 = price × 500000）")
    print("=" * 70)
    raw = sql("SELECT value FROM options WHERE key='ModelPrice'")
    prices = json.loads(raw)
    for k in sorted(prices):
        if any(w in k for w in WATCH):
            v = prices[k]
            print(f"  {k}")
            print(f"      {v}")
            if isinstance(v, (int, float)):
                print(f"      → ¥{v * 7.3:.4f}/秒")
            elif isinstance(v, dict):
                for size, ratio in v.items():
                    print(f"      size={size}: ratio={ratio}")

    print()
    print("=" * 70)
    print("2) models 表卡片描述（用户看到的文案）")
    print("=" * 70)
    rows = sql(
        "SELECT model_name || '|' || COALESCE(description,'') "
        "FROM models WHERE model_name LIKE '%wan3%' OR model_name LIKE '%prime%' "
        "OR model_name LIKE '%720p%' ORDER BY model_name"
    )
    for line in rows.splitlines():
        if not line.strip():
            continue
        parts = line.split("|", 1)
        name = parts[0] if parts else ""
        desc = parts[1] if len(parts) > 1 else ""
        print(f"  【{name}】")
        print(f"      描述: {desc[:400]}")

    print()
    print("=" * 70)
    print("3) YouzanWan3Pricing（上游成本 + 加价策略）")
    print("=" * 70)
    raw = sql("SELECT value FROM options WHERE key='YouzanWan3Pricing'")
    print(f"  {raw}")

    print()
    print("=" * 70)
    print("4) 渠道 12（DashScope-Wan3）的 model_mapping")
    print("=" * 70)
    raw = sql("SELECT COALESCE(model_mapping,'') FROM channels WHERE id=12")
    print(f"  {raw}")

    print()
    print("=" * 70)
    print("5) 渠道 12 的 models 列表")
    print("=" * 70)
    raw = sql("SELECT models FROM channels WHERE id=12")
    print(f"  {raw}")

    print()
    print("=" * 70)
    print("6) abilities 里相关模型的分组/优先级")
    print("=" * 70)
    raw = sql(
        "SELECT model || '|' || group || '|' || priority || '|' || enabled "
        "FROM abilities WHERE model LIKE '%wan3%' OR model LIKE '%prime%' "
        "OR model LIKE '%720p%' ORDER BY model"
    )
    for line in raw.splitlines():
        if line.strip():
            print(f"  {line}")


if __name__ == "__main__":
    main()
