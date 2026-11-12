-- 2026-11-12 seedance-2-5 常驻搬迁台账：豆包官网 ch20 → 视频生成车站 ch27（wan3.0-班车）
--
-- ⚠️ 2026-11-12 已在生产执行一次并验证（探针 ROUTE_OK）。**勿重跑**：
--    重跑会用迁移后状态覆盖 _bak_ 备份表，丢掉回滚基线。
--
-- 用户拍板（同日）：「全部走班车 这个模型不路由」+「客户价维持 ¥3.5/次不变」。
-- 只改常驻两处（channels.models 与 abilities 必须成对改），不新增/不修改任何
-- 路由规则；ModelPrice(0.4794520547945206) 不动。
--
-- 执行方式（生产）：
--   scp -P 877 scripts/prod-seedance25-banche-20261112.sql ubuntu@119.29.253.97:/tmp/
--   ssh -p 877 ubuntu@119.29.253.97 'sudo docker exec -i postgres psql -U root -d new-api -v ON_ERROR_STOP=1 -f /dev/stdin < /tmp/prod-seedance25-banche-20261112.sql'
-- 生效：MEMORY_CACHE_ENABLED 下 SyncChannelCache 每 60s 重建路由，等 60s 即可，
-- 无需重启。验证用文末 SELECT（应只剩 ch27 一行）+ 免费 400 探针（见文件尾注）。
--
-- 回滚：从两张 _bak_ 表还原 channels.models 与 abilities 行即可（表结构快照）。

BEGIN;

-- ── 0. 备份（先 DROP，防止复跑到空表把真实备份冲掉）─────────────────────
DROP TABLE IF EXISTS _bak_seed25_banche_abilities_20261112;
CREATE TABLE _bak_seed25_banche_abilities_20261112 AS
SELECT * FROM abilities WHERE model IN ('seedance-2-5', 'wan3.0-班车');

DROP TABLE IF EXISTS _bak_seed25_banche_channels_20261112;
CREATE TABLE _bak_seed25_banche_channels_20261112 AS
SELECT * FROM channels WHERE id IN (20, 27);

-- ── 1. ch20（漫屋ArcReel/豆包）models 移除 seedance-2-5 ─────────────────
UPDATE channels
SET models = (
    SELECT string_agg(x, ',')
    FROM unnest(string_to_array(models, ',')) AS x
    WHERE btrim(x) <> 'seedance-2-5'
)
WHERE id = 20 AND models LIKE '%seedance-2-5%';

-- ── 2. ch27（视频生成车站）models 加入 seedance-2-5（幂等）───────────────
UPDATE channels
SET models = models || ',seedance-2-5'
WHERE id = 27
  AND position(',seedance-2-5' IN ',' || models || ',') = 0;

-- ── 3. abilities：删 ch20 行、插 ch27 行（主键 group+model+channel_id）──
DELETE FROM abilities
WHERE "group" = 'default' AND model = 'seedance-2-5' AND channel_id = 20;

INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight)
VALUES ('default', 'seedance-2-5', 27, true, 0, 1)
ON CONFLICT ("group", model, channel_id) DO NOTHING;

COMMIT;

-- ── 验证 ────────────────────────────────────────────────────────────────
-- 期望：seedance-2-5 只剩 channel_id=27；wan3.0-班车 仍在 27；ch27 models 含两模型。
SELECT "group", model, channel_id, enabled, priority, weight
FROM abilities
WHERE model IN ('seedance-2-5', 'wan3.0-班车')
ORDER BY model, channel_id;

SELECT id, name, models FROM channels WHERE id IN (20, 27);

SELECT (SELECT count(*) FROM _bak_seed25_banche_abilities_20261112) AS bak_abilities,
       (SELECT count(*) FROM _bak_seed25_banche_channels_20261112)  AS bak_channels;
-- 备份期望：abilities=2（ch20 的 seedance-2-5 + ch27 的 wan3.0-班车）、channels=2。

-- 免费探针（缓存同步后执行，400 拒单不扣费不落上游）：
--   POST /v1/videos  body 含 {"model":"seedance-2-5","ratio":"not-a-ratio",...}
--   返回 "unsupported ratio ... adaptive" → 已路由 ch27（yuanmeng 文案）
--   返回 "ratio must be one of auto/..."   → 仍在 ch20（manwu 文案），缓存未同步或迁移未生效
