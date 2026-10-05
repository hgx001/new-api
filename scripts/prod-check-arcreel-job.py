import sqlite3

db = sqlite3.connect("/home/ubuntu/arcreel/projects/.arcreel.db")
tabs = [r[0] for r in db.execute("select name from sqlite_master where type='table'").fetchall()]
print("job/wallet tables:", [t for t in tabs if "remote" in t or "wallet" in t])
for t in tabs:
    if "remote_job" in t:
        cols = [c[1] for c in db.execute(f"PRAGMA table_info({t})").fetchall()]
        print(t, "cols:", cols)
        row = db.execute(f"select * from {t} where job_id=?", ("gen-03577f991a74e9669017e695",)).fetchone()
        if row:
            data = dict(zip(cols, row))
            keep = {k: data[k] for k in ("job_id", "status", "platform_id", "output_mode", "assignment_id", "updated_at", "lease_expires_at") if k in data}
            print("job:", keep)
if "wallet_reservations" in tabs:
    cols = [c[1] for c in db.execute("PRAGMA table_info(wallet_reservations)").fetchall()]
    print("reservation cols:", cols)
    for row in db.execute("select * from wallet_reservations where task_id=?", ("gen-03577f991a74e9669017e695",)).fetchall():
        print("reservation:", dict(zip(cols, row)))
