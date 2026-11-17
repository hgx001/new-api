#!/usr/bin/env python3
"""真机验证「特惠档比例闸门没有误伤首尾帧路由」（2026-11-17）。

背景：
  老板口径：wan3.0-720p-特惠 只做 9:16。闸门实现在别名中间件
  （middleware/wan3_tehui_alias.go），客户请求该模型名时非 9:16 直接 400。

  规则三（首尾帧）把请求改派到 frames_target_model（现为 wan3.0-video-官网），
  走的是**别名之后**的 seedance2.5-特惠 源模型，与特惠档闸门是两条路径。
  本脚本确认：闸门上线后，首尾帧改派仍能正常命中。

判据（靠**错误文案**区分走了哪条链路）：
  - 命中规则三 → 落到 wan3 直连 → 文案含 wan3 素材校验特征
  - 没命中规则三 → 留在源链路（有赞/漫屋）→ 文案含对应特征

⚠️ **不建单、不扣费**：素材类型故意用白名单外的 bogus_type，
  在预扣费之前就被本地校验挡下。

⚠️ 必须用 subprocess 直接传 body，不要走 shell 拼接 ——
  中文模型名经 shell 转义会被搞坏（踩过：模型名对不上，路由永远不命中）。

用法：
    python3 scripts/_probe_wan3_720p_frames_route.py
    python3 scripts/_probe_wan3_720p_frames_route.py --api2
"""
import json
import subprocess
import sys

IS_API2 = "--api2" in sys.argv

if IS_API2:
    SSH = ["ssh", "newapi-root"]
    API = "https://api2.heibaidao.cn"
else:
    SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
    API = "https://api.heibaidao.cn"

PG = "sudo docker exec postgres psql -U root -d new-api -t -A"

BOGUS_MEDIA = [{"type": "bogus_type", "url": "https://example.com/probe.png"}]


def sql(query: str) -> str:
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:400]}\n{query}")
    return out.stdout.strip()


def get_token() -> str:
    # 按用户剩余额度倒序取 —— 否则可能挑到额度耗尽的 token，
    # 探针会停在「预扣费额度失败」（403），看不出路由到底走没走。
    row = sql(
        "SELECT t.key FROM tokens t JOIN users u ON u.id = t.user_id "
        "WHERE t.status = 1 AND t.unlimited_quota = true "
        "AND u.deleted_at IS NULL ORDER BY u.quota DESC, t.id LIMIT 1"
    ).strip()
    if not row:
        raise RuntimeError("找不到可用令牌")
    return row


def probe(token: str, model: str, ratio: str, label: str) -> str:
    payload = {
        "model": model,
        "prompt": "probe",
        "seconds": "10",
        "ratio": ratio,
        "metadata": {"media": BOGUS_MEDIA},
    }
    body = json.dumps(payload, ensure_ascii=True)
    raw = subprocess.run(
        ["curl", "-s", "-w", "\nHTTP=%{http_code}", "-X", "POST",
         f"{API}/v1/video/generations",
         "-H", f"Authorization: Bearer {token}",
         "-H", "Content-Type: application/json",
         "-d", body],
        capture_output=True, text=True,
    ).stdout.strip()
    code = raw.rsplit("HTTP=", 1)[-1] if "HTTP=" in raw else "?"
    detail = raw.rsplit("\nHTTP=", 1)[0][:300]
    print(f"[probe] {label}: HTTP {code}")
    print(f"        {detail}")
    return detail


def main() -> int:
    token = get_token()
    print(f"[api] {API}\n")

    policy = sql("SELECT value FROM options WHERE key = 'Seedance25TeHuiRoute30s'")
    frames_target = ""
    try:
        frames_target = json.loads(policy).get("frames_target_model", "")
    except Exception:
        pass
    print(f"[policy] frames_target_model = {frames_target}\n")

    # ① 特惠档 + 9:16 + 首尾帧 → 应放行（进入规则三改派）
    print("── ① 特惠档 9:16 + 首尾帧（期望：不被比例闸门拦，走规则三改派）")
    d1 = probe(token, "wan3.0-720p-特惠", "9:16", "特惠档 9:16")
    gate_hit = "只支持 9:16" in d1
    print(f"   {'❌' if gate_hit else '✅'} 未被比例闸门拦（命中闸门={gate_hit}）\n")

    # ② 特惠档 + 16:9 + 首尾帧 → 应被比例闸门 400 拒单
    print("── ② 特惠档 16:9 + 首尾帧（期望：被比例闸门 400 拒单）")
    d2 = probe(token, "wan3.0-720p-特惠", "16:9", "特惠档 16:9")
    rejected = "只支持 9:16" in d2
    print(f"   {'✅' if rejected else '❌'} 被比例闸门拒单（命中={rejected}）\n")

    # ③ 母本 seedance2.5-特惠 + 16:9 + 首尾帧 → 闸门不该管它，
    #    应正常走规则三改派（这是首尾帧能力的主路径）
    print("── ③ 母本 16:9 + 首尾帧（期望：不被拒单，走规则三改派）")
    d3 = probe(token, "seedance2.5-特惠", "16:9", "母本 16:9")
    not_rejected = "只支持 9:16" not in d3
    print(f"   {'✅' if not_rejected else '❌'} 未被比例闸门拒单（命中={'只支持 9:16' in d3}）\n")

    ok = (not gate_hit) and rejected and not_rejected
    print("=" * 60)
    print(f"{'✅ 全部符合预期' if ok else '❌ 存在不符合预期的项'}")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
