-- =============================================================================
-- 上架 minimax_h3-文生 / minimax_h3-图生（SeetaCloud 自建 ComfyUI 实例）
-- 日期：2026-11-06
--
-- 前置：先发版到 146881b6e（含渠道类型 70 与 seeta 适配器），再执行本脚本。
--       顺序不能反 —— 库里先有模型、代码还不认识这个渠道类型时，
--       请求会路由到空适配器，直接 500。
--
-- 执行方式（API Key 不进仓库，走 psql 变量）：
--   psql -U root -d new-api -v sekey='seeta_xxx' -f 本文件
--
-- 商品定义：
--   H3 蒸馏视频模型，**按秒**计费：480P ¥0.03/秒、768p（720P 档）¥0.05/秒。
--   文生视频（minimax_h3-文生）与首帧图生视频（minimax_h3-图生）两个模型，
--   规格相同，价格相同。
--   时长 1-15 秒，6 档画面比例（480p/768p × 横/竖/1:1）。
--   ⚠️ 上游只支持 480p 与 768p 两档，**不支持 1080p**。
--   ⚠️ 参考图只收 data URI —— 网关会把客户的 http(s) 链接先下载再内联下发。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份（回滚依赖）----------
CREATE TABLE IF NOT EXISTS _bak_modelprice_20261106 AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';

-- ---------- 1. 渠道 ----------
-- key 用 psql 变量传入，不落库到仓库文件。
-- 幂等：按 type=70 判重，重复执行不会建出第二个渠道。
INSERT INTO channels (
    type, key, status, name, created_time, base_url, models,
    "group", priority, weight, model_mapping, auto_ban, remark
)
SELECT
    70,
    :'sekey',
    1,
    'SeetaCloud',
    EXTRACT(EPOCH FROM NOW())::bigint,
    'https://u1133789-a936-c07acd4f.westd.seetacloud.com:8443',
    'minimax_h3-文生,minimax_h3-图生',
    'default',
    0,
    0,
    '',
    1,
    'H3 蒸馏视频，480P ¥0.03/秒、768p ¥0.05/秒；时长 1-15 秒'
WHERE NOT EXISTS (SELECT 1 FROM channels WHERE type = 70);

-- ---------- 2. abilities（模型广场与路由都靠它）----------
-- ⚠️ 直改 channels.models 不会自动同步 abilities（只有后台界面保存渠道才重建），
--    漏了这步：广场看不到、路由也匹配不到，表现为「模型不存在」。
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT 'default', m.model, c.id, true, c.priority, c.weight, c.tag
  FROM channels c
  CROSS JOIN (VALUES ('minimax_h3-文生'), ('minimax_h3-图生')) AS m(model)
 WHERE c.type = 70
   AND NOT EXISTS (
       SELECT 1 FROM abilities a
        WHERE a.model = m.model AND a.channel_id = c.id
   );

-- ---------- 3. 模型广场卡片 ----------
INSERT INTO models (
    model_name, description, icon, tags, vendor_id, endpoints,
    status, sync_official, created_time, updated_time, name_rule
)
SELECT
    m.model,
    m.description,
    '',
    '',
    NULL,
    '',
    1,
    0,
    EXTRACT(EPOCH FROM NOW())::bigint,
    EXTRACT(EPOCH FROM NOW())::bigint,
    0
FROM (VALUES
  ('minimax_h3-文生',
   'H3 蒸馏视频模型，纯提示词生成视频。480P ¥0.03/秒、768p（720P 档）¥0.05/秒，**按秒计费**，时长 1-15 秒可选。支持 6 档画面比例（480p/768p × 横屏/竖屏/1:1）。⚠️ 最高只到 768p 档，不支持 1080p。'),
  ('minimax_h3-图生',
   'H3 蒸馏视频模型，用一张参考图作为首帧生成视频。480P ¥0.03/秒、768p（720P 档）¥0.05/秒，**按秒计费**，时长 1-15 秒可选。支持 6 档画面比例（480p/768p × 横屏/竖屏/1:1）。⚠️ 最高只到 768p 档，不支持 1080p。')
) AS m(model, description)
WHERE NOT EXISTS (
    SELECT 1 FROM models mo WHERE mo.model_name = m.model AND mo.deleted_at IS NULL
);

-- ---------- 4. 价格（ModelPrice）----------
-- 0.03 / 7.3 = 0.00410958904109589（内部按 USD 等值存储，站内展示 ¥0.03/秒）。
-- 与 setting/ratio_setting/model_ratio.go 的 defaultModelPrice 必须一致：
-- 代码默认值只在库里查不到时才兜底，两处不一致会出现「新装环境一个价、生产另一个价」。
-- 768p 档的 ¥0.05/秒由适配器的分辨率倍率 1.667 实现，不在 ModelPrice 里单列。
UPDATE options
   SET value = jsonb_set(
         jsonb_set(value::jsonb, '{minimax_h3-文生}', to_jsonb(0.00410958904109589)),
         '{minimax_h3-图生}', to_jsonb(0.00410958904109589))::text
 WHERE key = 'ModelPrice'
   AND NOT (value::jsonb ? 'minimax_h3-文生')
   AND NOT (value::jsonb ? 'minimax_h3-图生');

-- ---------- 校验 ----------
\echo '=== 渠道 ==='
SELECT id, type, name, status, base_url, models
  FROM channels WHERE type = 70;

\echo '=== abilities ==='
SELECT "group", model, channel_id, enabled, priority, weight
  FROM abilities WHERE model IN ('minimax_h3-文生', 'minimax_h3-图生');

\echo '=== 卡片 ==='
SELECT model_name, status, left(description, 60) AS desc_head
  FROM models WHERE model_name IN ('minimax_h3-文生', 'minimax_h3-图生') AND deleted_at IS NULL;

\echo '=== 价格（应显示 0.00410958904109589）==='
SELECT value::jsonb ->> 'minimax_h3-文生' AS 文生_price,
       value::jsonb ->> 'minimax_h3-图生' AS 图生_price
  FROM options WHERE key = 'ModelPrice';

COMMIT;

-- =============================================================================
-- 回滚（按需手执行）：
--   DELETE FROM abilities WHERE model IN ('minimax_h3-文生', 'minimax_h3-图生');
--   DELETE FROM models WHERE model_name IN ('minimax_h3-文生', 'minimax_h3-图生');
--   DELETE FROM channels WHERE type = 70;
--   UPDATE options SET value = (SELECT value FROM _bak_modelprice_20261106 WHERE key='ModelPrice')
--    WHERE key = 'ModelPrice';
-- ⚠️ 回滚后同样要等 ~60 秒路由/配置缓存过期才生效，别急着判定失败。
-- =============================================================================
