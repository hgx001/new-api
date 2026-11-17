#!/usr/bin/env python3
"""老机磁盘清理（2026-11-17）—— 移入隔离区而不是直接删。

用法：
    python3 _cleanup_disk_20261117.py           # dry-run，只列清单
    python3 _cleanup_disk_20261117.py --apply   # 真正移入隔离区

设计约束：
  * 白名单制：只处理下面显式列出的对象，绝不用通配符扫目录。
  * 隔离区 /tmp/trash-20261117 与源在同一分区，mv 是原子操作、不额外占空间。
  * 生成 manifest.json 记录 原路径 -> 隔离路径，误删可一条命令还原。
  * 最新回滚容器（new-api-prev-20261117094927）及其镜像不在清理范围。
  * rng.bin / claude-1000/db.sqlite 用途不明，刻意跳过。
"""
import json
import os
import shutil
import sys
from datetime import datetime
from pathlib import Path

APPLY = "--apply" in sys.argv
TRASH = Path("/tmp/trash-20261117")

# ---- 第 2 组：旧 runway 包（已确认无 yml/json 引用、access log 下载 0）----
RUNWAY = [
    "/var/www/license.heibaidao.cn/updates/v1.0.6/runway-setup.exe",
    "/var/www/license.heibaidao.cn/updates/v1.0.8/runway-setup.exe",
    "/var/www/license.heibaidao.cn/updates/v1.0.9/runway-setup.exe",
]

# ---- 第 3 组：9 月及更早备份 ----
# 刻意排除 sub2api-binary-20261105-*（11 月）与 account4-backup-20260924（账号配置）
BACKUPS = [
    "/home/ubuntu/sub2api-backups/sub2api-20260906-112225",
    "/home/ubuntu/new-api/vpn-deploy/new-api-vpn-deploy.tar.gz",
    "/home/ubuntu/new-api/vpn-deploy/new-api-youzan-fix.tar.gz",
    "/home/ubuntu/new-api-backup-20261025-1325",
    "/home/ubuntu/backups/server-backup-20260212_171313",
    "/home/ubuntu/backups/server-backup-20260212_171313.tar.gz",
]

# ---- 第 1 组的 /tmp 部分：只列明确可判定的测试产物 ----
TMPFILES = [
    "/tmp/arcreel-pre-migrate-20261115.db",
    "/tmp/sr720.mp4",
    "/tmp/wan3_final.mp4",
    "/tmp/sr_manual_AiJto.mp4",
    "/tmp/h3image-tiers-20261113/4k.png",
    "/tmp/h3image-4k2160-20261113/ascii_3840x2160.png",
]

GROUPS = {"runway": RUNWAY, "backups": BACKUPS, "tmpfiles": TMPFILES}


def tree_size(path: str) -> int:
    p = Path(path)
    if p.is_file():
        return p.stat().st_size
    if p.is_dir():
        total = 0
        for root, _dirs, files in os.walk(path):
            for f in files:
                try:
                    total += (Path(root) / f).stat().st_size
                except OSError:
                    pass
        return total
    return -1


def human(n: int) -> str:
    if n < 0:
        return "<缺失>"
    for unit in ("B", "K", "M", "G"):
        if n < 1024:
            return f"{n:.0f}{unit}"
        n /= 1024
    return f"{n:.1f}T"


def main() -> None:
    manifest = []
    total = 0
    print("模式:", "APPLY 移入隔离区" if APPLY else "DRY-RUN 只列清单")
    for group, paths in GROUPS.items():
        print(f"\n=== [{group}] ===")
        for src in paths:
            size = tree_size(src)
            exists = os.path.lexists(src)
            print(f"  {human(size):>8}  {'存在' if exists else '缺失'}  {src}")
            if exists:
                total += size
                manifest.append({
                    "group": group,
                    "source": src,
                    "size": size,
                    "dest": str(TRASH / group / Path(src).name),
                })

    # 30 天前日志：动态匹配
    log_dir = Path("/home/ubuntu/new-api/logs")
    old_logs = []
    if log_dir.is_dir():
        cutoff = datetime.now().timestamp() - 30 * 86400
        for f in log_dir.rglob("*"):
            try:
                if f.is_file() and f.stat().st_mtime < cutoff and f.name.endswith((".log", ".log.gz")):
                    old_logs.append(f)
            except OSError:
                pass
    log_bytes = sum(f.stat().st_size for f in old_logs)
    print(f"\n=== [old-logs] ===\n  {len(old_logs)} files  {human(log_bytes)}  {log_dir}")
    total += log_bytes
    for f in old_logs:
        manifest.append({
            "group": "old-logs",
            "source": str(f),
            "size": f.stat().st_size,
            "dest": str(TRASH / "old-logs" / f.name),
        })

    print(f"\n合计 {len(manifest)} 项 / {human(total)}")

    if not APPLY:
        print("\n[DRY-RUN] 未做任何改动。加 --apply 执行。")
        return

    # 创建隔离区
    for group in list(GROUPS) + ["old-logs"]:
        (TRASH / group).mkdir(parents=True, exist_ok=True)

    # 逐项移动，重名加序号
    moved, failed = 0, []
    seen = {}
    for item in manifest:
        src, dest = Path(item["source"]), Path(item["dest"])
        if not os.path.lexists(src):
            continue
        if dest.exists():
            seen[dest] = seen.get(dest, 0) + 1
            dest = dest.with_name(f"{dest.name}.{seen[dest]}")
        try:
            shutil.move(str(src), str(dest))
            item["dest"] = str(dest)
            moved += 1
        except Exception as exc:  # noqa: BLE001 - 清理脚本需要记录任何失败
            failed.append({"source": str(src), "error": str(exc)})

    # 同名日志可能冲突，上面已加序号；写清单
    with open(TRASH / "manifest.json", "w", encoding="utf-8") as fh:
        json.dump({
            "created_at": datetime.now().isoformat(timespec="seconds"),
            "host": "119.29.253.97",
            "moved": moved,
            "failed": failed,
            "items": manifest,
            "restore_cmd": "python3 /tmp/trash-20261117/restore.py",
        }, fh, ensure_ascii=False, indent=2)

    # 生成还原脚本
    with open(TRASH / "restore.py", "w", encoding="utf-8") as fh:
        fh.write(
            "#!/usr/bin/env python3\n"
            "import json, shutil, os\n"
            "from pathlib import Path\n"
            "m = json.load(open('/tmp/trash-20261117/manifest.json'))\n"
            "for it in m['items']:\n"
            "    src, dst = Path(it['dest']), Path(it['source'])\n"
            "    if src.exists():\n"
            "        dst.parent.mkdir(parents=True, exist_ok=True)\n"
            "        shutil.move(str(src), str(dst))\n"
            "        print('restored', dst)\n"
        )

    print(f"\n完成：移入隔离区 {moved} 项，失败 {len(failed)} 项")
    for f in failed[:5]:
        print("  FAIL", f)
    print("隔离区:", TRASH)
    print("还原命令: python3 /tmp/trash-20261117/restore.py")


if __name__ == "__main__":
    main()
