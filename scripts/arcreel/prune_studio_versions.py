#!/usr/bin/env python3
"""studio 历史版本的本地保留策略：每个资产只留最近 N 版，磁盘换秩序。

## 为什么需要它

`version_manager.py` 每生成一版就完整写一份 mp4（外加 HLS 分片），
**历史版本从不清理**。video-studio + canvas-studio 的 versions/ 已积到
~3.2G，是老机磁盘 90% 的主要构成之一。删除备份治标，这里治本。

## 保留规则

- 按 ``versions.json`` 索引逐资产处理（索引是唯一事实来源）。
- 每个资产保留：最近 ``--keep``（默认 3）版 + ``current_version``（无条件保留，
  这是用户当前正在用的那一版，`lib/version_manager.py` 的 ``is_current`` 语义）。
- 其余版本：删 mp4 源文件 + 对应 HLS 产物（``hls/<stem>.*``，m3u8 与 ts 分片
  必须一起删，留一半播放就废）+ 从 versions.json 移除记录。
- 只处理 ``--types``（默认 videos,storyboards）：characters/grids 等是小图，
  收益可忽略，不碰。
- 磁盘上存在但索引里没有的孤儿文件**只报告不删除**（它们可能被别处引用）。

## 安全性设计

1. **默认 dry-run**：不加 ``--apply`` 只打印计划与可回收字节数。
2. **索引原子改写**：先写临时文件再 ``os.replace``；改前的 versions.json
   备份到 ``<项目>/.trash/assets/<stamp>/``（沿用 asset_deletion.py 的约定，
   记录还在，理论上可手工回滚）。
3. **current_version 永不删**：哪怕它不在最近 N 版里。
4. **flock 非阻塞锁**：与每天的 prune_remote_outputs / backup-retention
   互不踩踏（/tmp/arcreel-prune-versions.lock）。
5. **直接删除而非搬回收站**：回收站没有自动清理机制，搬过去等于没省磁盘。
   索引备份 + dry-run 双保险足够；真要救个别文件，从 versions.json 备份
   里的记录可以知道删了什么。

## 用法

    # 看计划（两项目、默认留 3 版）
    sudo -u ubuntu .venv/bin/python scripts/prune_studio_versions.py

    # 真的删
    sudo -u ubuntu .venv/bin/python scripts/prune_studio_versions.py --apply

    # 只处理某个项目 / 改保留数
    sudo -u ubuntu .venv/bin/python scripts/prune_studio_versions.py --project video-studio --keep 5 --apply

## 定时任务

    # crontab -e（ubuntu 用户），每天 04:40 在备份之后跑：
    40 4 * * * cd /home/ubuntu/arcreel && .venv/bin/python scripts/prune_studio_versions.py --apply >> logs/prune_versions.log 2>&1
"""

from __future__ import annotations

import argparse
import fcntl
import json
import os
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

REPO = Path("/home/ubuntu/arcreel")
PROJECTS_ROOT = REPO / "projects"
LOCK_PATH = Path(tempfile.gettempdir()) / "arcreel-prune-versions.lock"
DEFAULT_TYPES = ("videos", "storyboards")
DEFAULT_KEEP = 3


