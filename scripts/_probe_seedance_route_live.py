"""条件路由的**零成本**线上探针（2026-10-30）。

不建单、不扣费的办法：用一个**白名单外**的素材类型打一发。
`ValidateRequestAndSetAction`（relay/relay_task.go:157）在**预扣费之前**校验，
素材类型非法 → 本地 400，不建任务、不扣钱。

⚠️ **不能用 first_frame**：那是合法类型（validateWan3Media 白名单含
first_frame/last_frame），拿它当探针会真的建单扣费。
必须用 bogus_type 这种白名单外的东西。

判据（靠**错误文案**区分走了哪条链路）：
  - 开关关闭 → 命中有赞链路 → 文案 "上游 media type \"bogus_type\" is unsupported"
  - 开关打开 + 30 秒 → 命中漫屋链路 → 文案是漫屋的
    （"prompt references ..." / "invalid_input_reference" 之类）
  - 开关打开 + 15 秒 → 仍走有赞链路（同关闭时的文案）

⚠️ **必须用 subprocess 直接传 body，不要走 shell 拼接** ——
中文模型名 "seedance2.5-特惠" 经 shell 转义后会被搞坏，
请求里就没有这个模型名了，会落到默认渠道，判据全部失真。
（踩过这个坑：第一次跑出来的文案是漫屋的，差点误判成路由已生效。）

用法：
    python3 _probe_seedance_route_live.py            # 开关关闭时的基线
    python3 _probe_seedance_route_live.py --enabled   # 先开开关再验
"""
import argparse
import json
import subprocess

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
API = "https://api.heibaidao.cn"

# 素材类型故意用白名单外的 bogus_type —— 只为触发预扣前的本地校验。
BOGUS_MEDIA = [{"type": "bogus_type", "url": "https://example.com/probe.png"}]

# 有赞链路的素材类型校验文案（relay/channel/task/youzanwan3/adaptor.go:441）。
YOUZAN_MARK = "bogus_type"
# 漫屋链路的参考图校验文案（relay/channel/task/manwu/adaptor.go:1674）。
MANWU_MARK = "reference image"


def sql(query: str) -> str:
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:400]}\n{query}")
    return out.stdout.strip()


def get_token() -> str:
    """取一个 unlimited_quota=true 的有效令牌。

    ⚠️ 必须看 unlimited_quota —— `ValidateUserToken` 里余额耗尽也返回
    "Invalid token"（model/token.go），只看 status=1 会拿到 401。
    """
    row = sql(
        "SELECT t.user_id || ' ' || t.key FROM tokens t "
        "JOIN users u ON u.id = t.user_id "
        "WHERE t.status = 1 AND t.unlimited_quota = true "
        "AND u.deleted_at IS NULL ORDER BY t.id LIMIT 1"
    ).strip()
    if not row:
        raise RuntimeError("找不到可用令牌")
    user_id, key = row.split(" ", 1)
    print(f"[token] user={user_id} key_len={len(key)}")
    return key


def set_switch(enabled: bool) -> None:
    # ⚠️ **不要用字符串拼接写 JSON** —— 中文会被 shell/SQL 两层转义吃掉，
    # 曾经把 "seedance2.5-特惠" 写成 "seedance2.5-u7279u60e0"，
    # 结果开关开了但模型名对不上，路由永远不命中（还看不出原因）。
    # 用 PostgreSQL 的 jsonb_build_object 构造，中文直接以 UTF-8 落库。
    sql(
        "UPDATE options SET value = jsonb_build_object("
        "'enabled', " + ("true" if enabled else "false") + ","
        "'source_model', 'seedance2.5-特惠',"
        "'target_model', 'seedance-2-5',"
        "'trigger_seconds', 30)::text "
        "WHERE key = 'Seedance25TeHuiRoute30s'"
    )
    print(f"[switch] enabled={enabled} 已写入（60 秒内热更新生效）")
    print("[switch] 实际落库值:", sql(
        "SELECT value FROM options WHERE key='Seedance25TeHuiRoute30s'"
    ).strip())


def probe(token: str, model: str, seconds: int, label: str) -> dict:
    payload = {
        "model": model,
        "prompt": "probe",
        "seconds": str(seconds),
        "metadata": {"media": BOGUS_MEDIA},
    }
    # ensure_ascii=True：把中文转成 \uXXXX 转义，绕开 shell/编码问题。
    body = json.dumps(payload, ensure_ascii=True)
    cmd = [
        "curl", "-s", "-w", "\nHTTP=%{http_code}",
        "-X", "POST", f"{API}/v1/video/generations",
        "-H", f"Authorization: Bearer {token}",
        "-H", "Content-Type: application/json",
        "-d", body,
    ]
    raw = subprocess.run(cmd, capture_output=True, text=True).stdout.strip()
    code = raw.rsplit("HTTP=", 1)[-1] if "HTTP=" in raw else "?"
    detail = raw.rsplit("\nHTTP=", 1)[0][:200]

    chain = "未知"
    if YOUZAN_MARK in detail:
        chain = "有赞（未路由）"
    elif MANWU_MARK in detail:
        chain = "漫屋（已路由）"

    print(f"[probe] {label}: HTTP {code} → 链路={chain}")
    print(f"        {detail}")
    return {"label": label, "code": code, "detail": detail, "chain": chain}


def task_count() -> int:
    return int(sql("SELECT count(*) FROM tasks").strip() or 0)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--enabled", action="store_true",
                        help="先把开关打开再验（验完记得关回去）")
    args = parser.parse_args()

    token = get_token()

    if args.enabled:
        set_switch(True)
        print("⚠️ 开关已打开，60 秒后生效")

    before = task_count()
    print(f"[baseline] 任务数 = {before}\n")

    probe(token, "seedance2.5-特惠", 30, "源模型 + 30 秒（应路由）")
    probe(token, "seedance2.5-特惠", 15, "源模型 + 15 秒（不应路由）")
    probe(token, "seedance-2-5", 30, "目标模型 + 30 秒（对照组）")

    after = task_count()
    print(f"\n[verify] 任务数 {before} → {after}（应相等：探针不建单不扣费）")
    if before != after:
        print("⚠️⚠️ 探针建单了！这是意外，必须立刻查扣费")


if __name__ == "__main__":
    main()
