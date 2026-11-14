#!/usr/bin/env python3
"""只读探测 COS 桶是否已被欠费限制。

做法：对桶内一个**必然不存在**的对象发签名 GET。
- 服务正常 → 404 NoSuchKey（说明鉴权与桶都可用）
- 欠费停服 → 403 且错误信息含 arrears/arrearage

不写入、不列举、不打印密钥。
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

OPTION_KEYS = ["TencentMPSSetting", "CosDirectSetting"]


def load_option(key):
    out = subprocess.check_output(
        [
            "sudo", "docker", "exec", "-i", "postgres",
            "psql", "-U", "root", "-d", "new-api", "-A", "-t", "-c",
            f"SELECT value FROM options WHERE key='{key}';",
        ]
    ).decode().strip()
    return json.loads(out) if out else None


def probe(bucket, region, sid, skey):
    host = f"{bucket}.cos.{region}.myqcloud.com"
    key = "healthcheck-probe-does-not-exist.bin"
    now = int(time.time())
    validity = now + 600
    key_time = f"{now};{validity}"

    # COS v5：SignKey = HMAC-SHA1(SecretKey, KeyTime)
    sign_key = hmac.new(skey.encode(), key_time.encode(), hashlib.sha1).hexdigest()

    # COS v5 签名（Authorization 必须带全 6 个字段，缺一个就整体验签失败）
    http_string = f"get\n/{key}\n\nhost={host}\n"
    string_to_sign = (
        "sha1\n" + key_time + "\n" + hashlib.sha1(http_string.encode()).hexdigest() + "\n"
    )
    signature = hmac.new(
        sign_key.encode(), string_to_sign.encode(), hashlib.sha1
    ).hexdigest()
    auth = (
        f"q-sign-algorithm=sha1&q-ak={sid}&q-sign-time={key_time}"
        f"&q-key-time={key_time}&q-header-list=host&q-url-param-list="
        f"&q-signature={signature}"
    )

    url = f"https://{host}/{urllib.parse.quote(key)}?{auth}"
    req = urllib.request.Request(url, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            return f"HTTP {resp.status} (意外成功)"
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", "replace")[:300]
        code = ""
        msg = ""
        if "<Code>" in body:
            code = body.split("<Code>")[1].split("</Code>")[0]
        if "<Message>" in body:
            msg = body.split("<Message>")[1].split("</Message>")[0]
        return f"HTTP {e.code} code={code} msg={msg}"
    except Exception as e:  # noqa: BLE001
        return f"ERR {type(e).__name__}: {e}"


def main():
    for opt in OPTION_KEYS:
        cfg = load_option(opt)
        if not cfg:
            print(f"{opt}: (不存在)")
            continue
        bucket = cfg.get("bucket", "")
        region = cfg.get("region", "ap-guangzhou")
        sid = (cfg.get("secret_id") or cfg.get("secretId") or "").strip()
        skey = (cfg.get("secret_key") or cfg.get("secretKey") or "").strip()
        if not (bucket and sid and skey):
            print(f"{opt}: 缺 bucket/密钥")
            continue
        print(f"{opt} bucket={bucket}: {probe(bucket, region, sid, skey)}")


if __name__ == "__main__":
    main()
