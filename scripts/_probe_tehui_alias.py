"""seedance2.5-特惠 路由到 wan3.0-video-prime：生产零成本探针（2026-10-29）。

目的（全部零成本，一条订单都不许建）：
  P1  `seedance2.5-特惠` + media 里放 first_frame
      → 必须 400 且含 "first_frame"，**不能** 503
      —— 400 说明 名字→能力→渠道 17→youzanwan3 adaptor 整条链是活的，
         并且别名确实落到了 prime 的校验分支（首尾帧已被上游取消）。
  P2  `wan3.0-video-prime` + 同样入参
      → 必须 400 且含 "first_frame"（对照组：别名与 prime 行为必须一致）。
  P3  `seedance2.5-特惠-不存在` + 同样入参
      → 必须 503（对照组：证明「模型不存在」长的是 503 这个形状，
         与 P1 的 400 能明确区分开）。

⚠️ 为什么用 first_frame 而不是真单：
   真单会建一条 30 秒 1080P 视频（¥9）并真实扣费。首尾帧在
   ValidateRequestAndSetAction → buildMedia → validateMedia 里被本地拒绝
   （relay/relay_task.go:157 调用，早于预扣），不建单、不扣费、不打上游。

纪律（同 scripts/_probe_wan3u_pricing.py）：
  跑完必须断言 sum(tokens.used_quota)、非终态任务数、ArcReel 建单日志数三项零变化。
  建单日志只数 `POST /api/v1/remote-generation/jobs`（宽松 grep 会把 GET 轮询算进来）。
"""
import json
import subprocess
import urllib.error
import urllib.request

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
HOST = "https://api.heibaidao.cn"
ARC_LOG = "/home/ubuntu/arcreel/logs/arcreel.log"

TEHUI = "seedance2.5-特惠"
PRIME = "wan3.0-video-prime"

BAD_MEDIA = [{"type": "first_frame", "url": "https://example.com/probe.png"}]


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
        ("P1 别名 seedance2.5-特惠 + 首尾帧", 400, "first_frame",
         {"model": TEHUI, "prompt": "probe", "metadata": {"media": BAD_MEDIA}}),
        ("P2 对照 wan3.0-video-prime + 首尾帧", 400, "first_frame",
         {"model": PRIME, "prompt": "probe", "metadata": {"media": BAD_MEDIA}}),
        ("P3 对照 不存在的名字", 503, "",
         {"model": f"{TEHUI}-不存在", "prompt": "probe", "metadata": {"media": BAD_MEDIA}}),
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
        raise SystemExit(1)
    print("\n全部通过。")


if __name__ == "__main__":
    main()
