-- 促销族母本改由有赞 mw-sd2.5-flash 执行（2026-11-20 老板口径）。
--
-- 老板原话：「把有赞的这个（mw-sd2.5-flash）接进来平台，替掉之前的
-- cp-seedance-2.5flash 那个模型 —— 也就是无条件兜底后就用这个模型，
-- 请求 2.5 特惠的模型就路由到这个」，活动版「下架，不做了」。
--
-- 代码侧已随 21bf18f83 上线：
--   · `promoResidentModel = mw-sd2.5-flash`（原 SJ-wan3-prime 退出）
--   · `upstreamModelFor(特惠) → mw-sd2.5-flash`，1080P 一律 400
--   · 素材闸门放开：mw 收参考视频/音频（≤5/≤5）与首尾帧（10 图）
--   · 规则三/九新增按源模型开关（frames_rule_enabled / ref_av_rule_enabled）
--
-- 本脚本只做**数据侧收敛**：把当初为「SJ 物理接不住」而存在的六条改派
-- 对特惠全关，请求才会真正留在 mw 上；同时下架活动版。
--
-- 为什么必须关规则八（全量兜底）：它就是「无条件改派 CapCut」，
-- 开着的话母本换成 mw 也白换 —— 请求在进渠道前就被改写走了。
-- 生产实测（logs 17593/17594、18798）今天的特惠请求确实全被它改派到
-- ch20 的 cp-seedance-2.0 / cp-seedance-2.5flash，并大量失败。
--
-- ⚠️ 不重启容器：写库后最多 60 秒（SYNC_FREQUENCY）由 SyncOptions 载入内存。
--    在这 60 秒窗口里请求仍按旧策略走 —— 本次实测就踩到了（17:26:55 的
--    活动版请求、17:27:18 的特惠请求都在窗口内被规则八改派）。
--    改完策略请等一轮同步再做端到端验证。

BEGIN;

-- 1) 备份（回滚用）
CREATE TABLE IF NOT EXISTS bak_20261120_mw_sd25_option AS
SELECT * FROM options WHERE key = 'Seedance25TeHuiRoute30s';
CREATE TABLE IF NOT EXISTS bak_20261120_mw_sd25_channels AS
SELECT id, models, status FROM channels WHERE id IN (17, 20);
CREATE TABLE IF NOT EXISTS bak_20261120_mw_sd25_abilities AS
SELECT * FROM abilities WHERE channel_id IN (17, 20);

-- 2) 渠道 17（有赞 Wan3）：下架活动版。
--    mw-sd2.5-flash 是**内部执行模型**，刻意不写进 channel.models ——
--    与上一任 SJ-wan3-prime 同口径（它同样不在列表里，链路照跑）。
--    写进去等于多一个可公开调用、但没有 ModelPrice 的 SKU。
UPDATE channels
   SET models = 'wan3.0-video,wan3.0-video-prime,wan3.0-smart,seedance2.5-特惠,wan-3.0-pro,wan-3.0-yz,hk-wan3'
 WHERE id = 17;

DELETE FROM abilities WHERE channel_id = 17 AND model = 'seedance2.5-活动';

-- 3) 渠道 20（漫屋 CapCut）：**保留** cp-seedance-2.5flash。
--
--    老板说的「替掉」指的是「它不再是特惠的兜底目标」，不是「把这个 SKU 删掉」。
--    它今天仍有两条真实用途，删掉就是把可用路径变成 503：
--      · wan3.0-video 直连 + 参考音视频 → 规则九改派到它（有赞标准版只收参考图）；
--      · failover_alt 备胎链的最后一环（DefaultFailoverAltModel）。
--    特惠侧的引用已由第 4 步的按源开关全部切断，不需要动这个 SKU。
--    （生产实测：撤掉它之后 3428 仍然路由到 ch20 并正常出片，说明这条链路是活的。）

-- 4) 路由策略：源模型只剩特惠；六条「补位 CapCut」规则按源模型全关。
--
--    total_fallback_enabled=false       规则八 全量兜底（无条件改派 CapCut）
--    short_rule_enabled=false           规则六 短时长分流（≤10s → 心笔/标准版/CapCut）
--    ecommerce_rule_enabled=false       规则七 电商内容改派（有赞「接不住」的判定
--                                       仍按 SJ 口径写死：音视频 / <5 秒 → CapCut）
--    frames_rule_enabled=false          规则三 首尾帧改派（mw 自己能收首尾帧）
--    ref_av_rule_enabled=false          规则九 音视频改派（mw 自己收 5 视频 / 5 音频）
--    seconds_rule_enabled / ratio_rule_enabled 原本就是 false，保留
--
--    为什么规则八走全局关而不是按源关：活动版下架后源模型只剩特惠一个，
--    全局关与按源关等价；不新加字段也就少一处「配置能配错」的面。
UPDATE options
   SET value = (
         value::jsonb
         || jsonb_build_object(
              'source_models', '["seedance2.5-特惠"]'::jsonb,
              'total_fallback_enabled', false,
              'source_overrides', jsonb_build_object(
                 'seedance2.5-特惠',
                 '{"ratio_rule_enabled":false,"seconds_rule_enabled":false,"short_rule_enabled":false,"ecommerce_rule_enabled":false,"frames_rule_enabled":false,"ref_av_rule_enabled":false}'::jsonb
              )
            )
       )::text
 WHERE key = 'Seedance25TeHuiRoute30s';

COMMIT;

-- 5) 核对
SELECT jsonb_pretty(value::jsonb) AS policy FROM options WHERE key = 'Seedance25TeHuiRoute30s';
SELECT id, models FROM channels WHERE id IN (17, 20);
SELECT channel_id, model, enabled FROM abilities WHERE channel_id IN (17, 20) ORDER BY channel_id, model;

-- 6) 端到端验证（2026-11-20 实测结果）
--    POST /v1/videos {"model":"seedance2.5-特惠","seconds":"5","resolution":"720P"}
--      → 200，tasks.channel_id=17、platform=63，
--        上游回执 "model":"mw-sd2.5-flash"，无 route_target_model
--      → quota 68490 ≈ ¥1.00（5 秒 × ¥0.20/秒，按源模型计价，没被上游单价带偏）
--    POST 同上但 "resolution":"1080P"
--      → 400 invalid_resolution「该模型目前仅支持 720P 交付（不支持 1080P）」
--    POST {"model":"seedance2.5-活动"}
--      → 503 model_not_found 无可用渠道
--    GET  /v1/models、/api/pricing 均不再出现活动版与 mw-sd2.5-flash

-- 回滚（数据侧）：
--   UPDATE channels SET models = (SELECT models FROM bak_20261120_mw_sd25_channels WHERE id = 17) WHERE id = 17;
--   UPDATE channels SET models = (SELECT models FROM bak_20261120_mw_sd25_channels WHERE id = 20) WHERE id = 20;
--   UPDATE options SET value = (SELECT value FROM bak_20261120_mw_sd25_option) WHERE key = 'Seedance25TeHuiRoute30s';
--   INSERT INTO abilities SELECT * FROM bak_20261120_mw_sd25_abilities ON CONFLICT DO NOTHING;
