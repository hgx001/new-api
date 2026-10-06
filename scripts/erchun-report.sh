#!/usr/bin/env bash
# 把三份数据关联成一张表，并抽出 wan3 系列的完整目录条目。
set -uo pipefail
python3 <<'PY'
import json
L=lambda p: json.load(open(f"/tmp/erchun_{p}.json"))["data"]
models,catalog,pricing = L("models"), L("catalog"), L("pricing")

cby={c["model"]:c for c in catalog}
pby={p["model"]:p for p in pricing}
mids={m["id"] for m in models}

print("="*104)
print("模型清单（按 display_name 展示；稳定 ID 用于关联价格/能力/请求）")
print("="*104)
print(f"{'展示名':<26}{'稳定ID':<26}{'输出':<7}{'计费模式':<11}{'价格':<22}{'端点'}")
print("-"*104)
for m in sorted(models, key=lambda x: (x.get("output_type",""), x.get("display_name",""))):
    c=cby.get(m["id"],{}); p=pby.get(m["id"])
    pr = "—"
    if p:
        pr=f"{p['billing_mode']} {json.dumps(p['prices'],ensure_ascii=False)[:34]}"
        pr=pr.replace("'",'"')[:44]
    print(f"{m.get('display_name') or '(名称未配置)':<26}{m['id']:<26}{m.get('output_type',''):<7}"
          f"{(p or {}).get('billing_mode','—'):<11}{pr:<46}{m.get('endpoint','')}")

print()
print("="*104)
print("三份数据 ID 一致性核对")
print("="*104)
cid={c["model"] for c in catalog}; pid={p["model"] for p in pricing}
print(f"/v1/models {len(mids)} 个 | /v1/catalog {len(cid)} 个 | /v1/pricing {len(pid)} 个")
for label,s in (("catalog- models", cid-mids), ("models- catalog", mids-cid),
                ("pricing- catalog", pid-cid), ("catalog- pricing", cid-pid)):
    print(f"  {label}: {sorted(s) if s else '无'}")

print()
print("="*104)
print("wan3 系列完整目录条目（你只要这个）")
print("="*104)
for c in catalog:
    if "wan3" in c["model"].lower() or "wan3" in (c.get("display_name") or "").lower() \
       or "wan3" in (c.get("name") or "").lower():
        print(json.dumps(c, ensure_ascii=False, indent=2))
        print("-"*104)
PY
