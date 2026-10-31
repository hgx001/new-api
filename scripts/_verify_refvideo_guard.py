#!/usr/bin/env python3
"""验证「参考视频超 wan3 限制时不改派」的挡板（2026-10-31）。

背景：生产 log 9379（01:10）客户传 32.39 秒参考视频 + 9:16，被改派到
wan3.0-720p-特惠，提交阶段 probe 直接 400：
    reference video duration 32.39s exceeds the 15s per-video limit

挡板改完后，同一条请求应当**不改派**、原样落回源链路（ch17 有赞）。
源链路对参考视频没有时长上限，只校验 >0 秒。

预期结果：
  1. 后端日志出现「条件路由跳过 wan3，交回源链路：参考视频预检不通过：… exceeds the 15s …」
  2. 响应里**没有** wan3 的 400 文案（因为根本没走 wan3）
  3. 新日志的 other 里**没有** route_target_model

成本：源链路会尝试建单，但 data URL 上游下载不到 → 任务失败全额退款。
      若连建单都没成功，净成本 0。

用法：python scripts/_verify_refvideo_guard.py
"""
import base64
import json
import struct
import subprocess
import sys
import time
import urllib.error
import urllib.request

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
HOST = "https://api.heibaidao.cn"
MODEL = "seedance2.5-特惠"
REF_SECONDS = 32.39


def sql(query):
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:300]}")
    return out.stdout.strip()


def admin_token():
    return sql("SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id "
               "WHERE t.status=1 AND u.role>=10 AND t.deleted_at IS NULL "
               "AND length(t.key) > 20 ORDER BY t.id LIMIT 1").strip()


# ---- 构造一个时长可控的最小 MP4（与 wan3 单测同款）----

def box(box_type, payload):
    return struct.pack(">I", 8 + len(payload)) + box_type.encode() + payload


def mvhd(seconds):
    payload = bytes([0, 0, 0, 0])          # version 0 + flags
    payload += struct.pack(">I", 1)        # creation_time
    payload += struct.pack(">I", 2)        # modification_time
    payload += struct.pack(">I", 1000)     # timescale
    payload += struct.pack(">I", int(seconds * 1000))
    tail = bytearray(80)
    struct.pack_into(">I", tail, 0, 0x00010000)
    tail[4] = 0x01
    struct.pack_into(">I", tail, 16, 0x00010000)
    struct.pack_into(">I", tail, 32, 0x00010000)
    struct.pack_into(">I", tail, 48, 0x40000000)
    struct.pack_into(">I", tail, 76, 1)
    return box("mvhd", payload + bytes(tail))


def mp4(seconds):
    return box("ftyp", b"isom\x00\x00\x02\x00") + box("mdat", bytes(16)) + box("moov", mvhd(seconds))


def data_url_video(seconds):
    return "data:video/mp4;base64," + base64.b64encode(mp4(seconds)).decode()


def post(path, token, body):
    data = json.dumps(body, ensure_ascii=False).encode()
    req = urllib.request.Request(HOST + path, data=data, method="POST",
                                 headers={"Authorization": "Bearer " + token,
                                          "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as err:
        return err.code, err.read().decode("utf-8", "replace")


def main():
    token = admin_token()
    print(f"[token] admin token len={len(token)}")
    print(f"[case] 9:16 + 5 秒生成 + {REF_SECONDS} 秒参考视频 → 应当**不改派**")

    body = {
        "model": MODEL,
        "prompt": "refvideo guard probe",
        "seconds": "5",
        "metadata": {"ratio": "9:16"},
        "media": [{"type": "reference_video", "url": data_url_video(REF_SECONDS)}],
    }
    code, text = post("/v1/videos", token, body)
    print(f"[resp] HTTP {code}")
    print(f"[resp] {text[:500]}")

    failures = []
    if "exceeds the 15s per-video limit" in text and "wan3" in text.lower():
        failures.append("响应里出现了 wan3 的 400 文案 → 仍然被改派了")

    # 后端日志：挡板应当留下「条件路由跳过 wan3」
    time.sleep(2)
    logs = subprocess.run(
        SSH + ["docker logs --since 3m new-api 2>&1 | grep -F '条件路由跳过 wan3' | tail -5"],
        capture_output=True, text=True).stdout.strip()
    if logs:
        print("[log ] 挡板命中：")
        for line in logs.splitlines():
            print("       " + line[:220])
    else:
        print("[log ] ⚠️ 没找到「条件路由跳过 wan3」——检查容器名或日志级别")
        failures.append("后端日志里没有挡板记录")

    # 数据库：这条请求的日志里不该有 route_target_model
    row = sql("SELECT id, channel_id, quota, coalesce(other::json->>'route_target_model','-') "
              "FROM logs WHERE model_name LIKE 'seedance2.5%' ORDER BY id DESC LIMIT 1")
    print(f"[db  ] 最新日志 id|channel|quota|route_target = {row}")
    if "|" in row:
        parts = row.split("|")
        if len(parts) >= 4 and parts[3] != "-":
            failures.append(f"日志里出现了 route_target_model={parts[3]} → 仍然被改派了")

    print()
    if failures:
        print("FAIL:")
        for item in failures:
            print("  - " + item)
        sys.exit(1)
    print("PASS：挡板生效，请求未被改派")


if __name__ == "__main__":
    main()
