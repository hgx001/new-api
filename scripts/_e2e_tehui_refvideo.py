"""seedance2.5-特惠 参考视频加价验证（2026-10-29，老板授权「跑一条 2 秒带参考视频」）。

测什么：2 秒生成 + 1 个 2 秒参考视频，验证「生成秒数 + 参考视频秒数」的加秒计费
真的落到 quota 上，而不是只在代码里。

预期：
  - 计费秒数 = 2（生成）+ 2（参考视频）= 4
  - quota ≈ ModelPrice(¥0.20/秒) × 4 = ¥0.80
  - 上游回执（tasks.data）里 media 含 1 项 reference_video

成本：¥0.80（720P，4 秒口径）。上游是否也按加秒收费要看回执的 quantity。

为什么用 data URL：省掉「图床能不能访问 / SSRF 拦截 / 域名白名单」这一整类干扰，
测的就是加秒计费本身。

用法：python scripts/_e2e_tehui_refvideo.py
"""
import base64
import json
import os
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
HOST = "https://api.heibaidao.cn"
MODEL = "seedance2.5-特惠"

ASSET_DIR = tempfile.mkdtemp(prefix="w3refv_")
REF_VIDEO = os.path.join(ASSET_DIR, "ref.mp4")

# 参考视频 2.0 秒，与生成时长同长：计费秒数应为 4，能明确区分「加秒」与「没加秒」。
REF_SECONDS = 2.0

PROMPT = "参考这段视频的镜头运动与节奏，生成一段 2 秒的简洁动态画面。"


def sql(query):
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:300]}\n{query}")
    return out.stdout.strip()


def make_reference_video():
    """2.0 秒 1280x720 h264 mp4：上游与本站探测都只认 MP4/MOV 的 mvhd。"""
    subprocess.run(
        ["ffmpeg", "-y", "-loglevel", "error",
         "-f", "lavfi", "-i", f"testsrc2=size=1280x720:rate=24:duration={REF_SECONDS}",
         "-c:v", "libx264", "-pix_fmt", "yuv420p", "-profile:v", "main",
         "-movflags", "+faststart", REF_VIDEO],
        check=True,
    )
    probe = subprocess.run(
        ["ffprobe", "-v", "error", "-show_entries", "format=duration",
         "-of", "default=nw=1:nk=1", REF_VIDEO],
        capture_output=True, text=True, check=True,
    )
    duration = float(probe.stdout.strip())
    size = os.path.getsize(REF_VIDEO)
    print(f"[asset] ref.mp4 duration={duration:.3f}s size={size} bytes")
    if abs(duration - REF_SECONDS) > 0.1:
        raise SystemExit(f"[FAIL] 参考视频时长 {duration} 不符合预期 {REF_SECONDS}")
    return duration


def data_url(path, mime):
    with open(path, "rb") as handle:
        encoded = base64.b64encode(handle.read()).decode("ascii")
    return f"data:{mime};base64,{encoded}"


