#!/usr/bin/env python3
"""只读查询腾讯云账户余额（MPS 与 COS 直存两套凭据分别查）。

密钥从 options 表读取，不打印、不落盘，只输出余额数字。
"""

import hashlib
import hmac
import json
import subprocess
import sys
import urllib.error
import urllib.request
from datetime import datetime, timezone

HOST = "billing.tencentcloudapi.com"
SERVICE = "billing"
VERSION = "2018-07-09"

OPTION_KEYS = ["TencentMPSSetting", "CosDirectSetting"]


def load_option(key):
    out = subprocess.check_output(
        [
            "sudo", "docker", "exec", "-i", "postgres",
            "psql", "-U", "root", "-d", "new-api", "-A", "-t", "-c",
            f"SELECT value FROM options WHERE key='{key}';",
        ]
    ).decode().strip()
    if not out:
        return None
    return json.loads(out)


def sha256_hex(s):
    return hashlib.sha256(s.encode()).hexdigest()


def hmac256(key, msg):
    return hmac.new(key, msg.encode(), hashlib.sha256).digest()


def call(action, body, sid, skey):
    now = datetime.now(timezone.utc)
    date = now.strftime("%Y-%m-%d")
    ts = str(int(now.timestamp()))

    payload = json.dumps(body, separators=(",", ":"))
    ctype = "application/json; charset=utf-8"
    canonical_headers = f"content-type:{ctype}\nhost:{HOST}\n"
    signed_headers = "content-type;host"
    canonical_request = "\n".join(
        ["POST", "/", "", canonical_headers, signed_headers, sha256_hex(payload)]
    )
    scope = f"{date}/{SERVICE}/tc3_request"
    string_to_sign = "\n".join(
        ["TC3-HMAC-SHA256", ts, scope, sha256_hex(canonical_request)]
    )
    k_date = hmac256(("TC3" + skey).encode(), date)
    k_service = hmac256(k_date, SERVICE)
    k_signing = hmac256(k_service, "tc3_request")
    signature = hmac.new(k_signing, string_to_sign.encode(), hashlib.sha256).hexdigest()

    req = urllib.request.Request(
        f"https://{HOST}/",
        data=payload.encode(),
        method="POST",
        headers={
            "Authorization": (
                f"TC3-HMAC-SHA256 Credential={sid}/{scope}, "
                f"SignedHeaders={signed_headers}, Signature={signature}"
            ),
            "Content-Type": ctype,
            "Host": HOST,
            "X-TC-Action": action,
            "X-TC-Version": VERSION,
            "X-TC-Timestamp": ts,
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        return json.loads(e.read().decode())


def main():
    for key in OPTION_KEYS:
        cfg = load_option(key)
        if not cfg:
            print(f"{key}: (option 不存在)")
            continue
        sid = (cfg.get("secret_id") or cfg.get("secretId") or "").strip()
        skey = (cfg.get("secret_key") or cfg.get("secretKey") or "").strip()
        bucket = cfg.get("bucket") or ""
        if not sid or not skey:
            print(f"{key}: (无密钥)")
            continue
        res = call("DescribeAccountBalance", {}, sid, skey)
        r = res.get("Response", {})
        if "Error" in r:
            err = r["Error"]
            print(f"{key} bucket={bucket}: FAIL code={err.get('Code')} msg={err.get('Message')}")
            continue
        keep = {
            k: r.get(k)
            for k in ("Balance", "RealBalance", "CreditBalance", "IsArrears")
            if k in r
        }
        print(f"{key} bucket={bucket}: {json.dumps(keep, ensure_ascii=False)}")


if __name__ == "__main__":
    main()
