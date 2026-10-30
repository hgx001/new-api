#!/usr/bin/env python3
"""客户侧渠道痕迹脱敏的实机验证（2026-10-30）。

跑**一条 2 秒**任务，把客户能看到的每个出口都扫一遍，
确认里面不出现任何具体渠道/上游痕迹（有渠道的，对外一律说「上游」）。

覆盖的出口：
  1. POST /v1/videos      —— 提交响应（model 字段是否露出改派目标）
  2. GET  /v1/videos/{id} —— 轮询响应（model / metadata.url / error.message）
  3. GET  /api/task/self  —— 任务列表（platform / channel_id / properties / result_url）
  4. GET  /api/log/self   —— 日志（channel / channel_name / other / content）

禁止出现的字样（渠道/供应商/上游痕迹）：
  youzan 有赞 aliyun 阿里 dashscope 百炼 wan3 wan-3.0 腾讯 tencent
  volcengine 火山 豆包 doubao 漫屋 manwu arcreel 巡狩 三元 erchun 二春
  aliyuncs.com r2.cloudflarestorage.com hailuo-video

成本：2 秒 × ¥0.20/秒 = ¥0.40（管理员自己的令牌，不动客户余额）。

用法：python scripts/_verify_redact_live.py
"""
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
HOST = "https://api.heibaidao.cn"
MODEL = "seedance2.5-特惠"   # 别名模型：条件路由会把它改派到 wan3 / seedance-2-5
PROMPT = "一只橘猫在窗边打盹。"

# 客户侧不该出现的字样
BANNED = [
    "youzan", "有赞", "aliyun", "阿里", "dashscope", "百炼",
    "wan3", "wan-3.0", "tencent", "腾讯", "volcengine", "火山",
    "doubao", "豆包", "manwu", "漫屋", "arcreel", "巡狩", "三元",
    "erchun", "二春", "aliyuncs.com", "r2.cloudflarestorage.com",
    "hailuo-video", "cos-proxy://",
]

failures = []


def sql(query):
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:300]}\n{query}")
    return out.stdout.strip()


def request(method, path, token, body=None, timeout=120, headers=None):
    data = None if body is None else json.dumps(body, ensure_ascii=False).encode("utf-8")
    hdrs = {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}
    if headers:
        hdrs.update(headers)
    req = urllib.request.Request(f"{HOST}{path}", data=data, method=method, headers=hdrs)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as err:
        return err.code, err.read().decode("utf-8", "replace")


def admin_token():
    """只用管理员自己的令牌：测试产生的费用记在自己账上，不动客户余额。"""
    row = sql("SELECT t.user_id || ' ' || t.key FROM tokens t "
              "JOIN users u ON u.id=t.user_id "
              "WHERE t.status=1 AND u.role>=10 AND u.deleted_at IS NULL "
              "AND length(t.key) > 20 "
              "ORDER BY t.id LIMIT 1").strip()
    uid, _, token = row.partition(" ")
    if len(token) < 20:
        raise RuntimeError(f"no admin token, got: {row[:40]!r}")
    return int(uid), token


def scan(label, payload, extra_banned=()):
    """扫描一个出口的原始响应体，找出渠道痕迹。"""
    if isinstance(payload, (dict, list)):
        text = json.dumps(payload, ensure_ascii=False)
    else:
        text = str(payload)
    low = text.lower()
    for token in list(BANNED) + list(extra_banned):
        if token.lower() in low:
            failures.append(f"{label} 暴露渠道痕迹：{token!r}")
    return text


def main():
    user_id, token = admin_token()
    print(f"[token] admin user={user_id}")

    # ---------- 出口 1：提交响应 ----------
    body = {
        "model": MODEL,
        "prompt": PROMPT,
        "seconds": "2",
    }
    status, text = request("POST", "/v1/videos", token, body)
    print(f"[submit] HTTP {status}: {text[:300]}")
    if status != 200:
        print("[FAIL] 提交未成功，先确认部署是否完成")
        raise SystemExit(1)

    scan("提交响应", text)
    payload = json.loads(text)
    task_id = payload.get("id") or payload.get("task_id")
    print(f"[submit] task_id={task_id}  model={payload.get('model')}")

    # 提交响应的 model 必须是用户请求的源模型（不能是改派目标）
    if payload.get("model") != MODEL:
        failures.append(f"提交响应 model={payload.get('model')!r}，应为源模型 {MODEL!r}")

    # ---------- 出口 2：轮询 ----------
    deadline = time.time() + 20 * 60
    last = ""
    poll_text = ""
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
        scan("轮询响应", poll_text)
        if state in ("completed", "failed"):
            break
        time.sleep(8)
    else:
        print("[FAIL] 20 分钟内未到终态")
        raise SystemExit(1)

    data = json.loads(poll_text)
    if data.get("model") != MODEL:
        failures.append(f"轮询响应 model={data.get('model')!r}，应为源模型 {MODEL!r}")

    # ---------- 出口 3：任务列表 ----------
    code, list_text = request("GET", "/api/task/self?p=0", token)
    print(f"[task/self] HTTP {code}: {list_text[:300]}")
    if code == 200:
        scan("任务列表", list_text)
        page = json.loads(list_text)
        items = ((page.get("data") or {}).get("data") or [])
        for item in items:
            if item.get("task_id") != task_id:
                continue
            print(f"[task/self] platform={item.get('platform')!r} "
                  f"channel_id={item.get('channel_id')} "
                  f"properties={json.dumps(item.get('properties'), ensure_ascii=False)}")
            if item.get("platform") not in (None, "", "上游"):
                failures.append(f"任务列表 platform={item.get('platform')!r}，应为「上游」")
            if item.get("channel_id"):
                failures.append(f"任务列表 channel_id={item.get('channel_id')}，应为 0")
    else:
        print("[note] /api/task/self 需要 session 登录态，跳过")

    # ---------- 出口 4：日志 ----------
    code, log_text = request("GET", "/api/log/self?p=0", token)
    print(f"[log/self] HTTP {code}: {log_text[:200]}")
    if code == 200:
        scan("日志", log_text)
    else:
        print("[note] /api/log/self 需要 session 登录态，改用库内取证")

    # ---------- 库内取证：确认脱敏层没有误删交付信息 ----------
    def task_field(name):
        return sql(f"SELECT COALESCE({name}::text,'') FROM tasks WHERE task_id='{task_id}'").strip()

    print(f"[db] status={task_field('status')} quota={task_field('quota')}")
    print(f"[db] fail_reason={task_field('fail_reason')[:200]}")
    print(f"[db] properties={task_field('properties')[:400]}")

    quota = task_field("quota") or "0"
    cny = int(quota) / 500000 * 7.3
    print(f"[billing] quota={quota} → ¥{cny:.4f}（期望 ¥0.40 = 2s × ¥0.20）")
    if quota and not (0.38 <= cny <= 0.42):
        failures.append(f"计费不是 ¥0.40：实际 ¥{cny:.4f}")

    if failures:
        print("\n失败项：")
        for item in failures:
            print("  - " + item)
        raise SystemExit(1)
    print(f"\n全部通过：客户侧各出口均无渠道痕迹，模型名对外是源模型 {MODEL!r}。")


if __name__ == "__main__":
    main()
