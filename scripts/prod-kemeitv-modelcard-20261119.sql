-- ---------------------------------------------------------------------------
-- 客美 Kemeitv 模型卡片文案补音频格式约束（2026-11-19 真机实测后）
--
-- 背景：真机发现上游**按文件后缀**判素材格式，参考音频只收 wav / mp3。
-- 传 .m4a 会被上游拒（"format m4a is not supported"），且拒绝发生在建单之后
-- （钱已预扣、客户等退款）。网关已在校验阶段前置拦截，但客户看到的应当是
-- 卡片文案里写明的约束，而不是撞一次才知道。
--
-- 幂等：可重复执行。
-- ---------------------------------------------------------------------------

BEGIN;

UPDATE models
SET description = replace(
        description,
        '参考素材支持图片（最多 10）、视频（最多 5）、音频（最多 5），均须公网 http(s) URL。',
        '参考素材支持图片（最多 10，jpg/jpeg/png/bmp/webp）、视频（最多 5，mp4/mov）、音频（最多 5，wav/mp3），均须公网 http(s) URL。'
    )
WHERE model_name IN ('wan3.0-video-tj', 'wan3.0-video-prime-tj')
  AND description LIKE '%音频（最多 5），均须公网 http(s) URL。%';

COMMIT;
