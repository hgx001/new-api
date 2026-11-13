"""规则六的真机 E2E：2 秒 + 带图片，看它是否被改派到 wan3.0-video。

做法：
  1. 本地用标准库生成一张 1280x720 的 PNG（纯色 + 条纹，压缩后几 KB），
     转成 data URL 塞进 media —— 不依赖任何外网图片是否可达。
  2. JSON 与 shell 脚本一起 scp 到生产机。
  3. 远程 curl 发请求（token 从 DB 读，不经过本地日志）。
  4. 抓容器日志里的「命中短时长」与任务落库后的模型名。

为什么用 data URL：adaptor 的 toDataURL 对 `data:` 前缀**原样透传**，
省掉上游下载这一步，也排除了「图片 404 导致测试失败」的干扰。
"""
import base64
import json
import os
import struct
import subprocess
import tempfile
import zlib

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
SCP = ["scp", "-P", "877"]


def make_png(width: int, height: int) -> bytes:
    """生成一张带横向条纹的 PNG（标准库实现，无第三方依赖）。"""

    def chunk(tag: bytes, data: bytes) -> bytes:
        return (
            struct.pack(">I", len(data))
            + tag
            + data
            + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
        )

    rows = bytearray()
    for y in range(height):
        rows.append(0)  # filter type: none
        # 每 90 行换一种暖色，让图有可辨识的结构（纯色图容易被判成空图）
        band = (y // 90) % 3
        color = [(232, 168, 96), (120, 168, 232), (168, 232, 120)][band]
        rows.extend(bytes(color) * width)

    ihdr = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", ihdr)
        + chunk(b"IDAT", zlib.compress(bytes(rows), 9))
        + chunk(b"IEND", b"")
    )


def build_request(ratio: str, prompt: str) -> dict:
    png = make_png(1280, 720)
    data_url = "data:image/png;base64," + base64.b64encode(png).decode()
    return {
        "model": "seedance2.5-特惠",
        "prompt": prompt,
        "seconds": 2,
        "media": [{"type": "reference_image", "url": data_url}],
        "metadata": {"ratio": ratio},
    }


def main():
    req = build_request("16:9", "一只橘猫在窗台上打哈欠，镜头缓慢推近，暖光")
    body = json.dumps(req, ensure_ascii=False)
    print(f"请求体大小: {len(body)} 字节（data URL {len(req['media'][0]['url'])} 字节）")

    # ⚠️ 必须用 Windows 的临时目录：Python 把 "/tmp/x" 解析成 C:\tmp\x，
    # 而 Git Bash 的 scp 把 /tmp 解析成 MSYS 目录 —— 两边不是同一个地方。
    workdir = tempfile.mkdtemp(prefix="shortrule_")
    req_path = os.path.join(workdir, "short_rule_req.json")
    sh_path = os.path.join(workdir, "run_short_test.sh")

    with open(req_path, "w", encoding="utf-8") as fh:
        fh.write(body)

    script = """#!/bin/bash
set -uo pipefail
KEY=$(docker exec postgres psql -U root -d new-api -t -A -c 'SELECT "key" FROM tokens WHERE id=144' | tr -d '\\r\\n')
# 库里存的是不带前缀的 48 位随机串，对外请求必须带 sk-
KEY="sk-$KEY"
echo "token 长度: ${#KEY}"
echo "=== 发请求 ==="
curl -s -X POST http://127.0.0.1:3000/v1/video/generations \\
  -H "Authorization: Bearer $KEY" \\
  -H "Content-Type: application/json" \\
  -d @/tmp/short_rule_req.json
echo
echo "=== 容器日志（条件路由相关） ==="
sleep 3
docker logs --since 90s new-api 2>&1 | grep -E "条件路由|短时长|error" | tail -10
"""
    with open(sh_path, "w", encoding="utf-8", newline="\n") as fh:
        fh.write(script)

    for local in (req_path, sh_path):
        subprocess.run(SCP + [local, "ubuntu@119.29.253.97:/tmp/"], check=True)

    out = subprocess.run(
        SSH + ["bash /tmp/run_short_test.sh"], capture_output=True, text=True
    )
    print(out.stdout)
    if out.stderr.strip():
        print("[stderr]", out.stderr.strip()[:500])


if __name__ == "__main__":
    main()
