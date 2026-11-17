#!/usr/bin/env python3
"""真机负向验证（2026-11-17，用户授权，限 1 条）：

wan3.0-video-prime 直发 2 秒 + 参考音视频。
预期：本站 adaptor validateExplicitDurationForModel 400（prime 只接 30 秒），
请求不达上游、零成本。若意外受理（任务号返回），立即停止并报告。

用法（服务器上）：
  KEY=$(psql 取 token 144) NAPI_BASE=https://api.heibaidao.cn python3 _probe_prime_2s_refav.py
"""
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request

BASE = os.environ.get("NAPI_BASE", "https://api.heibaidao.cn")
KEY = os.environ["NAPI_TOKEN"]
if not KEY.startswith("sk-"):
    KEY = "sk-" + KEY

BUCKET, REGION = "mps-test-1397292858", "ap-guangzhou"
# 用今天上午验证过的超分产物（对象确认存在，1280x720/30s）
OBJ = "/mps_api_test/sr_task_vjORy3ND43b76fQqcr2foOAM73wGO0ip.mp4"


def presign(obj: str) -> str:
    out = subprocess.run(
        ["python3", "/tmp/_sign_cos_url.py", BUCKET, REGION, obj],
        capture_output=True, text=True, timeout=30,
    )
    urls = [ln for ln in out.stdout.splitlines() if ln.startswith("http")]
    if not urls:
        raise RuntimeError("presign failed: " + out.stdout[-200:] + out.stderr[-200:])
    return urls[-1]


def redact(text: str) -> str:
    return text.replace(KEY, "sk-***").replace(KEY[3:], "***")


def main() -> None:
    ref = presign(OBJ)
    body = {
        "model": "wan3.0-video-prime",
        "prompt": "测试：2 秒任务，带参考视频与参考音频（负向验证，预期本地 400）",
        "duration": 2,
        "media": [
            {"type": "reference_video", "url": ref},
            {"type": "reference_audio", "url": ref},
        ],
    }
    req = urllib.request.Request(
        BASE + "/v1/videos",
        data=json.dumps(body, ensure_ascii=False).encode("utf-8"),
        headers={"Authorization": "Bearer " + KEY, "Content-Type": "application/json"},
        method="POST",
    )
    print("POST", BASE + "/v1/videos", "model=wan3.0-video-prime duration=2 media=video+audio")
    try:
        resp = urllib.request.urlopen(req, timeout=90)
        status, payload = resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as exc:
        status, payload = exc.code, exc.read().decode("utf-8", "replace")
    print("HTTP", status)
    print(redact(payload)[:1200])

    lower = payload.lower()
    if status in (200, 202) and ("task_id" in lower or '"id"' in lower):
        print("\n[!] 意外受理——按测试纪律立即停止，勿等待产物，请人工评估取消/计费影响")


if __name__ == "__main__":
    sys.exit(main())
