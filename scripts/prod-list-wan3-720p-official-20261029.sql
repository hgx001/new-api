-- new-api 生产库：上架 wan3.0-720p-官网（2026-10-29）
--
-- 是什么：一个**新的对外模型名**，输出固定 720P，售价 ¥0.25/秒。
-- 与既有模型名的分工：
--   wan3.0-video-官网  →  上游直接生成 480P / 720P / 1080P，按档位计价
--                          （480P ¥0.20/s、720P ¥0.45/s、1080P ¥0.80/s）
--   wan3.0-720p-官网   →  上游只生成 480P，成片再经一道处理出 720P，
--                          统一价 ¥0.25/s（单一价位，不乘分辨率倍率）
--
-- ⚠️ 定价依据（成本核算，2026-10-29 用官方计费文档核实）：
--   上游 480P 生成      ¥0.20/秒
--   成片处理            ¥0.0067/秒（MPS 超分 0.3 元/分钟 + 极速高清转码 0.099 元/分钟）
--   合计成本            ¥0.2067/秒 → 售价 ¥0.25/秒（毛利约 17%）
--   比官网原生 720P（¥0.45/秒）便宜 44%。
--
-- ⚠️ 执行顺序：**必须先发版**（后端 commit 含 wan3.0-720p-官网 的路由、
--    计费倍率恒 1.0、白名单放行），再跑本脚本。只跑本脚本不改代码 →
--    请求会被路由到通用 wan3 逻辑，size 倍率落进 1/2/4 表，用户传 720P 时
--    价格算成 ¥0.50/秒（翻倍），成片也不是本档位该出的片子。
--
-- 四件事必须同时做（缺一不可）：
--   ① channels.models 追加名字（否则 FixAbility 重建能力行时不会带上它）
--   ② abilities 插行（否则路由不到渠道）
--   ③ models 广场卡片（否则广场看不到）
--   ④ ModelPrice 写键（否则算不出价）
--
-- 全程单事务 + 备份表，幂等可重复执行。

BEGIN;

-- ---------- 备份 ----------
-- models 备份此刻应为空表：这正是回滚的基线（回滚 = DELETE 新增行）。
CREATE TABLE IF NOT EXISTS _bak_wan3720p_20261029_channels AS
  SELECT * FROM channels WHERE id = 12;

CREATE TABLE IF NOT EXISTS _bak_wan3720p_20261029_abilities AS
  SELECT * FROM abilities WHERE channel_id = 12;

CREATE TABLE IF NOT EXISTS _bak_wan3720p_20261029_models AS
  SELECT * FROM models WHERE model_name = 'wan3.0-720p-官网';

CREATE TABLE IF NOT EXISTS _bak_wan3720p_20261029_options AS
  SELECT * FROM options WHERE key = 'ModelPrice';

-- ---------- ① channels.models：追加模型名 ----------
-- 路由源头。只改 abilities 的话，渠道一旦被后台编辑，model.UpdateAbilities 会按
-- channels.models 删光重建 abilities → 新模型消失。**必须先改这里。**
-- 用追加而非整体覆写：避免把执行前别人新增的模型名冲掉。
-- 守卫条件保证重复执行不会拼接出两份模型名（幂等）。
UPDATE channels
   SET models = CASE
         WHEN COALESCE(models, '') = '' THEN 'wan3.0-720p-官网'
         ELSE models || ',wan3.0-720p-官网'
       END
 WHERE id = 12
   AND NOT (COALESCE(models, '') ~ '(^|,)wan3\.0-720p-官网(,|$)');

-- ---------- ② abilities：新增一行能力 ----------
-- 同渠道既有行（group=default / enabled=t / priority=0 / weight=0 / tag=NULL）原样复制。
-- 模板行取 wan3.0-video-官网（同渠道同类型），NOT EXISTS 守卫保证不产生重复行。
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'wan3.0-720p-官网', channel_id, enabled, priority, weight, tag
  FROM abilities
 WHERE channel_id = 12 AND model = 'wan3.0-video-官网'
   AND NOT EXISTS (SELECT 1 FROM abilities
                    WHERE channel_id = 12 AND model = 'wan3.0-720p-官网');

-- ---------- ③ models：模型广场卡片（description = 广场正文）----------
-- ⚠️ 正文**不提**「超分 / 增强 / 腾讯云 / 转码」等内部实现词
--    （2026-10-29 老板明确要求）：客户只需要知道这个模型出 720P、多少钱。
--    露出实现细节等于把成本结构和真实货源写在脸上，客户会拿
--    「这不是原生 720P」来质疑定价。
-- icon / tags / vendor_id / endpoints 从 wan3.0-video-官网 复制，保持视觉与筛选一致。
-- sync_official=0：这个卡片是本地自定义的，不参与官方模型同步覆盖
-- （否则一次同步就可能把正文改掉）。
INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints,
                    status, sync_official, created_time, updated_time, name_rule)
SELECT 'wan3.0-720p-官网',
       'Wan3.0 视频生成，固定输出 720P。支持文生视频、图生视频与多模态参考（图 / 视频 / 音频），时长 2-30 秒。计费按秒 ¥0.25，单一价位，不随分辨率变化。⚠️ 带参考视频时按「生成秒数 + 参考视频总秒数（四舍五入）」× ¥0.25 计费，例如 10 秒参考视频 + 生成 10 秒 = ¥5.00（不传参考视频只要 ¥2.50）。',
       m.icon, m.tags, m.vendor_id, m.endpoints, 1, 0,
       EXTRACT(EPOCH FROM now())::bigint, EXTRACT(EPOCH FROM now())::bigint, 0
  FROM models m
 WHERE m.model_name = 'wan3.0-video-官网'
   AND m.deleted_at IS NULL
 LIMIT 1;

