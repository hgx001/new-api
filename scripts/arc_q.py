"""只读查询 ArcReel 远端 worker / 账号 / job 状态。生产验证脚本用，避免 shell 嵌套引号地狱。

用法: python3 arc_q.py "SELECT ..."        # 走 .arcreel.db
      python3 arc_q.py --log <job_id>      # 在 arcreel.log 里查该 job 的关键行
"""
import sqlite3
import sys

DB = "/home/ubuntu/arcreel/projects/.arcreel.db"
LOG = "/home/ubuntu/arcreel/logs/arcreel.log"


def q(sql):
    db = sqlite3.connect(DB)
    for row in db.execute(sql):
        print("|".join("" if v is None else str(v) for v in row))


if sys.argv[1] == "--log":
    needle = sys.argv[2]
    with open(LOG, encoding="utf-8", errors="replace") as fh:
        hits = [ln.rstrip() for ln in fh if needle in ln]
    for ln in hits[-40:]:
        print(ln[:400])
    print(f"-- {len(hits)} matching lines --")
else:
    q(sys.argv[1])
