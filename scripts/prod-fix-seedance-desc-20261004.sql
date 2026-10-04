-- 模型广场描述修正：两个 seedance 模型都继承了旧行文案
-- 「漫屋 Seedance 2.5 文/图生视频 5-30秒」，与实际参数不符（2.0 是 5/10/15 秒，2.5 固定 30 秒 720P）。
-- 文案风格对齐同厂商邻居行（gemini-web-video / Nano Banana Pro：中文短句，不重复价格，卡片单独显示价格）。
BEGIN;

CREATE TABLE IF NOT EXISTS _bak_seedance_desc_20261004 AS
  SELECT model_name, description FROM models
 WHERE model_name IN ('seedance-2.0', 'seedance-2.5');

UPDATE models
   SET description = '漫屋 Dola Seedance 2.0 文/图生视频，5/10/15 秒，6 档比例，≤10 张参考图',
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'seedance-2.0' AND deleted_at IS NULL;

UPDATE models
   SET description = '漫屋 Dola Seedance 2.5 文/图生视频，固定 30 秒 720P，6 档比例，≤10 张参考图',
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'seedance-2.5' AND deleted_at IS NULL;

COMMIT;

SELECT model_name, description FROM models
 WHERE model_name IN ('seedance-2.0', 'seedance-2.5') AND deleted_at IS NULL
 ORDER BY model_name;
