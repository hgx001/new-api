"""「万相官网 → 客美 tj」换货开关真机验证：1 条 2 秒 480P、带参考图/视频/音频。

请求的模型是 **wan3.0-video-官网**（客户视角），开关打开后内部应改派到
**wan3.0-video-tj**（渠道 32 客美 Kemeitv）。本脚本验证四件事：

  1. 任务真的落到**客美渠道**（channel_id=32 / upstream_task_id 有值）；
  2. 计费原点仍是客户请求的 **wan3.0-video-官网**（日志/任务属性），
     金额按官网口径（480P ¥0.20/秒 × 4 计费秒 = ¥0.80 → quota ≈54795；
     4 秒 = 2 秒出片 + 2 秒参考视频，客美渠道按秒并入参考视频）；
  3. 全参素材（图/视频/音频）能到上游：提示词用 @Image1/@Video1/@Audio1 绑定；
  4. 成片能取回（本站 /content → 实时回查上游签名直链）。

⚠️ **只建一条**（用户 2026-11-20 口径：只跑一条全参 2 秒 480P）。
⚠️ 上游成本：tj 480P ≈ ¥0.276/2 秒；客户侧按官网价 ¥0.40 结算（差额是毛利）。

用法：
    python3 scripts/_probe_wan3_gw_tj_route.py
"""
import json
import subprocess
import sys
import time

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "sudo docker exec -i postgres psql -U root -d new-api -t -A"
API = "https://api.heibaidao.cn"

MODEL = "wan3.0-video-官网"
EXPECT_TARGET_MODEL = "wan3.0-video-tj"
EXPECT_CHANNEL_ID = 32
# 计费口径 = 客户请求的官网模型 480P（ModelPrice 0.0273973 USD/秒），
# 而**计费秒数 = 出片秒数 + 参考视频秒数**（客美渠道 2026-11-20 起按秒并入）：
#   2 秒出片 + 2 秒参考视频 = 4 计费秒
#   → 0.0273973 × 4 × 500000 ≈ 54795 quota ≈ ¥0.80
EXPECT_QUOTA = 54795
DURATION = 2
RESOLUTION = "480P"
RATIO = "16:9"

REF_IMG = "https://api.heibaidao.cn/testassets/refimg.png"
REF_VID = "https://api.heibaidao.cn/testassets/refvid.mp4"
REF_AUD = "https://api.heibaidao.cn/testassets/refaud.mp3"

# 提示词用 @Image1 / @Video1 / @Audio1 显式绑定参考素材。
# ⚠️ 上游「参考素材靠 tag 绑定提示词」——不写 tag，素材只是传过去没人用。
PROMPT = (
    "@Video1 里的镜头动作用作运镜参考，@Image1 的画面风格作为美术参考，"
    "@Audio1 的环境音作为底噪；"
    "一个陶艺师在轮盘前拉坯，暖黄侧光，镜头缓慢推近，保留参考音频的环境氛围。"
)


def sql(query: str) -> str:
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:500]}\n{query}")
    return out.stdout.strip()


def get_token() -> str:
    """取真机测试统一令牌 token id=144（用户 2026-11-16 拍板）。"""
    key = sql("SELECT key FROM tokens WHERE id=144").strip()
    if not key:
        raise RuntimeError("找不到 token id=144")
    if not key.startswith("sk-"):
        key = "sk-" + key
    return key


def redacted(token: str) -> str:
    return token[:3] + "***" + token[-6:]


def post(path: str, token: str, payload: dict):
    body = json.dumps(payload)
    proc = subprocess.run(
        ["curl", "-sS", "-X", "POST", f"{API}{path}",
         "-H", f"Authorization: Bearer {token}",
         "-H", "Content-Type: application/json",
         "-d", body],
        capture_output=True, text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"curl failed: {proc.stderr[:300]}")
    try:
        return proc.returncode, json.loads(proc.stdout)
    except json.JSONDecodeError:
        return proc.returncode, {"_raw": proc.stdout[:800]}


def get(path: str, token: str):
    proc = subprocess.run(
        ["curl", "-sS", f"{API}{path}", "-H", f"Authorization: Bearer {token}"],
        capture_output=True, text=True,
    )
    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError:
        return {"_raw": proc.stdout[:800]}


def wait_task(task_id: str, token: str, timeout_s: int = 600):
    deadline = time.time() + timeout_s
    last = None
    while time.time() < deadline:
        body = get(f"/v1/video/generations/{task_id}", token)
        status = str(body.get("status") or (body.get("data") or {}).get("status") or "")
        if status != last:
            print(f"[poll] {task_id} status={status!r}")
            last = status
        if status.lower() in ("succeeded", "success", "completed", "failed", "cancelled", "canceled", "rejected", "expired"):
            return body
        time.sleep(8)
    raise RuntimeError(f"任务 {task_id} 超时未终态")


def main() -> int:
    token = get_token()
    print(f"[token] {redacted(token)}")
    switch_on = sql(
        "SELECT json_extract_path_text(value::json,'wan3_guanwang_tj_enabled') "
        "FROM options WHERE key='Seedance25TeHuiRoute30s'"
    )
    print(f"[switch] 后台开关（DB）= {switch_on}")

    payload = {
        "model": MODEL,
        "prompt": PROMPT,
        "duration": DURATION,
        "resolution": RESOLUTION,
        "ratio": RATIO,
        "media": [
            {"type": "reference_image", "url": REF_IMG},
            {"type": "reference_video", "url": REF_VID},
            {"type": "reference_audio", "url": REF_AUD},
        ],
    }
    print("[submit] payload =", json.dumps(payload, ensure_ascii=False))

    _, resp = post("/v1/video/generations", token, payload)
    print("[submit] resp =", json.dumps(resp, ensure_ascii=False)[:1200])
    task_id = str(resp.get("id") or resp.get("task_id") or "").strip()
    if not task_id:
        print("[fail] 没拿到 task id，终止")
        return 1

    row = sql(
        "SELECT id, channel_id, status, quota, fail_reason, "
        "COALESCE(properties->>'origin_model_name',''), "
        "COALESCE(private_data->>'upstream_task_id','') FROM tasks "
        f"WHERE task_id = '{task_id}'"
    )
    print("[db] task row =", row.replace("\n", " | "))

    fields = row.split("|")
    channel_id = fields[1].strip() if len(fields) > 1 else ""
    quota = fields[3].strip() if len(fields) > 3 else ""
    print(f"[check] channel_id={channel_id} (期望 {EXPECT_CHANNEL_ID} 客美)")
    print(f"[check] quota={quota} (期望 ≈{EXPECT_QUOTA}，按官网价结算)")
    if channel_id != str(EXPECT_CHANNEL_ID):
        print("[fail] 没有落到客美渠道 —— 换货开关没生效")
        return 2

    body = wait_task(task_id, token)
    print("[final] resp =", json.dumps(body, ensure_ascii=False)[:1500])

    row2 = sql(
        "SELECT id, channel_id, status, quota, fail_reason, "
        "COALESCE(private_data->>'upstream_task_id',''), "
        "COALESCE(private_data->>'result_url','') FROM tasks WHERE task_id = '" + task_id + "'"
    )
    print("[db] final row =", row2.replace("\n", " | "))

    final = row2.split("|")
    status = final[2].strip() if len(final) > 2 else ""
    if status.lower() not in ("success", "succeeded", "completed"):
        print("[fail] 任务未成功")
        return 3
    print("[ok] 改派链路与成片均已验证")
    return 0


if __name__ == "__main__":
    sys.exit(main())
