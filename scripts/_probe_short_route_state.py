"""查生产现状：规则六（≤10 秒 → wan3.0-video）能不能生效。

四件事：
  1. 路由 option 里有没有 short_target_model（没有 = 规则六不会触发）
  2. wan3.0-video 在有赞渠道上有没有可用 abilities（没有 = 预检不通过，落回源链路）
  3. 线上二进制里有没有「命中短时长」这句日志（没有 = 新代码还没部署）
  4. 磁盘余量（部署前必须看，40G 盘踩过 no space left）
"""
import subprocess

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"


def run(remote_cmd: str) -> str:
    out = subprocess.run(SSH + [remote_cmd], capture_output=True, text=True)
    if out.returncode != 0:
        return f"[ERR] {out.stderr.strip()[:300]}"
    return out.stdout.strip()


def sql(query: str) -> str:
    return run(f'{PG} -c "{query}"')


def main():
    print("=" * 72)
    print("1) 路由 option（含 short_target_model 吗）")
    print("=" * 72)
    print(run(f"""{PG} -c "SELECT value FROM options WHERE key='Seedance25TeHuiRoute30s'" """))

    print()
    print("=" * 72)
    print("2) wan3.0-video / seedance2.5-特惠 的 abilities")
    print("=" * 72)
    print(run(
        f"""{PG} -c "SELECT model || ' | grp=' || abilities.group || ' | ch=' || channel_id """
        f"""|| ' | enabled=' || enabled FROM abilities """
        f"""WHERE model IN ('wan3.0-video','seedance2.5-特惠','wan3.0-video-prime') ORDER BY model" """
    ))

    print()
    print("=" * 72)
    print("3) 线上二进制是否含新日志（0 = 还没部署）")
    print("=" * 72)
    print(run(
        "docker exec new-api sh -c \"grep -c '命中短时长' /new-api 2>/dev/null || echo 0\""
    ))
    print("容器:")
    print(run("docker ps --format '{{.Names}}\\t{{.Image}}\\t{{.Status}}'"))

    print()
    print("=" * 72)
    print("4) 磁盘")
    print("=" * 72)
    print(run("df -h / | tail -1"))


if __name__ == "__main__":
    main()
