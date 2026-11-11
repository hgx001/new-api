-- new-api 生产库：seedance2.5-活动 常驻渠道从心笔切回有赞（2026-11-11）
--
-- 需求：有赞原生 720P 效果更好，活动版不再走心笔的「480P 生成 + 增强」链路。
--
-- 变更范围（只动活动版）：
--   ① channels.models(ch17 有赞) 追加 seedance2.5-活动；
--   ② abilities 新增 (default, seedance2.5-活动, 17)，参数逐字复制母本
--      (default, seedance2.5-特惠, 17) 的行；
--   ③ abilities 删除 (default, seedance2.5-活动, 25 心笔) 的行。
--
-- 不动：ch25 / ch17 的其它模型、母本 seedance2.5-特惠 的能力行、
--       活动版的 ModelPrice、条件路由策略（9:16 关闭与豆包改派照旧）。
--
-- ⚠️ 必须先发版 63e18bb31（删除「活动版参考视频改派有赞」规则）：
--    旧二进制虽能跑，但会先把参考视频请求改写成特惠，多绕一次素材搬运。
-- ⚠️ routes 由 SyncChannelCache 每 60s 重建；执行完等 ≥90s 再验证。
--
-- 回滚见文件尾。

BEGIN;

-- ① 备份渠道行与活动版能力行
CREATE TABLE IF NOT EXISTS _bak_seedance25_huodong_youzan_20261111 (
  kind      text NOT NULL,
  ref_id    text NOT NULL,
  payload   jsonb,
  backup_at timestamptz DEFAULT now(),
  PRIMARY KEY (kind, ref_id)
);

INSERT INTO _bak_seedance25_huodong_youzan_20261111 (kind, ref_id, payload)
SELECT 'channel', id::text, to_jsonb(c) FROM channels c WHERE id IN (17, 25)
ON CONFLICT (kind, ref_id) DO UPDATE
  SET payload = EXCLUDED.payload, backup_at = now();

INSERT INTO _bak_seedance25_huodong_youzan_20261111 (kind, ref_id, payload)
SELECT 'ability_activity', "group" || '|' || model || '|' || channel_id::text, to_jsonb(a)
  FROM abilities a WHERE model = 'seedance2.5-活动'
ON CONFLICT (kind, ref_id) DO UPDATE
  SET payload = EXCLUDED.payload, backup_at = now();

-- ② 有赞渠道模型清单追加活动版（幂等）
UPDATE channels
   SET models = CASE
         WHEN models LIKE '%seedance2.5-活动%' THEN models
         ELSE models || ',seedance2.5-活动'
       END
 WHERE id = 17;

-- ③ 新增 ch17 能力行：参数逐字复制母本，避免优先级/权重漂移
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'seedance2.5-活动', 17, enabled, priority, weight, tag
  FROM abilities
 WHERE model = 'seedance2.5-特惠' AND channel_id = 17
ON CONFLICT ("group", model, channel_id) DO UPDATE
  SET enabled  = EXCLUDED.enabled,
      priority = EXCLUDED.priority,
      weight   = EXCLUDED.weight,
      tag      = EXCLUDED.tag;

-- ④ 删除心笔能力行：活动版不再走 ch25
DELETE FROM abilities
 WHERE model = 'seedance2.5-活动' AND channel_id = 25;

-- ⑤ 心笔渠道模型清单移除活动版（能力声明与能力行必须一致，
--    否则 next 同步/人工按 channels.models 重建 abilities 时会把活动版带回心笔）
UPDATE channels
   SET models = array_to_string(
         array_remove(string_to_array(models, ','), 'seedance2.5-活动'), ',')
 WHERE id = 25
   AND models LIKE '%seedance2.5-活动%';

COMMIT;

-- ⑥ 校验：活动版应只剩 ch17；两个渠道的模型清单状态
\echo '--- 活动版路由 ---'
SELECT a."group", a.model, a.channel_id, a.enabled, a.priority, a.weight
  FROM abilities a WHERE a.model = 'seedance2.5-活动' ORDER BY a.channel_id;

\echo '--- 渠道模型清单是否含活动版 ---'
SELECT id, name, models LIKE '%seedance2.5-活动%' AS has_huodong FROM channels WHERE id IN (17, 25);

-- =====================================================================
-- 回滚：恢复渠道清单与活动版能力行（两条一起删掉 ch17 行、恢复 ch25 行）
-- =====================================================================
-- BEGIN;
-- UPDATE channels c SET models = (b.payload ->> 'models')
--   FROM _bak_seedance25_huodong_youzan_20261111 b
--  WHERE b.kind = 'channel' AND b.ref_id = c.id::text AND c.id IN (17, 25);
-- DELETE FROM abilities WHERE model = 'seedance2.5-活动';
-- INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
-- SELECT payload ->> 'group', payload ->> 'model', (payload ->> 'channel_id')::bigint,
--        (payload ->> 'enabled')::boolean, (payload ->> 'priority')::bigint,
--        (payload ->> 'weight')::bigint, payload ->> 'tag'
--   FROM _bak_seedance25_huodong_youzan_20261111
--  WHERE kind = 'ability_activity';
-- COMMIT;
