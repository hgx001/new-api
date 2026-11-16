# -*- coding: utf-8 -*-
"""把过时的路由卡片文案替换成与当前代码一致的口径。

一次性的运维脚本：旧 key 删除、新 key 写入（en 的 value 等于 key 本身，
其它语言写各自的翻译）。
"""
import json
import os

LOCALES = os.path.join("web", "default", "src", "i18n", "locales")

# (旧 key, 新 key, {lang: value})
PAIRS = [
    (
        "E-commerce or product-recommendation prompts are routed to {{target}}; requests with a reference video are never rerouted",
        "E-commerce and product-recommendation prompts stay on the resident Youzan link; only requests Youzan cannot take (below its duration floor, or with reference audio/video) go to {{target}}",
        {
            "zh": "电商与产品推荐类提示词继续走常驻有赞链路；只有有赞接不住的请求（低于其时长下限，或带参考音视频）才改派 {{target}}",
            "fr": "Les prompts e-commerce et de recommandation produit restent sur le lien Youzan permanent ; seules les requêtes que Youzan ne peut pas prendre (durée sous son seuil, ou audio/vidéo de référence) sont redirigées vers {{target}}",
            "ja": "电商・商品紹介系のプロンプトは常設の Youzan 回線のまま処理されます。Youzan が受けられないリクエスト（下限未満の秒数、または参照音声・動画あり）だけが {{target}} に振り分けられます",
            "ru": "Промпты электронной коммерции и рекомендаций товаров остаются на постоянном канале Youzan; на {{target}} переносятся только запросы, которые Youzan не может принять (длительность ниже минимума или есть референсное аудио/видео)",
            "vi": "Prompt thương mại điện tử và gợi ý sản phẩm vẫn chạy trên tuyến Youzan thường trực; chỉ những yêu cầu Youzan không nhận được (thời lượng dưới ngưỡng tối thiểu, hoặc có âm thanh/video tham chiếu) mới được chuyển sang {{target}}",
        },
    ),
    (
        "Turn on to route e-commerce or product-recommendation prompts to {{target}}",
        "Turn on to keep e-commerce and product-recommendation prompts on the resident Youzan link instead of letting the duration or ratio rules reroute them",
        {
            "zh": "打开后，电商与产品推荐类提示词留在常驻有赞链路，不再被时长或比例规则改派出去",
            "fr": "Activez pour garder les prompts e-commerce et de recommandation produit sur le lien Youzan permanent, au lieu de laisser les règles de durée ou de ratio les rediriger",
            "ja": "オンにすると、电商・商品紹介系のプロンプトは常設の Youzan 回線に留まり、秒数・比率ルールによる振り分けの対象になりません",
            "ru": "Включите, чтобы промпты электронной коммерции и рекомендаций товаров оставались на постоянном канале Youzan, а не переносились правилами длительности или пропорций",
            "vi": "Bật để prompt thương mại điện tử và gợi ý sản phẩm ở lại tuyến Youzan thường trực, thay vì bị quy tắc thời lượng hoặc tỷ lệ chuyển đi",
        },
    ),
    (
        "When checked, requests specifying {{ratio}} are rerouted even if the prompt is not e-commerce content",
        "When checked, requests specifying {{ratio}} also stay on the Youzan link, even if the prompt is not e-commerce content",
        {
            "zh": "勾选后，即使提示词不是电商类，指定 {{ratio}} 的请求也留在有赞链路",
            "fr": "Si coché, les requêtes en {{ratio}} restent aussi sur le lien Youzan, même si le prompt n'est pas du contenu e-commerce",
            "ja": "チェックすると、プロンプトが电商系でなくても {{ratio}} 指定のリクエストは Youzan 回線に留まります",
            "ru": "Если включено, запросы с {{ratio}} также остаются на канале Youzan, даже если промпт не относится к электронной коммерции",
            "vi": "Khi chọn, yêu cầu có tỷ lệ {{ratio}} cũng ở lại tuyến Youzan, kể cả khi prompt không phải nội dung thương mại điện tử",
        },
    ),
    (
        "When checked, requests over the threshold are rerouted even if the prompt is not e-commerce content",
        "When checked, requests over the threshold also stay on the Youzan link, even if the prompt is not e-commerce content",
        {
            "zh": "勾选后，即使提示词不是电商类，超过阈值的请求也留在有赞链路",
            "fr": "Si coché, les requêtes dépassant le seuil restent aussi sur le lien Youzan, même si le prompt n'est pas du contenu e-commerce",
            "ja": "チェックすると、プロンプトが电商系でなくても閾値を超えるリクエストは Youzan 回線に留まります",
            "ru": "Если включено, запросы длиннее порога также остаются на канале Youzan, даже если промпт не относится к электронной коммерции",
            "vi": "Khi chọn, yêu cầu vượt ngưỡng cũng ở lại tuyến Youzan, kể cả khi prompt không phải nội dung thương mại điện tử",
        },
    ),
    (
        "These requests still stay on the source chain: 1080P, 2–3s, reference video/audio, and adaptive ratio with reference images.",
        "These requests still stay on the source chain: 1080P, 2–3s, requests with first/last frames, and adaptive ratio combined with reference images.",
        {
            "zh": "这些请求仍留在源链路：1080P、2–3 秒、带首尾帧，以及「自适应比例 + 参考图」",
            "fr": "Ces requêtes restent sur le canal source : 1080P, 2–3 s, requêtes avec première/dernière image, et ratio adaptatif combiné à des images de référence.",
            "ja": "これらのリクエストは元の回線に残ります：1080P、2〜3 秒、先頭/末尾フレーム付き、および「アダプティブ比率 + 参照画像」。",
            "ru": "Эти запросы остаются на исходном канале: 1080P, 2–3 с, запросы с первым/последним кадром и адаптивное соотношение с референсными изображениями.",
            "vi": "Các yêu cầu này vẫn ở lại tuyến nguồn: 1080P, 2–3 giây, có khung đầu/cuối, và tỷ lệ thích ứng kèm ảnh tham chiếu.",
        },
    ),
    (
        "Requests up to {{seconds}}s route to {{target}}; the Xinbi link and the Youzan fallback are turned off automatically",
        "Requests from {{min}}s to just under {{seconds}}s route to {{target}}; the Xinbi link and the Youzan fallback are turned off automatically",
        {
            "zh": "{{min}} 秒到 {{seconds}} 秒（不含）的请求改派 {{target}}；同时自动关闭心笔链路与有赞兜底",
            "fr": "Les requêtes de {{min}} s à moins de {{seconds}} s sont redirigées vers {{target}} ; le lien Xinbi et le secours Youzan sont désactivés automatiquement",
            "ja": "{{min}} 秒以上 {{seconds}} 秒未満のリクエストは {{target}} に振り分けられます。Xinbi 回線と Youzan フォールバックは自動的にオフになります",
            "ru": "Запросы от {{min}} с до {{seconds}} с (не включая) переносятся на {{target}}; канал Xinbi и резерв Youzan отключаются автоматически",
            "vi": "Yêu cầu từ {{min}} giây đến dưới {{seconds}} giây được chuyển sang {{target}}; tuyến Xinbi và dự phòng Youzan tự động tắt",
        },
    ),
    (
        "Turn on to route requests up to {{seconds}}s to {{target}} when the Xinbi link cannot take them",
        "Turn on to route requests from {{min}}s to just under {{seconds}}s to {{target}} when neither the Xinbi link nor the Youzan fallback can take them",
        {
            "zh": "打开后，心笔链路与有赞兜底都接不住时，{{min}} 秒到 {{seconds}} 秒（不含）的请求改派 {{target}}",
            "fr": "Activez pour rediriger vers {{target}} les requêtes de {{min}} s à moins de {{seconds}} s lorsque ni le lien Xinbi ni le secours Youzan ne peuvent les prendre",
            "ja": "オンにすると、Xinbi 回線も Youzan フォールバックも受けられない {{min}} 秒以上 {{seconds}} 秒未満のリクエストを {{target}} に振り分けます",
            "ru": "Включите, чтобы переносить на {{target}} запросы от {{min}} с до {{seconds}} с (не включая), когда ни канал Xinbi, ни резерв Youzan не могут их принять",
            "vi": "Bật để chuyển sang {{target}} các yêu cầu từ {{min}} giây đến dưới {{seconds}} giây khi cả tuyến Xinbi và dự phòng Youzan đều không nhận được",
        },
    ),
    (
        "When the Xinbi link cannot take it, requests up to {{seconds}}s route to {{target}}; the Xinbi link takes priority when both are on",
        "When the Xinbi link cannot take it, requests from {{min}}s up to {{seconds}}s route to {{target}}; the Xinbi link takes priority when both are on",
        {
            "zh": "心笔链路接不住时，{{min}}–{{seconds}} 秒的请求改派 {{target}}；两个开关都开时心笔优先",
            "fr": "Lorsque le lien Xinbi ne peut pas les prendre, les requêtes de {{min}} s à {{seconds}} s sont redirigées vers {{target}} ; le lien Xinbi est prioritaire quand les deux sont actifs",
            "ja": "Xinbi 回線が受けられない場合、{{min}}〜{{seconds}} 秒のリクエストは {{target}} に振り分けられます。両方オンのときは Xinbi 回線が優先されます",
            "ru": "Если канал Xinbi не может принять запрос, запросы от {{min}} до {{seconds}} с переносятся на {{target}}; при включении обоих приоритет у канала Xinbi",
            "vi": "Khi tuyến Xinbi không nhận được, yêu cầu từ {{min}} đến {{seconds}} giây được chuyển sang {{target}}; khi bật cả hai, tuyến Xinbi được ưu tiên",
        },
    ),
    (
        "Short requests (up to {{seconds}}s) stay on the source chain when the Xinbi link cannot take them; turn on to reroute them to {{target}}",
        "Short requests from {{min}}s up to {{seconds}}s stay on the source chain when the Xinbi link cannot take them; turn on to reroute them to {{target}}",
        {
            "zh": "心笔链路接不住时，{{min}}–{{seconds}} 秒的短请求留在源链路；打开后改派 {{target}}",
            "fr": "Les requêtes courtes de {{min}} s à {{seconds}} s restent sur le canal source lorsque le lien Xinbi ne peut pas les prendre ; activez pour les rediriger vers {{target}}",
            "ja": "Xinbi 回線が受けられない場合、{{min}}〜{{seconds}} 秒の短いリクエストは元の回線に残ります。オンにすると {{target}} に振り分けます",
            "ru": "Короткие запросы от {{min}} до {{seconds}} с остаются на исходном канале, если канал Xinbi не может их принять; включите, чтобы переносить их на {{target}}",
            "vi": "Yêu cầu ngắn từ {{min}} đến {{seconds}} giây vẫn ở tuyến nguồn khi tuyến Xinbi không nhận được; bật để chuyển sang {{target}}",
        },
    ),
]


def main():
    for lang in ["en", "zh", "fr", "ja", "ru", "vi"]:
        path = os.path.join(LOCALES, lang + ".json")
        with open(path, encoding="utf-8") as fh:
            data = json.load(fh)
        table = data["translation"]
        removed = added = 0
        for old, new, values in PAIRS:
            if old not in table:
                print(f"[{lang}] MISSING old key: {old[:60]}…")
                continue
            del table[old]
            removed += 1
            table[new] = new if lang == "en" else values[lang]
            added += 1
        with open(path, "w", encoding="utf-8", newline="\n") as fh:
            json.dump(data, fh, ensure_ascii=False, indent=2)
            fh.write("\n")
        print(f"[{lang}] removed={removed} added={added} total={len(table)}")


if __name__ == "__main__":
    main()
