"""素材链路改造后的真机回归：走 http 参考图，覆盖「下载 → 重编码 → 上传」全路径。

与 _e2e_short_rule.py 的区别：那条用 data URL，**跳过了下载这一步**，
而本次改的正是下载的 context 与预算。所以这里必须用真实 http(s) 素材。

素材用任务 2627 里上游回执的签名地址（有赞自己的 media，仍可访问），
好处是不依赖任何第三方图床是否被墙。
"""
import json
import os
import subprocess
import tempfile
import time

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
SCP = ["scp", "-P", "877"]

REFERENCE_IMAGE = (
    "https://ai.youzan666.vip/media/ast_0ae619d6-c3b9-4259-bd70-5c6b06a499c4"
    "?exp=1797085773&sig=QEFk1L752h3mnBG6z7jAVa1Esfdq9tvaX_9P97CUmwk"
)


def build_request() -> dict:
    return {
        "model": "seedance2.5-特惠",
        "prompt": "一只橘猫在窗台上打哈欠，镜头缓慢推近，暖光",
        "duration": 2,
        "media": [{"type": "reference_image", "url": REFERENCE_IMAGE}],
        "metadata": {"ratio": "16:9"},
    }


SCRIPT = """#!/bin/bash
set -uo pipefail
KEY=$(docker exec postgres psql -U root -d new-api -t -A -c 'SELECT "key" FROM tokens WHERE id=144' | tr -d '\\r\\n')
KEY="sk-$KEY"
echo "=== 发请求（http 参考图，走下载+重编码+上传） ==="
START=$(date +%s.%N)
RESP=$(curl -s -m 120 -X POST http://127.0.0.1:3000/v1/video/generations \\
  -H "Authorization: Bearer $KEY" \\
  -H "Content-Type: application/json" \\
  -d @/tmp/assetfix_req.json)
END=$(date +%s.%N)
echo "$RESP"
echo "提交耗时: $(echo "$END - $START" | bc) 秒"
echo
echo "=== 容器日志（素材/路由相关） ==="
sleep 2
docker logs --since 60s new-api 2>&1 | grep -E "条件路由|素材|超时|error|ERROR" | tail -10
"""


def main():
    body = json.dumps(build_request(), ensure_ascii=False)
    workdir = tempfile.mkdtemp(prefix="assetfix_")
    req_path = os.path.join(workdir, "assetfix_req.json")
    sh_path = os.path.join(workdir, "run_assetfix.sh")
    with open(req_path, "w", encoding="utf-8") as fh:
        fh.write(body)
    with open(sh_path, "w", encoding="utf-8", newline="\n") as fh:
        fh.write(SCRIPT)

    for local in (req_path, sh_path):
        subprocess.run(SCP + [local, "ubuntu@119.29.253.97:/tmp/"], check=True)

    started = time.time()
    out = subprocess.run(
        SSH + ["bash /tmp/run_assetfix.sh"], capture_output=True, text=True
    )
    print(out.stdout)
    if out.stderr.strip():
        print("[stderr]", out.stderr.strip()[:500])
    print(f"[本地观测总耗时] {time.time() - started:.1f}s")


if __name__ == "__main__":
    main()
