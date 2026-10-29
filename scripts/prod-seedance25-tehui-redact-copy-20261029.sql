-- 生产：seedance2.5-特惠 的广场卡片描述去货源化（2026-10-29）
--
-- 背景：2026-10-29 把它作为 wan3.0-video-prime 的对外别名重新上架时，卡片描述
-- 写成了「Wan3 Prime 增强画质（seedance2.5-特惠 为同一模型）」—— 等于把真实上游
-- 直接告诉客户，前面整套日志脱敏（消费日志删 upstream_model_name、错误文案抹
-- wan3/有赞/youzan 字样并摘掉地址、任务 DTO 的 Data 置 nil、成片走本站代理）
-- 全部白做：客户拿这句描述去比对，立刻知道该盯哪个模型的日志和报错。
--
-- 改法：描述只讲**它自己**是什么（高清、按秒分档、参考素材上限），不提任何
-- 其它模型名。定价、能力、渠道配置一律不动 —— 本脚本只改 models.id = 61 的
-- description（该卡片的 tags / endpoints / icon 本来就是空的，不动）。
--
-- 同步改动：web/default/src/features/tutorials/content-models.ts（索引行、
-- 小节标题、计费项、hint 四处）+ content-models.test.ts 新增防回归守卫
-- 「seedance2.5-特惠 的对外文案不暴露真实货源」。

BEGIN;

-- 1) 备份原描述
CREATE TABLE IF NOT EXISTS _bak_tehui_card_20261029 AS
SELECT id, model_name, description, now() AS snap_at
  FROM models
 WHERE id = 61;

-- 2) 换文案
UPDATE models
   SET description = '高清视频生成。按分辨率分档：720P ¥0.20/秒、1080P ¥0.30/秒，时长 2-30 秒；支持文生视频、图生视频与多模态参考，参考素材：图 ≤10 张、视频 ≤5 个、音频 ≤5 条，三类合计 ≤20 项。'
 WHERE id = 61
   AND model_name = 'seedance2.5-特惠';

-- 3) 自检：描述里不得再出现任何真实上游字样。命中就整事务回滚，不留半成品。
DO $$
DECLARE
  d text;
BEGIN
  SELECT description INTO d FROM models WHERE id = 61;
  IF d IS NULL THEN
    RAISE EXCEPTION '卡片 61 不存在或描述为空，请先核对模型是否还在售';
  END IF;
  IF d ~* '(wan3|wan-3|wan_3|prime|有赞|youzan|aliyun|万相|seedance2\.5-特惠 为同一模型)' THEN
    RAISE EXCEPTION '卡片描述仍暴露真实货源：%', d;
  END IF;
END $$;

COMMIT;

-- 4) 结果
SELECT id, model_name, description
  FROM models
 WHERE id = 61;

-- 回滚（如需）：
--   UPDATE models m SET description = b.description
--     FROM _bak_tehui_card_20261029 b
--    WHERE m.id = b.id;
--   DROP TABLE _bak_tehui_card_20261029;
