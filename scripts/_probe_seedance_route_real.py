"""条件路由的真机验证（2026-10-30）。

跑 1 条真机任务，验证：
  1. 任务落在**渠道 20（漫屋）**还是**渠道 17（有赞）**
  2. 计费金额对不对（漫屋按次 ¥3.00 / 有赞按秒 ¥0.20×秒数）
  3. 成片能正常取回

⚠️ **开关变更后必须等 60 秒热更新**（main.go 的 SyncOptions 周期）。
   写完 SQL 立刻提交任务，内存里还是旧值 —— 曾经因此误判「开关关了却还在路由」。

💰 **成本提醒**：有赞侧按秒计费（¥0.20/秒），验证用 **2 秒**（¥0.40）就够；
   漫屋侧按次计费 ¥3.00/次，且路由条件本身就是 30 秒 → 只能 30 秒。

用法：
    python3 _probe_seedance_route_real.py --seconds 2                # 有赞基线（开关关）
    python3 _probe_seedance_route_real.py --seconds 30 --enable      # 路由验证（开关开）
"""
import argparse
import json
import subprocess
import time

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
API = "https://api.heibaidao.cn"

# option 热更新周期（main.go: go model.SyncOptions(common.SyncFrequency)）
SYNC_WAIT = 75


def sql(query: str) -> str:
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:400]}\n{query}")
    return out.stdout.strip()


def get_admin_token() -> str:
    """取 admin（user 28）的无限额度令牌。

    ⚠️ 必须 unlimited_quota=true —— `ValidateUserToken` 里余额耗尽
    也返回 "Invalid token"（model/token.go）。
    """
    row = sql(
        "SELECT t.key FROM tokens t JOIN users u ON u.id = t.user_id "
        "WHERE t.user_id = 28 AND t.status = 1 AND t.unlimited_quota = true "
        "AND u.deleted_at IS NULL ORDER BY t.id LIMIT 1"
    ).strip()
    if not row:
        raise RuntimeError("找不到 admin 的可用令牌")
    print(f"[token] user=28 key_len={len(row)}")
    return row


def set_switch(enabled: bool) -> None:
    # 用 jsonb_build_object 构造，避免中文被 shell 转义吃掉。
    sql(
        "UPDATE options SET value = jsonb_build_object("
        "'enabled', " + ("true" if enabled else "false") + ","
        "'source_model', 'seedance2.5-特惠',"
        "'target_model', 'seedance-2-5',"
        "'trigger_seconds', 30)::text "
        "WHERE key = 'Seedance25TeHuiRoute30s'"
    )
    print(f"[switch] enabled={enabled} 已写入，等 {SYNC_WAIT}s 热更新…")
    time.sleep(SYNC_WAIT)
    actual = sql("SELECT value FROM options WHERE key='Seedance25TeHuiRoute30s'").strip()
    print(f"[switch] 落库值: {actual}")


def submit(token: str, seconds: int) -> str:
    payload = {
        "model": "seedance2.5-特惠",
        "prompt": "一只橘猫在窗台上打盹，午后阳光斜照",
        "seconds": str(seconds),
    }
    body = json.dumps(payload, ensure_ascii=True)
    out = subprocess.run(
        ["curl", "-s", "-X", "POST", f"{API}/v1/video/generations",
         "-H", f"Authorization: Bearer {token}",
         "-H", "Content-Type: application/json",
         "-d", body],
        capture_output=True, text=True,
    ).stdout.strip()
    print(f"[submit] {out[:300]}")
    return out


def wait_task(task_id: str, timeout: int = 900) -> str:
    deadline = time.time() + timeout
    while time.time() < deadline:
        row = sql(
            "SELECT status || ' | ch=' || channel_id || ' | q=' || quota "
            "FROM tasks WHERE id = " + task_id
        ).strip()
        status = row.split("|")[0].strip() if row else "?"
        print(f"  [{time.strftime('%H:%M:%S')}] {row}")
        if status in ("SUCCESS", "FAILURE"):
            return row
        time.sleep(15)
    return "TIMEOUT"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--seconds", type=int, default=2,
                        help="任务时长（有赞按秒计费，验证用 2 秒就够）")
    parser.add_argument("--enable", action="store_true",
                        help="跑之前把路由开关打开（会等热更新）")
    parser.add_argument("--disable", action="store_true",
                        help="跑之前把路由开关关掉（会等热更新）")
    args = parser.parse_args()

    token = get_admin_token()

    if args.enable:
        set_switch(True)
    elif args.disable:
        set_switch(False)

    before = sql("SELECT count(*) FROM tasks").strip()
    submit(token, args.seconds)
    time.sleep(3)
    task_id = sql("SELECT id FROM tasks ORDER BY id DESC LIMIT 1").strip()
    print(f"\n[task] id={task_id}（任务数 {before} → {task_id}）\n")

    row = wait_task(task_id)

    print("\n=== 结果 ===")
    print(f"  状态/渠道/额度: {row}")
    quota = int(row.split("q=")[1].split("|")[0].strip()) if "q=" in row else 0
    cny = quota / 500000 * 7.3
    print(f"  计费: {quota} quota = ¥{cny:.4f}")
    if "ch=20" in row:
        print("  ✅ 落在渠道 20（漫屋）→ 路由命中")
    elif "ch=17" in row:
        print("  ➡️ 落在渠道 17（有赞）→ 未路由")
    else:
        print("  ⚠️ 渠道异常，需排查")

    print("\n=== 任务属性 ===")
    print(sql("SELECT properties::text FROM tasks WHERE id = " + task_id)[:400])

    print("\n=== 成片检查 ===")
    url = sql(
        "SELECT coalesce(data::jsonb->>'url','') FROM tasks WHERE id = " + task_id
    ).strip()
    if not url:
        url = sql(
            "SELECT coalesce(private_data::jsonb->>'result_url','') FROM tasks WHERE id = " + task_id
        ).strip()
    print(f"  成片地址: {url[:160]}")
    if url:
        code = subprocess.run(
            ["curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "-L", url],
            capture_output=True, text=True).stdout.strip()
        print(f"  成片可达: HTTP {code}")


if __name__ == "__main__":
    main()
