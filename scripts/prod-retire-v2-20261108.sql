-- new-api 生产库：minimax_h3_v2 退役下架（2026-11-08）
--
-- 背景：ArcReel-H3 网关侧已下架 V2（/v1/models 只留 omni，V2 提交 404），
-- Omni 完整覆盖 V2 的文生 / 图生 / 首尾帧，并额外支持 9 图 + 3 视频 +
-- 3 音频参考与 1080p。本脚本把站端剩下的四处 V2 痕迹清干净。
--
-- 前置（AGENTS.md：模型变更先发版、再改渠道/价表数据）：
--   后端已发版到「V2 不在 ModelList / 不进广场白名单 / 无代码默认价」的提交。
--
-- 范围（最小操作，只动 V2）：
--   1) channels id=28 的 models 逗号串去掉 v2，保留 omni
--   2) abilities 里 ch28 / v2 这一行删除（有备份表可恢复）
--   3) models 表 id=71 广场条目软下线（与 67/68 旧名下架同口径）
--   4) options.ModelPrice 删掉 minimax_h3_v2 一个键
-- 不动：minimax_h3_omni、两个已退役旧中文名、其它渠道/其它公开模型。
--
-- ⚠️ ModelPrice 是「整表替换」语义（types.LoadFromJsonStringWithCallback）：
-- 生产 option 里没有的键会被代码默认值冲掉。因此代码 model_ratio.go 里
-- 的 minimax_h3_v2 默认价必须一并删掉，否则删了 option 键也会被默认值复活。
--
-- 执行：ssh -p 877 ubuntu@119.29.253.97
--       sudo docker exec -i postgres psql -U root -d new-api < 本文件
-- 回滚：见文件末尾。

BEGIN;

-- ---------- 备份（幂等：重复执行不会覆盖首次快照） ----------
CREATE TABLE IF NOT EXISTS _bak_v2retire_channels_20261108 AS
  SELECT * FROM channels WHERE id = 28;

CREATE TABLE IF NOT EXISTS _bak_v2retire_abilities_20261108 AS
  SELECT * FROM abilities WHERE channel_id = 28 AND model = 'minimax_h3_v2';

CREATE TABLE IF NOT EXISTS _bak_v2retire_models_20261108 AS
  SELECT * FROM models WHERE id = 71 AND model_name = 'minimax_h3_v2';

CREATE TABLE IF NOT EXISTS _bak_v2retire_options_20261108 AS
  SELECT * FROM options WHERE key = 'ModelPrice';

-- ---------- 1. 渠道模型清单 ----------
-- 精确匹配整串才改：若运营期间手工调整过，宁可不改也不要覆盖掉别人的编辑。
UPDATE channels
   SET models = 'minimax_h3_omni'
 WHERE id = 28
   AND models = 'minimax_h3_v2,minimax_h3_omni';

-- ---------- 2. abilities ----------
DELETE FROM abilities
 WHERE channel_id = 28
   AND model = 'minimax_h3_v2';

-- ---------- 3. 广场模型条目（软下线，不物理删除） ----------
UPDATE models
   SET status = 0,
       sync_official = 0,
       deleted_at = now(),
       updated_time = extract(epoch from now())::bigint
 WHERE id = 71
   AND model_name = 'minimax_h3_v2'
   AND deleted_at IS NULL;

-- ---------- 4. ModelPrice ----------
UPDATE options
   SET value = (value::jsonb - 'minimax_h3_v2')::text
 WHERE key = 'ModelPrice'
   AND value::jsonb ? 'minimax_h3_v2';

COMMIT;

-- ---------- 验证（COMMIT 后另跑） ----------
-- SELECT id, models FROM channels WHERE id = 28;
-- SELECT * FROM abilities WHERE channel_id = 28;
-- SELECT id, model_name, status, deleted_at FROM models WHERE model_name LIKE 'minimax_h3%';
-- SELECT t.key FROM options o, jsonb_each_text(o.value::jsonb) t(key,value)
--  WHERE o.key='ModelPrice' AND t.key LIKE '%minimax_h3_v2%';
--
-- ⚠️ 直接 SQL 改库不会触发应用内定价缓存失效，跑完需 docker restart new-api
--    （几秒）并复验 /api/status。

-- ---------- 回滚 ----------
-- BEGIN;
-- UPDATE channels c SET models = b.models
--   FROM _bak_v2retire_channels_20261108 b WHERE c.id = 28 AND b.id = 28;
-- INSERT INTO abilities SELECT * FROM _bak_v2retire_abilities_20261108
--   ON CONFLICT ("group", model, channel_id) DO NOTHING;
-- UPDATE models m SET status = b.status, sync_official = b.sync_official,
--        deleted_at = b.deleted_at, updated_time = b.updated_time
--   FROM _bak_v2retire_models_20261108 b WHERE m.id = 71 AND b.id = 71;
-- UPDATE options o SET value = b.value
--   FROM _bak_v2retire_options_20261108 b WHERE o.key = 'ModelPrice' AND b.key = 'ModelPrice';
-- COMMIT;
-- 回滚后还要：网关侧设 ARCREEL_ENABLE_V2_MODEL=1 并重启；代码层恢复
-- ModelList / isAllowedPricingModel / model_ratio.go 的 V2 条目后重新发版。
