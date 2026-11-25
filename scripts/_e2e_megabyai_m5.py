"""MegaAI m5 两档真机验证：各一条 4 秒 480p 全参任务（图 + 视频 + 音频）。

授权：用户 2026-11-24 明确要求「上架后模拟用户调用 newapi 的方式各测试一条
4 秒 480p 的带音视频图片的全参任务，成功就行」—— 真实生成，共 2 条。

用法（在生产机上）：python3 /tmp/m5_e2e.py
"""
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:3000"
IMG = "https://api.heibaidao.cn/testassets/refimg.png"
VID = "https://api.heibaidao.cn/testassets/refvid720.mp4"
AUD = "https://api.heibaidao.cn/testassets/refaud.mp3"
PROMPT = "参考图片的配色与构图，跟随参考视频的镜头节奏与音频节拍，生成一段 4 秒的简约动态画面。"

key = subprocess.run(
    ["sudo", "docker", "exec", "postgres", "psql", "-U", "root", "-d", "new-api", "-tAc",
     "SELECT key FROM tokens WHERE id=144"],
    capture_output=True, text=True, check=True).stdout.strip()
if not key:
    print("NO_TOKEN")
    sys.exit(1)
AUTH = {"Authorization": "Bearer sk-" + key, "Content-Type": "application/json"}


def call(url, payload=None):
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(BASE + url, data=data, headers=AUTH)
    try:
        with urllib.request.urlopen(req, timeout=120) as r:
            return r.status, json.loads(r.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        try:
            return e.code, json.loads(body)
        except Exception:
            return e.code, {"raw": body[:500]}


def submit(model):
    payload = {
        "model": model,
        "prompt": PROMPT,
        "duration": 4,
        "resolution": "480p",
        "ratio": "9:16",
        "images": [IMG],
        "media": [
            {"type": "reference_video", "url": VID},
            {"type": "reference_audio", "url": AUD},
        ],
    }
    status, body = call("/v1/videos", payload)
    print("[%s] submit -> %s %s" % (model, status, json.dumps(body, ensure_ascii=False)[:400]))
    if status != 200:
        return None
    data = body.get("data") or body
    return data.get("id") or data.get("task_id")


def poll(model, task_id, timeout=900):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        status, body = call("/v1/videos/" + task_id)
        st = (body.get("status") or "").lower()
        if st != last:
            print("[%s] %s status=%s" % (model, task_id, st))
            last = st
        if st in ("succeeded", "success", "completed", "failed", "failure", "cancelled"):
            return body
        time.sleep(15)
    print("[%s] TIMEOUT" % model)
    return None


for model in ["seedance-2.0-m5"]:
    tid = submit(model)
    if not tid:
        continue
    result = poll(model, tid) or {}
    url = result.get("url") or result.get("video_url") or ""
    print("[%s] FINAL status=%s url=%s" % (model, result.get("status"), str(url)[:100]))
    if result.get("fail_reason"):
        print("[%s] fail_reason=%s" % (model, str(result["fail_reason"])[:300]))
