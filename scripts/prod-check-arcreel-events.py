import json
import sqlite3

db = sqlite3.connect("/home/ubuntu/arcreel/projects/.arcreel.db")
job_id = "gen-03577f991a74e9669017e695"
cols = [c[1] for c in db.execute("PRAGMA table_info(remote_generation_events)").fetchall()]
print("event cols:", cols)
rows = db.execute(
    f"select * from remote_generation_events where job_id=? order by id", (job_id,)
).fetchall()
for row in rows[-12:]:
    d = dict(zip(cols, row))
    payload = str(d.get("payload_json") or d.get("payload") or "")[:200]
    print(d.get("created_at"), d.get("event_type") or d.get("type"), "|", payload)

print("--- workers ---")
wcols = [c[1] for c in db.execute("PRAGMA table_info(remote_worker_nodes)").fetchall()]
for row in db.execute("select * from remote_worker_nodes").fetchall():
    d = dict(zip(wcols, row))
    print({k: d.get(k) for k in ("worker_id", "status", "platforms", "last_seen_at", "capabilities") if k in d})
