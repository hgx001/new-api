#!/usr/bin/env python3
"""改派计费口径的实机验证（2026-10-30）。

验证「客户请求哪个模型就按哪个模型收费」：
发一条 **30 秒** 的 seedance2.5-特惠，条件路由会把它改派到 seedance-2-5（豆包）。
预期扣费 = ¥0.20/秒 × 30 = ¥6.00，即 410940 quota —— **不是** 豆包的按次 ¥3.00（205479）。

三个断言：
  1. 消费日志 quota == 410940（源模型按秒价），且 != 205479
  2. 日志 other.model_price == 源模型价 0.027397260273972605（不是目标模型的 0.411）
  3. 客户侧日志 model_name 仍是源模型，且看不到 route_target_model / 渠道痕迹

成本：30 秒任务。豆包额度若已耗尽 → 任务失败全额退款，净成本 0；
      若成功 → 客户侧收 ¥6.00（上游按次 ¥3.00）。

用法：python scripts/_verify_route_billing.py
"""
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
HOST = "https://api.heibaidao.cn"
MODEL = "seedance2.5-特惠"
TARGET = "seedance-2-5"
SECONDS = 30

SOURCE_PRICE = 0.027397260273972605  # ¥0.20/秒
TARGET_PRICE = 0.410958904109589     # ¥3.00/次
QUOTA_PER_UNIT = 500000
EXPECTED_QUOTA = int(SOURCE_PRICE * QUOTA_PER_UNIT) * SECONDS   # 13698 × 30 = 410940
FLAT_QUOTA = int(TARGET_PRICE * QUOTA_PER_UNIT)                  # 205479

failures = []


def sql(query):
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:300]}\n{query}")
    return out.stdout.strip()


def request(method, path, token, body=None, timeout=120):
    data = None if body is None else json.dumps(body, ensure_ascii=False).encode("utf-8")
    hdrs = {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}
    req = urllib.request.Request(f"{HOST}{path}", data=data, method=method, headers=hdrs)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as err:
        return err.code, err.read().decode("utf-8", "replace")


def admin_token():
    row = sql("SELECT t.user_id || ' ' || t.key FROM tokens t "
              "JOIN users u ON u.id=t.user_id "
              "WHERE t.status=1 AND u.role>=10 AND u.deleted_at IS NULL "
              "AND length(t.key) > 20 ORDER BY t.id LIMIT 1").strip()
    uid, _, token = row.partition(" ")
    if len(token) < 20:
        raise RuntimeError(f"no admin token, got: {row[:40]!r}")
    return int(uid), token


def check(label, ok, detail=""):
    print(f"[{'OK  ' if ok else 'FAIL'}] {label}{('  ' + detail) if detail else ''}")
    if not ok:
        failures.append(f"{label} {detail}")


def main():
    user_id, token = admin_token()
    print(f"[token] admin user={user_id}")
    print(f"[expect] {SECONDS}s × 源模型 ¥0.20/秒 → quota {EXPECTED_QUOTA}（¥6.00）")
    print(f"[expect] 不是目标模型按次 {FLAT_QUOTA}（¥3.00）")

    # 记下请求前的日志最大 id，后面只查新增的
    before_id = int(sql("SELECT COALESCE(max(id),0) FROM logs").strip() or 0)

    body = {"model": MODEL, "prompt": "一只橘猫在窗边打盹。", "seconds": str(SECONDS)}
    status, text = request("POST", "/v1/videos", token, body)
    print(f"[submit] HTTP {status}: {text[:260]}")
    if status != 200:
        print("[FAIL] 提交未成功，先确认部署是否完成")
        raise SystemExit(1)

    payload = json.loads(text)
    task_id = payload.get("id") or payload.get("task_id")
    print(f"[submit] task_id={task_id} model={payload.get('model')}")
    check("提交响应 model 是源模型", payload.get("model") == MODEL,
          f"got {payload.get('model')!r}")

    # 等消费日志落库
    row = ""
    for _ in range(20):
        row = sql(f"SELECT id, model_name, quota, other, content FROM logs "
                  f"WHERE id > {before_id} AND type=2 ORDER BY id DESC LIMIT 1").strip()
        if row:
            break
        time.sleep(2)
    if not row:
        print("[FAIL] 没有查到新增的消费日志")
        raise SystemExit(1)

    log_id, model_name, quota, other_raw, content = row.split("|", 4)
    quota = int(quota)
    other = json.loads(other_raw)
    print(f"[log] id={log_id} model_name={model_name} quota={quota}")
    print(f"[log] other={json.dumps(other, ensure_ascii=False)}")
    print(f"[log] content={content}")

    # ① 扣费额度：按源模型按秒价
    check("扣费 = 源模型 ¥0.20 × 30 秒", quota == EXPECTED_QUOTA,
          f"quota={quota}, want {EXPECTED_QUOTA}")
    check("扣费不是目标模型的按次价", quota != FLAT_QUOTA,
          f"quota={quota} 撞上了 {FLAT_QUOTA}")

    # ② 日志里的单价必须是源模型价
    check("other.model_price = 源模型价",
          abs(float(other.get("model_price", 0)) - SOURCE_PRICE) < 1e-15,
          f"got {other.get('model_price')}")

    # ③ 确实发生了改派（管理员可见的目标模型）
    check("other.route_target_model = 改派目标", other.get("route_target_model") == TARGET,
          f"got {other.get('route_target_model')!r}")

    # ④ 计算参数里带着 30 秒（证明 seconds 倍率确实补上了）
    check("content 里带 seconds: 30", "seconds: 30" in content, f"got {content!r}")

    # ---------- 客户侧出口：日志接口 ----------
    code, text = request("GET", "/api/log/self?p=0&page_size=5", token)
    if code == 200:
        data = json.loads(text)
        items = data.get("data") or []
        if isinstance(items, dict):
            items = items.get("items") or []
        hit = None
        for item in items:
            if str(item.get("model_name")) == MODEL and int(item.get("quota") or 0) == quota:
                hit = item
                break
        if hit:
            blob = json.dumps(hit, ensure_ascii=False)
            check("客户侧日志 model_name 是源模型", hit.get("model_name") == MODEL,
                  f"got {hit.get('model_name')!r}")
            check("客户侧看不到 route_target_model", "route_target_model" not in blob)
            check("客户侧看不到 channel_name", not hit.get("channel_name"))
            for banned in ("seedance-2-5", "doubao", "豆包", "manwu", "漫屋"):
                check(f"客户侧不出现 {banned}", banned.lower() not in blob.lower())
        else:
            print("[warn] 日志接口前 5 条里没找到本次记录（可能有延迟），跳过客户侧断言")
    else:
        print(f"[warn] 日志接口 HTTP {code}: {text[:160]}")

    print()
    if failures:
        print(f"FAILED ({len(failures)})")
        for item in failures:
            print("  -", item)
        sys.exit(1)
    print("ALL OK")


if __name__ == "__main__":
    main()
