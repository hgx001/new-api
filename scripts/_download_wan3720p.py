"""下载 wan3.0-720p-特惠 的真机产物并 ffprobe 确认分辨率（2026-10-30 收尾）。

不重新提交任务 —— 复用已完成的任务，只下载成片，**不额外消耗生成费用**。

用法：python scripts/_download_wan3720p.py [task_id]
"""
import json
import os
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
HOST = "https://api.heibaidao.cn"

DEFAULT_TASK = "task_UBRmqxFxtUQLazsJORe1Jc41BvGGYqDy"


def sql(query):
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:300]}\n{query}")
    return out.stdout.strip()


def download(task_id, token, out_path):
    req = urllib.request.Request(
        f"{HOST}/v1/videos/{task_id}/content",
        headers={"Authorization": f"Bearer {token}"},
    )
    try:
        with urllib.request.urlopen(req, timeout=180) as resp:
            data = resp.read()
            print(f"[http] status={resp.status} bytes={len(data)} "
                  f"content-type={resp.headers.get('Content-Type')}")
    except urllib.error.HTTPError as err:
        body = err.read().decode("utf-8", "replace")
        raise RuntimeError(f"HTTP {err.code}: {body[:400]}") from err
    with open(out_path, "wb") as fh:
        fh.write(data)
    return len(data)


def ffprobe(path):
    probe = subprocess.run(
        ["ffprobe", "-v", "error", "-select_streams", "v:0",
         "-show_entries", "stream=width,height:format=duration",
         "-of", "json", path],
        capture_output=True, text=True,
    )
    info = json.loads(probe.stdout or "{}")
    stream = (info.get("streams") or [{}])[0]
    duration = float((info.get("format") or {}).get("duration") or 0)
    return stream.get("width"), stream.get("height"), duration


def main():
    task_id = sys.argv[1] if len(sys.argv) > 1 else DEFAULT_TASK

    # 令牌必须是 unlimited_quota=t：ValidateUserToken 里
    # `!UnlimitedQuota && RemainQuota <= 0` 会直接返回 ErrTokenInvalid(401)。
    # 之前 401 的根因就是这个，不是 key 复制错。
    row = sql("SELECT t.user_id || ' ' || t.key FROM tokens t "
              "JOIN users u ON u.id=t.user_id "
              "WHERE t.user_id=28 AND t.status=1 AND t.unlimited_quota=true "
              "AND u.deleted_at IS NULL ORDER BY t.id LIMIT 1").strip()
    uid, _, token = row.partition(" ")
    if len(token) < 20:
        raise RuntimeError(f"no token, got: {row[:60]!r}")
    print(f"[token] user={uid} key_len={len(token)}")

    owner = sql(f"SELECT user_id FROM tasks WHERE task_id='{task_id}'").strip()
    print(f"[task] owner_user={owner} (token user must match, else 404)")

    tmp = tempfile.mkdtemp(prefix="w3720p_dl_")
    out = os.path.join(tmp, "out.mp4")
    size = download(task_id, token, out)
    w, h, dur = ffprobe(out)
    print(f"[file] size={size/1024/1024:.2f}MB path={out}")
    print(f"[ffprobe] {w}x{h} duration={dur:.3f}s")
    short = min(w, h) if w and h else None
    if short == 720:
        print("[VERDICT] PASS: 短边=720 —— 交付的是 720P 成片，不是 480P 原片")
    else:
        print(f"[VERDICT] FAIL: 短边={short}，期望 720")


if __name__ == "__main__":
    main()
