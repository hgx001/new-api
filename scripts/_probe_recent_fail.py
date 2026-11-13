"""捞最近的失败任务，看上游超时到底发生在哪一步。

SQL 从 stdin 喂给 psql，避免 JSON/引号被远端 shell 拆解。
"""
import subprocess
import sys

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec -i postgres psql -U root -d new-api"


def sql(query: str) -> str:
    out = subprocess.run(
        SSH + [PG],
        input=query,
        capture_output=True,
        text=True,
    )
    if out.returncode != 0:
        return f"[ERR] {out.stderr.strip()[:400]}"
    return out.stdout.strip()


QUERY = r"""
\pset border 2
\pset format aligned
SELECT id,
       to_char(to_timestamp(submit_time), 'MM-DD HH24:MI:SS') AS submit,
       channel_id AS ch,
       status,
       action,
       left(regexp_replace(coalesce(fail_reason, ''), '\s+', ' ', 'g'), 200) AS fail
FROM tasks
WHERE submit_time > extract(epoch FROM now())::bigint - 259200
  AND status NOT IN ('SUCCESS')
ORDER BY id DESC
LIMIT 40;
"""

if __name__ == "__main__":
    print(sql(sys.argv[1] if len(sys.argv) > 1 else QUERY))
