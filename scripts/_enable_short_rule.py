"""给生产 option 加规则六的配置（short_target_model）。

口径（2026-11-13 老板定）：
  - 只对母本 seedance2.5-特惠 生效 → 加 short_target_model = wan3.0-video
  - 活动版（按次 ¥4.5/条）**显式关掉**：它按次计价，分流到标准版只会
    把毛利从 ¥3.00 压到 ¥2.70，与「短任务减压」的诉求无关，先不动。
  - 阈值用默认 10 秒（不写 short_trigger_seconds，让服务端归一成 10）

⚠️ 直接改 DB 后必须重启容器才生效（option 在内存里，没有热加载）。
   本脚本只负责写库，重启由 replace-container.sh 完成。
"""
import json
import subprocess
import time

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
KEY = "Seedance25TeHuiRoute30s"

# 原值（2026-11-12 22:14 读到的生产配置），在此基础上加字段，其余逐字保留。
POLICY = {
    "enabled": True,
    "source_model": "seedance2.5-特惠",
    "source_models": ["seedance2.5-特惠", "seedance2.5-活动"],
    "target_model": "seedance-2-5",
    "trigger_seconds": 20,
    "seconds_rule_ratio_gate": "9:16",
    "ratio_target_model": "wan3.0-720p-特惠-内链",
    "trigger_ratio": "9:16",
    "frames_target_model": "wan3.0-720p-特惠",
    # ↓ 新增：规则六
    "short_target_model": "wan3.0-video",
    "failover_source_model": "seedance-2-5",
    "failover_target_model": "wan3.0-班车",
    "doubao_link_off_until": 1794506400,
    "failover_enabled": True,
    "source_overrides": {
        "seedance2.5-活动": {
            "target_model": "seedance-2-5",
            "ratio_rule_enabled": False,
            "failover_after_doubao_enabled": False,
            # ↓ 新增：活动版不参与短任务分流
            "short_rule_enabled": False,
        },
        "seedance2.5-特惠": {"ratio_rule_enabled": False},
    },
    "updated_at": int(time.time()),
}


def psql(query: str) -> str:
    """把 SQL 从 **stdin** 喂给 psql。

    为什么不用 `psql -c "..."`：那条路径要经过远端 bash 解析，
    JSON 里的 `$` 会被当变量展开、双引号也会截断参数 —— 第一次尝试就是这么
    把 `$json$...$json$` 拆成命令的。stdin 不经过 shell，引号与 $ 都安全。
    """
    out = subprocess.run(
        SSH + ["docker exec -i postgres psql -U root -d new-api -t -A"],
        input=query, capture_output=True, text=True,
    )
    if out.returncode != 0:
        raise RuntimeError(out.stderr.strip()[:500])
    return out.stdout.strip()


def read_option() -> str:
    return psql(f"SELECT value FROM options WHERE key = '{KEY}';")


def main():
    payload = json.dumps(POLICY, ensure_ascii=False, separators=(",", ":"))

    print("=" * 72)
    print("写入前")
    print("=" * 72)
    print(read_option()[:200], "...")

    psql(f"UPDATE options SET value = '{payload}' WHERE key = '{KEY}';")

    print()
    print("=" * 72)
    print("写入后（校验三个新字段）")
    print("=" * 72)
    value = read_option()
    parsed = json.loads(value)
    print("short_target_model  =", parsed.get("short_target_model"))
    print("short_trigger_seconds =", parsed.get("short_trigger_seconds", "(未写，服务端归一为 10)"))
    print("活动版 short_rule_enabled =",
          parsed["source_overrides"]["seedance2.5-活动"].get("short_rule_enabled"))
    print("母本 ratio_rule_enabled =",
          parsed["source_overrides"]["seedance2.5-特惠"].get("ratio_rule_enabled"))
    assert parsed["short_target_model"] == "wan3.0-video"
    assert parsed["target_model"] == "seedance-2-5", "规则一的目标被改坏了"
    assert parsed["trigger_seconds"] == 20, "规则一的阈值被改坏了"
    print("\nOK: 其余字段逐字保留")


if __name__ == "__main__":
    main()
