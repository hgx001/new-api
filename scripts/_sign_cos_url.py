#!/usr/bin/env python3
"""为指定 COS 对象生成 10 分钟有效的只读签名 URL，输出到 stdout。

只用于本机测速：URL 里是临时签名，不是长期凭据。
"""

import hashlib
import hmac
import json
import subprocess
import sys
import time
import urllib.parse

BUCKET = sys.argv[1]
REGION = sys.argv[2]
KEY = sys.argv[3]
OPTION = sys.argv[4] if len(sys.argv) > 4 else "TencentMPSSetting"


def load_option(key):
    out = subprocess.check_output(
        [
            "sudo", "docker", "exec", "-i", "postgres",
            "psql", "-U", "root", "-d", "new-api", "-A", "-t", "-c",
            f"SELECT value FROM options WHERE key='{key}';",
        ]
    ).decode().strip()
    return json.loads(out)


cfg = load_option(OPTION)
sid = (cfg.get("secret_id") or "").strip()
skey = (cfg.get("secret_key") or "").strip()

host = f"{BUCKET}.cos.{REGION}.myqcloud.com"
now = int(time.time())
key_time = f"{now};{now + 600}"
sign_key = hmac.new(skey.encode(), key_time.encode(), hashlib.sha1).hexdigest()

pathname = "/" + KEY
http_string = f"get\n{pathname}\n\nhost={host}\n"
string_to_sign = "sha1\n" + key_time + "\n" + hashlib.sha1(
    http_string.encode()
).hexdigest() + "\n"
signature = hmac.new(
    sign_key.encode(), string_to_sign.encode(), hashlib.sha1
).hexdigest()

auth = (
    f"q-sign-algorithm=sha1&q-ak={sid}&q-sign-time={key_time}"
    f"&q-key-time={key_time}&q-header-list=host&q-url-param-list="
    f"&q-signature={signature}"
)
print(f"https://{host}{urllib.parse.quote(pathname)}?{auth}")
