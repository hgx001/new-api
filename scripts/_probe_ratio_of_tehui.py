"""用成片实际宽高比反推：今天 seedance2.5-特惠 的 23 条任务，客户到底传没传比例参数。

原理：ratio 只在 metadata 里透传，不落库；上游回执的 ratio 字段传/不传都是 adaptive，
无法区分。但**成片的实际宽高比**是物证：
  - 客户传了 16:9 → 成片就是 16:9（1280x720）
  - 客户没传（adaptive）→ 上游按素材自适应，比例可能不是 16:9

只下载已完成的成片，不产生任何新费用。

用法：python scripts/_probe_ratio_of_tehui.py [task_id ...]
"""
import json
import os
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"
HOST = "https://api.heibaidao.cn"
FFPROBE = shutil.which("ffprobe") if (shutil := __import__("shutil")) else "ffprobe"


def sql(query):
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:300]}\n{query}")
    return out.stdout.strip()


def admin_token():
    row = sql("SELECT t.user_id || ' ' || t.key FROM tokens t "
              "JOIN users u ON u.id=t.user_id "
              "WHERE t.user_id=28 AND t.status=1 AND t.unlimited_quota=true "
              "AND u.deleted_at IS NULL ORDER BY t.id LIMIT 1").strip()
    uid, _, token = row.partition(" ")
    return int(uid), token


def probe_bytes(data, path):
    with open(path, "wb") as fh:
        fh.write(data)
    r = subprocess.run(
        [FFPROBE, "-v", "error", "-select_streams", "v:0",
         "-show_entries", "stream=width,height:format=duration",
         "-of", "json", path],
        capture_output=True, text=True,
    )
    info = json.loads(r.stdout or "{}")
    st = (info.get("streams") or [{}])[0]
    dur = float((info.get("format") or {}).get("duration") or 0)
    return st.get("width"), st.get("height"), dur


def main():
    ids = sys.argv[1:]
    uid, token = admin_token()
    tmp = tempfile.mkdtemp(prefix="ratio_probe_")

    if ids:
        where = " OR ".join(f"id={i}" for i in ids)
    else:
        where = ("properties::text LIKE '%seedance2.5-特惠%' AND user_id=36 "
                 "AND status='SUCCESS' AND submit_time >= EXTRACT(EPOCH FROM "
                 "TIMESTAMP '2026-10-30 00:00:00' AT TIME ZONE 'Asia/Shanghai')")

    rows = sql(f"SELECT id || E'\\t' || coalesce(private_data::jsonb->>'result_url','') "
               f"FROM tasks WHERE {where} ORDER BY id").strip().splitlines()
    if not rows:
        print("no rows")
        return

    for row in rows:
        tid, _, url = row.partition("\t")
        if not url:
            print(f"[{tid}] no result_url")
            continue
        try:
            req = urllib.request.Request(url, headers={"User-Agent": "probe"})
            with urllib.request.urlopen(req, timeout=120) as resp:
                data = resp.read()
            path = os.path.join(tmp, f"{tid}.mp4")
            w, h, dur = probe_bytes(data, path)
            ratio = (w / h) if w and h else 0
            # 常见比例对照
            named = None
            for label, target in (("16:9", 16/9), ("9:16", 9/16), ("1:1", 1.0),
                                  ("4:3", 4/3), ("3:4", 3/4), ("21:9", 21/9)):
                if abs(ratio - target) < 0.02:
                    named = label
                    break
            print(f"[{tid}] {w}x{h} ratio={ratio:.3f} -> {named or '其他'} dur={dur:.2f}s")
        except Exception as exc:  # noqa: BLE001
            print(f"[{tid}] FAIL {exc}")


if __name__ == "__main__":
    main()
