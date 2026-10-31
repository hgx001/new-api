-- 2026-10-31 给「参考视频按秒计费」的模型补上对外说明
--
-- 背景：wan3 / 有赞两条链路的计费秒数 = 生成秒数 + 参考视频总秒数，
-- 但客户在模型卡上只看到「¥0.20/秒」，不知道传 15 秒参考视频会按 17 秒扣。
-- 已经写过的两个模型（wan3.0-720p-特惠 / wan3.0-video-官网）保留原样，只补 15 秒上限；
-- 有赞链路三个模型与缺失元数据的 wan3.0-720p-官网 补齐完整说明。
--
-- 幂等：所有语句都带「不含关键串才写」的守卫，重复执行不会叠加文案。

BEGIN;

-- 1) wan3.0-720p-官网：没有 models 行，补一条。
--    定价与 wan3.0-720p-特惠 完全一致（ModelPrice 同值、倍率恒 1.0、固定 720P）。
INSERT INTO models (model_name, description, status, sync_official, created_time, updated_time, name_rule)
SELECT
  'wan3.0-720p-官网',
  'Wan3.0 视频生成，固定输出 720P。支持文生视频、图生视频与多模态参考（图 / 视频 / 音频），时长 2-30 秒。'
  '计费按秒 ¥0.20，单一价位，不随分辨率变化。'
  '⚠️ 带参考视频时按「生成秒数 + 参考视频总秒数（四舍五入）」× ¥0.20 计费，'
  '例如 10 秒参考视频 + 生成 10 秒 = ¥4.00（不传参考视频只要 ¥2.00）。参考视频单条最长 15 秒。',
  1, 0, extract(epoch from now())::bigint, extract(epoch from now())::bigint, 0
WHERE NOT EXISTS (
  SELECT 1 FROM models WHERE model_name = 'wan3.0-720p-官网' AND deleted_at IS NULL
);

-- 2) seedance2.5-特惠（有赞，720P ¥0.20/秒、1080P ¥0.30/秒）
UPDATE models SET
  description = description ||
    '⚠️ 带参考视频时按「生成秒数 + 参考视频总秒数（四舍五入）」× 对应档位单价计费，'
    '例如 10 秒参考视频 + 生成 10 秒 720P = ¥4.00（不传参考视频只要 ¥2.00）。参考视频单条最长 15 秒。',
  updated_time = extract(epoch from now())::bigint
WHERE model_name = 'seedance2.5-特惠'
  AND deleted_at IS NULL
  AND COALESCE(description, '') NOT LIKE '%参考视频总秒数%';

-- 3) wan3.0-video（有赞，480P/720P/1080P 同价 ¥0.23/秒）
UPDATE models SET
  description = description ||
    '⚠️ 带参考视频时按「生成秒数 + 参考视频总秒数（四舍五入）」× ¥0.23 计费，'
    '例如 10 秒参考视频 + 生成 10 秒 = ¥4.60（不传参考视频只要 ¥2.30）。参考视频单条最长 15 秒。',
  updated_time = extract(epoch from now())::bigint
WHERE model_name = 'wan3.0-video'
  AND deleted_at IS NULL
  AND COALESCE(description, '') NOT LIKE '%参考视频总秒数%';

-- 4) wan3.0-video-prime（有赞，720P ¥0.25/秒、1080P ¥0.30/秒）
UPDATE models SET
  description = description ||
    '⚠️ 带参考视频时按「生成秒数 + 参考视频总秒数（四舍五入）」× 对应档位单价计费，'
    '例如 10 秒参考视频 + 生成 10 秒 720P = ¥5.00（不传参考视频只要 ¥2.50）。参考视频单条最长 15 秒。',
  updated_time = extract(epoch from now())::bigint
WHERE model_name = 'wan3.0-video-prime'
  AND deleted_at IS NULL
  AND COALESCE(description, '') NOT LIKE '%参考视频总秒数%';

-- 5) 已经写过计费说明的两个，只补「单条最长 15 秒」这句
UPDATE models SET
  description = description || '参考视频单条最长 15 秒。',
  updated_time = extract(epoch from now())::bigint
WHERE model_name IN ('wan3.0-720p-特惠', 'wan3.0-video-官网')
  AND deleted_at IS NULL
  AND description LIKE '%参考视频总秒数%'
  AND description NOT LIKE '%单条最长 15 秒%';

COMMIT;
