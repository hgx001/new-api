"""验证「素材源站问题 = 400，不 500」这条边界在线上真的生效。

2026-11-12 审查发现的隐患：素材 404 原本走 500 build_request_failed，
而 controller/relay.go 对非 LocalError 会 processChannelError ——
客户填错一个 URL 就能把有赞渠道打到 AutoBan。

跑三组：
  1. 素材地址 404        → 期望 400 + 中文文案，且渠道保持启用
  2. 素材地址指向内网    → 期望 400（SSRF 拦截）
  3. 正常 http 参考图    → 期望 202/200 建单成功（回归）
"""
import json
import os
import subprocess
import tempfile
import time

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
SCP = ["scp", "-P", "877"]

VALID_IMAGE = (
    "https://ai.youzan666.vip/media/ast_0ae619d6-c3b9-4259-bd70-5c6b06a499c4"
    "?exp=1797085773&sig=QEFk1L752h3mnBG6z7jAVa1Esfdq9tvaX_9P97CUmwk"
)
MISSING_IMAGE = "https://ai.youzan666.vip/media/ast_this_asset_does_not_exist"


def request_for(url: str) -> dict:
    return {
        "model": "seedance2.5-特惠",
        "prompt": "一只橘猫在窗台上打哈欠",
        "duration": 2,
        "media": [{"type": "reference_image", "url": url}],
        "metadata": {"ratio": "16:9"},
    }


SCRIPT = """#!/bin/bash
set -uo pipefail
KEY=$(docker exec postgres psql -U root -d new-api -t -A -c 'SELECT "key" FROM tokens WHERE id=144' | tr -d '\\r\\n')
KEY="sk-$KEY"

send() {
  local label="$1" file="$2"
  echo "=== $label ==="
  curl -s -m 120 -o /tmp/assetfix_resp.json -w "HTTP %{http_code}  耗时 %{time_total}s\\n" \\
    -X POST http://127.0.0.1:3000/v1/video/generations \\
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \\
    -d @"$file"
  head -c 400 /tmp/assetfix_resp.json; echo; echo
}

send "1) 素材 404" /tmp/assetfix_missing.json
send "2) 素材指向内网" /tmp/assetfix_internal.json
send "3) 正常参考图" /tmp/assetfix_valid.json

echo "=== 渠道 17 状态（必须仍为启用） ==="
docker exec postgres psql -U root -d new-api -t -A -c \\
  "SELECT id, name, status, coalesce(left(disabled_reason,60),'-') FROM channels WHERE id=17"
"""


def main():
    workdir = tempfile.mkdtemp(prefix="assetfix_e2e_")
    payloads = {
        "assetfix_missing.json": request_for(MISSING_IMAGE),
        "assetfix_internal.json": request_for("http://10.0.0.8/secret.jpg"),
        "assetfix_valid.json": request_for(VALID_IMAGE),
    }
    paths = []
    for name, payload in payloads.items():
        path = os.path.join(workdir, name)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(json.dumps(payload, ensure_ascii=False))
        paths.append(path)

    sh_path = os.path.join(workdir, "run_assetfix_e2e.sh")
    with open(sh_path, "w", encoding="utf-8", newline="\n") as fh:
        fh.write(SCRIPT)
    paths.append(sh_path)

    for local in paths:
        subprocess.run(SCP + [local, "ubuntu@119.29.253.97:/tmp/"], check=True)

    started = time.time()
    out = subprocess.run(
        SSH + ["bash /tmp/run_assetfix_e2e.sh"], capture_output=True, text=True
    )
    print(out.stdout)
    if out.stderr.strip():
        print("[stderr]", out.stderr.strip()[:500])
    print(f"[总耗时] {time.time() - started:.1f}s")


if __name__ == "__main__":
    main()
