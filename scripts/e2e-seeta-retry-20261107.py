#!/usr/bin/env python3
"""E2E 补跑：v2 图生 + omni 多模态参考（修正版——用程序生成的合法 PNG）。

上一轮两个带图用例失败根因是测试脚本里手写的 base64 PNG 数据损坏
（上游 avcodec [png] inflate returned error -3），与适配器无关。
本脚本用 zlib+struct 现场构造 64x64 合法 PNG，并修正负向判定逻辑
（直接用 req 的完整 payload 判定，不再经 submit 丢字段）。
"""
import base64
import json
import math
import struct
import time
import urllib.error
import urllib.request
import wave
import zlib

BASE = "https://api.heibaidao.cn/v1"
TOKEN = open("/tmp/e2e_token").read().strip()
HDR = {"Authorization": "Bearer " + TOKEN, "Content-Type": "application/json"}


def make_png(w=64, h=64):
    """现场构造一张合法的 RGB PNG（红蓝渐变），零第三方依赖。"""
    raw = b"".join(
        b"\x00" + b"".join(bytes((int(255 * x / w), int(255 * y / h), 128))
                            for x in range(w))
        for y in range(h)
    )

    def chunk(tag, data):
        c = struct.pack(">I", len(data)) + tag + data
        return c + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)

    ihdr = struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr)
            + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b""))


PNG_DATA_URI = "data:image/png;base64," + base64.b64encode(make_png()).decode()

buf = __import__("io").BytesIO()
with wave.open(buf, "wb") as w:
    w.setnchannels(1); w.setsampwidth(2); w.setframerate(8000)
    w.writeframes(b"".join(
        struct.pack("<h", int(8000 * math.sin(2 * 3.14159 * 440 * i / 8000)))
        for i in range(8000)))
WAV_DATA_URI = "data:audio/wav;base64," + base64.b64encode(buf.getvalue()).decode()


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


def poll(name, task_id, deadline_s=1500):
    t0 = time.time()
    while time.time() - t0 < deadline_s:
        code, p = req("GET", BASE + "/videos/" + task_id)
        st = p.get("status") if code == 200 else "http_%s" % code
        if st in ("completed", "succeeded", "success"):
            print("DONE   %-26s completed %5.1fs" % (name, time.time() - t0), flush=True)
            return "completed"
        if st in ("failed", "failure", "error", "cancelled"):
            print("FAIL   %-26s %s %s" % (name, st, str((p.get("error") or {}).get("message", ""))[:200]), flush=True)
            return "failed"
        time.sleep(6)
    print("TIMEOUT %-26s" % name, flush=True)
    return "timeout"


POSITIVE = [
    ("v2-i2v-1img", {"model": "minimax_h3_v2", "prompt": "镜头缓缓推进，画面里的景色活起来",
                     "resolution": "480p横", "seconds": "5", "images": [PNG_DATA_URI]}),
    ("omni-setref-2img-audio", {"model": "minimax_h3_omni",
                                "prompt": "<Picture 1> 中的场景缓缓展开，配上 <Audio 1> 的氛围",
                                "resolution": "480p横", "seconds": "5",
                                "images": [PNG_DATA_URI, PNG_DATA_URI],
                                "audios": [WAV_DATA_URI]}),
]
results = {}
for name, body in POSITIVE:
    code, p = req("POST", BASE + "/videos", body)
    tid = p.get("id")
    print("SUBMIT %-26s -> %s %s" % (name, code, tid or p), flush=True)
    results[name] = poll(name, tid) if tid else "submit_%s" % code

print("\n=== SUMMARY positive=%d/%d ===" % (
    list(results.values()).count("completed"), len(POSITIVE)), flush=True)
