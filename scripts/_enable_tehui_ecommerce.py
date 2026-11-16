#!/usr/bin/env python3
"""在主机上清除 source_overrides["seedance2.5-特惠"].ecommerce_rule_enabled。

为什么删键而不是写 true：该字段是**三态指针**（*bool），语义是
「没配 = 跟随全局开关」。写 true 会把特惠钉死成「即使全局关掉也生效」，
与另外三个按源模型开关的语义不一致。删键才是「回到默认」。

用法：python3 _enable_tehui_ecommerce.py [--apply]
不带 --apply 时只打印将要写入的值（dry-run）。
"""
import json
import subprocess
import sys
import time

OPTION_KEY = "Seedance25TeHuiRoute30s"
SOURCE = "seedance2.5-特惠"
FIELD = "ecommerce_rule_enabled"
PSQL = ["sudo", "docker", "exec", "-i", "postgres", "psql", "-U", "root", "-d", "new-api"]


def read_option() -> str:
    out = subprocess.run(
        PSQL + ["-A", "-t", "-c", f"select value from options where key='{OPTION_KEY}'"],
        capture_output=True, text=True, check=True,
    )
    return out.stdout.strip()


def write_option(value: str) -> None:
    # 用 dollar-quoting 传值，避免 JSON 里的单引号打断 SQL 字面量。
    sql = f"UPDATE options SET value = $seedance${value}$seedance$ WHERE key = '{OPTION_KEY}';"
    subprocess.run(
        PSQL + ["-v", "ON_ERROR_STOP=1", "-f", "-"],
        input=sql, text=True, check=True,
    )


def main() -> int:
    raw = read_option()
    if not raw:
        print("ERROR: option not found in database")
        return 1

    policy = json.loads(raw)
    overrides = policy.get("source_overrides") or {}
    override = overrides.get(SOURCE)

    print("=== before ===")
    print(f"ecommerce_enabled (global) = {policy.get('ecommerce_enabled')}")
    print(f"source_overrides[{SOURCE}] = {json.dumps(override, ensure_ascii=False)}")

    if override is None:
        print(f"\nSKIP: {SOURCE} 没有 override，本来就跟随全局开关，无需改动")
        return 0
    if FIELD not in override:
        print(f"\nSKIP: override 里没有 {FIELD}，本来就跟随全局开关")
        return 0

    del override[FIELD]
    if not override:
        # override 已经不承载任何语义，留着空对象只会让后来人以为「这里配过东西」。
        del overrides[SOURCE]
        print(f"\nnote: {SOURCE} 的 override 清空后已整体移除")
    policy["updated_at"] = int(time.time())

    new_value = json.dumps(policy, ensure_ascii=False, separators=(",", ":"))
    print("\n=== after ===")
    print(f"ecommerce_enabled (global) = {policy.get('ecommerce_enabled')}")
    print(f"source_overrides = {json.dumps(policy.get('source_overrides'), ensure_ascii=False)}")

    if "--apply" not in sys.argv:
        print("\n[dry-run] 未写入。加 --apply 执行。")
        return 0

    with open("/tmp/tehui_ecom_backup.json", "w", encoding="utf-8") as fh:
        fh.write(raw)
    write_option(new_value)
    print("\nwritten. backup -> /tmp/tehui_ecom_backup.json")
    return 0


if __name__ == "__main__":
    sys.exit(main())
