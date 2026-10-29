"""seedance2.5-特惠 全参数真实生成测试（2026-10-29，老板授权「只测一条成功」）。

测什么：时长 2 秒 + 5 张参考图 + 1 条音频，一次提交，验证参数是否都生效。

为什么素材用 data URL 而不是外链：
  youzanwan3 adaptor 的 readAsset 先下载素材、再 POST /v1/assets 上传给上游。
  用 data URL 省掉「图床能不能访问 / SSRF 拦截 / 域名白名单」这一整类干扰，
  测的就是参数传递本身。

成本：2 秒 × ¥0.25/秒 = ¥0.50（720P，2026-10-29 第二轮调价后的价）。

验证口径（全部要过）：
  1. 提交 200，且响应体里**不出现** wan3 / 有赞（别名脱敏在出口层）
  2. 任务终态 succeeded（不是 failed）
  3. tasks.data（上游原始回执）里 duration=2、resolution=720P、media 6 项
     —— 这是「参数真的传上去了」的唯一硬证据
  4. tasks.quota 折算 ≈ ¥0.50（不是 0.40 也不是 0.60：档位与单价都对）
  5. 成片 ffprobe：时长 ≈2 秒、高度 720

用法：python scripts/_e2e_tehui_full_params.py
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
PRIME = "wan3.0-video-prime"

ASSET_DIR = tempfile.mkdtemp(prefix="w3ref_")
IMAGES = [os.path.join(ASSET_DIR, f"ref_{i}.jpg") for i in range(5)]
AUDIO = os.path.join(ASSET_DIR, "ref.mp3")

PROMPT = "参考这几张图片的配色与构图，配合音频的节奏，生成一段简洁的 2 秒动态画面，镜头缓慢推进。"


def sql(query):
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:300]}\n{query}")
    return out.stdout.strip()


def make_assets():
    """5 张不同色相的 256x256 JPEG + 1 条 2 秒 440Hz MP3。"""
    for index, path in enumerate(IMAGES):
        hue = index * 72
        subprocess.run(
            ["ffmpeg", "-y", "-loglevel", "error", "-f", "lavfi",
             "-i", "testsrc2=size=256x256:rate=1", "-vf", f"hue=h={hue}",
             "-frames:v", "1", "-q:v", "4", path],
            check=True,
        )
    subprocess.run(
        ["ffmpeg", "-y", "-loglevel", "error", "-f", "lavfi",
         "-i", "sine=frequency=440:duration=2", "-c:a", "libmp3lame",
         "-b:a", "64k", AUDIO],
        check=True,
    )


def data_url(path, mime):
    with open(path, "rb") as handle:
        encoded = base64.b64encode(handle.read()).decode("ascii")
    return f"data:{mime};base64,{encoded}"


def request(method, path, token, body=None, timeout=120):
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
    """只用管理员自己的令牌：测试产生的 ¥0.50 记在自己账上，不动客户余额。"""
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
    make_assets()
    user_id, token = admin_token()
    used_before, tasks_before = snapshot(user_id)
    print(f"=== baseline: user={user_id} used_quota={used_before}  tasks={tasks_before} ===")

    media = [{"type": "reference_image", "url": data_url(path, "image/jpeg")} for path in IMAGES]
    media.append({"type": "reference_audio", "url": data_url(AUDIO, "audio/mpeg")})
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
        print("[FAIL] 提交未成功，未建单（若为 4xx 则不扣费）")
        raise SystemExit(1)
    for leaked in ("wan3", "wan-3", "有赞", "youzan", "aliyun"):
        if leaked.lower() in text.lower():
            failures.append(f"提交响应暴露真实货源：{leaked}")

    payload = json.loads(text)
    task_id = payload.get("id") or payload.get("task_id")
    print(f"[submit] task_id={task_id}")

    # ---- 轮询 ----
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
            result_url = (data.get("metadata") or {}).get("url", "")
            break
        time.sleep(5)
    else:
        print("[FAIL] 15 分钟内未到终态")
        raise SystemExit(1)

    # ---- 库内取证 ----
    # 逐字段查，不拼成一行再切：fail_reason / data 里都可能出现分隔符。
    def task_field(name):
        return sql(f"SELECT COALESCE({name}::text,'') FROM tasks WHERE task_id='{task_id}'").strip()

    status_db = task_field("status")
    quota = task_field("quota") or "0"
    fail_reason = task_field("fail_reason")
    props = task_field("properties")
    upstream = task_field("data")
    print(f"[db] status={status_db} quota={quota} fail_reason={fail_reason[:160]}")
    print(f"[db] properties={props[:600]}")
    print(f"[db] data={upstream[:900]}")

    # 库里存的是 model.TaskStatusSuccess = "SUCCESS"（大写），API 层才映射成
    # "completed" / "succeeded"。第一版这里按 "succeeded" 比较，把成功单判成了失败。
    succeeded = status_db.lower() in ("success", "succeeded")
    if not succeeded:
        failures.append(f"任务未成功：status={status_db} fail_reason={fail_reason[:200]}")

    # 上游回执：参数是否真的传上去了。解析 JSON 数，不靠字符串计数。
    receipt = json.loads(upstream) if upstream else {}
    upstream_task = receipt.get("task") or {}
    media_items = upstream_task.get("media") or []
    image_count = sum(1 for item in media_items if item.get("type") == "reference_image")
    audio_count = sum(1 for item in media_items if item.get("type") == "reference_audio")
    print(f"[receipt] model={upstream_task.get('model')} duration={upstream_task.get('duration')} "
          f"resolution={upstream_task.get('resolution')} ratio={upstream_task.get('ratio')} "
          f"images={image_count} audios={audio_count} unitPrice={upstream_task.get('unitPrice')}")

    if upstream_task.get("duration") != 2:
        failures.append(f"上游回执里 duration 不是 2：{upstream_task.get('duration')}")
    if upstream_task.get("resolution") != "720P":
        failures.append(f"上游回执里 resolution 不是 720P：{upstream_task.get('resolution')}")
    if upstream_task.get("ratio") != "16:9":
        failures.append(f"上游回执里 ratio 不是 16:9：{upstream_task.get('ratio')}")
    if image_count != 5:
        failures.append(f"上游回执里 reference_image 不是 5 项（实际 {image_count}）")
    if audio_count != 1:
        failures.append(f"上游回执里 reference_audio 不是 1 项（实际 {audio_count}）")
    if upstream_task.get("model") != PRIME:
        failures.append(f"上游回执里不是 {PRIME}（说明没走 prime 分支）")

    # 计费：2 秒 × ¥0.25/秒 = ¥0.50
    cny = int(quota) / 500000 * 7.3
    print(f"[billing] quota={quota} → ¥{cny:.4f}（期望 ¥0.50 = 2s × ¥0.25）")
    if not (0.48 <= cny <= 0.52):
        failures.append(f"计费不是 ¥0.50：实际 ¥{cny:.4f}")

    # ---- 成片取证 ----
    if succeeded and result_url:
        video_path = os.path.join(ASSET_DIR, "out.mp4")
        print(f"[download] {result_url[:120]}")
        with urllib.request.urlopen(result_url, timeout=180) as resp, open(video_path, "wb") as handle:
            handle.write(resp.read())
        probe = subprocess.run(
            ["ffprobe", "-v", "error", "-select_streams", "v:0",
             "-show_entries", "stream=width,height:format=duration",
             "-of", "json", video_path],
            capture_output=True, text=True,
        )
        info = json.loads(probe.stdout or "{}")
        stream = (info.get("streams") or [{}])[0]
        duration = float((info.get("format") or {}).get("duration") or 0)
        print(f"[ffprobe] {stream.get('width')}x{stream.get('height')} duration={duration:.3f}s")
        if not (1.8 <= duration <= 2.3):
            failures.append(f"成片时长不是 2 秒：{duration:.3f}s")
        if stream.get("height") != 720:
            failures.append(f"成片不是 720P：{stream.get('width')}x{stream.get('height')}")
        audio_streams = subprocess.run(
            ["ffprobe", "-v", "error", "-select_streams", "a", "-show_entries", "stream=codec_name",
             "-of", "csv=p=0", video_path],
            capture_output=True, text=True,
        ).stdout.strip()
        print(f"[ffprobe] 音轨={audio_streams or '(无)'}")

    used_after, tasks_after = snapshot(user_id)
    print(f"=== after   : admin used_quota={used_after}  tasks={tasks_after} ===")
    if int(tasks_after) - int(tasks_before) != 1:
        failures.append(f"任务数变化异常：{tasks_before} → {tasks_after}")
    if int(used_after) <= int(used_before):
        failures.append("额度没有扣减（说明计费没走）")

    if failures:
        print("\n失败项：")
        for item in failures:
            print("  - " + item)
        raise SystemExit(1)
    print("\n全部通过：参数生效、计费正确、对外不暴露上游。")


if __name__ == "__main__":
    main()
