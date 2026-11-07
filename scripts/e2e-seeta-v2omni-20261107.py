#!/usr/bin/env python3
"""new-api 用户侧 E2E：minimax_h3_v2 / minimax_h3_omni（2026-11-07 改版）。

正向：
  1. v2 纯文生 5s 480p横            （合并语义：不带图=文生）
  2. v2 带 1 张参考图（首帧图生）
  3. omni 2 张设定参考 + 1 段音频    （多模态参考：ref_image_N + ref_audio_0）
  4. omni 1080p横 5s                （新分辨率档，倍率 2.0）
负向（免费）：
  N1. v2 带 3 张图 → 400（关键帧上限 2）
  N2. v2 带音频 → 400（V2 无音频槽位）
  N3. v2 resolution=1080p横 → 400（1080p 仅 Omni）
"""
import base64
import json
import struct
import sys
import time
import urllib.error
import urllib.request
import uuid
import wave

BASE = "https://api.heibaidao.cn/v1"
# default 组用户令牌（server 端注入 /tmp/e2e_token）
TOKEN = open("/tmp/e2e_token").read().strip()

HDR = {"Authorization": "Bearer " + TOKEN, "Content-Type": "application/json"}

# 64x64 纯色 PNG 当参考图（1x1 会被某些链路拒，64px 稳妥）
PNG_64 = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAEAAAABACAYAAACqaXHeAAAAKklEQVR4nO3BAQ0AAADCoPdMbQ8H"
    "FAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA8wIFhAAGu7m4hAAAAAElFTkSuQmCC"
)
PNG_DATA_URI = "data:image/png;base64," + base64.b64encode(PNG_64).decode()

# 1 秒 440Hz 正弦 WAV 当参考音频（RIFF 魔数，audio/wav）
import io as _io
_buf = _io.BytesIO()
with wave.open(_buf, "wb") as w:
    w.setnchannels(1); w.setsampwidth(2); w.setframerate(8000)
    w.writeframes(b"".join(
        struct.pack("<h", int(8000 * __import__("math").sin(2 * 3.14159 * 440 * i / 8000)))
        for i in range(8000)))
WAV_DATA_URI = "data:audio/wav;base64," + base64.b64encode(_buf.getvalue()).decode()


def req(method, url, body=None, timeout=60):
    data = json.dumps(body, ensure_ascii=False).encode() if body is not None else None
    r = urllib.request.Request(url, data=data, headers=HDR, method=method)
    try:
        with urllib.request.urlopen(r, timeout=timeout) as resp:
            return resp.status, json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", "replace")
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"detail": raw[:300]}


def submit(name, body):
    code, payload = req("POST", BASE + "/videos", body)
    print("SUBMIT %-28s -> %s %s" % (name, code, payload.get("id") or payload), flush=True)
    return code, payload.get("id")


def poll(name, task_id, deadline_s=1500):
    t0 = time.time()
    while time.time() - t0 < deadline_s:
        code, p = req("GET", BASE + "/videos/" + task_id)
        st = p.get("status") if code == 200 else "http_%s" % code
        if st in ("completed", "succeeded", "success"):
            print("DONE   %-28s completed %5.1fs" % (name, time.time() - t0), flush=True)
            return "completed"
        if st in ("failed", "failure", "error", "cancelled"):
            err = (p.get("error") or {}).get("message", "")
            print("FAIL   %-28s %s %s" % (name, st, str(err)[:200]), flush=True)
            return "failed"
        time.sleep(6)
    print("TIMEOUT %-28s" % name, flush=True)
    return "timeout"


results = {}
POSITIVE = [
    ("v2-t2v-5s", {"model": "minimax_h3_v2", "prompt": "平静的湖面日出，微风吹皱水面，写实镜头",
                   "resolution": "480p横", "seconds": "5"}),
    ("v2-i2v-1img", {"model": "minimax_h3_v2", "prompt": "镜头缓缓推进，画面里的景色活起来",
                     "resolution": "480p横", "seconds": "5", "images": [PNG_DATA_URI]}),
    ("omni-setref-2img-audio", {"model": "minimax_h3_omni",
                                "prompt": "<Picture 1> 中的场景缓缓展开，配上 <Audio 1> 的氛围",
                                "resolution": "480p横", "seconds": "5",
                                "images": [PNG_DATA_URI, PNG_DATA_URI],
                                "audios": [WAV_DATA_URI]}),
    ("omni-1080p-5s", {"model": "minimax_h3_omni", "prompt": "未来城市夜景航拍，霓虹闪烁",
                       "resolution": "1080p横", "seconds": "5"}),
]
for name, body in POSITIVE:
    code, tid = submit(name, body)
    if tid:
        results[name] = poll(name, tid)
    else:
        results[name] = "submit_%s" % code

NEGATIVE = [
    ("neg-v2-3img", {"model": "minimax_h3_v2", "prompt": "p", "seconds": "5",
                     "images": [PNG_DATA_URI, PNG_DATA_URI, PNG_DATA_URI]}, "2"),
    ("neg-v2-audio", {"model": "minimax_h3_v2", "prompt": "p", "seconds": "5",
                      "audios": [WAV_DATA_URI]}, "audio"),
    ("neg-v2-1080p", {"model": "minimax_h3_v2", "prompt": "p", "seconds": "5",
                      "resolution": "1080p横"}, "resolution"),
]
neg_ok = 0
for name, body, want in NEGATIVE:
    code, payload = submit(name, body)
    if code == 400 and want in json.dumps(payload, ensure_ascii=False):
        neg_ok += 1
        print("NEG-OK %-28s 400 (含 %s)" % (name, want), flush=True)
    else:
        print("NEG-BAD %-28s code=%s %s" % (name, code, str(payload)[:200]), flush=True)

print("\n=== SUMMARY positive=%d/%d negative=%d/%d ===" % (
    list(results.values()).count("completed"), len(POSITIVE), neg_ok, len(NEGATIVE)), flush=True)
sys.exit(0 if list(results.values()).count("completed") == len(POSITIVE) and neg_ok == len(NEGATIVE) else 1)
