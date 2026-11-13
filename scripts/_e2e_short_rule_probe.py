"""规则六的边界与优先级真机验证。

两条：
  A) 11 秒 + 16:9  → **不该**分流（阈值是 ≤10 秒），应留在源链路 prime
  B)  2 秒 + 9:16  → **该**分流（规则六优先于规则二的比例规则）

为什么必须真机验：单测里 stub 了渠道探针、比例目标也是测试值；
生产配的 ratio_target_model 是 `wan3.0-720p-特惠-内链`（心笔），
与单测不同 —— 优先级这件事只有在生产配置下跑一遍才算数。
"""
import base64
import json
import os
import struct
import subprocess
import sys
import tempfile
import zlib

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
SCP = ["scp", "-P", "877"]
TOKEN_ID = 144


def make_png(width=1280, height=720) -> bytes:
    def chunk(tag: bytes, data: bytes) -> bytes:
        return (struct.pack(">I", len(data)) + tag + data
                + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))

    rows = bytearray()
    for y in range(height):
        rows.append(0)
        band = (y // 90) % 3
        color = [(232, 168, 96), (120, 168, 232), (168, 232, 120)][band]
        rows.extend(bytes(color) * width)
    ihdr = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr)
            + chunk(b"IDAT", zlib.compress(bytes(rows), 9)) + chunk(b"IEND", b""))


def send(seconds: int, ratio: str, prompt: str, workdir: str,
         resolution: str | None = None) -> None:
    png = make_png()
    payload = {
        "model": "seedance2.5-特惠",
        "prompt": prompt,
        "seconds": seconds,
        "media": [{"type": "reference_image",
                   "url": "data:image/png;base64," + base64.b64encode(png).decode()}],
        "metadata": {"ratio": ratio},
    }
    if resolution:
        payload["resolution"] = resolution
    body = json.dumps(payload, ensure_ascii=False)

    req_path = os.path.join(workdir, "req.json")
    with open(req_path, "w", encoding="utf-8") as fh:
        fh.write(body)

    script = f"""#!/bin/bash
set -uo pipefail
KEY=$(docker exec postgres psql -U root -d new-api -t -A -c 'SELECT "key" FROM tokens WHERE id={TOKEN_ID}' | tr -d '\\r\\n')
KEY="sk-$KEY"
echo "=== 请求（{seconds} 秒 / {ratio}） ==="
RESP=$(curl -s -X POST http://127.0.0.1:3000/v1/video/generations \\
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \\
  -d @/tmp/req.json)
echo "$RESP"
TID=$(echo "$RESP" | sed -n 's/.*"id":"\\(task_[^"]*\\)".*/\\1/p')
echo "task_id=$TID"
sleep 3
echo "=== 路由日志 ==="
docker logs --since 40s new-api 2>&1 | grep "条件路由" | tail -3
echo "=== 任务落库 ==="
docker exec postgres psql -U root -d new-api -t -A -F"|" -c \\
  "SELECT id,channel_id,quota,status,properties->>'origin_model_name' FROM tasks WHERE task_id='$TID'"
"""
    sh_path = os.path.join(workdir, "run.sh")
    with open(sh_path, "w", encoding="utf-8", newline="\n") as fh:
        fh.write(script)

    subprocess.run(SCP + [req_path, "ubuntu@119.29.253.97:/tmp/req.json"], check=True)
    subprocess.run(SCP + [sh_path, "ubuntu@119.29.253.97:/tmp/run.sh"], check=True)
    out = subprocess.run(SSH + ["bash /tmp/run.sh"], capture_output=True, text=True)
    print(out.stdout)
    if out.stderr.strip():
        print("[stderr]", out.stderr.strip()[:300])


def main():
    workdir = tempfile.mkdtemp(prefix="shortprobe_")
    # 带参数时只跑一条：`probe.py 2 16:9 1080P`
    if len(sys.argv) >= 3:
        resolution = sys.argv[3] if len(sys.argv) > 3 else None
        print("#" * 72)
        print(f"# 单条：{sys.argv[1]} 秒 / {sys.argv[2]} / {resolution or '默认档'}")
        print("#" * 72)
        send(int(sys.argv[1]), sys.argv[2], "海边礁石，镜头缓慢推近，晨雾", workdir,
             resolution=resolution)
        return

    print("#" * 72)
    print("# A) 11 秒 + 16:9 —— 期望不分流（留在 prime）")
    print("#" * 72)
    send(11, "16:9", "一只橘猫在窗台上打哈欠，镜头缓慢推近，暖光", workdir)
    print()
    print("#" * 72)
    print("# B) 2 秒 + 9:16 —— 期望分流（规则六优先于规则二）")
    print("#" * 72)
    send(2, "9:16", "雨后老街的霓虹倒影，镜头缓慢横移", workdir)


if __name__ == "__main__":
    sys.exit(main())
