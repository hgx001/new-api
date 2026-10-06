import json
cat = json.load(open("/tmp/erchun_catalog.json"))["data"]
pri = {p["model"]: p for p in json.load(open("/tmp/erchun_pricing.json"))["data"]}
W = "mdl_ec286eb1bc618249d65b87bc79366417"
c = [x for x in cat if x["model"] == W][0]

print("### Wan3.0 其余参数字段")
for k in ("durations", "requires_image", "parameter_status", "qualities", "default_quality"):
    print(f"  {k} = {json.dumps(c.get(k), ensure_ascii=False)}")

print("\n### /v1/pricing 条目")
print(json.dumps(pri[W], ensure_ascii=False, indent=2))

print("\n### 全目录：输出类型 + 计费模式 + 价格（核对 units）")
for x in cat:
    p = pri.get(x["model"], {})
    pr = p.get("prices", {})
    print(f"  {x['display_name'][:22]:<24} {x['output_type']:<6} {p.get('billing_mode',''):<22} {json.dumps(pr, ensure_ascii=False)[:78]}")
