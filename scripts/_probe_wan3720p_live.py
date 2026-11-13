#!/usr/bin/env python3
"""wan3.0-720p-官网 上线后零成本探针。

原理：用**非法素材类型**打一发 —— wan3 的 ValidateRequestAndSetAction 在**预扣费之前**
校验媒体类型，本地就返回 400，不建单、不扣费、不产生上游费用。

因此这个探针能验证「路由到了 wan3 渠道 + 模型名被识别 + 计费倍率表选对了」，
但**验证不了成片质量**（那需要真跑一条，会花钱）。

用法：
    python scripts/_probe_wan3720p_live.py <token> [base_url]
"""
import json
import sys
import urllib.request
import urllib.error

BASE = (sys.argv[2] if len(sys.argv) > 2 else "https://api.heibaidao.cn").rstrip("/")
TOKEN = sys.argv[1]

# 非法媒体类型：bogus_type 不在 wan3 白名单里 → 400。
# 这一步在预扣之前，本地就拒绝，不花钱。
#
# ⚠️ 不要用 first_frame 做探针：它在 wan3 里是**合法**类型
#（validateWan3Media 的白名单含 first_frame/last_frame），
# 会通过校验、真的建单并扣费。
BAD_MEDIA = [{"type": "bogus_type", "url": "https://example.com/a.png"}]


def probe(model_name: str, resolution: str) -> dict:
    body = {
        "model": model_name,
        "prompt": "一只橘猫在窗边打盹",
        "seconds": "2",
        "resolution": resolution,
        "metadata": {"media": BAD_MEDIA},
    }
    req = urllib.request.Request(
        f"{BASE}/v1/videos",
        data=json.dumps(body).encode(),
        headers={
            "Authorization": f"Bearer {TOKEN}",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return {"model": model_name, "http": resp.status,
                    "body": resp.read(400).decode(errors="replace")}
    except urllib.error.HTTPError as e:
        return {"model": model_name, "http": e.code,
                "body": e.read(400).decode(errors="replace")}
    except Exception as e:  # noqa: BLE001
        return {"model": model_name, "http": -1, "body": repr(e)}


CASES = [
    # 新模型：路由到 wan3 → 媒体类型非法 → 400 invalid_input_reference
    ("wan3.0-720p-官网", "720P"),
    # 同一渠道的既有模型：同样 400（证明渠道本身通、不是只有新模型能到）
    ("wan3.0-video-官网", "720P"),
    # 不存在的模型名 → 503（模型未上架/不可路由）
    ("wan3.0-720p-官网-不存在", "720P"),
]

for model_name, resolution in CASES:
    r = probe(model_name, resolution)
    print(f"=== {model_name} (resolution={resolution}) ===")
    print(f"  HTTP {r['http']}")
    body = r["body"].strip().replace("\n", " ")
    print(f"  {body[:300]}")
    print()