def request(method, path, token, body=None, timeout=180):
    data = None if body is None else json.dumps(body, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        f"{HOST}{path}", data=data, method=method,
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as err:
        return err.code, err.read().decode("utf-8", "replace")


def admin_token():
    row = sql("SELECT t.user_id || ' ' || t.key FROM tokens t "
              "JOIN users u ON u.id=t.user_id "
              "WHERE t.status=1 AND u.role>=10 AND u.deleted_at IS NULL "
              "ORDER BY t.id LIMIT 1").strip()
    uid, _, token = row.partition(" ")
    if len(token) < 20:
        raise RuntimeError(f"no admin token, got: {row[:40]!r}")
    return int(uid), token


def snapshot(user_id):
    used = sql(f"SELECT COALESCE(SUM(used_quota),0) FROM tokens WHERE user_id={user_id}")
    tasks = sql(f"SELECT count(*) FROM tasks WHERE user_id={user_id}")
    return used, tasks


def main():
    ref_duration = make_reference_video()
    user_id, token = admin_token()
    used_before, tasks_before = snapshot(user_id)
    print(f"=== baseline: user={user_id} used_quota={used_before} tasks={tasks_before} ===")

    media = [{"type": "reference_video", "url": data_url(REF_VIDEO, "video/mp4")}]
    body = {
        "model": MODEL,
        "prompt": PROMPT,
        "seconds": "2",
        "resolution": "720P",
        "metadata": {"ratio": "16:9", "media": media},
    }

    failures = []
    status, text = request("POST", "/v1/videos", token, body)
    print(f"[submit] HTTP {status}")
    print(f"         {text[:400]}")
    if status != 200:
        print("[FAIL] 提交未成功（4xx 不建单不扣费）")
        raise SystemExit(1)
    for leaked in ("wan3", "wan-3", "有赞", "youzan", "aliyun"):
        if leaked.lower() in text.lower():
            failures.append(f"提交响应暴露真实货源：{leaked}")

    payload = json.loads(text)
    task_id = payload.get("id") or payload.get("task_id")
    print(f"[submit] task_id={task_id}")

    deadline = time.time() + 15 * 60
    last = ""
    while time.time() < deadline:
        code, poll_text = request("GET", f"/v1/videos/{task_id}", token, timeout=60)
        if code != 200:
            print(f"[poll] HTTP {code}: {poll_text[:200]}")
            time.sleep(5)
            continue
        data = json.loads(poll_text)
        state = data.get("status")
        if state != last:
            print(f"[poll] status={state} progress={data.get('progress')}")
            last = state
        for leaked in ("wan3", "wan-3", "有赞", "youzan", "aliyun"):
            if leaked.lower() in poll_text.lower():
                failures.append(f"轮询响应暴露真实货源：{leaked}")
        if state in ("completed", "failed"):
            break
        time.sleep(5)
    else:
        print("[FAIL] 15 分钟内未到终态")
        raise SystemExit(1)

    def task_field(name):
        return sql(f"SELECT COALESCE({name}::text,'') FROM tasks WHERE task_id='{task_id}'").strip()

    status_db = task_field("status")
    quota = int(task_field("quota") or "0")
    fail_reason = task_field("fail_reason")
    upstream = task_field("data")
    print(f"[db] status={status_db} quota={quota} fail_reason={fail_reason[:160]}")
    print(f"[db] data={upstream[:900]}")

    if status_db.lower() not in ("success", "succeeded"):
        print(f"[FAIL] 任务未成功：{status_db} {fail_reason[:200]}")
        raise SystemExit(1)

    # ---- 加秒计费核对 ----
    # 2 秒生成 + 2 秒参考视频 = 4 秒 × ¥0.20/秒 = ¥0.80
    expected_seconds = 2 + int(round(ref_duration))
    expected_cny = expected_seconds * 0.20
    actual_cny = quota / 500000 * 7.3
    print(f"[bill] expected {expected_seconds}s = ¥{expected_cny:.2f}, actual ¥{actual_cny:.4f} (quota={quota})")
    if abs(actual_cny - expected_cny) > 0.02:
        failures.append(f"计费不符：期望 ¥{expected_cny:.2f}（{expected_seconds} 秒），实际 ¥{actual_cny:.4f}")

    # 未加秒的口径是 2 秒 ¥0.40：如果落在这个数上，说明 context 里的秒数没被读到。
    if abs(actual_cny - 0.40) < 0.02:
        failures.append("计费退化成纯生成时长（¥0.40）：参考视频秒数没并入 seconds")

    # ---- 上游回执核对 ----
    if upstream:
        try:
            parsed = json.loads(upstream)
        except json.JSONDecodeError:
            parsed = {}
        # 上游回执把 media 放在 task 下面（不是顶层，也不是 request 下）。
        # 第一版只读顶层 media，于是成功单被自己的断言判成失败。
        raw_media = (
            (parsed.get("task") or {}).get("media")
            or parsed.get("media")
            or (parsed.get("request") or {}).get("media")
            or []
        )
        kinds = [item.get("type") for item in raw_media if isinstance(item, dict)]
        print(f"[db] upstream media kinds={kinds}")
        if "reference_video" not in kinds:
            failures.append(f"上游回执里没有 reference_video：{kinds}")
        for key in ("duration", "quantity", "unitPrice", "quotedAmount", "resolution"):
            if key in parsed:
                print(f"[db] upstream {key}={parsed[key]}")
        task_node = parsed.get("task") or {}
        for key in ("duration", "quantity", "unitPrice", "quotedAmount", "resolution"):
            if key in task_node:
                print(f"[db] upstream task.{key}={task_node[key]}")

    # ---- 余额核对 ----
    used_after, tasks_after = snapshot(user_id)
    print(f"=== after: used_quota={used_after} tasks={tasks_after} ===")
    if int(tasks_after) - int(tasks_before) != 1:
        failures.append(f"建单数异常：{tasks_before} → {tasks_after}")
    delta = int(used_after) - int(used_before)
    print(f"[bill] used_quota delta={delta} (¥{delta / 500000 * 7.3:.4f})")
    if delta != quota:
        failures.append(f"扣费额与 tasks.quota 不一致：delta={delta} quota={quota}")

    print()
    if failures:
        for item in failures:
            print(f"[FAIL] {item}")
        raise SystemExit(1)
    print("[PASS] 参考视频加秒计费生效：2 秒生成 + 2 秒参考视频 按 4 秒收费")


if __name__ == "__main__":
    main()
