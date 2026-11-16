# -*- coding: utf-8 -*-
"""恢复 footer 品牌键的 \\u0061 转义形式。

i18n 同步脚本把该键定义为 OBFUSCATED_KEYS，序列化时必须保留 `\\u0061`
（避免源码/产物里出现完整品牌名）。json.dump 会把转义还原成 "a"，所以
重写语言包后要按文本层面还原回来。
"""
import json
import os

LANGS = ["en", "zh", "fr", "ja", "ru", "vi"]
LOCALES = os.path.join("web", "default", "src", "i18n", "locales")

PLAIN = '"footer.newapi.projectAttributionSuffix"'
ESCAPED = '"footer.new' + chr(92) + 'u0061pi.projectAttributionSuffix"'

for lang in LANGS:
    path = os.path.join(LOCALES, lang + ".json")
    with open(path, encoding="utf-8") as fh:
        text = fh.read()
    hits = text.count(PLAIN)
    if hits:
        text = text.replace(PLAIN, ESCAPED)
        with open(path, "w", encoding="utf-8", newline="\n") as fh:
            fh.write(text)
    # 校验：解析后 key 仍是 footer.newapi...，且文件里不含明文品牌键
    data = json.loads(text)
    ok = PLAIN[1:-1] in data["translation"]
    leaked = PLAIN in text
    print(f"{lang}: replaced={hits} key_ok={ok} plain_text_leak={leaked}")
