-- new-api 生产库：下架 wan3.0-video-prime-1080p（2026-10-25 老板指令）
--
-- 注意别和同渠道的 wan3.0-1080p-慢速 混了：那是拼车慢速档 ¥2.00/次，已于同日下架；
-- 本模型是三元巡狩的 WAN 3.0 实时满血档 ¥8.00/次（固定 1080P、固定 30 秒）。
-- 两个都摘掉之后，渠道 23「三元巡狩」会变成空渠道，按 SOP 置 status=2。
--
-- 执行前核实（2026-10-25 13:0x）：
--   · 历史调用 2 次，最近一次 2026-10-16 01:42，近 7 天 0
--   · 在途任务 0（tasks.properties->>'model' 无命中）
--   · 无任何 token 的 model_limits 引用
--   · 渠道 23 的 models 只剩这一个名字
--
-- 代码侧不动（与 10-25 那批下架同口径）：`wan3.0-video-prime-1080p` 是
-- sanyuan / youzanwan3 adaptor 的常量、model/pricing.go 靠 `wan3.0-video` 前缀放行、
-- model_ratio.go 有默认倍率。没有 ability 就没有路由，这些都走不到。
--
-- 客户可见文档同步（前端 tutorials/content-models.ts）：总览条目、计费说明、
-- 耗时实测、metadata.audio 与首尾帧两条通用提示里的 prime 子句、
-- 详情章节 model-wan30-prime、文档目录锚点，全部移除。
--
-- 幂等，单事务 + 备份表。

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_delist_wan3prime_20261025_channels AS
  SELECT * FROM channels WHERE id = 23;
CREATE TABLE IF NOT EXISTS _bak_delist_wan3prime_20261025_abilities AS
  SELECT * FROM abilities WHERE model = 'wan3.0-video-prime-1080p';
CREATE TABLE IF NOT EXISTS _bak_delist_wan3prime_20261025_models AS
  SELECT * FROM models WHERE model_name = 'wan3.0-video-prime-1080p';
CREATE TABLE IF NOT EXISTS _bak_delist_wan3prime_20261025_options AS
  SELECT * FROM options WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio');

-- ---------- ① 从渠道 models 里摘掉 ----------
UPDATE channels
   SET models = array_to_string(
         array_remove(string_to_array(models, ','), 'wan3.0-video-prime-1080p'), ',')
 WHERE models ~ '(^|,)wan3\.0-video-prime-1080p(,|$)';

-- ---------- ② 删能力行 ----------
DELETE FROM abilities WHERE model = 'wan3.0-video-prime-1080p';

-- ---------- ③ 删广场卡片 ----------
DELETE FROM models WHERE model_name = 'wan3.0-video-prime-1080p';

-- ---------- ④ 摘价格键 ----------
UPDATE options
   SET value = (value::jsonb - 'wan3.0-video-prime-1080p')::text
 WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio')
   AND value::jsonb ? 'wan3.0-video-prime-1080p';

-- ---------- ⑤ 渠道 23 被摘空 → 置为手动禁用 ----------
-- 同渠道的 wan3.0-1080p-慢速 已在下架清单里摘掉，这里摘完 models 就是空串。
-- 空串 + status=1 是 model/ability.go 的已知坑（strings.Split 不过滤空值），
-- 一旦有人存该渠道或跑 FixAbility() 就会生成 model='' 的垃圾能力行。
UPDATE channels
   SET status = 2
 WHERE id = 23
   AND status = 1
   AND COALESCE(models, '') = '';

COMMIT;

-- ---------- 复核 ----------
\echo '--- ① 渠道 23（应 models 为空、status=2）---'
SELECT id, name, status, models FROM channels WHERE id = 23;

\echo '--- ② 能力 / 卡片 / 价格（应全 0 行）---'
SELECT 'abilities' AS src, count(*) FROM abilities WHERE model = 'wan3.0-video-prime-1080p'
UNION ALL
SELECT 'cards', count(*) FROM models WHERE model_name = 'wan3.0-video-prime-1080p'
UNION ALL
SELECT 'prices', count(*) FROM options
 WHERE key IN ('ModelPrice','ModelRatio','CompletionRatio')
   AND value::jsonb ? 'wan3.0-video-prime-1080p';

\echo '--- ③ 全库一致性（应 0 行）---'
SELECT a.channel_id, a.model FROM abilities a JOIN channels c ON c.id = a.channel_id
 WHERE NOT (a.model = ANY (string_to_array(c.models, ',')))
UNION ALL
SELECT c.id, m.model FROM channels c, unnest(string_to_array(c.models, ',')) AS m(model)
 WHERE m.model <> ''
   AND NOT EXISTS (SELECT 1 FROM abilities a WHERE a.channel_id = c.id AND a.model = m.model);

\echo '--- ④ 三元巡狩渠道是否还有别的模型在售（应 0）---'
SELECT model, enabled FROM abilities WHERE channel_id = 23;

-- =====================================================================
-- 回滚（整体恢复到执行前）
-- =====================================================================
--
-- BEGIN;
-- UPDATE channels c SET models = b.models, status = b.status
--   FROM _bak_delist_wan3prime_20261025_channels b WHERE c.id = b.id;
-- INSERT INTO abilities SELECT * FROM _bak_delist_wan3prime_20261025_abilities
--   ON CONFLICT DO NOTHING;
-- INSERT INTO models SELECT * FROM _bak_delist_wan3prime_20261025_models
--   ON CONFLICT (id) DO NOTHING;
-- UPDATE options
--    SET value = jsonb_set(value::jsonb, '{wan3.0-video-prime-1080p}',
--                          to_jsonb(8.0 / 7.3::numeric))::text
--  WHERE key = 'ModelPrice';
-- COMMIT;
--
-- ⚠️ 前端教程文案是另一次发版改的，回滚数据不会把文档带回来，要 revert 对应 commit 再发前端。
