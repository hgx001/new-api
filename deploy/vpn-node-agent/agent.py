#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""VPN 节点 agent：把网站签发的每订单凭据同步进 sing-box，并把连接来源 IP 回报网站。

设计约束（改代码前请先读）：

1. **只碰带 name 的用户**。节点上 8443 入站里有两个早期共享 UUID（老付费用户与内部
   用途），它们没有 ``name`` 字段。agent 按 ``^v\\d+$`` 识别托管用户，其余用户原样保留，
   所以老用户不会被卷进新规则。
2. **任何配置变更都必须先过 ``sing-box check``**，失败就不落地、不重启，保留旧配置。
   重启后校验全部入站端口仍在监听，否则自动回滚。
3. **日志解析按连接 ID 配对**。info 级别下 sing-box 分两行输出：
       inbound/vless[tag]: inbound connection from 1.2.3.4:5678
       inbound/vless[tag]: [v3] inbound connection to example.com:443
   第一行有源 IP 没有用户名，第二行有用户名没有源 IP，只有连接 ID 相同，必须配对。
4. **首次运行从文件末尾开始**。否则会把历史日志里的旧 IP 全部上报，可能反而把自己的
   付费用户误判成共享。
"""

import argparse
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
from collections import OrderedDict

DEFAULT_ENV_FILE = "/etc/vpn-node-agent/agent.env"
MANAGED_NAME_RE = re.compile(r"^v\d+$")

# 两行日志的匹配式。connid 形如 `[527659267 0ms]`，毫秒位可能是小数。
CONN_PREFIX = r"\[(\d+)\s+[\d.]+ms\]"
RE_FROM = re.compile(
    CONN_PREFIX + r"\s+inbound/vless\[([^\]]+)\]:\s+inbound connection from\s+([0-9a-fA-F.:]+):\d+\s*$"
)
RE_TO = re.compile(
    CONN_PREFIX + r"\s+inbound/vless\[([^\]]+)\]:\s+\[([^\]]+)\]\s+inbound connection to"
)

MAX_READ_BYTES = 8 * 1024 * 1024
MAX_PENDING_CONN = 4096
BACKUP_KEEP = 10


def log(msg):
    sys.stderr.write("[%s] %s\n" % (time.strftime("%Y-%m-%d %H:%M:%S"), msg))


def load_env_file(path):
    """极简 KEY=VALUE 解析。不用 dotenv：节点上不该为一个配置文件装依赖。"""
    data = {}
    if not os.path.exists(path):
        return data
    with open(path, "r", encoding="utf-8") as handle:
        for raw in handle:
            line = raw.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, value = line.split("=", 1)
            value = value.strip()
            if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
                value = value[1:-1]
            data[key.strip()] = value
    return data


class Config(object):
    def __init__(self, env_file):
        from_file = load_env_file(env_file)

        def get(key, default=""):
            return os.environ.get(key) or from_file.get(key, default)

        self.api_base = get("VPN_API_BASE", "https://api.heibaidao.cn").rstrip("/")
        self.token = get("VPN_AGENT_TOKEN")
        self.inbound_tag = get("VPN_INBOUND_TAG", "")
        self.inbound_port = int(get("VPN_INBOUND_PORT", "8443"))
        self.singbox_config = get("VPN_SINGBOX_CONFIG", "/etc/sing-box/config.json")
        self.log_file = get("VPN_LOG_FILE", "/var/log/sing-box/sing-box.log")
        self.state_dir = get("VPN_STATE_DIR", "/var/lib/vpn-node-agent")
        self.service = get("VPN_SINGBOX_SERVICE", "sing-box")
        self.singbox_bin = get("VPN_SINGBOX_BIN", "sing-box")
        self.timeout = int(get("VPN_HTTP_TIMEOUT", "15"))

    @property
    def state_file(self):
        return os.path.join(self.state_dir, "log-state.json")


def http_json(cfg, method, path, payload=None):
    url = cfg.api_base + path
    body = None
    headers = {
        "Authorization": "Bearer " + cfg.token,
        "Accept": "application/json",
        "User-Agent": "vpn-node-agent/1.0",
    }
    if payload is not None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(url, data=body, headers=headers, method=method)
    with urllib.request.urlopen(request, timeout=cfg.timeout) as response:
        raw = response.read()
    if not raw:
        return {}
    return json.loads(raw.decode("utf-8"))


# ---------------------------------------------------------------- sing-box 配置


def read_json_file(path):
    with open(path, "r", encoding="utf-8") as handle:
        return json.load(handle)


def detect_indent(text):
    """沿用原文件缩进，避免一次自动同步把整个配置重排成看不懂的大 diff。"""
    for line in text.splitlines():
        stripped = line.lstrip(" ")
        if stripped and stripped != line:
            return len(line) - len(stripped)
    return 2


def find_inbound(config, cfg):
    for inbound in config.get("inbounds", []):
        if inbound.get("type") != "vless":
            continue
        if cfg.inbound_tag:
            if inbound.get("tag") == cfg.inbound_tag:
                return inbound
            continue
        if int(inbound.get("listen_port", 0)) == cfg.inbound_port:
            return inbound
    return None


def inbound_ports(config):
    ports = []
    for inbound in config.get("inbounds", []):
        port = inbound.get("listen_port")
        if isinstance(port, int):
            ports.append(port)
    return ports


def build_users(existing, manifest_users):
    """保留所有非托管用户（老的共享 UUID、内部用途），只重建 ^v\\d+$ 那批。"""
    kept = [user for user in existing if not MANAGED_NAME_RE.match(str(user.get("name", "")))]
    flow = ""
    for user in existing:
        if user.get("flow"):
            flow = user["flow"]
            break
    managed = []
    for item in manifest_users:
        name = str(item.get("name", "")).strip()
        uuid = str(item.get("uuid", "")).strip()
        if not MANAGED_NAME_RE.match(name) or not uuid:
            continue
        user = {"name": name, "uuid": uuid}
        if flow:
            user["flow"] = flow
        managed.append(user)
    return kept + managed, kept, managed


def plan_change(cfg, manifest, dry_run):
    with open(cfg.singbox_config, "r", encoding="utf-8") as handle:
        original_text = handle.read()
    config = json.loads(original_text)
    inbound = find_inbound(config, cfg)
    if inbound is None:
        raise SystemExit("找不到目标 vless 入站（tag=%r port=%r）" % (cfg.inbound_tag, cfg.inbound_port))

    existing = inbound.get("users", [])
    users, kept, managed = build_users(existing, manifest.get("users", []))
    changed = [(u.get("name"), u.get("uuid")) for u in existing if MANAGED_NAME_RE.match(str(u.get("name", "")))] != [
        (u.get("name"), u.get("uuid")) for u in managed
    ]

    summary = {
        "inbound": inbound.get("tag"),
        "kept": len(kept),
        "managed": len(managed),
        "changed": changed,
    }
    if not changed:
        return summary, None

    inbound["users"] = users
    indent = detect_indent(original_text)
    candidate_text = json.dumps(config, indent=indent, ensure_ascii=False) + "\n"
    return summary, candidate_text


def run_command(args, timeout=60):
    completed = subprocess.run(
        args, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout
    )
    return completed.returncode, completed.stdout.decode("utf-8", "replace").strip()


def check_config(cfg, path):
    code, output = run_command([cfg.singbox_bin, "check", "-c", path], timeout=60)
    return code == 0, output


def backup_config(cfg, text):
    backup_dir = os.path.join(os.path.dirname(cfg.singbox_config), "backup")
    os.makedirs(backup_dir, exist_ok=True)
    stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    path = os.path.join(backup_dir, "config-%s.json" % stamp)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(text)
    os.chmod(path, 0o600)
    backups = sorted(
        [os.path.join(backup_dir, name) for name in os.listdir(backup_dir) if name.startswith("config-")]
    )
    for stale in backups[:-BACKUP_KEEP]:
        try:
            os.remove(stale)
        except OSError:
            pass
    return path


def ports_listening(ports):
    """用 ss 判断端口是否在监听；ss 不可用时返回 None 表示“无法判断”，不阻塞重启。"""
    try:
        code, output = run_command(["ss", "-H", "-tln"], timeout=15)
    except (OSError, subprocess.SubprocessError):
        return None
    if code != 0:
        return None
    listening = set()
    for line in output.splitlines():
        fields = line.split()
        if len(fields) < 4:
            continue
        local = fields[3]
        if ":" in local:
            try:
                listening.add(int(local.rsplit(":", 1)[1]))
            except ValueError:
                continue
    return all(port in listening for port in ports)


def restart_service(cfg, ports, previous_text):
    code, output = run_command(["systemctl", "restart", cfg.service], timeout=90)
    if code != 0:
        return False, "systemctl restart 失败: " + output
    deadline = time.time() + 15
    while time.time() < deadline:
        code, _ = run_command(["systemctl", "is-active", "--quiet", cfg.service], timeout=15)
        if code == 0:
            state = ports_listening(ports)
            if state is None or state:
                return True, "服务已重启，端口正常"
        time.sleep(1)
    # 回滚：把刚才的备份写回并重启，宁可回退也不能让节点停在半死状态
    with open(cfg.singbox_config, "w", encoding="utf-8") as handle:
        handle.write(previous_text)
    run_command(["systemctl", "restart", cfg.service], timeout=90)
    return False, "重启后校验失败，已回滚到旧配置"


# ------------------------------------------------------------------ 日志采集


def load_state(cfg):
    try:
        with open(cfg.state_file, "r", encoding="utf-8") as handle:
            return json.load(handle)
    except (IOError, ValueError):
        return None


def save_state(cfg, state):
    os.makedirs(cfg.state_dir, exist_ok=True)
    tmp = cfg.state_file + ".tmp"
    with open(tmp, "w", encoding="utf-8") as handle:
        json.dump(state, handle)
    os.replace(tmp, cfg.state_file)


def read_new_lines(cfg, state):
    """返回 (新行列表, 新 state)。

    - 首次运行（没有 state）直接从文件末尾开始：宁可漏掉历史，也不要把陈年 IP
      当成今天的共享证据。
    - logrotate 用的是 rename + create：sing-box 的 fd 跟着改名的旧文件继续写，
      所以 inode 变了要先从轮转后的旧文件里把尾巴读完，再切到新文件。
    """
    try:
        stat = os.stat(cfg.log_file)
    except OSError:
        return [], state

    if state is None:
        return [], {"inode": stat.st_ino, "offset": stat.st_size}

    inode = state.get("inode")
    offset = int(state.get("offset", 0))

    lines = []
    if inode != stat.st_ino:
        rotated = find_rotated_file(cfg, inode)
        if rotated is not None:
            lines.extend(read_tail(rotated, offset))
        offset = 0
    elif stat.st_size < offset:
        # 文件被截断（copytruncate 或人工清空）：从头再读
        offset = 0

    chunk_lines, offset = read_chunk(cfg.log_file, offset, stat.st_size)
    lines.extend(chunk_lines)
    return lines, {"inode": stat.st_ino, "offset": offset}


def find_rotated_file(cfg, inode):
    """在 log_file.1 / .2.gz 里按 inode 找到刚被轮转走的那个文件。"""
    directory = os.path.dirname(cfg.log_file) or "."
    base = os.path.basename(cfg.log_file)
    try:
        names = os.listdir(directory)
    except OSError:
        return None
    for name in names:
        if name == base or not name.startswith(base):
            continue
        path = os.path.join(directory, name)
        try:
            if os.stat(path).st_ino == inode:
                return path
        except OSError:
            continue
    return None


def read_tail(path, offset):
    """读轮转后旧文件剩下的完整行；压缩过的文件（.gz）读不了，直接跳过。"""
    if path.endswith(".gz"):
        return []
    try:
        with open(path, "rb") as handle:
            handle.seek(0, os.SEEK_END)
            size = handle.tell()
            if size <= offset:
                return []
            handle.seek(max(offset, size - MAX_READ_BYTES))
            chunk = handle.read()
    except OSError:
        return []
    cut = chunk.rfind(b"\n")
    if cut < 0:
        return []
    return chunk[: cut + 1].decode("utf-8", "replace").splitlines()


def read_chunk(path, offset, size):
    if size <= offset:
        return [], offset
    start = max(offset, size - MAX_READ_BYTES)
    with open(path, "rb") as handle:
        handle.seek(start)
        chunk = handle.read()
    if not chunk:
        return [], size
    # 按字节找最后一个换行，偏移量才不会被多字节字符或坏字节带偏。
    cut = chunk.rfind(b"\n")
    if cut < 0:
        # 只有半行，等下一轮补齐
        return [], start
    return chunk[: cut + 1].decode("utf-8", "replace").splitlines(), start + cut + 1


def collect_events(lines, tag=None):
    """按连接 ID 把「源 IP 行」和「用户名行」配对成 (name, ip)。"""
    pending = OrderedDict()
    events = []
    for line in lines:
        match = RE_FROM.search(line)
        if match:
            conn_id, inbound_tag, ip = match.group(1), match.group(2), match.group(3)
            if tag and inbound_tag != tag:
                continue
            pending[conn_id] = ip
            while len(pending) > MAX_PENDING_CONN:
                pending.popitem(last=False)
            continue
        match = RE_TO.search(line)
        if match:
            conn_id, inbound_tag, name = match.group(1), match.group(2), match.group(3)
            if tag and inbound_tag != tag:
                continue
            ip = pending.pop(conn_id, None)
            if ip and MANAGED_NAME_RE.match(name):
                events.append({"name": name, "ip": ip})
    return events


# ---------------------------------------------------------------------- 主流程


def sync_config(cfg, manifest, dry_run):
    summary, candidate = plan_change(cfg, manifest, dry_run)
    log(
        "清单: 托管用户 %d 条，保留非托管用户 %d 条，变更=%s"
        % (summary["managed"], summary["kept"], "是" if summary["changed"] else "否")
    )
    if candidate is None:
        return True
    if dry_run:
        log("dry-run：检测到配置需要变更，未写入")
        return True

    with open(cfg.singbox_config, "r", encoding="utf-8") as handle:
        previous_text = handle.read()
    candidate_path = cfg.singbox_config + ".new"
    with open(candidate_path, "w", encoding="utf-8") as handle:
        handle.write(candidate)
    ok, output = check_config(cfg, candidate_path)
    if not ok:
        os.remove(candidate_path)
        log("候选配置未通过 sing-box check，放弃本次变更：" + output)
        return False

    backup = backup_config(cfg, previous_text)
    log("已备份旧配置: " + backup)
    os.replace(candidate_path, cfg.singbox_config)
    ports = inbound_ports(read_json_file(cfg.singbox_config))
    ok, message = restart_service(cfg, ports, previous_text)
    log(message)
    if not ok:
        return False
    log("配置已生效，托管用户 %d 条" % summary["managed"])
    return True


def report_events(cfg, events):
    """同一轮里 (name, ip) 去重；服务端还会按 24h 窗口再做一次幂等。"""
    seen = set()
    payload = []
    for event in events:
        key = (event["name"], event["ip"])
        if key in seen:
            continue
        seen.add(key)
        payload.append(event)
    if not payload:
        return
    for start in range(0, len(payload), 1000):
        batch = payload[start : start + 1000]
        try:
            result = http_json(cfg, "POST", "/api/vpn/node/connections", {"events": batch})
        except (urllib.error.URLError, ValueError) as error:
            log("连接事件上报失败（下一轮重试）: %s" % error)
            return
        frozen = result.get("frozen") or []
        if frozen:
            log("服务端已冻结凭据: " + ", ".join(frozen))
    log("已上报连接事件 %d 条" % len(payload))


def main():
    parser = argparse.ArgumentParser(description="VPN 节点 agent")
    parser.add_argument("--env-file", default=os.environ.get("VPN_AGENT_ENV", DEFAULT_ENV_FILE))
    parser.add_argument("--dry-run", action="store_true", help="只打印将要发生的变更，不写配置")
    parser.add_argument("--skip-log", action="store_true", help="本轮不采集日志")
    args = parser.parse_args()

    cfg = Config(args.env_file)
    if not cfg.token:
        log("缺少 VPN_AGENT_TOKEN，无法继续")
        return 2

    try:
        manifest = http_json(cfg, "GET", "/api/vpn/node/manifest")
    except (urllib.error.URLError, ValueError) as error:
        log("拉取凭据清单失败: %s" % error)
        return 1
    if not isinstance(manifest, dict):
        log("凭据清单格式异常")
        return 1

    ok = sync_config(cfg, manifest, args.dry_run)
    if not ok:
        return 1
    if args.dry_run or args.skip_log:
        return 0

    state = load_state(cfg)
    lines, next_state = read_new_lines(cfg, state)
    if next_state != state:
        save_state(cfg, next_state)
    tag = cfg.inbound_tag or None
    events = collect_events(lines, tag=tag)
    if events:
        report_events(cfg, events)
    return 0


if __name__ == "__main__":
    sys.exit(main())
