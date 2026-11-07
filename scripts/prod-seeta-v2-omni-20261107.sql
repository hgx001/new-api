-- 2026-11-07 SeetaCloud 渠道模型改版：文生/图生合并 + 新增多模态参考
--
-- 上游（ArcReel H3 网关 /v1/models）已切换为：
--   minimax_h3_v2   = FastH3 V2 8 步蒸馏（文生/图生/首尾帧三合一）
--   minimax_h3_omni = FastH3-Omni ref2va（9 图 + 3 视频 + 3 音频 + 1080p 档）
-- 旧名 minimax_h3-文生 / minimax_h3-图生 随旧工作流退役。
-- 适配器代码（relay/channel/task/seeta）保留旧名隐藏别名；本 SQL 同步路由层。
--
-- 执行方式：cat 本文件 | sudo docker exec -i postgres psql -U root -d new-api
-- 回滚： bak_20261107_seeta_channels / bak_20261107_seeta_abilities

BEGIN;

-- 0) 备份（幂等）
CREATE TABLE IF NOT EXISTS bak_20261107_seeta_channels  AS SELECT * FROM channels  WHERE id=28;
CREATE TABLE IF NOT EXISTS bak_20261107_seeta_abilities AS SELECT * FROM abilities WHERE channel_id=28;

-- 1) 通道模型列表换成合并后的两个模型
UPDATE channels SET models='minimax_h3_v2,minimax_h3_omni' WHERE id=28;

-- 2) 旧 abilities 行退役、新行上架（abilities 不会随 channels 自动重建）
DELETE FROM abilities WHERE channel_id=28 AND model IN ('minimax_h3-文生','minimax_h3-图生');
INSERT INTO abilities ("group",model,channel_id,enabled,priority,weight)
SELECT ch."group",m.model,28,true,0,0
FROM channels ch, (VALUES ('minimax_h3_v2'),('minimax_h3_omni')) AS m(model)
WHERE ch.id=28
ON CONFLICT DO NOTHING;

-- 3) options.ModelPrice 追加两个模型的按秒基价（¥0.03/秒 ÷ 7.3）
--    ⚠️ 实际已通过 scripts/merge-modelprice-seeta-20261107.py 以「读现有 JSON 取并集」方式写入，
--    此处仅存档口径，不做整字段覆盖（重复执行本文件时跳过）。
DO $$
DECLARE cur text; merged jsonb;
BEGIN
  SELECT value INTO cur FROM options WHERE key='ModelPrice';
  merged := cur::jsonb
    || jsonb_build_object('minimax_h3_v2',   0.03/7.3)
    || jsonb_build_object('minimax_h3_omni', 0.03/7.3);
  UPDATE options SET value=merged::text WHERE key='ModelPrice';
END $$;

COMMIT;

-- 验证：
--   SELECT model,channel_id,enabled FROM abilities WHERE channel_id=28;
--   SELECT value::jsonb->'minimax_h3_v2', value::jsonb->'minimax_h3_omni' FROM options WHERE key='ModelPrice';
