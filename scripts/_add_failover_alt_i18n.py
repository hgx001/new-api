import io, os, sys

BASE = r"c:\work\new-api-src\web\default\src\i18n\locales"

T = {
    "en": [
        ("Failover alternative (CapCut 2.5 Flash)", "Failover alternative (CapCut 2.5 Flash)"),
        ("Failover alternative is on", "On"),
        ("Failover alternative is off", "Off"),
        ("On but not taking effect", "On but not taking effect"),
        ("When the Doubao link is unavailable, requests route to {{target}}: generated at 480p, upscaled to 720P before delivery",
         "When the Doubao link is unavailable, requests route to {{target}}: generated at 480p, upscaled to 720P before delivery"),
        ("When the Doubao link is unavailable, requests still go to the shuttle queue; turn on to reroute them to {{target}} (480p generation upscaled to 720P)",
         "When the Doubao link is unavailable, requests still go to the shuttle queue; turn on to reroute them to {{target}} (480p generation upscaled to 720P)"),
    ],
    "zh": [
        ("Failover alternative (CapCut 2.5 Flash)", "故障转移备选（CapCut 2.5 Flash）"),
        ("Failover alternative is on", "已开启"),
        ("Failover alternative is off", "已关闭"),
        ("On but not taking effect", "已开启但不生效"),
        ("When the Doubao link is unavailable, requests route to {{target}}: generated at 480p, upscaled to 720P before delivery",
         "豆包链路不可用时改派 {{target}}：按 480p 生成，交付前超分到 720P"),
        ("When the Doubao link is unavailable, requests still go to the shuttle queue; turn on to reroute them to {{target}} (480p generation upscaled to 720P)",
         "豆包链路不可用时仍走班车；打开可改派 {{target}}（480p 生成 + 超分 720P 交付）"),
    ],
    "fr": [
        ("Failover alternative (CapCut 2.5 Flash)", "Alternative de bascule (CapCut 2.5 Flash)"),
        ("Failover alternative is on", "Activée"),
        ("Failover alternative is off", "Désactivée"),
        ("On but not taking effect", "Activée mais sans effet"),
        ("When the Doubao link is unavailable, requests route to {{target}}: generated at 480p, upscaled to 720P before delivery",
         "Lorsque la liaison Doubao est indisponible, les requêtes sont réacheminées vers {{target}} : génération en 480p, puis suréchantillonnage en 720P avant livraison"),
        ("When the Doubao link is unavailable, requests still go to the shuttle queue; turn on to reroute them to {{target}} (480p generation upscaled to 720P)",
         "Lorsque la liaison Doubao est indisponible, les requêtes passent toujours par la file de la navette ; activez pour les réacheminer vers {{target}} (génération 480p suréchantillonnée en 720P)"),
    ],
    "ja": [
        ("Failover alternative (CapCut 2.5 Flash)", "フェイルオーバー代替（CapCut 2.5 Flash）"),
        ("Failover alternative is on", "オン"),
        ("Failover alternative is off", "オフ"),
        ("On but not taking effect", "オンだが有効になっていません"),
        ("When the Doubao link is unavailable, requests route to {{target}}: generated at 480p, upscaled to 720P before delivery",
         "Doubao 回線が利用できない場合、リクエストは {{target}} に振り分けられます：480p で生成し、配信前に 720P へ超解像します"),
        ("When the Doubao link is unavailable, requests still go to the shuttle queue; turn on to reroute them to {{target}} (480p generation upscaled to 720P)",
         "Doubao 回線が利用できない場合、リクエストはシャトルキューに流れます。オンにすると {{target}} へ振り分けます（480p 生成＋720P 超解像）"),
    ],
    "ru": [
        ("Failover alternative (CapCut 2.5 Flash)", "Резервный вариант отказоустойчивости (CapCut 2.5 Flash)"),
        ("Failover alternative is on", "Включён"),
        ("Failover alternative is off", "Выключен"),
        ("On but not taking effect", "Включено, но не действует"),
        ("When the Doubao link is unavailable, requests route to {{target}}: generated at 480p, upscaled to 720P before delivery",
         "Когда канал Doubao недоступен, запросы перенаправляются на {{target}}: генерация в 480p с последующим апскейлом до 720P перед выдачей"),
        ("When the Doubao link is unavailable, requests still go to the shuttle queue; turn on to reroute them to {{target}} (480p generation upscaled to 720P)",
         "Когда канал Doubao недоступен, запросы по-прежнему идут в очередь шаттла; включите, чтобы перенаправлять их на {{target}} (генерация 480p с апскейлом до 720P)"),
    ],
    "vi": [
        ("Failover alternative (CapCut 2.5 Flash)", "Phương án dự phòng chuyển tuyến (CapCut 2.5 Flash)"),
        ("Failover alternative is on", "Đang bật"),
        ("Failover alternative is off", "Đang tắt"),
        ("On but not taking effect", "Đang bật nhưng không có hiệu lực"),
        ("When the Doubao link is unavailable, requests route to {{target}}: generated at 480p, upscaled to 720P before delivery",
         "Khi tuyến Doubao không khả dụng, yêu cầu được chuyển sang {{target}}: tạo ở 480p, siêu phân giải lên 720P trước khi giao"),
        ("When the Doubao link is unavailable, requests still go to the shuttle queue; turn on to reroute them to {{target}} (480p generation upscaled to 720P)",
         "Khi tuyến Doubao không khả dụng, yêu cầu vẫn đi vào hàng đợi shuttle; bật để chuyển sang {{target}} (tạo 480p và siêu phân giải 720P)"),
    ],
}

import json

ANCHOR = '    "Youzan standard fallback":'

for lang, pairs in T.items():
    path = os.path.join(BASE, lang + ".json")
    with io.open(path, "r", encoding="utf-8") as fh:
        lines = fh.readlines()

    existing = set()
    out = []
    inserted = False
    for line in lines:
        stripped = line.strip()
        if stripped.startswith('"') and '":' in stripped:
            key = stripped.split('":', 1)[0][1:]
            existing.add(key)
        if not inserted and line.startswith(ANCHOR):
            indent = "    "
            for key, val in pairs:
                if key in existing:
                    continue
                out.append(indent + json.dumps(key, ensure_ascii=False) + ": " + json.dumps(val, ensure_ascii=False) + ",\n")
            inserted = True
        out.append(line)

    if not inserted:
        print("ANCHOR NOT FOUND in", lang)
        sys.exit(1)

    with io.open(path, "w", encoding="utf-8", newline="") as fh:
        fh.writelines(out)
    print("ok", lang)