def acquire_lock() -> object | None:
    fh = open(LOCK_PATH, "w")
    try:
        fcntl.flock(fh, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        fh.close()
        print("另一个 prune 实例正在运行，退出", file=sys.stderr)
        return None
    return fh


def hls_candidates(version_file: Path) -> list[Path]:
    """mp4 对应的 HLS 产物：hls/<stem>.m3u8 与 hls/<stem>*_*.ts。"""
    hls_dir = version_file.parent / "hls"
    if version_file.parent.name != "videos" or not hls_dir.is_dir():
        return []
    stem = version_file.stem
    return sorted(hls_dir.glob(f"{stem}*"))


def fmt_mb(n: int) -> str:
    return f"{n / (1 << 20):.1f}MB"


def plan_project(project: Path, types: tuple[str, ...], keep: int, apply: bool) -> tuple[int, int]:
    """返回（可回收字节, 删除文件数）。"""
    index_path = project / "versions" / "versions.json"
    if not index_path.is_file():
        print(f"  [skip] {project.name}: 无 versions.json")
        return 0, 0

    with index_path.open(encoding="utf-8") as fh:
        index = json.load(fh)

    reclaimed = 0
    removed_files = 0
    orphans = 0
    changed = False

    for asset_type in types:
        type_data = index.get(asset_type)
        if not isinstance(type_data, dict):
            continue
        for asset_id, resource in type_data.items():
            if not isinstance(resource, dict):
                continue
            versions = resource.get("versions") or []
            if not versions:
                continue
            current = resource.get("current_version")
            # 按 version 号从新到旧排序，最新 keep 版 + current 版保留
            ordered = sorted(
                versions,
                key=lambda v: v.get("version", 0),
                reverse=True,
            )
            keep_versions = {v.get("version") for v in ordered[:keep]}
            if isinstance(current, int):
                keep_versions.add(current)

            doomed = [v for v in ordered if v.get("version") not in keep_versions]
            survivors = [v for v in ordered if v.get("version") in keep_versions]

            for entry in doomed:
                rel = entry.get("file", "")
                if not rel:
                    continue
                fpath = project / rel
                size = fpath.stat().st_size if fpath.is_file() else 0
                if fpath.is_file():
                    reclaimed += size
                    removed_files += 1
                else:
                    orphans += 1
                hs = hls_candidates(fpath)
                for h in hs:
                    if h.is_file():
                        reclaimed += h.stat().st_size
                        removed_files += 1
                print(
                    f"  [plan] {project.name}/{asset_type}/{asset_id} "
                    f"v{entry.get('version')} {rel} ({fmt_mb(size)})"
                )
                if apply:
                    try:
                        if fpath.is_file():
                            fpath.unlink()
                        for h in hs:
                            h.unlink(missing_ok=True)
                    except OSError as exc:
                        print(f"    [ERROR] 删除失败，保留记录：{exc}", file=sys.stderr)
                        survivors.append(entry)
                        continue
                    changed = True

            if apply and changed:
                # survivors 按 version 升序写回（与原索引同序）
                resource["versions"] = sorted(
                    survivors, key=lambda v: v.get("version", 0)
                )
                # current_version 指向的版本一定还在
                if len(resource["versions"]) == 1:
                    resource["current_version"] = resource["versions"][0]["version"]

    if orphans:
        print(f"  [note] {project.name}: {orphans} 条记录的文件已不存在（只清记录）")

    if apply and changed:
        stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        trash = project / ".trash" / "assets" / stamp
        trash.mkdir(parents=True, exist_ok=True)
        backup = trash / "versions.json"
        backup.write_text(index_path.read_text(encoding="utf-8"), encoding="utf-8")
        tmp = index_path.with_suffix(".json.tmp")
        tmp.write_text(
            json.dumps(index, ensure_ascii=False, indent=2), encoding="utf-8"
        )
        os.replace(tmp, index_path)
        print(f"  [index] {project.name}: versions.json 已改写，原文件备份到 {backup}")

    return reclaimed, removed_files


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--apply", action="store_true", help="真删（默认 dry-run）")
    parser.add_argument("--keep", type=int, default=DEFAULT_KEEP, help="每资产保留的最近版本数")
    parser.add_argument(
        "--project", action="append", default=None,
        help="只处理指定项目（可重复）；默认全部 studio 项目",
    )
    parser.add_argument(
        "--types", default=",".join(DEFAULT_TYPES), help="处理的资产类型，逗号分隔",
    )
    args = parser.parse_args()

    types = tuple(t.strip() for t in args.types.split(",") if t.strip())
    projects = (
        [PROJECTS_ROOT / p for p in args.project]
        if args.project
        else [d for d in sorted(PROJECTS_ROOT.iterdir()) if (d / "versions" / "versions.json").is_file()]
    )
    if not projects:
        print("没有可处理的 studio 项目")
        return 0

    lock = acquire_lock()
    if lock is None:
        return 1

    mode = "APPLY" if args.apply else "DRY-RUN"
    print(f"== prune_studio_versions {mode} keep={args.keep} types={types} ==")
    total_bytes = 0
    total_files = 0
    for project in projects:
        print(f"-- {project.name}")
        reclaimed, files = plan_project(project, types, args.keep, args.apply)
        total_bytes += reclaimed
        total_files += files
        print(f"   小计：{files} 个文件，{fmt_mb(reclaimed)}")

    print(f"== 总计：{total_files} 个文件，{'已回收' if args.apply else '可回收'} {fmt_mb(total_bytes)} ==")
    try:
        lock.close()
    except Exception:
        pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
