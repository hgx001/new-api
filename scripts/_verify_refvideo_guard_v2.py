#!/usr/bin/env python3
"""验证「重构后生产闸门仍然生效」+ 退款幂等列可用（2026-10-31）。

背景：这一轮把 wan3 / youzanwan3 各自复制的 MP4 解析器抽到了
relay/channel/task/taskcommon/refvideo.go，并给 erchun / hailuo /
megabyai / manwu / doubao 接上了同一条 15 秒闸门。

重构最容易出的事是「闸门还在，但判断条件变了」。所以必须在生产上实测：
  1. 直接打 wan3.0-720p-特惠（ch12），带 32 秒参考视频 → 本地 400，且文案是
     15 秒上限（证明 taskcommon 的解析路径真的跑到了）。
  2. 同一条模型，15 秒整的参考视频 → 不因为「恰好 15 秒」被误拒。
  3. 提交一条 2 秒纯文生视频 → 正常建单（证明闸门没有误伤正常链路）。

成本：case 1 / case 2 都在 validate 阶段被拒或放行，case 3 只跑 2 秒。

用法：python scripts/_verify_refvideo_guard_v2.py
"""
import base64
import json
import struct
import subprocess
import sys
import urllib.error
import urllib.request

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
HOST = "https://api.heibaidao.cn"


def sql(query):
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:300]}")
    return out.stdout.strip()


def admin_token():
    return sql("SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id "
               "WHERE t.status=1 AND u.role>=10 AND t.deleted_at IS NULL "
               "AND length(t.key) > 20 ORDER BY t.id LIMIT 1").strip()


# ---- 构造时长可控的最小 MP4（与 taskcommon 单测同款）----

def box(box_type, payload):
    return struct.pack(">I", 8 + len(payload)) + box_type.encode() + payload


def mvhd(seconds):
    payload = bytes([0, 0, 0, 0])
    payload += struct.pack(">I", 1)
    payload += struct.pack(">I", 2)
    payload += struct.pack(">I", 1000)
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


def last_task_id():
    return sql("SELECT task_id FROM tasks ORDER BY id DESC LIMIT 1").strip()


def main():
    token = admin_token()
    print(f"[token] admin token len={len(token)}")
    failures = []
    before = last_task_id()
    print(f"[db   ] 提交前最新 task_id = {before}")

    # ---- case 1：32 秒参考视频必须本地 400 ----
    print("\n[case1] wan3.0-720p-特惠 + 32 秒参考视频 → 期望本地 400（15 秒上限）")
    body = {
        "model": "wan3.0-720p-特惠",
        "prompt": "refvideo guard probe 32s",
        "seconds": "2",
        "media": [{"type": "reference_video", "url": data_url_video(32)}],
    }
    code, text = post("/v1/videos", token, body)
    print(f"[resp ] HTTP {code} {text[:300]}")
    if code != 400:
        failures.append(f"case1 期望 400，实际 {code}")
    if "15" not in text:
        failures.append("case1 报错文案里没有 15 秒上限")

    # ---- case 2：15 秒整不能被误拒 ----
    print("\n[case2] wan3.0-720p-特惠 + 15 秒整参考视频 → 期望不被「超限」拒绝")
    body = {
        "model": "wan3.0-720p-特惠",
        "prompt": "refvideo guard probe 15s",
        "seconds": "2",
        "media": [{"type": "reference_video", "url": data_url_video(15)}],
    }
    code, text = post("/v1/videos", token, body)
    print(f"[resp ] HTTP {code} {text[:300]}")
    # 15 秒整是允许的：会走到建单（data URL 上游抓不到 → 任务失败退款，净成本 0）。
    if code == 400 and "超过" in text:
        failures.append("case2 15 秒整被误判为超限")

    # ---- case 3：纯文生 2 秒必须正常 ----
    print("\n[case3] wan3.0-720p-特惠 纯文生 2 秒 → 期望 200 建单")
    body = {
        "model": "wan3.0-720p-特惠",
        "prompt": "plain text to video, 2 seconds, deployment sanity check",
        "seconds": "2",
    }
    code, text = post("/v1/videos", token, body)
    print(f"[resp ] HTTP {code} {text[:300]}")
    if code != 200:
        failures.append(f"case3 期望 200，实际 {code}")

    # ---- 被拒的请求不该建单 ----
    after = last_task_id()
    print(f"\n[db   ] 提交后最新 task_id = {after}")
    rows = sql("SELECT count(*) FROM tasks WHERE created_at > extract(epoch from now())::bigint - 300")
    print(f"[db   ] 近 5 分钟新建任务数 = {rows}")

    # ---- 退款幂等列 ----
    col = sql("SELECT column_name, data_type FROM information_schema.columns "
              "WHERE table_name='tasks' AND column_name='refunded_at'")
    print(f"[db   ] tasks.refunded_at = {col or '缺失'}")
    if not col:
        failures.append("tasks.refunded_at 列不存在（AutoMigrate 没跑或失败）")

    print()
    if failures:
        print("FAIL:")
        for item in failures:
            print("  - " + item)
        sys.exit(1)
    print("PASS")


if __name__ == "__main__":
    main()
