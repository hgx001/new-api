"""wan-3.0-无限 分分辨率定价：生产零成本探针（2026-10-25）。

目的（全部零成本，一条订单都不许建）：
  P1  `wan-3.0-无限` + resolution=360P  → 必须 400（校验拦下），**不能** 503
      —— 400 说明模型/能力/渠道/adaptor 全链路是活的；503 说明模型没了。
  P2  `wan-3.0-无限-480p` + resolution=480P → 必须 503（无可用渠道）
      —— 证明拆分名已从 channels.models / abilities 彻底摘除。
  P3  `wan-3.0-无限-720p` + resolution=720P → 必须 503（同上）。

⚠️ 为什么不直接打一条 480P 真单验证 75 折：
   480P 会真的建一条 Picsart 视频（15–25 分钟、无法取消）并扣 ¥1.50。
   2026-10-25 已因探针误建过一条（task_YFynRVnV9hF5irPLEQvSwLd3cN6i6VU7）。
   倍率本身的正确性由单测覆盖：
     TestPicsartUnlimitedBillingByResolution / TestPicsartUnlimited480PRatioMatchesPriceList
   真机验价请由老板显式授权后再跑。

纪律（同 scripts/_wan27_e2e.py 的 T3）：
  跑完必须断言 sum(tokens.used_quota)、非终态任务数、ArcReel 建单日志数三项零变化。

⚠️ 建单日志只数 `POST /api/v1/remote-generation/jobs`。
   第一版抄了 _wan27_e2e.py 的宽松 grep `remote-generation/jobs`，把客户端
   **GET 轮询**（`GET .../jobs/gen-xxx`）也算进去了，线上有真实流量在轮询，
   于是 870→871 被误报成「有副作用」。实测 used_quota 与任务数都没动，
   差额就是一条 GET 轮询日志。计数口径必须只认 POST。
"""
import json
import subprocess
import sys
import urllib.error
import urllib.request

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
HOST = "https://api.heibaidao.cn"
ARC_LOG = "/home/ubuntu/arcreel/logs/arcreel.log"

WAN3U = "wan-3.0-无限"


def sql(q):
    out = subprocess.run(SSH + [f'{PG} -c "{q}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:300]}\n{q}")
    return out.stdout.strip()


def grep_count(pattern, path):
    out = subprocess.run(
        SSH + [f"grep -c '{pattern}' {path} 2>/dev/null || echo 0"],
        capture_output=True, text=True,
    )
    return int(out.stdout.strip().split("\n")[-1] or 0)


def post(token, body):
    data = json.dumps(body, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        f"{HOST}/v1/videos", data=data, method="POST",
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")


def get_token():
    tk = sql("SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id "
             "WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1").strip()
    if len(tk) < 20:
        raise RuntimeError(f"no usable token, got: {tk[:40]!r}")
    return tk


def baselines():
    return (
        sql("SELECT COALESCE(SUM(used_quota),0) FROM tokens"),
        sql("SELECT count(*) FROM tasks WHERE status NOT IN ('succeeded','failed','cancelled')"),
        grep_count("POST /api/v1/remote-generation/jobs", ARC_LOG),
    )


def main():
    token = get_token()
    before = baselines()
    print(f"=== baseline: used_quota={before[0]}  非终态任务={before[1]}  建单日志={before[2]} ===")

    cases = [
        # (标签, 期望状态码, 期望响应里出现的关键字, 请求体)
        ("P1 现存模型 + 非法分辨率", 400, "resolution",
         {"model": WAN3U, "prompt": "probe", "resolution": "360P"}),
        ("P2 已摘除的 -480p 名", 503, "",
         {"model": f"{WAN3U}-480p", "prompt": "probe", "resolution": "480P"}),
        ("P3 已摘除的 -720p 名", 503, "",
         {"model": f"{WAN3U}-720p", "prompt": "probe", "resolution": "720P"}),
    ]

    failed = []
    for label, want_status, want_kw, body in cases:
        status, text = post(token, body)
        ok = status == want_status and (not want_kw or want_kw in text.lower())
        print(f"[{'PASS' if ok else 'FAIL'}] {label}: HTTP {status} (期望 {want_status})")
        print(f"       {text[:220]}")
        if not ok:
            failed.append(label)

    after = baselines()
    print(f"=== after   : used_quota={after[0]}  非终态任务={after[1]}  建单日志={after[2]} ===")
    if before != after:
        failed.append(f"副作用检测：baseline {before} != after {after}")
        print("[FAIL] 有副作用！额度/任务/建单日志发生了变化")
    else:
        print("[PASS] 零副作用：额度、任务数、ArcReel 建单日志均未变化")

    if failed:
        print("\n失败项：" + "; ".join(failed))
        sys.exit(1)
    print("\n全部通过")


if __name__ == "__main__":
    main()
