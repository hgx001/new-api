-- 补充清洗：图标与未上广场但已写死的描述（2026-10-13）
--
-- 1) icon = 'Doubao.Color' 本身就是豆包品牌标识，6 张**在售**卡片挂着它，
--    文字洗掉了但图标照样告诉客户「这家是豆包」。清成空（wan 系列本来就是
--    空图标且渲染正常，证明空值安全）。
-- 2) wan3.0-video 描述里写了「阿里云百炼」，不匹配上一轮的正则所以漏网；
--    该模型当前未上广场，但描述仍在库里，将来放开就会漏。

BEGIN;

CREATE TABLE IF NOT EXISTS _bak_icon_scrub_models_20261013 AS
  SELECT model_name, description, icon FROM models WHERE deleted_at IS NULL;

-- ---------- 洗掉豆包品牌图标 ----------
UPDATE models SET icon = NULL
 WHERE icon = 'Doubao.Color' AND deleted_at IS NULL;

-- ---------- 阿里云百炼 ----------
UPDATE models SET description = 'Wan3.0 视频生成。支持 480P（¥2.66/s）和 720P（¥5.20/s），通过 metadata.resolution 参数控制，默认 480P。时长 2-30 秒。'
 WHERE model_name = 'wan3.0-video' AND deleted_at IS NULL;

UPDATE models SET description = 'Wan3.0 视频生成 2-30 秒，480P/720P/1080P'
 WHERE model_name = 'aliyun:wan-3.0' AND deleted_at IS NULL;

COMMIT;

\echo '=== 复查：可见卡片里是否还有任何供应商标识（描述或图标）==='
SELECT m.model_name, m.description, m.icon
  FROM models m
  LEFT JOIN (SELECT DISTINCT model FROM abilities WHERE enabled) a ON a.model = m.model_name
 WHERE m.deleted_at IS NULL AND a.model IS NOT NULL
   AND (m.description ~ '漫屋|豆包|Dola|有赞|二春|Erchun|官网|即梦|Gemini|Veo|官方|阿里云|百炼'
     OR m.icon IN ('Doubao.Color','Doubao','Aliyun'))
 ORDER BY m.model_name;

\echo '=== 剩余 Doubao.Aliyun 图标归属 ==='
SELECT model_name, icon, (SELECT count(*) FROM abilities ab WHERE ab.model = models.model_name AND ab.enabled) AS enabled_abilities
  FROM models WHERE deleted_at IS NULL AND icon IN ('Doubao.Color','Doubao','Aliyun')
 ORDER BY model_name;

-- 回滚：
--   UPDATE models m SET icon = b.icon, description = b.description
--     FROM _bak_icon_scrub_models_20261013 b WHERE b.model_name = m.model_name;