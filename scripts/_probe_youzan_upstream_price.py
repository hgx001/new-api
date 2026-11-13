"""查有赞上游的真实价目表（2026-10-30）。

老板说「wan3.0-video-prime 描述和定价不一致，要以真实定价为准」。
**真实定价 = 上游 `/v1/models` 返回的 unitPrice / resolutionPrices**，
然后叠加我们自己的 markup（加价）。

本脚本直接打上游，拿到原始数据 + 算出我们实际该卖多少。
"""
import json
import subprocess
import sys

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"

BASE = "https://youzan666.vip"


def sql(query: str) -> str:
    out = subprocess.run(SSH + [f'{PG} -c "{query}"" '], capture_output=True, text=True)
    return out.stdout.strip()


def sql_safe(query: str) -> str:
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:300]}")
    return out.stdout.strip()


def main():
    # 从生产渠道 17 取 key（不打印明文，只用）
    raw = sql_safe(
        "SELECT key FROM channels WHERE id=17"
    )
    api_key = raw.strip()
    if not api_key:
        print("拿不到渠道 17 的 key")
        return 1

    print("=== 拉取上游 /v1/models（真实价目表）===")
    out = subprocess.run(
        ["curl", "-sS", "--max-time", "30", "-H", f"Authorization: Bearer {api_key}",
         f"{BASE}/v1/models"],
        capture_output=True, text=True,
    )
    if out.returncode != 0:
        print("请求失败:", out.stderr[:300])
        return 1

    try:
        data = json.loads(out.stdout)
    except Exception:
        print("非 JSON 响应:", out.stdout[:500])
        return 1

    models = data.get("data") or data.get("models") or []
    if isinstance(models, dict):
        models = models.get("data", [])

    for m in models:
        name = m.get("id") or m.get("name") or "?"
        if "prime" not in name and "wan3" not in name:
            continue
        print(f"\n【{name}】")
        print(f"  完整字段: {json.dumps(m, ensure_ascii=False, indent=2)[:2000]}")

    # ---- 我们自己的 markup 策略 ----
    print("\n" + "=" * 70)
    print("我们的加价策略（options.YouzanWan3Pricing）")
    print("=" * 70)
    raw = sql_safe("SELECT value FROM options WHERE key='YouzanWan3Pricing'")
    policy = json.loads(raw)
    markup = policy.get("markup", {})
    ratios = policy.get("ratios", {})

    print(f"  markup: {json.dumps(markup, ensure_ascii=False)}")
    print(f"  ratios: {json.dumps(ratios, ensure_ascii=False)}")

    # ---- 逐档算：上游成本 × ratio + markup = 我们该卖的价 ----
    print("\n" + "=" * 70)
    print("推算各档真实售价（上游成本 × size ratio + 加价）")
    print("=" * 70)
    for m in models:
        name = m.get("id") or m.get("name") or "?"
        if name not in markup:
            continue
        rp = m.get("resolutionPrices") or {}
        mk = markup[name]
        print(f"\n【{name}】")
        for res, info in rp.items():
            cost = info.get("price") if isinstance(info, dict) else info
            unit = info.get("unit") if isinstance(info, dict) else None
            size_ratio = (ratios.get(name, {}) or {}).get(res, 1)
            add = mk.get(res, mk.get("*", 0))
            sell = float(cost) * float(size_ratio) + float(add)
            print(f"  {res}: 上游 {cost} {unit or ''} × ratio {size_ratio} + 加价 {add} "
                  f"= 售价 ¥{sell:.4f}")

    return 0


if __name__ == "__main__":
    sys.exit(main())
