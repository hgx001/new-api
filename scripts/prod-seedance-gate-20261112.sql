-- 2026-11-12 seedance2.5 规则一收窄：
--   1) seconds_rule_ratio_gate = "9:16" —— 只有 9:16 且 >20 秒才改派豆包；
--   2) doubao_link_off_until = 0 —— 立即打开豆包链路开关（原定关到明日 02:00）。
-- 备份表 _bak_seedance_gate_20261112。
-- 注意：该表在本脚本早期版本（key 写错时）建成过**空表**，故先删后建，
-- 防止 IF NOT EXISTS 跳过导致备份里没有真实数据。
DROP TABLE IF EXISTS _bak_seedance_gate_20261112;
CREATE TABLE _bak_seedance_gate_20261112 AS
SELECT * FROM options WHERE key = 'Seedance25TeHuiRoute30s';

UPDATE options
SET value = jsonb_set(
      jsonb_set(
        jsonb_set(value::jsonb,
          '{seconds_rule_ratio_gate}'::text[], '"9:16"'::jsonb),
        '{doubao_link_off_until}'::text[], '0'::jsonb),
      '{updated_at}'::text[], to_char(extract(epoch from now()), 'FM999999999999')::jsonb
    )::text
WHERE key = 'Seedance25TeHuiRoute30s';

SELECT jsonb_pretty(value::jsonb) FROM options WHERE key = 'Seedance25TeHuiRoute30s';
