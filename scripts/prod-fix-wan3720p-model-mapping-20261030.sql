-- 补齐渠道 12 的 model_mapping：新增 wan3.0-720p-官网 → wan3.0-video
--
-- 背景（2026-10-30）
-- ────────────
-- 渠道 12（DashScope-Wan3）的 model_mapping 原本只有：
--     {"wan3.0-video-官网": "wan3.0-video"}
--
-- 10-29 上架 wan3.0-720p-官网 时**漏配**了这条映射。
--
-- 后果（代码侧已兜底，这里是第二道保险）：
--   渠道映射里没有这个键 → ModelMappedHelper 的 IsModelMapped 保持 false
--   → info.UpstreamModelName 仍是「wan3.0-720p-官网」
--   → 上游收到一个它根本不认识的模型名 → 直接拒单。
--   表现为「用户付了钱、上游报错」，且**每次都失败**。
--
-- 代码侧已在 relay/channel/task/wan3/constants.go 加了 upstreamModelFor 兜底
--（官网这两个对外名的上游语义固定是标准版 wan3.0-video，不依赖可配的渠道配置），
-- 所以本 SQL 不是「修 bug 的唯一手段」，而是把渠道配置补齐到自洽状态：
-- 管理员在后台看渠道配置时不该看到一个「有个模型名没映射」的渠道。
--
-- ⚠️ 为什么不在代码兜底就完事：渠道 model_mapping 是管理员可见可改的配置。
--   配置与实际行为不一致时，管理员改了别的映射、或者「同步上游模型」覆盖了它，
--   就会出现「后台看着配好了，线上却不生效」的诡异状态。两边对齐最省心。
--
-- 幂等：重复执行结果相同。
-- 回滚：见文件末尾。

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_wan3720p_mapping_20261030_channels AS
  SELECT * FROM channels WHERE id = 12;

-- ---------- 补映射 ----------
-- 用 jsonb 合并而不是字符串替换：避免手写 JSON 时漏引号/漏逗号。
-- 已有键不覆盖（jsonb 合并语义：后面的覆盖前面的，所以把新键放前面不行，
-- 这里用 || 让已有的赢 —— 但我们本来就希望新键加入而非覆盖，
-- 所以改成分别判断：已有键保留原值，新键追加）。
UPDATE channels
SET model_mapping = (
      CASE
        WHEN model_mapping IS NULL OR btrim(model_mapping) = '' THEN
          '{"wan3.0-720p-官网": "wan3.0-video"}'::jsonb
        ELSE
          -- 已有键优先保留（不覆盖管理员配的值），新键只补缺失的
          (model_mapping::jsonb) ||
          (CASE WHEN (model_mapping::jsonb) ? 'wan3.0-720p-官网'
                THEN '{}'::jsonb
                ELSE '{"wan3.0-720p-官网": "wan3.0-video"}'::jsonb END)
      END
    )::text
WHERE id = 12;

-- ---------- 复核 ----------
\echo '--- ① 渠道 12 的 model_mapping（应含两个键，都指向 wan3.0-video）---'
SELECT id, name, model_mapping FROM channels WHERE id = 12;

\echo '--- ② 两个官网名都能映射到上游标准版 ---'
SELECT kv.key AS 对外模型名, kv.value AS 上游模型名
FROM channels c, LATERAL jsonb_each(c.model_mapping::jsonb) kv
WHERE c.id = 12 AND kv.key IN ('wan3.0-video-官网', 'wan3.0-720p-官网')
ORDER BY kv.key;

\echo '--- ③ 映射后必须仍是上游认识的名字（不能是带 -官网 的对外名）---'
-- kv.value 是 jsonb，要用 ->> 取文本再比 LIKE（直接 LIKE 会报 operator does not exist）
SELECT count(*) AS 仍有对外名残留_应为0
FROM channels c, LATERAL jsonb_each(c.model_mapping::jsonb) kv
WHERE c.id = 12 AND (kv.value #>> '{}') LIKE '%官网%';

\echo '--- ④ 渠道 models 仍含两个对外名 ---'
SELECT id, name,
       (models LIKE '%wan3.0-video-官网%')  AS 含官网标准版,
       (models LIKE '%wan3.0-720p-官网%')  AS 含官网720P档
FROM channels WHERE id = 12;

COMMIT;

-- ---------- 回滚 ----------
-- UPDATE channels
-- SET model_mapping = b.model_mapping
-- FROM _bak_wan3720p_mapping_20261030_channels b
-- WHERE channels.id = b.id;
