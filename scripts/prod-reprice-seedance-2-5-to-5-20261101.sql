-- 生产：seedance-2-5（豆包官网）按次价 ¥3.00 → ¥5.00（2026-11-01）
--
-- 需求方：老板 —— 「把 seedance2-5 就是 3 块的那个模型结果调整为五块」。
--
-- 模型辨析（三个名字相近，别改错）：
--   seedance-2-5    豆包官网 Seedance 2.5（漫屋 ArcReel 渠道 20），**按次** ← 本次目标
--   seedance-2.5    dola 官网 Seedance 2.5，按次 ¥0.80
--   seedance2.5-特惠 有赞 wan3.0-video-prime 的对外别名，**按秒** ¥0.20/秒
--
-- 口径：ModelPrice 存 USD 等值，人民币价 = USD × 7.3。
--   ¥5.00 / 7.3 = 0.6849315068493151（不取整，与同步任务同口径）
--   扣费 quota = 0.6849315068493151 × 500000 = 342465.7534 → 342465
--
-- ⚠️ 值的 JSON 类型必须是**数字**（不是字符串）。写成 "0.6849..." 会让
--    map[string]float64 整表加载失败，M3/M3.1 那次已经踩过这个坑。
--    这里用 to_jsonb(0.6849315068493151::numeric) 保证是数字类型。
--
-- 本脚本只动 options 里的 ModelPrice，渠道/能力/卡片/文案都不动
-- （前端文案与后端默认值由同一次代码提交同步上线）。

BEGIN;

-- 1) 备份（可回滚）
CREATE TABLE IF NOT EXISTS _bak_seedance_2_5_price_20261101 AS
SELECT key, value, now() AS snap_at
  FROM options
 WHERE key IN ('ModelPrice');

-- 2) 改价：seedance-2-5 → 0.6849315068493151（= ¥5.00/次）
UPDATE options
   SET value = jsonb_set(value::jsonb,
                 ARRAY['seedance-2-5'],
                 to_jsonb(0.6849315068493151::numeric),
                 false)::text
 WHERE key = 'ModelPrice';

-- 2b) 模型广场卡片描述里的价格也要跟着改 —— 那是 models.description 的
--     静态文本，不会随 ModelPrice 自动更新。漏改 = 广场显示 ¥3、实扣 ¥5。
UPDATE models
   SET description = replace(description, '¥3.00/次', '¥5.00/次')
 WHERE model_name = 'seedance-2-5'
   AND deleted_at IS NULL
   AND description LIKE '%¥3.00/次%';

-- 3) 自检：目标价必须精确等于 ¥5.00；相邻的两个名字必须没被动过
DO $$
DECLARE
  target_usd numeric;
  dola25_usd numeric;
  tehui_usd numeric;
  target_type text;
BEGIN
  SELECT (value::jsonb ->> 'seedance-2-5')::numeric,
         (value::jsonb ->> 'seedance-2.5')::numeric,
         (value::jsonb ->> 'seedance2.5-特惠')::numeric,
         jsonb_typeof(value::jsonb -> 'seedance-2-5')
    INTO target_usd, dola25_usd, tehui_usd, target_type
    FROM options WHERE key = 'ModelPrice';

  IF target_type <> 'number' THEN
    RAISE EXCEPTION 'seedance-2-5 的 JSON 类型是 %，必须是 number', target_type;
  END IF;
  IF round(target_usd * 7.3, 4) <> 5.00 THEN
    RAISE EXCEPTION 'seedance-2-5 不是 ¥5.00/次，实际是 ¥%', target_usd * 7.3;
  END IF;
  IF round(dola25_usd * 7.3, 4) <> 0.80 THEN
    RAISE EXCEPTION 'seedance-2.5（dola）被误改了：¥%', dola25_usd * 7.3;
  END IF;
  IF round(tehui_usd * 7.3, 4) <> 0.20 THEN
    RAISE EXCEPTION 'seedance2.5-特惠（按秒）被误改了：¥%', tehui_usd * 7.3;
  END IF;

  -- 卡片描述里不许再留 ¥3.00/次（漏改 = 广场显示 ¥3、实扣 ¥5）
  IF EXISTS (SELECT 1 FROM models
              WHERE model_name = 'seedance-2-5'
                AND deleted_at IS NULL
                AND description LIKE '%¥3.00/次%') THEN
    RAISE EXCEPTION '模型广场卡片描述里还写着 ¥3.00/次';
  END IF;
END $$;

COMMIT;

-- 4) 结果：三个相近模型的对照（人民币）
SELECT
    e.key AS model,
    round(e.value::numeric * 7.3, 4) AS rmb,
    CASE
      WHEN e.key = 'seedance-2-5'    THEN '按次，本次改为 ¥5.00'
      WHEN e.key = 'seedance-2.5'    THEN 'dola，按次 ¥0.80（不应变动）'
      WHEN e.key = 'seedance2.5-特惠' THEN '有赞，按秒 ¥0.20（不应变动）'
    END AS note
  FROM options o,
       LATERAL jsonb_each_text(o.value::jsonb) AS e(key, value)
 WHERE o.key = 'ModelPrice'
   AND e.key IN ('seedance-2-5', 'seedance-2.5', 'seedance2.5-特惠');

-- 回滚（如需）：
--   UPDATE options o SET value = b.value
--     FROM _bak_seedance_2_5_price_20261101 b
--    WHERE o.key = b.key AND o.key = 'ModelPrice';
--   DROP TABLE _bak_seedance_2_5_price_20261101;
