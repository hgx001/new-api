"""查生产库条件路由相关的现状（2026-10-30）。

一次性确认：
  1. 两个模型的 abilities 行是否就位
  2. 路由 option 是否已存在
  3. 两个模型各自绑定的渠道
"""
import subprocess

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"


def sql(query: str) -> str:
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:400]}\n{query}")
    return out.stdout.strip()


def main():
    print("=" * 70)
    print("1) abilities（源=seedance2.5-特惠 目标=seedance-2-5）")
    print("=" * 70)
    print(sql(
        "SELECT model || ' | grp=' || abilities.group || ' | ch=' || channel_id "
        "|| ' | enabled=' || enabled "
        "FROM abilities WHERE model IN ('seedance2.5-特惠','seedance-2-5') ORDER BY model"
    ))

    print()
    print("=" * 70)
    print("2) 路由 option 是否已存在（应为 0，还没写过）")
    print("=" * 70)
    print(sql("SELECT count(*) FROM options WHERE key='Seedance25TeHuiRoute30s'"))

    print()
    print("=" * 70)
    print("3) 两个模型在 channels.models 里的归属渠道")
    print("=" * 70)
    print(sql(
        "SELECT id || ' | ' || name || ' | type=' || type "
        "|| ' | has_src=' || (models LIKE '%seedance2.5-特惠%') "
        "|| ' | has_tgt=' || (models LIKE '%seedance-2-5%') "
        "FROM channels WHERE models LIKE '%seedance2.5-特惠%' OR models LIKE '%seedance-2-5%'"
    ))

    print()
    print("=" * 70)
    print("4) 最近 seedance2.5-特惠 的任务（确认在跑、有 30 秒档）")
    print("=" * 70)
    print(sql(
        "SELECT id || ' | ' || channel_id || ' | ' || status || ' | ' "
        "|| to_char(to_timestamp(submit_time), 'MM-DD HH24:MI') "
        "FROM tasks WHERE properties::text LIKE '%seedance2.5-特惠%' "
        "ORDER BY id DESC LIMIT 5"
    ))


if __name__ == "__main__":
    main()
