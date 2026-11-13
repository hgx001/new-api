"""定位 22:29:33（本地）那条任务的完整信息。

服务器按 UTC 存 epoch，这里统一用 UTC+8 显示，并按时间窗过滤。
"""
import subprocess
import sys

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec -i postgres psql -U root -d new-api"


def sql(query: str) -> str:
    out = subprocess.run(SSH + [PG], input=query, capture_output=True, text=True)
    if out.returncode != 0:
        return f"[ERR] {out.stderr.strip()[:400]}"
    return out.stdout.strip()


QUERY = r"""
\pset border 2
\pset format aligned
SELECT id,
       to_char(to_timestamp(submit_time) AT TIME ZONE 'UTC' + interval '8 hour', 'MM-DD HH24:MI:SS') AS submit_cst,
       channel_id AS ch,
       status,
       action,
       token_id AS tok,
       quota,
       finish_time - submit_time AS dur_s,
       left(regexp_replace(coalesce(fail_reason, ''), '\s+', ' ', 'g'), 300) AS fail
FROM tasks
WHERE submit_time >= extract(epoch FROM now())::bigint - 5400
ORDER BY id;
"""

if __name__ == "__main__":
    print(sql(sys.argv[1] if len(sys.argv) > 1 else QUERY))
