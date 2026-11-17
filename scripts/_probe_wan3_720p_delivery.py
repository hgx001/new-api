#!/usr/bin/env python3
"""只读核查：wan3.0-720p-特惠 的实际交付分辨率与画幅比例。

用途：
  1. 确认「官网生成 480P → 云端超分 720P → 交付用户」这条链在生产上真实成立；
  2. 确认交付产物的**像素尺寸**与**宽高比**（用来判断竖屏 9:16 是否被压成横屏）。

只读：只读 COS 对象、只探测，不改任何配置、不删任何文件。
"""

import hashlib
import hmac
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request

BUCKET = "mps-test-1397292858"
REGION = "ap-guangzhou"


def run_sql(sql: str) -> str:
    return subprocess.check_output(
        [
            "sudo", "docker", "exec", "-i", "postgres",
            "psql", "-U", "root", "-d", "new-api", "-A", "-t", "-F", "\t", "-c", sql,
        ]
    ).decode().strip()


def load_option(key: str):
    raw = run_sql(f"SELECT value FROM options WHERE key='{key}';")
    if not raw:
        return None
    return json.loads(raw)


def signed_url(sid, skey, path):
    host = f"{BUCKET}.cos.{REGION}.myqcloud.com"
    now = int(time.time())
    key_time = f"{now};{now + 600}"
    sign_key = hmac.new(skey.encode(), key_time.encode(), hashlib.sha1).hexdigest()
    http_string = f"get\n{path}\n\nhost={host}\n"
    string_to_sign = (
        "sha1\n" + key_time + "\n" + hashlib.sha1(http_string.encode()).hexdigest() + "\n"
    )
    signature = hmac.new(sign_key.encode(), string_to_sign.encode(), hashlib.sha1).hexdigest()
    auth = (
        f"q-sign-algorithm=sha1&q-ak={sid}&q-sign-time={key_time}"
        f"&q-key-time={key_time}&q-header-list=host&q-url-param-list="
        f"&q-signature={signature}"
    )
    return f"https://{host}{path}?{auth}"


def main():
    cfg = load_option("TencentMPSSetting") or {}
    sid = cfg.get("secret_id") or ""
    skey = cfg.get("secret_key") or ""
    if not sid or not skey:
        print("MPS 凭据未配置，无法签名")
        return 1

    print("=== MPS 输出尺寸配置 ===")
    print(json.dumps(
        {k: cfg.get(k) for k in
         ("output_width", "output_height", "output_bitrate_kbps", "super_resolution_type",
          "bucket", "region", "output_dir", "enabled")},
        ensure_ascii=False, indent=1))

    rows = run_sql(
        "SELECT id, task_id, private_data::text FROM tasks "
        "WHERE properties::text LIKE '%wan3.0-720p-特惠%' "
        "AND channel_id=12 AND status='SUCCESS' "
        "ORDER BY id DESC LIMIT 6;"
    )
    if not rows:
        print("没有找到渠道 12 的特惠成功任务")
        return 1

    print("\n=== 逐条探测交付产物 ===")
    for line in rows.splitlines():
        parts = line.split("\t")
        if len(parts) < 3:
            continue
        tid, task_id, pd = parts[0], parts[1], parts[2]
        try:
            data = json.loads(pd)
        except Exception:
            continue
        pp = (data.get("post_process") or {})
        out_path = pp.get("output_path") or ""
        result_url = data.get("result_url") or ""
        stage = pp.get("stage")
        if not out_path:
            print(f"task {task_id}: 无超分产物（stage={stage}）result_url={result_url[:80]}")
            continue

        url = signed_url(sid, skey, out_path)
        try:
            with urllib.request.urlopen(url, timeout=30) as resp:
                data_bytes = resp.read()
        except urllib.error.HTTPError as exc:
            print(f"task {task_id}: COS 取文件失败 HTTP {exc.code} path={out_path}")
            continue
        except Exception as exc:
            print(f"task {task_id}: COS 取文件异常 {exc}")
            continue

        tmp = f"/tmp/probe_{task_id}.mp4"
        with open(tmp, "wb") as fh:
            fh.write(data_bytes)

        probe = subprocess.run(
            ["ffprobe", "-v", "error", "-select_streams", "v:0",
             "-show_entries", "stream=width,height,codec_name,avg_frame_rate,duration",
             "-of", "json", tmp],
            capture_output=True, text=True,
        )
        if probe.returncode != 0:
            print(f"task {task_id}: ffprobe 失败 {probe.stderr.strip()[:200]}")
            continue
        stream = (json.loads(probe.stdout).get("streams") or [{}])[0]
        w = stream.get("width")
        h = stream.get("height")
        ratio = f"{w}:{h}" if w and h else "?"
        print(f"task {task_id} (db id {tid}): {w}x{h}  ratio≈{ratio}  "
              f"codec={stream.get('codec_name')} dur={stream.get('duration')}  "
              f"bytes={len(data_bytes)}  stage={stage}")
        print(f"    path={out_path}")

    return 0


if __name__ == "__main__":
    sys.exit(main())
