#!/usr/bin/env python3
"""把新的「电商 → 有赞优先」口径套到 api2 实例的策略上。

与老机（api.heibaidao.cn）对齐的三件事：
  1. ecommerce_enabled = true（api2 原来整条规则七是关的 —— 缺键）；
  2. 删掉 source_overrides["seedance2.5-特惠"].ecommerce_rule_enabled
     （三态指针，删键 = 跟随全局；写 true 会把特惠钉死成「全局关了也生效」）；
  3. updated_at 刷成当前时间（否则后台显示的更新时间是旧的）。

**不动** api2 自己的 ecommerce_prompt_keywords（它比默认表更细，
是这台实例的运营配置，覆盖掉等于替别人改业务）。

用法：python3 _apply_ecom_policy.py [--apply]
"""
import json
import subprocess
import sys
import time

OPTION_KEY = "Seedance25TeHuiRoute30s"
TEHUI = "seedance2.5-特惠"
FIELD = "ecommerce_rule_enabled"
PSQL = ["docker", "exec", "-i", "postgres", "psql", "-U", "root", "-d", "new-api"]


def read_option() -> str:
    out = subprocess.run(
        PSQL + ["-A", "-t", "-c", f"select value from options where key='{OPTION_KEY}'"],
        capture_output=True, text=True, check=True,
    )
    return out.stdout.strip()


def write_option(value: str) -> None:
    sql = f"UPDATE options SET value = $seedance${value}$seedance$ WHERE key = '{OPTION_KEY}';"
    subprocess.run(PSQL + ["-v", "ON_ERROR_STOP=1", "-f", "-"], input=sql, text=True, check=True)


def main() -> int:
    raw = read_option()
    if not raw:
        print("ERROR: option row missing")
        return 1

    policy = json.loads(raw)
    print("=== before ===")
    print(f"ecommerce_enabled          = {policy.get('ecommerce_enabled', '<缺 = 关>')}")
    print(f"ecommerce_ratio_condition  = {policy.get('ecommerce_ratio_condition', '<缺>')}")
    print(f"keywords count             = {len(policy.get('ecommerce_prompt_keywords') or [])}")
    print(f"overrides[特惠]            = {json.dumps((policy.get('source_overrides') or {}).get(TEHUI), ensure_ascii=False)}")

    changed = False
    if policy.get("ecommerce_enabled") is not True:
        policy["ecommerce_enabled"] = True
        changed = True

    overrides = policy.get("source_overrides") or {}
    override = overrides.get(TEHUI)
    if isinstance(override, dict) and FIELD in override:
        del override[FIELD]
        if not override:
            del overrides[TEHUI]
        changed = True

    if not changed:
        print("\nSKIP: 已经是对齐状态，无需改动")
        return 0

    policy["updated_at"] = int(time.time())
    print("\n=== after ===")
    print(f"ecommerce_enabled          = {policy.get('ecommerce_enabled')}")
    print(f"keywords count             = {len(policy.get('ecommerce_prompt_keywords') or [])}（保持不变）")
    print(f"overrides[特惠]            = {json.dumps(overrides.get(TEHUI), ensure_ascii=False)}")

    if "--apply" not in sys.argv:
        print("\n[dry-run] 未写入。加 --apply 执行。")
        return 0

    with open("/tmp/tehui_ecom_backup_api2.json", "w", encoding="utf-8") as fh:
        fh.write(raw)
    write_option(json.dumps(policy, ensure_ascii=False, separators=(",", ":")))
    print("\nwritten. backup -> /tmp/tehui_ecom_backup_api2.json")
    return 0


if __name__ == "__main__":
    sys.exit(main())
