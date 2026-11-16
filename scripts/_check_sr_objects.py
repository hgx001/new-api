#!/usr/bin/env python3
"""只读核对：MPS 超分产物是否仍在 COS 桶里、大小是否正常。

用法：python3 _check_sr_objects.py <objectPath> [objectPath...]

对每个对象生成 10 分钟签名 URL 并 HEAD，只打印 path / HTTP 状态 / 字节数。
不打印签名、不打印密钥、不写任何东西。
"""

import hashlib
import hmac
import json
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

BUCKET = "mps-test-1397292858"
REGION = "ap-guangzhou"
OPTION = "TencentMPSSetting"


def load_option(key):
    out = subprocess.check_output(
        [
            "sudo", "docker", "exec", "-i", "postgres",
            "psql", "-U", "root", "-d", "new-api", "-A", "-t", "-c",
            f"SELECT value FROM options WHERE key='{key}';",
        ]
    ).decode().strip()
    return json.loads(out)


def sign(sid, skey, key):
    host = f"{BUCKET}.cos.{REGION}.myqcloud.com"
    now = int(time.time())
    key_time = f"{now};{now + 600}"
    sign_key = hmac.new(skey.encode(), key_time.encode(), hashlib.sha1).hexdigest()
    pathname = "/" + key.lstrip("/")
    http_string = f"head\n{pathname}\n\nhost={host}\n"
    string_to_sign = (
        "sha1\n" + key_time + "\n" + hashlib.sha1(http_string.encode()).hexdigest() + "\n"
    )
    signature = hmac.new(sign_key.encode(), string_to_sign.encode(), hashlib.sha1).hexdigest()
    auth = (
        f"q-sign-algorithm=sha1&q-ak={sid}&q-sign-time={key_time}"
        f"&q-key-time={key_time}&q-header-list=host&q-url-param-list="
        f"&q-signature={signature}"
    )
    return f"https://{host}/{urllib.parse.quote(key.lstrip('/'))}?{auth}"


def main():
    cfg = load_option(OPTION)
    sid = (cfg.get("secret_id") or "").strip()
    skey = (cfg.get("secret_key") or "").strip()
    for key in sys.argv[1:]:
        url = sign(sid, skey, key)
        req = urllib.request.Request(url, method="HEAD")
        try:
            with urllib.request.urlopen(req, timeout=20) as resp:
                size = resp.headers.get("Content-Length", "?")
                print(f"{resp.status}\t{size}\t{key}")
        except urllib.error.HTTPError as exc:
            print(f"{exc.code}\t-\t{key}\t{exc.reason}")
        except Exception as exc:  # noqa: BLE001
            print(f"ERR\t-\t{key}\t{exc}")


if __name__ == "__main__":
    main()
