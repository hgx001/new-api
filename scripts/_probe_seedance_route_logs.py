"""查最近的错误日志，看条件路由探针实际命中了哪个渠道（2026-10-30）。"""
import subprocess

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"


def sql(query: str) -> str:
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:400]}\n{query}")
    return out.stdout.strip()


def main():
    print("=== 1) 最近 10 条错误日志（看 channel_id / model_name / 错误文案）===")
    print(sql(
        "SELECT id || ' | ch=' || channel_id "
        "|| ' | model=' || model_name || ' | ' || left(content, 120) "
        "FROM logs WHERE type = 5 ORDER BY id DESC LIMIT 10"
    ))

    print()
    print("=== 2) 最近 10 条所有类型日志（确认探针有没有留下记录）===")
    print(sql(
        "SELECT id || ' | type=' || type || ' | ch=' || channel_id "
        "|| ' | model=' || model_name || ' | q=' || quota "
        "FROM logs ORDER BY id DESC LIMIT 10"
    ))

    print()
    print("=== 3) 任务表最近 5 条（探针不应建单）===")
    print(sql(
        "SELECT id || ' | ch=' || channel_id || ' | ' || status || ' | q=' || quota "
        "|| ' | ' || to_char(to_timestamp(submit_time), 'MM-DD HH24:MI:SS') "
        "FROM tasks ORDER BY id DESC LIMIT 5"
    ))


if __name__ == "__main__":
    main()
