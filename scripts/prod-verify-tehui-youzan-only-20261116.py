"""线上探针：验证「特惠只有 <10 秒走 CapCut，其余都走有赞」（2026-11-16 口径）。

要验的三条边界（每条跑一条真机任务，看**实际落到哪个渠道 + 改派成什么模型**）：
  A)  8 秒 → 应落 CapCut 满血档（渠道 20 漫屋，模型 cp-seedance-2.0）
  B) 10 秒 → 应落有赞标准版（渠道 17 有赞，模型 wan3.0-video）
  C) 20 秒 → 应留在源模型 seedance2.5-特惠（渠道 17 有赞）
  D) 30 秒 → 应留在源模型，但**上游模型是 wan3.0-video-prime**（画质增强）

⚠️ 成本提醒：特惠按秒计费，8 秒 ≈ ¥1.60、10 秒 ≈ ¥2.00、20 秒 ≈ ¥4.00、
   30 秒 ≈ ¥6.00（prime 720P 成本 ¥0.15/秒 ≈ ¥4.50）。用 admin 无限额度令牌。

⚠️ 请求体走 **base64** 传输：JSON 里的中文经 bash heredoc 会截断
   （踩过一次 —— 表现为 curl 提交失败、task_id 为空）。
"""
import base64
import json
import os
import subprocess
import tempfile
import time

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
TOKEN_ID = 144

# 期望：秒数 → (期望渠道, 期望上游模型, 说明)
CASES = [
    (8, "20", "cp-seedance-2.0", "CapCut 满血档"),
    (10, "17", "wan3.0-video", "有赞标准版"),
    (20, "17", "seedance2.5-特惠", "源模型（标准版执行）"),
    (30, "17", "seedance2.5-特惠", "源模型（prime 执行）"),
]


def psql(query: str) -> str:
    out = subprocess.run(
        SSH + ["docker exec -i postgres psql -U root -d new-api -t -A"],
        input=query, capture_output=True, text=True,
    )
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:400]}\n{query}")
    return out.stdout.strip()


def submit(seconds: int) -> str:
    payload = {
        "model": "seedance2.5-特惠",
        "prompt": "a tabby cat napping on a windowsill, warm afternoon sunlight",
        "seconds": str(seconds),
    }
    body = base64.b64encode(
        json.dumps(payload, ensure_ascii=False).encode("utf-8")
    ).decode()
    script = f"""#!/bin/bash
set -uo pipefail
KEY=$(docker exec postgres psql -U root -d new-api -t -A -c 'SELECT "key" FROM tokens WHERE id={TOKEN_ID}' | tr -d '\\r\\n')
echo '{body}' | base64 -d > /tmp/req_probe.json
curl -s -X POST http://127.0.0.1:3000/v1/video/generations \\
  -H "Authorization: Bearer sk-$KEY" -H "Content-Type: application/json" \\
  -d @/tmp/req_probe.json
"""
    # ⚠️ 脚本必须走 **scp**：本地 `ssh host bash -s` 的 stdin 会被
    # 远端 bash 与本地的输入缓冲互相吃掉（症状：curl 根本没发出、
    # task_id 为空、没有任何报错）。scp 传文件后单独执行没有这个问题。
    with tempfile.NamedTemporaryFile("w", suffix=".sh", delete=False,
                                     encoding="utf-8", newline="\n") as fh:
        fh.write(script)
        local_path = fh.name
    remote_path = "/tmp/probe_submit.sh"
    subprocess.run(["scp", "-P", "877", local_path,
                    "ubuntu@119.29.253.97:" + remote_path], check=True)
    os.unlink(local_path)
    out = subprocess.run(SSH + ["bash", remote_path], capture_output=True, text=True)
    raw = out.stdout.strip().splitlines()[-1] if out.stdout.strip() else ""
    if not raw:
        print("  [submit stderr]", out.stderr.strip()[:300])
    try:
        resp = json.loads(raw)
    except Exception:
        print("  [submit 原始响应]", raw[:300])
        return ""
    return str(resp.get("task_id") or resp.get("id") or "")


def query_task(task_id: str) -> dict:
    """按 task_id 精确查（不用时间窗口，避免撞上上一条）。"""
    row = psql(
        "SELECT id || '|' || coalesce(channel_id::text,'') || '|' || "
        "coalesce(status,'') || '|' || coalesce(quota::text,'') "
        f"FROM tasks WHERE task_id = '{task_id}'"
    )
    if not row:
        return {}
    parts = row.split("|")
    if len(parts) < 4 or not parts[1]:
        return {}
    props = psql(
        "SELECT coalesce(properties->>'upstream_model_name','') || '|' || "
        "coalesce(properties->>'origin_model_name','') "
        f"FROM tasks WHERE id={parts[0]}"
    )
    return {
        "task_id": parts[0],
        "channel_id": parts[1],
        "status": parts[2],
        "quota": parts[3],
        "upstream": props.split("|")[0] if props else "",
        "origin": props.split("|")[1] if "|" in props else "",
    }


def wait_task(task_id: str, timeout: int = 90) -> dict:
    deadline = time.time() + timeout
    while time.time() < deadline:
        info = query_task(task_id)
        if info:
            return info
        time.sleep(4)
    return {}


def main():
    results = []
    for seconds, want_channel, want_model, desc in CASES:
        print("=" * 72)
        print(f"探针：{seconds} 秒 → 期望渠道 {want_channel} / {want_model}（{desc}）")
        print("=" * 72)
        task_id = submit(seconds)
        print(f"  已提交 task_id={task_id}")
        if not task_id:
            print("  ⚠️ 提交失败")
            results.append((seconds, want_channel, want_model, "NO_TASK", ""))
            print()
            continue
        time.sleep(5)
        info = wait_task(task_id)
        if not info:
            print("  ⚠️ 任务未落库")
            results.append((seconds, want_channel, want_model, "NO_TASK", ""))
            print()
            continue
        got_channel = info["channel_id"]
        got_model = info["upstream"]
        channel_ok = got_channel == want_channel
        # 上游模型名有两类：改派后的模型名（cp-seedance-2.0 / wan3.0-video）
        # 与留在源模型时的对外名（seedance2.5-特惠）。
        model_ok = got_model == want_model
        flag = "✅" if (channel_ok and model_ok) else "❌"
        quota_cny = int(info["quota"]) / 500000 * 7.3 if info["quota"].isdigit() else 0
        print(f"  {flag} task={info['task_id']} channel={got_channel} "
              f"upstream_model={got_model} origin={info['origin']} "
              f"status={info['status']} 计费≈¥{quota_cny:.2f}")
        results.append((seconds, want_channel, want_model, got_channel, got_model))
        print()

    print("=" * 72)
    print("汇总")
    print("=" * 72)
    for seconds, wc, wm, gc, gm in results:
        flag = "✅" if (gc == wc and gm == wm) else "❌"
        print(f"  {flag} {seconds:>2} 秒：期望 {wc}/{wm}，实际 {gc}/{gm}")


if __name__ == "__main__":
    main()
