-- 2026-11-12 seedance-2-5 常驻**回迁豆包**：视频生成车站 ch27 → 漫屋ArcReel ch20
--
-- 背景：2026-11-12 上午按「全部走班车」把 seedance-2-5 的常驻渠道从豆包官网
-- （ch20，type 66，https://arcreel.heibaidao.cn）搬到视频生成车站
-- （ch27，type 69，https://yuanmengstudio.com），台账见
-- scripts/prod-seedance25-banche-20261112.sql。
-- 当天下午老板拍板「恢复回去 改为豆包的」，本脚本执行回迁。
--
-- 只改两处（channels.models 与 abilities 必须成对改）：
--   1. ch20.models 加回 seedance-2-5
--   2. ch27.models 移除 seedance-2-5
--   3. abilities：删 ch27 行、插 ch20 行（主键 group+model+channel_id）
--
-- **不动**路由配置（options.Seedance25TeHuiRoute30s）：target_model 仍是
-- seedance-2-5，回迁后规则一改派自然落回 ch20；failover_target_model 仍是
-- wan3.0-班车（ch27），回迁后源与备胎**分属不同渠道**，规则五从「同渠道空转」
-- 变成真正生效的降级链。
-- **不动** ModelPrice / ModelRatio（seedance-2-5 = 0.4794520547945206 按次）。
--
-- 执行方式（生产）：
--   scp -P 877 scripts/prod-seedance25-restore-doubao-20261112.sql \
--       ubuntu@119.29.253.97:/tmp/
--   ssh -p 877 ubuntu@119.29.253.97 \
--     'sudo docker exec -i postgres psql -U root -d new-api -v ON_ERROR_STOP=1 -f /dev/stdin < /tmp/prod-seedance25-restore-doubao-20261112.sql'
--
-- 生效：MEMORY_CACHE_ENABLED 下 SyncChannelCache 每 60s 重建路由，等 60s 即可，
-- 无需重启。
--
-- 回滚：从两张 _bak_ 表还原（表结构快照）：
--   UPDATE channels SET models = (SELECT models FROM _bak_seed25_restore_doubao_channels_20261112 WHERE id=channels.id)
--    WHERE id IN (20,27);
--   DELETE FROM abilities WHERE model='seedance-2-5';
--   INSERT INTO abilities SELECT * FROM _bak_seed25_restore_doubao_abilities_20261112;

BEGIN;

-- ── 0. 备份（IF NOT EXISTS：重跑时保留**回迁前**基线，绝不覆盖回滚依据）──
CREATE TABLE IF NOT EXISTS _bak_seed25_restore_doubao_abilities_20261112 AS
SELECT * FROM abilities WHERE model IN ('seedance-2-5', 'wan3.0-班车');

CREATE TABLE IF NOT EXISTS _bak_seed25_restore_doubao_channels_20261112 AS
SELECT * FROM channels WHERE id IN (20, 27);

-- ── 1. ch20（漫屋ArcReel/豆包）models 加回 seedance-2-5（幂等）───────────
-- 守卫按 ",seedance-2-5," 两侧逗号匹配：只写右侧逗号会把
-- "seedance-2-5-turbo" 这类前缀同名模型误判为已存在。
UPDATE channels
SET models = models || ',seedance-2-5'
WHERE id = 20
  AND position(',seedance-2-5,' IN ',' || models || ',') = 0;

-- ── 2. ch27（视频生成车站）models 移除 seedance-2-5 ─────────────────────
UPDATE channels
SET models = (
    SELECT string_agg(x, ',')
    FROM unnest(string_to_array(models, ',')) AS x
    WHERE btrim(x) <> 'seedance-2-5'
)
WHERE id = 27 AND models LIKE '%seedance-2-5%';

-- ── 3. abilities：删 ch27 行、插 ch20 行 ────────────────────────────────
DELETE FROM abilities
WHERE "group" = 'default' AND model = 'seedance-2-5' AND channel_id = 27;

INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight)
VALUES ('default', 'seedance-2-5', 20, true, 0, 1)
ON CONFLICT ("group", model, channel_id) DO NOTHING;

COMMIT;

-- ── 验证 ────────────────────────────────────────────────────────────────
-- 期望：seedance-2-5 只剩 channel_id=20；wan3.0-班车 仍在 27；
--       ch20.models 含 seedance-2-5，ch27.models 只剩 wan3.0-班车。
SELECT "group", model, channel_id, enabled, priority, weight
FROM abilities
WHERE model IN ('seedance-2-5', 'wan3.0-班车')
ORDER BY model, channel_id;

SELECT id, name, models FROM channels WHERE id IN (20, 27) ORDER BY id;
