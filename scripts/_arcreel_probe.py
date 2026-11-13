import subprocess

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
DB = "/home/ubuntu/arcreel/projects/.arcreel.db"


def run(sql):
    cmd = SSH + [f'sqlite3 -header -column "{DB}" "{sql}"']
    out = subprocess.run(cmd, capture_output=True, text=True)
    if out.returncode != 0:
        return f"ERR: {out.stderr.strip()[:300]}"
    return out.stdout.strip()


print("=== remote_worker_accounts 列 ===")
print(run("SELECT sql FROM sqlite_master WHERE name='remote_worker_accounts'"))
print("\n=== 在线 Worker 的账号 ===")
print(run("SELECT * FROM remote_worker_accounts ORDER BY worker_id, platform_id LIMIT 40"))
print("\n=== 最近 8 条远端任务 ===")
print(run(
    "SELECT job_id, platform_id, execution_mode, generation_policy, status, "
    "COALESCE(account_id,'') AS acct, datetime(created_at,'localtime') AS created "
    "FROM remote_generation_jobs ORDER BY created_at DESC LIMIT 8"))
print("\n=== 最近 3 条任务的 video_params / inputs ===")
print(run(
    "SELECT job_id, COALESCE(video_params_json,'') AS vp FROM remote_generation_jobs "
    "ORDER BY created_at DESC LIMIT 3"))
