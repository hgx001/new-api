#!/usr/bin/env python3
"""真机验证 wan3.0-720p-特惠 的比例闸门（2026-11-17）。

背景：
  老板口径：特惠档只做 9:16。代码在 relay/channel/task/wan3/adaptor.go 的
  ValidateRequestAndSetAction 最前面加了比例闸门，非 9:16 直接 400 拒单。

本脚本验证两件事：
  1. 16:9 请求 → **被 400 明确拒单**（而不是静默改成 9:16 交付）
  2. 9:16 请求 → **正常受理建单**

⚠️ 成本说明：
  - 16:9 那条被闸门挡住，**不建单、不扣费**（闸门在素材探测/转存之前）。
  - 9:16 那条会真建单并扣费（2 秒 × 特惠价 ≈ 几分钱）。

用法：
    python scripts/_probe_wan3_720p_ratio_gate.py            # 老机 api.heibaidao.cn
    python scripts/_probe_wan3_720p_ratio_gate.py --api2     # 新机 api2.heibaidao.cn
"""
import json
import subprocess
import sys
import time

IS_API2 = "--api2" in sys.argv

if IS_API2:
    SSH = ["ssh", "newapi-root"]
    API = "https://api2.heibaidao.cn"
else:
    SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
    API = "https://api.heibaidao.cn"

PG = "sudo docker exec postgres psql -U root -d new-api -t -A"

MODEL = "wan3.0-720p-特惠"
# 特惠档最短 4 秒（时长校验先于比例校验执行，2 秒会被 invalid_duration 先拦下）
SECONDS = 4


def sql(query: str) -> str:
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:400]}")
    return out.stdout.strip()


def get_token() -> str:
    row = sql(
        "SELECT t.key FROM tokens t JOIN users u ON u.id = t.user_id "
        "WHERE t.user_id = 28 AND t.status = 1 AND t.unlimited_quota = true "
        "AND u.deleted_at IS NULL ORDER BY t.id LIMIT 1"
    ).strip()
    if not row:
        raise RuntimeError("找不到 admin 的可用令牌")
    return row


def submit(token: str, payload: dict) -> str:
    body = json.dumps(payload, ensure_ascii=True)
    return subprocess.run(
        ["curl", "-s", "-w", "\n__HTTP__%{http_code}", "-X", "POST",
         f"{API}/v1/video/generations",
         "-H", f"Authorization: Bearer {token}",
         "-H", "Content-Type: application/json",
         "-d", body],
        capture_output=True, text=True,
    ).stdout.strip()


def split_http(raw: str):
    if "\n__HTTP__" in raw:
        body, code = raw.rsplit("\n__HTTP__", 1)
        return body.strip(), code.strip()
    return raw.strip(), "?"


def main() -> int:
    token = get_token()
    print(f"[api] {API}  model={MODEL}\n")

    # ── 负向：16:9 应被 400 拒单 ─────────────────────────────────────
    payload_bad = {
        "model": MODEL,
        "prompt": "一只橘猫在窗台上打盹，午后阳光斜照",
        "seconds": str(SECONDS),
        "ratio": "16:9",
    }
    print(f"[submit] 16:9 payload = {json.dumps(payload_bad, ensure_ascii=False)}")
    raw = submit(token, payload_bad)
    body, code = split_http(raw)
    print(f"[http]  {code}")
    print(f"[body] {body[:400]}\n")

    rejected = code == "400" and "9:16" in body
    print(f"{'✅' if rejected else '❌'} 16:9 被 400 拒单且提示 9:16（期望两者皆是）")
    print(f"   实际: http={code}  命中9:16文案={'9:16' in body}\n")

    # ── 正向：9:16 应正常建单 ───────────────────────────────────────
    payload_ok = dict(payload_bad, ratio="9:16")
    print(f"[submit] 9:16 payload = {json.dumps(payload_ok, ensure_ascii=False)}")
    raw = submit(token, payload_ok)
    body2, code2 = split_http(raw)
    print(f"[http]  {code2}")
    print(f"[body] {body2[:400]}\n")

    accepted = code2 == "200"
    task_id = ""
    try:
        task_id = str(json.loads(body2).get("data", {}).get("id") or
                      json.loads(body2).get("id") or "")
    except Exception:
        pass
    print(f"{'✅' if accepted else '❌'} 9:16 正常受理（期望 200）")
    print(f"   task_id = {task_id or '(未解析出)'}")

    time.sleep(4)
    if task_id:
        snap = sql(
            "SELECT payload::text FROM task_logs WHERE stage = 'submit' "
            f"ORDER BY id DESC LIMIT 1"
        ).strip()
        try:
            params = (json.loads(snap) or {}).get("parameters") or {}
            print(f"[snapshot] channel_id = {json.loads(snap).get('channel_id')}")
            for k in ("ratio", "ratio_source"):
                if k in params:
                    print(f"   {k:14s} = {params[k]!r}")
        except Exception as exc:
            print(f"[snapshot] 解析失败：{exc}")

    return 0 if (rejected and accepted) else 1


if __name__ == "__main__":
    sys.exit(main())
