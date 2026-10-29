"""wan3.0-720p-官网 两跳链路真机验证（2026-10-30）。

老板授权「跑一条 2 秒」。

测什么（一条任务覆盖全部关键点）：
  1. 提交成功、模型路由到 wan3 渠道
  2. 上游回执里 resolution 是 **480P**（两跳链路的第一跳必须锁 480P，
     否则用户付 480P 的钱却生成 720P，两跳形同虚设）
  3. 第二跳（MPS 2 倍提升）跑完，产物真的是 **720P**
  4. 交付地址能下到文件，且下到的确实是 720P
  5. 扣费 = ¥0.25/秒 × 2 秒 = ¥0.50
  6. 对外文案不暴露内部实现（超分/腾讯云/MPS/COS）

成本：2 秒 × ¥0.25/秒 = ¥0.50 + 腾讯云提升费约 ¥0.013。

用法：python scripts/_e2e_wan3720p.py
"""
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
MODEL = "wan3.0-720p-官网"

ASSET_DIR = tempfile.mkdtemp(prefix="w3720p_")
OUT_MP4 = os.path.join(ASSET_DIR, "out.mp4")

PROMPT = "一只橘猫在窗边打盹，阳光缓慢移动，镜头轻微推进。"


def sql(query):
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:300]}\n{query}")
    return out.stdout.strip()


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
    user_id, token = admin_token()
    used_before, tasks_before = snapshot(user_id)
    print(f"=== baseline: user={user_id} used_quota={used_before}  tasks={tasks_before} ===")

    body = {
        "model": MODEL,
        "prompt": PROMPT,
        "seconds": "2",
        "resolution": "720P",
    }

    failures = []
    status, text = request("POST", "/v1/videos", token, body)
    print(f"[submit] HTTP {status}")
    print(f"         {text[:400]}")
    if status != 200:
        print("[FAIL] 提交未成功")
        raise SystemExit(1)

    payload = json.loads(text)
    task_id = payload.get("id") or payload.get("task_id")
    print(f"[submit] task_id={task_id}")

    # ---- 轮询：两跳链路耗时更久（官网约 6 分钟 + 提升约 1-2 分钟）----
    deadline = time.time() + 20 * 60
    last = ""
    result_url = ""
    while time.time() < deadline:
        code, poll_text = request("GET", f"/v1/videos/{task_id}", token, timeout=60)
        if code != 200:
            print(f"[poll] HTTP {code}: {poll_text[:200]}")
            time.sleep(8)
            continue
        data = json.loads(poll_text)
        state = data.get("status")
        if state != last:
            print(f"[poll] status={state} progress={data.get('progress')}")
            last = state
        # 对外文案检查（脱敏）
        for leaked in ("超分", "腾讯", "tencent", "mps", "dashscope", "aliyun", "有赞", "youzan", "cos"):
            if leaked in poll_text.lower():
                failures.append(f"轮询响应暴露内部实现：{leaked}")
        if state in ("completed", "failed"):
            result_url = (data.get("metadata") or {}).get("url", "")
            break
        time.sleep(8)
    else:
        print("[FAIL] 20 分钟内未到终态")
        raise SystemExit(1)

    # ---- 库内取证 ----
    def task_field(name):
        return sql(f"SELECT COALESCE({name}::text,'') FROM tasks WHERE task_id='{task_id}'").strip()

    status_db = task_field("status")
    quota = task_field("quota") or "0"
    fail_reason = task_field("fail_reason")
    props = task_field("properties")
    upstream = task_field("data")
    private = task_field("private_data")
    print(f"[db] status={status_db} quota={quota}")
    print(f"[db] fail_reason={fail_reason[:200]}")
    print(f"[db] properties={props[:400]}")

    succeeded = status_db.lower() in ("success", "succeeded")
    if not succeeded:
        failures.append(f"任务未成功：status={status_db} fail_reason={fail_reason[:200]}")

    # 第一跳必须是 480P
    receipt = json.loads(upstream) if upstream else {}
    output = receipt.get("output") or {}
    usage = output.get("usage") or {}
    print(f"[receipt] task_status={output.get('task_status')} "
          f"usage={json.dumps(usage, ensure_ascii=False)[:200]}")
    if usage.get("SR") not in (480, None):
        failures.append(f"第一跳分辨率不是 480P（usage.SR={usage.get('SR')}）")

    # 第二跳状态
    pp = (json.loads(private) if private else {}).get("post_process") or {}
    print(f"[post_process] stage={pp.get('stage')} provider={pp.get('provider')} "
          f"error={pp.get('error')}")
    if pp.get("stage") == "success":
        print(f"[post_process] 输出路径 = {pp.get('output_path')}")
    elif pp.get("stage") == "failed":
        failures.append(f"第二跳失败（回落到 480P 原片）：{pp.get('error')}")

    # 计费：2 秒 × ¥0.25/秒 = ¥0.50
    cny = int(quota) / 500000 * 7.3
    print(f"[billing] quota={quota} → ¥{cny:.4f}（期望 ¥0.50 = 2s × ¥0.25）")
    if not (0.48 <= cny <= 0.52):
        failures.append(f"计费不是 ¥0.50：实际 ¥{cny:.4f}")

    # ---- 成片取证 ----
    if result_url:
        print(f"[download] {result_url[:140]}")
        try:
            with urllib.request.urlopen(result_url, timeout=300) as resp, open(OUT_MP4, "wb") as handle:
                handle.write(resp.read())
        except Exception as exc:  # noqa: BLE001
            failures.append(f"成片下载失败：{exc!r}")
            print(f"[download] FAILED: {exc!r}")
        else:
            size = os.path.getsize(OUT_MP4)
            w, h, dur = ffprobe(OUT_MP4)
            print(f"[ffprobe] {w}x{h} duration={dur:.3f}s  size={size/1024/1024:.2f}MB")
            if not (1.8 <= dur <= 2.5):
                failures.append(f"成片时长异常：{dur:.3f}s")
            # 720P 的短边应为 720（横屏 1280x720 / 竖屏 720x1280）
            short_side = min(w or 0, h or 0)
            if short_side != 720:
                failures.append(f"成片短边不是 720（实际 {w}x{h}，短边 {short_side}）")

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
    print("\n全部通过：两跳链路成立、产物 720P、计费正确、对外不暴露内部实现。")
    print(f"成片已存：{OUT_MP4}")


if __name__ == "__main__":
    main()
