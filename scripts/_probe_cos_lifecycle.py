#!/usr/bin/env python3
"""只读查询 COS 桶的生命周期与版本控制配置。

用途：确认超分产物不会被生命周期规则提前删除（交付链接长期有效的隐含前提）。
只读，不创建/修改任何规则。
"""

import hashlib
import hmac
import json
import subprocess
import time
import urllib.error
import urllib.request

BUCKET = "mps-test-1397292858"
REGION = "ap-guangzhou"


def load_option(key):
    out = subprocess.check_output(
        [
            "sudo", "docker", "exec", "-i", "postgres",
            "psql", "-U", "root", "-d", "new-api", "-A", "-t", "-c",
            f"SELECT value FROM options WHERE key='{key}';",
        ]
    ).decode().strip()
    return json.loads(out)


def signed_url(sid, skey, path, params):
    host = f"{BUCKET}.cos.{REGION}.myqcloud.com"
    now = int(time.time())
    key_time = f"{now};{now + 600}"
    sign_key = hmac.new(skey.encode(), key_time.encode(), hashlib.sha1).hexdigest()
    param_list = ";".join(sorted(params))
    query = "&".join(f"{k}=" for k in sorted(params))
    http_string = f"get\n{path}\n{query}\nhost={host}\n"
    string_to_sign = (
        "sha1\n" + key_time + "\n" + hashlib.sha1(http_string.encode()).hexdigest() + "\n"
    )
    signature = hmac.new(sign_key.encode(), string_to_sign.encode(), hashlib.sha1).hexdigest()
    auth = (
        f"q-sign-algorithm=sha1&q-ak={sid}&q-sign-time={key_time}"
        f"&q-key-time={key_time}&q-header-list=host&q-url-param-list={param_list}"
        f"&q-signature={signature}"
    )
    url = f"https://{host}{path}"
    if query:
        url += "?" + query
    return url + ("&" if query else "?") + auth


def get(url):
    try:
        with urllib.request.urlopen(url, timeout=20) as resp:
            return resp.status, resp.read(4000).decode("utf-8", "replace")
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read(2000).decode("utf-8", "replace")


def main():
    cfg = load_option("TencentMPSSetting")
    sid = (cfg.get("secret_id") or "").strip()
    skey = (cfg.get("secret_key") or "").strip()
    for name, path, params in (
        ("lifecycle", "/", ["lifecycle"]),
        ("versioning", "/", ["versioning"]),
    ):
        code, body = get(signed_url(sid, skey, path, params))
        print(f"== {name} == http {code}")
        print(body[:1200])


if __name__ == "__main__":
    main()
