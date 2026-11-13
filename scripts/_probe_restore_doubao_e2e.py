"""seedance-2-5 常驻回迁豆包后的**真机端到端**验证（2026-11-12）。

跑 1 条真机任务（seedance2.5-特惠 · 30 秒 · 9:16），验证：
  1. 任务落在**渠道 20（漫屋ArcReel/豆包）**而不是 27（视频生成车站）
  2. 规则一确实改派（tasks.properties 里的原始模型仍是促销族，
     上游模型名变成 seedance-2-5）
  3. 计费金额对不对（特惠版按秒 ¥0.20/秒 × 30 = ¥6.00）
  4. 成片能正常取回

⚠️ **开关/渠道变更后必须等 60 秒热更新**（SyncChannelCache 周期）。

用法：
    python3 _probe_restore_doubao_e2e.py
"""
import json
import subprocess
import time

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A -F' | '"
API = "https://api.heibaidao.cn"


def sql(query: str) -> str:
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:400]}\n{query}")
    return out.stdout.strip()


def get_token() -> str:
    row = sql(
        "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id "
        "WHERE t.user_id=28 AND t.status=1 AND t.unlimited_quota=true "
        "AND u.deleted_at IS NULL ORDER BY t.id LIMIT 1"
    ).strip()
    if not row:
        raise RuntimeError("找不到 admin 的可用令牌")
    return row


def submit(token: str, seconds: int, ratio: str) -> dict:
    payload = {
        "model": "seedance2.5-特惠",
        "prompt": "一只橘猫在窗台上打盹，午后阳光斜照",
        "seconds": str(seconds),
        "ratio": ratio,
    }
    body = json.dumps(payload, ensure_ascii=True)
    out = subprocess.run(
        ["curl", "-s", "-X", "POST", f"{API}/v1/video/generations",
         "-H", f"Authorization: Bearer {token}",
         "-H", "Content-Type: application/json",
         "-d", body],
        capture_output=True, text=True,
    )
    print(f"[submit] {out.stdout.strip()[:300]}")
    return json.loads(out.stdout)


def main() -> None:
    token = get_token()
    print(f"[token] len={len(token)}")

    resp = submit(token, 30, "9:16")
    # 视频提交接口回的是 dto.OpenAIVideo —— id / task_id 都在**顶层**，
    # 没有 data 包装（旧的 resp["data"]["id"] 取法永远拿到空）。
    task_id = (
        resp.get("id")
        or resp.get("task_id")
        or resp.get("data", {}).get("id")
        or resp.get("data", {}).get("task_id")
    )
    if not task_id:
        print("!! 没拿到 task_id，先看上面的响应体")
        return

    print(f"[task] {task_id} 等待落库…")
    time.sleep(5)

    row = sql(
        f"SELECT id, channel_id, status, quota, "
        f"coalesce(properties->>'origin_model_name',''), "
        f"coalesce(private_data->>'upstream_task_id',''), "
        f"coalesce(private_data->>'upstream_model_name',''), "
        f"left(coalesce(fail_reason,''),120) "
        f"FROM tasks WHERE task_id='{task_id}';"
    )
    print(f"[db] {row}")

    print("[wait] 轮询任务状态…")
    for _ in range(40):
        time.sleep(15)
        st = sql(f"SELECT status || ' | ' || coalesce(progress,'') || ' | ' || left(coalesce(fail_reason,''),120) FROM tasks WHERE task_id='{task_id}';")
        print(f"[poll] {st}")
        if st.split(" | ")[0] in ("SUCCESS", "FAILURE"):
            break

    final = sql(
        f"SELECT id, channel_id, status, quota, "
        f"coalesce(properties->>'origin_model_name',''), "
        f"coalesce(private_data->>'upstream_model_name',''), "
        f"left(coalesce(data::text,''),200) "
        f"FROM tasks WHERE task_id='{task_id}';"
    )
    print(f"[final] {final}")


if __name__ == "__main__":
    main()