-- 兜底：源卡片不存在时插一条最小可用的（icon / vendor_id 留空，前端回落默认图标）。
INSERT INTO models (model_name, description, status, sync_official, created_time, updated_time, name_rule)
SELECT 'wan3.0-720p-官网',
       'Wan3.0 视频生成，固定输出 720P。支持文生视频、图生视频与多模态参考（图 / 视频 / 音频），时长 2-30 秒。计费按秒 ¥0.25，单一价位，不随分辨率变化。',
       1, 0, EXTRACT(EPOCH FROM now())::bigint, EXTRACT(EPOCH FROM now())::bigint, 0
 WHERE NOT EXISTS (
       SELECT 1 FROM models
        WHERE model_name = 'wan3.0-720p-官网' AND deleted_at IS NULL);

-- ---------- ④ options.ModelPrice：写单价 ----------
-- 单位 USD 等值；换算 1 元 = 1/7.3 USD，与 wan3.0-video-官网 同口径。
-- ¥0.25/秒 → 0.25/7.3 = 0.0342465753424658
-- 计费链路（任务模型）：quota = int(ModelPrice × QuotaPerUnit) × seconds × size
--   - seconds：生成秒数 + 参考视频总秒数（四舍五入）→ 代码
--   - size：**恒为 1.0**（本档只有 720P 一个输出档位，价格是复合价）→ 代码
-- 复核值（QuotaPerUnit=500000，Price=7.3）：10 秒 = ¥2.50；+10 秒参考 = ¥5.00。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{wan3.0-720p-官网}', to_jsonb(0.25 / 7.3::numeric))::text
 WHERE key = 'ModelPrice'
   AND COALESCE((value::jsonb ->> 'wan3.0-720p-官网')::numeric, -1) <> (0.25 / 7.3)::numeric;

COMMIT;

-- ---------- 复核（事务外只读，脚本跑完必须逐项核对）----------
\echo '--- ① 渠道 12（models 应含 wan3.0-video-官网 与 wan3.0-720p-官网，status=1）---'
SELECT id, name, type, status, "group", priority, weight, models FROM channels WHERE id = 12;

\echo '--- ② 能力行（应 2 行，都 enabled=t）---'
SELECT "group", model, channel_id, enabled, priority, weight FROM abilities
 WHERE channel_id = 12 ORDER BY model;

\echo '--- ③ 广场卡片（应 1 行）---'
SELECT model_name, icon, tags, vendor_id, status, sync_official,
       left(description, 60) AS description_head
  FROM models WHERE model_name = 'wan3.0-720p-官网' AND deleted_at IS NULL;

\echo '--- ④ 价格键（应等于 0.0342465753424658，×7.3 = 0.25）---'
SELECT e.key, e.value, round((e.value::numeric * 7.3)::numeric, 4) AS cny
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key = 'wan3.0-720p-官网';

\echo '--- ⑤ 一致性：abilities 与 channels.models 是否对齐（应 0 行）---'
SELECT a.channel_id, a.model FROM abilities a JOIN channels c ON c.id = a.channel_id
 WHERE NOT (a.model = ANY (string_to_array(c.models, ',')));

\echo '--- ⑥ 卡片正文不得出现内部实现词（应 0 行）---'
SELECT model_name FROM models
 WHERE model_name = 'wan3.0-720p-官网' AND deleted_at IS NULL
   AND (description LIKE '%超分%' OR description LIKE '%增强%'
        OR description LIKE '%腾讯%' OR description LIKE '%MPS%'
        OR description LIKE '%转码%');

-- =====================================================================
-- 回滚 SQL（仅在上架后发现错误时使用；反向执行，逐项注释说明）
-- =====================================================================
--
-- BEGIN;
-- -- ④ 摘除单价
-- UPDATE options
--    SET value = (value::jsonb - 'wan3.0-720p-官网')::text
--  WHERE key = 'ModelPrice';
-- -- ③ 摘除广场卡片
-- DELETE FROM models WHERE model_name = 'wan3.0-720p-官网';
-- -- ② 摘除能力行
-- DELETE FROM abilities WHERE channel_id = 12 AND model = 'wan3.0-720p-官网';
-- -- ① 摘除路由模型名（保留其余模型名，只删这一个）
-- UPDATE channels
--    SET models = array_to_string(
--          array_remove(string_to_array(models, ','), 'wan3.0-720p-官网'), ',')
--  WHERE id = 12;
-- COMMIT;
--
-- 若需整体回退到执行前状态：
--   channels  → UPDATE channels c SET models = b.models
--                 FROM _bak_wan3720p_20261029_channels b WHERE c.id = b.id;
--   abilities → DELETE FROM abilities WHERE channel_id = 12;
--                INSERT INTO abilities SELECT * FROM _bak_wan3720p_20261029_abilities;
--   models    → DELETE FROM models WHERE model_name = 'wan3.0-720p-官网';
--                INSERT INTO models SELECT * FROM _bak_wan3720p_20261029_models;
--   options   → UPDATE options SET value = (SELECT value FROM _bak_wan3720p_20261029_options) WHERE key = 'ModelPrice';
--
-- ⚠️ 若同时回滚代码，记得把 model/pricing.go 的白名单放行、
--    setting/ratio_setting/model_ratio.go 的 ModelPrice 默认值、
--    前端 isPerSecondModel / getWan3SampleResolution / mock-stats 参数表
--    一并撤掉。
