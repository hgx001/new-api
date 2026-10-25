-- new-api 生产库：恢复 wan3.0-video-官网（2026-10-25 老板指令）
--
-- 背景：2026-10-23 该模型因上游账号问题被停用——渠道 12「DashScope-Wan3」
-- （type=60 ChannelTypeWan3）的 models 被清空、abilities 行被删，但**其它都没动**：
--   · channels 12 本身 status=1（一直是启用的空渠道）
--   · ModelPrice['wan3.0-video-官网'] = 0.02739726（¥0.20/秒）仍在
--   · models 表卡片 id=26 status=1 仍在，正文写着 480P ¥0.20/s、720P ¥0.45/s、1080P ¥0.80/s
--   · 代码侧全在：wan3/constants.go OfficialModelName、model/pricing.go 的
--     strings.HasPrefix(name, "wan3.0-video") 白名单、model_ratio.go 默认倍率 0.2/USD2RMB
--   · 教程文档也一直在写它（10-23 之后文档与货架不一致，本次一并修好）
-- 所以恢复只需两步：把名字加回 channels.models + 补回 ability 行。
--
-- 原始状态取自 10-23 停用时的备份表（同一份库里的 ground truth）：
--   _bak_stop_wan3official_20261023_channels  → id=12, models='wan3.0-video-官网'
--   _bak_stop_wan3official_20261023_abilities → (default, wan3.0-video-官网, 12, enabled=t,
--                                                priority=0, weight=0, tag='')
--
-- 幂等：已存在则不动。全程单事务 + 备份表。

BEGIN;

-- ---------- 备份当前状态 ----------
CREATE TABLE IF NOT EXISTS _bak_restore_wan3official_20261025_channels AS
  SELECT * FROM channels WHERE id = 12;
CREATE TABLE IF NOT EXISTS _bak_restore_wan3official_20261025_abilities AS
  SELECT * FROM abilities WHERE channel_id = 12;

-- ---------- ① 把模型名加回渠道 12 的 models ----------
-- 空则直接写入；已有其它名字则追加；已包含则跳过（幂等）。
UPDATE channels
   SET models = CASE
         WHEN COALESCE(models, '') = '' THEN 'wan3.0-video-官网'
         ELSE models || ',wan3.0-video-官网'
       END
 WHERE id = 12
   AND NOT (COALESCE(models, '') ~ '(^|,)wan3\.0-video-官网(,|$)');

-- ---------- ② 补回能力行（属性按渠道当前值推导，与 10-23 备份一致）----------
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT c."group", 'wan3.0-video-官网', c.id,
       c.status = 1, c.priority, c.weight, c.tag
  FROM channels c
 WHERE c.id = 12
ON CONFLICT ("group", model, channel_id) DO NOTHING;

COMMIT;

-- ---------- 复核 ----------
\echo '--- ① 渠道 12 现状（models 应含 wan3.0-video-官网，status 应为 1）---'
SELECT id, name, type, status, "group", priority, weight, models FROM channels WHERE id = 12;

\echo '--- ② 能力行（应 1 行 enabled=t）---'
SELECT "group", model, channel_id, enabled, priority, weight FROM abilities
 WHERE channel_id = 12 ORDER BY model;

\echo '--- ③ 价格与卡片（应各 1 行，¥0.20/s）---'
SELECT 'ModelPrice' AS src, e.value, round((e.value::numeric*7.3)::numeric,4) AS cny
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key = 'wan3.0-video-官网'
UNION ALL
SELECT 'models 卡片 id=' || id, model_name, status FROM models
 WHERE model_name = 'wan3.0-video-官网' AND deleted_at IS NULL;

\echo '--- ④ 一致性：abilities 与 channels.models 是否对齐（应 0 行）---'
SELECT a.channel_id, a.model FROM abilities a JOIN channels c ON c.id = a.channel_id
 WHERE NOT (a.model = ANY (string_to_array(c.models, ',')));

-- =====================================================================
-- 回滚（恢复到本次执行前：渠道 12 models 清空、删掉该 ability 行）
-- =====================================================================
--
-- BEGIN;
-- UPDATE channels SET models = '' WHERE id = 12;
-- DELETE FROM abilities WHERE channel_id = 12 AND model = 'wan3.0-video-官网';
-- COMMIT;
--
-- 若需整体还原到执行前快照：
--   UPDATE channels c SET models = b.models
--     FROM _bak_restore_wan3official_20261025_channels b WHERE c.id = b.id;
--   DELETE FROM abilities WHERE channel_id = 12;
--   INSERT INTO abilities SELECT * FROM _bak_restore_wan3official_20261025_abilities;
