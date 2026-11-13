"""万相 wan2.7 免灵感值双模型 生产 T3 零成本路由探针 + T4 真机 E2E。

⚠️ 为什么必须用 Python 而不是 bash + curl（2026-10-16 实测，血泪）：
脚本文件本身是正确的 UTF-8，但经 Git Bash 交给 curl 时被按 **GBK(cp936)** 编码，
服务器按 UTF-8 解析 → `wan2.7-图生` 变成 `wan2.7-ͼ\ufffd\ufffd` → 路由 503
"No available channel for model"。**凡是要把中文模型名送进 HTTP 请求，
一律用 Python/Node 显式 UTF-8 编码，禁止 bash + curl。**

纪律（同 probe-manwu-dola-dual.sh）：T3 只用**必然被 ValidateRequestAndSetAction
拦下的入参**，一条都不许真建单。跑完断言 used_quota / 非终态任务 / ArcReel 建单
日志三项零变化。
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

I2V = "wan2.7-图生"
EDIT = "wan2.7-编辑"


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
            "Content-Type": "application/json; charset=utf-8",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=120) as r:
            return r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")


def get_token():
    tk = sql("SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id "
             "WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1").strip()
    # 库里存的是裸 key（不带 sk- 前缀，前缀由运行时拼），只要非空即可。
    if len(tk) < 20:
        raise RuntimeError(f"no usable token, got: {tk[:40]!r}")
    return tk


def baselines():
    return (
        sql("SELECT COALESCE(SUM(used_quota),0) FROM tokens"),
        sql("SELECT count(*) FROM tasks WHERE status NOT IN ('succeeded','failed','cancelled')"),
        grep_count("remote-generation/jobs", ARC_LOG),
    )


def show_baselines(tag, b):
    print(f"=== {tag}：sum(token.used_quota)={b[0]}  非终态任务={b[1]}  ArcReel 建单日志数={b[2]} ===")


# ---------------------------------------------------------------- T3
def t3():
    tk = get_token()
    b0 = baselines()
    show_baselines("起始", b0)
    abilities = sql("SELECT model || '/' || enabled || '/ch' || channel_id FROM abilities "
                    "WHERE model LIKE 'wan2.7-%' ORDER BY model")
    print("=== 现网 abilities（wan2.7-%）===")
    print(abilities)

    key = "w27t3-" + sql("SELECT to_char(now(),'YYYYMMDD')")
    # expect：门禁类应为 400（adaptor 拦下）；未知模型类应为 503（new-api 分发层
    # 在到达 adaptor 之前就无渠道可选 —— 同样零建单零计费，是**正确**行为）。
    probes = [
        ("图生 seconds=4（<5 越界）", {"model": I2V, "prompt": "probe", "idempotency_key": key + "a1", "seconds": 4}, 400),
        ("图生 seconds=16（>15 越界）", {"model": I2V, "prompt": "probe", "idempotency_key": key + "a2", "seconds": 16}, 400),
        ("图生 resolution=480P", {"model": I2V, "prompt": "probe", "idempotency_key": key + "a3", "resolution": "480P"}, 400),
        ("图生 n=2（按次模型拒 n）", {"model": I2V, "prompt": "probe", "idempotency_key": key + "a4", "n": 2}, 400),
        ("图生带参考视频（应走编辑）", {"model": I2V, "prompt": "probe", "idempotency_key": key + "a5",
                              "videos": ["https://example.com/a.mp4"]}, 400),
        ("图生参考图 3 张（>2）", {"model": I2V, "prompt": "probe", "idempotency_key": key + "a6",
                          "input_reference": ["https://example.com/1.png", "https://example.com/2.png",
                                              "https://example.com/3.png"]}, 400),
        ("编辑缺主视频", {"model": EDIT, "prompt": "probe", "idempotency_key": key + "b1"}, 400),
        ("编辑主视频 2 个", {"model": EDIT, "prompt": "probe", "idempotency_key": key + "b2",
                       "videos": ["https://example.com/a.mp4", "https://example.com/b.mp4"]}, 400),
        ("编辑主视频非 http(s)", {"model": EDIT, "prompt": "probe", "idempotency_key": key + "b3",
                          "videos": ["not-a-url"]}, 400),
        ("编辑 seconds=20（越界）", {"model": EDIT, "prompt": "probe", "idempotency_key": key + "b4",
                            "videos": ["https://example.com/a.mp4"], "seconds": 20}, 400),
        ("未知模型 wan2.7-视频", {"model": "wan2.7-视频", "prompt": "probe", "idempotency_key": key + "c1"}, 503),
        ("上游真名 Wan2_7 不可直卖", {"model": "Wan2_7", "prompt": "probe", "idempotency_key": key + "c2"}, 503),
        ("旧命名 wan2.7-i2v", {"model": "wan2.7-i2v", "prompt": "probe", "idempotency_key": key + "c3"}, 503),
    ]

    rc = 0
    for label, body, want in probes:
        code, text = post(tk, body)
        if code == want:
            print(f"  OK   {label:<28} HTTP {code}（期望 {want}）  {text[:100]}")
        else:
            print(f"  FAIL {label:<28} HTTP {code}（期望 {want}）  {text[:200]}")
            rc = 1

    b1 = baselines()
    show_baselines("结束", b1)
    checks = [
        ("无扣费", b0[0] == b1[0], f"{b0[0]} -> {b1[0]}"),
        ("无新非终态任务", b0[1] == b1[1], f"{b0[1]} -> {b1[1]}"),
        ("ArcReel 无建单", b0[2] == b1[2], f"{b0[2]} -> {b1[2]}"),
    ]
    for name, ok, detail in checks:
        print(("  OK   " if ok else "  FAIL ") + f"{name}（{detail}）")
        rc = rc or (0 if ok else 1)
    print("T3 通过：路由/门禁全部符合预期，零成本零建单。" if rc == 0 else "T3 失败：见上方 FAIL 行。")
    return rc


# ---------------------------------------------------------------- T4
def t4():
    """真机 E2E：图生 + 编辑各 1 单。需显式 ALLOW_PAID=1。

    分开发布（WHICH=a / b / both）：先发 a，确认已被 Worker 领走并进入 running，
    再发 b —— 避免派单链路本身就有问题时一次烧掉两单。
    """
    import os
    if os.environ.get("ALLOW_PAID") != "1":
        print("ABORT: T4 会真建单并真实扣第三方额度，需 ALLOW_PAID=1 确认。")
        return 2
    tk = get_token()
    b0 = baselines()
    show_baselines("起始", b0)

    which = os.environ.get("WHICH", "both").strip().lower()
    video_url = os.environ.get("WAN27_EDIT_VIDEO_URL", "").strip()
    if which in ("b", "both") and not video_url:
        print("ABORT: 编辑模型需要公网主视频 URL，请设 WAN27_EDIT_VIDEO_URL。")
        return 2

    key = "w27t4-" + sql("SELECT to_char(now(),'YYYYMMDDHH24MI')")
    orders = {
        "a": ("T4-a 图生（纯文生 5s/720P）", {"model": I2V, "prompt": "一只橘猫在阳光下伸懒腰，电影感",
                                    "seconds": 5, "resolution": "720P", "idempotency_key": key + "-i2v"}),
        "b": ("T4-b 编辑（5s/720P + 主视频）", {"model": EDIT, "prompt": "把画面渐变为冬季雪景，保持花卉主体不变",
                                    "seconds": 5, "resolution": "720P", "videos": [video_url],
                                    "idempotency_key": key + "-edit"}),
    }
    seq = ["a", "b"] if which == "both" else [which]
    task_ids = []
    for k in seq:
        label, body = orders[k]
        code, text = post(tk, body)
        short = text[:200].replace("\n", " ")
        print(f"{label}: HTTP {code}  {short}")
        if code in (200, 201):
            try:
                tid = json.loads(text).get("id") or json.loads(text).get("task_id")
                if tid:
                    task_ids.append((label, tid))
            except Exception:
                pass
        else:
            print(f"  !! {label} 未建成，后续请人工核查")
    if task_ids:
        print("\n=== 已建 task id（用于后续对账）===")
        for label, tid in task_ids:
            print(f"  {label}: {tid}")
    print("\n提示：单条约 3 小时出片。后续轮询：")
    print(f'  curl -s {HOST}/v1/videos/<task_id> -H "Authorization: Bearer $TK"')
    b1 = baselines()
    show_baselines("建单后", b1)
    return 0


if __name__ == "__main__":
    mode = sys.argv[1] if len(sys.argv) > 1 else "t3"
    sys.exit({"t3": t3, "t4": t4, "schema": lambda: 0}[mode]())
