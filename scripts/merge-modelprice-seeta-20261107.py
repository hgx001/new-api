#!/usr/bin/env python3
"""合并更新 new-api options.ModelPrice：新增 minimax_h3_v2 / minimax_h3_omni。

按运维约定：先拉现有 JSON，取并集后再写回，绝不整字段覆盖。
基价 ¥0.03/秒 ÷ 7.3 = 0.00410958904109589（与退役旧名同口径）。
"""
import json
import subprocess

PSQL = ["sudo", "docker", "exec", "-i", "postgres", "psql", "-U", "root", "-d", "new-api", "-t", "-A", "-c"]
NEW_ENTRIES = {
    "minimax_h3_v2": 0.00410958904109589,
    "minimax_h3_omni": 0.00410958904109589,
}


def psql(sql: str) -> str:
    return subprocess.run(PSQL + [sql], capture_output=True, text=True, check=True).stdout.strip()


raw = psql("SELECT value FROM options WHERE key='ModelPrice';")
data = json.loads(raw)
added = {}
for k, v in NEW_ENTRIES.items():
    if k in data:
        print(f"skip {k}: already {data[k]}")
    else:
        data[k] = v
        added[k] = v

if added:
    merged = json.dumps(data, ensure_ascii=False)
    escaped = merged.replace("'", "''")
    psql(f"UPDATE options SET value='{escaped}' WHERE key='ModelPrice';")
    print("added:", added)
else:
    print("nothing to add")

# 复核
check = json.loads(psql("SELECT value FROM options WHERE key='ModelPrice';"))
for k in NEW_ENTRIES:
    print("verify", k, "=", check.get(k))
