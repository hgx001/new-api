-- 新机（api2）对齐老机（api）的模型目录　生成时间 2026-11-22 17:48
-- 由 scripts/gen_align_new_server_models.py 自动生成，数据逐行取自老机生产库。
-- ⚠️ 只在新机（114.132.252.150 / api2）跑；老机不用跑。
-- ⚠️ 跑完等 60 秒（SyncChannelCache / SyncOptions 周期）再验证，不要立刻重启。
-- ⚠️ 幂等：可重复执行。

BEGIN;

-- ── 0. 备份 ──
CREATE TABLE IF NOT EXISTS bak_20261122_align_channels AS SELECT * FROM channels;
CREATE TABLE IF NOT EXISTS bak_20261122_align_abilities AS SELECT * FROM abilities;
CREATE TABLE IF NOT EXISTS bak_20261122_align_models AS SELECT * FROM models;
CREATE TABLE IF NOT EXISTS bak_20261122_align_options AS SELECT * FROM options;

-- ── 1. 渠道 models 列表（按渠道**名字**对齐，逐字一致）──

-- ── 2. abilities（模型广场与路由都靠它；渠道按名字映射）──
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'MiniMax-M2.7', 'default', 1, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'MiniMax-M2.7' AND x.channel_id = 1 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'MiniMax-M3', 'default', 1, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'MiniMax-M3' AND x.channel_id = 1 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'MiniMax-M3.1', 'default', 1, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'MiniMax-M3.1' AND x.channel_id = 1 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'MiniMax-M3.1', 'm3only', 26, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'MiniMax-M3.1' AND x.channel_id = 26 AND x."group" = 'm3only');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'Nano Banana Pro', 'm3only', 30, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'Nano Banana Pro' AND x.channel_id = 30 AND x."group" = 'm3only');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'Nano Banana Pro', 'default', 20, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'Nano Banana Pro' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'Seedream 4.5', 'm3only', 30, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'Seedream 4.5' AND x.channel_id = 30 AND x."group" = 'm3only');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'Seedream 4.5', 'default', 20, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'Seedream 4.5' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'Seedream 5.0 Flash', 'm3only', 30, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'Seedream 5.0 Flash' AND x.channel_id = 30 AND x."group" = 'm3only');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'Seedream 5.0 Flash', 'default', 20, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'Seedream 5.0 Flash' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'el-gemini-1.2flash', 'default', 20, true, 0, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'el-gemini-1.2flash' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'el-seedance-2.0mini', 'default', 20, true, 0, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'el-seedance-2.0mini' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'el-seedance-2.5flash', 'default', 20, true, 0, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'el-seedance-2.5flash' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'gpt-image-2.5-官方', 'm3only', 29, true, 1, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'gpt-image-2.5-官方' AND x.channel_id = 29 AND x."group" = 'm3only');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'h3-image', 'default', 31, true, 0, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'h3-image' AND x.channel_id = 31 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'hk-wan3', 'default', 17, true, 10, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'hk-wan3' AND x.channel_id = 17 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'jimeng-video-reverse', 'default', 20, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'jimeng-video-reverse' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'minimax_h3_omni', 'default', 28, true, 0, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'minimax_h3_omni' AND x.channel_id = 28 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'sd-seedance-2.0', 'default', 20, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'sd-seedance-2.0' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'sd-seedance-2.0fast', 'default', 20, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'sd-seedance-2.0fast' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'seedance-2-0', 'default', 20, true, 0, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'seedance-2-0' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'seedance-2-0-fast', 'default', 20, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'seedance-2-0-fast' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'seedance-2-5', 'default', 20, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'seedance-2-5' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'seedance-2.0', 'default', 20, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'seedance-2.0' AND x.channel_id = 20 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'seedance2.5-特惠', 'default', 17, true, 10, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'seedance2.5-特惠' AND x.channel_id = 17 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'wan-3.0-pro', 'default', 17, true, 0, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'wan-3.0-pro' AND x.channel_id = 17 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'wan-3.0-yz', 'default', 17, true, 10, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'wan-3.0-yz' AND x.channel_id = 17 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'wan3.0-720p-官网', 'default', 12, true, 0, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'wan3.0-720p-官网' AND x.channel_id = 12 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'wan3.0-720p-特惠-内链', 'default', 25, true, 0, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'wan3.0-720p-特惠-内链' AND x.channel_id = 25 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'wan3.0-smart', 'default', 17, true, 10, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'wan3.0-smart' AND x.channel_id = 17 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'wan3.0-video', 'default', 17, true, 10, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'wan3.0-video' AND x.channel_id = 17 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'wan3.0-video-prime', 'default', 17, true, 10, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'wan3.0-video-prime' AND x.channel_id = 17 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'wan3.0-video-prime-tj', 'default', 33, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'wan3.0-video-prime-tj' AND x.channel_id = 33 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'wan3.0-video-tj', 'default', 33, true, 0, 1, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'wan3.0-video-tj' AND x.channel_id = 33 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'wan3.0-video-官网', 'default', 12, true, 0, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'wan3.0-video-官网' AND x.channel_id = 12 AND x."group" = 'default');
INSERT INTO abilities (model, "group", channel_id, enabled, priority, weight, tag) SELECT 'wan3.0-班车', 'default', 27, true, 0, 0, NULL WHERE NOT EXISTS (SELECT 1 FROM abilities x WHERE x.model = 'wan3.0-班车' AND x.channel_id = 27 AND x."group" = 'default');

-- ── 3. ModelPrice（老机为基准；缺失的补齐，同键以老机为准）──
--    jsonb_build_object 的键要用单引号；它最多 100 个参数，所以分批。
-- 分批合并（第 1/3 批）
UPDATE options SET value = (value::jsonb || jsonb_build_object('MiniMax-H3', 0.0684931506849315, 'MiniMax-H3-Context-IR', 0.136986301369863, 'MiniMax-H3-Max', 0.05, 'Nano Banana Pro', 0.0410958904109589, 'Seedream 4.5', 0.0273972602739726, 'Seedream 5.0 Flash', 0.0273972602739726, 'aliyun:wan-3.0', 0.035445, 'autodl:minimax-h3-b99-12s', 0.01369863, 'autodl:minimax-h3-image-audio-10s', 0.0136986301369863, 'autodl:minimax-h3-image-audio-15s', 0.0136986301369863, 'autodl:minimax-h3-lightx2v-v5', 0.01369863, 'autodl:minimax-h3-lightx2v-v5-15s', 0.01369863, 'autodl:minimax-h3-lipsync', 0.0136986301369863, 'autodl:minimax-h3-text-to-video', 0.01369863, 'autodl:minimax-h3-u08', 0.0136986301369863, 'autodl:minimax-h3-u24', 0.0136986301369863, 'black-forest-labs/flux-1.1-pro', 0.004109, 'cp-seedance-2.0', 0.020547945205, 'cp-seedance-2.0fast', 0.01095890411, 'cp-seedance-2.0mini', 0.006849315068, 'cp-seedance-2.5', 0.027397260274, 'cp-seedance-2.5flash', 0.020547945205, 'dall-e-3', 0.004109, 'doubao-seedream-5-0-260128', 0.061438, 'el-gemini-1.2flash', 0.03424657534246575, 'el-seedance-2.0mini', 0.03424657534246575, 'el-seedance-2.5flash', 0.03424657534246575, 'gemini-3-pro-image-preview', 0.066164, 'gemini-3.1-flash-image-preview', 0.04726, 'gpt-4-gizmo-*', 0.010274, 'gpt-4o-mini-tts', 0.030822, 'gpt-image-1', 0.004109, 'gpt-image-1.5', 0.002569, 'gpt-image-2', 0.01780821917808219, 'gpt-image-2.5', 0.02054794520547945, 'gpt-image-2.5-官方', 0.02054794520547945, 'h3-image', 0.00684931506849315, 'hailuo-h3-cankaosheng-fast', 0.025994, 'hailuo-h3-cankaosheng-night', 0.012997, 'hk-wan3', 0.024657534246575342))::text WHERE key = 'ModelPrice';
-- 分批合并（第 2/3 批）
UPDATE options SET value = (value::jsonb || jsonb_build_object('imagen-3.0-generate-002', 0.003083, 'jimeng-video-reverse', 0.136986301369863, 'minimax_h3_omni', 0.0027397260273972603, 'mj_blend', 0.010274, 'mj_custom_zoom', 0, 'mj_describe', 0.005137, 'mj_edits', 0.010274, 'mj_high_variation', 0.010274, 'mj_imagine', 0.010274, 'mj_inpaint', 0, 'mj_low_variation', 0.010274, 'mj_modal', 0.010274, 'mj_pan', 0.010274, 'mj_reroll', 0.010274, 'mj_shorten', 0.010274, 'mj_upload', 0.005137, 'mj_upscale', 0.005137, 'mj_variation', 0.010274, 'mj_video', 0.082192, 'mj_zoom', 0.010274, 'sd-seedance-2.0', 0.030136986301, 'sd-seedance-2.0fast', 0.013698630137, 'sd2.5慢速15秒内排队满血版', 0.708904, 'sd2.5慢速20秒内排队满血版', 0.827055, 'seedance-2-0', 0.03424657534246575, 'seedance-2-0-fast', 0.0136986301369863, 'seedance-2-5', 0.4794520547945206, 'seedance-2.0', 0.136986301369863, 'seedance-2.5', 0.10958904109589042, 'seedance2.0特惠版', 0.236301, 'seedance2.5-活动', 0.6164383561643836, 'seedance2.5-特惠', 0.027397260273972605, 'seedance2.5特惠版', 0.821918, 'sora-2', 0.030822, 'sora-2-pro', 0.05137, 'suno_lyrics', 0.001027, 'suno_music', 0.010274, 'swap_face', 0.005137, 'veo-3.0-fast-generate-001', 0.015411, 'veo-3.0-generate-001', 0.041096))::text WHERE key = 'ModelPrice';
-- 分批合并（第 3/3 批）
UPDATE options SET value = (value::jsonb || jsonb_build_object('veo-3.1-fast-generate-preview', 0.015411, 'veo-3.1-generate-preview', 0.041096, 'wan-3.0-pro', 0.684931506849315, 'wan-3.0-yz', 0.027397260273972605, 'wan3.0-720p-官网', 0.0273972602739726, 'wan3.0-smart', 0.038356164383561646, 'wan3.0-video', 0.031506849315068496, 'wan3.0-video-prime', 0.03424657534246575, 'wan3.0-video-prime-tj', 0.03424657534246575, 'wan3.0-video-tj', 0.02054794520547945, 'wan3.0-video-官网', 0.0273972602739726, 'wan3.0-班车', 0.4794520547945206, 'xuan:MiniMax-H3-2K', 0.189041, 'xuan:seedance-2.0-special', 0.037808))::text WHERE key = 'ModelPrice';

-- ── 4. models 表元数据（广场卡片：描述/标签/供应商/端点/上下架状态）──
--    供应商按**名字**对齐：两机 vendor id 编号不同（阿里巴巴/MiniMax 顺序相反）。
DO $do$
DECLARE v_id int;
BEGIN
  SELECT id INTO v_id FROM vendors WHERE name = 'Google';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('Google', 'Gemini.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = 'Google';
  END IF;
  UPDATE models SET description='图片生成与编辑', icon=NULL, tags='image', vendor_id=v_id, endpoints=NULL, status=1, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='Nano Banana Pro' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'Nano Banana Pro', '图片生成与编辑', NULL, 'image', v_id, NULL, 1, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='Nano Banana Pro' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='dola 官网图像生成模型，按张计费 ¥0.20/张。比例支持自动/9:16/2:3/3:4/1:1/4:3/3:2/16:9，未指定时保持官网出厂的「自动」档。单次最多 4 张。', icon=NULL, tags='image', vendor_id=v_id, endpoints=NULL, status=1, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='Seedream 4.5' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'Seedream 4.5', 'dola 官网图像生成模型，按张计费 ¥0.20/张。比例支持自动/9:16/2:3/3:4/1:1/4:3/3:2/16:9，未指定时保持官网出厂的「自动」档。单次最多 4 张。', NULL, 'image', v_id, NULL, 1, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='Seedream 4.5' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='豆包官网图像生成模型，按张计费 ¥0.20/张。比例支持自动/9:16/2:3/3:4/1:1/4:3/3:2/16:9，未指定时保持官网出厂的「自动」档。单次最多 4 张。', icon=NULL, tags='image', vendor_id=v_id, endpoints=NULL, status=1, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='Seedream 5.0 Flash' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'Seedream 5.0 Flash', '豆包官网图像生成模型，按张计费 ¥0.20/张。比例支持自动/9:16/2:3/3:4/1:1/4:3/3:2/16:9，未指定时保持官网出厂的「自动」档。单次最多 4 张。', NULL, 'image', v_id, NULL, 1, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='Seedream 5.0 Flash' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='Seedance 2.0 文/图生视频。时长仅支持 5 / 8 / 10 / 12 / 15 秒（共 5 档固定档位）；4、6、7、9、11、13、14 秒及 15 秒以上均不支持——非上述档位会直接失败，不会自动取整到最近档位。6 档比例（16:9 / 9:16 / 1:1 / 3:4 / 4:3 / 21:9），画质 480p / 720p，按秒计费：480p ¥0.15/秒、720p ¥0.30/秒。参考素材：支持图片与音频（≤30 张参考图），不支持参考视频。', icon=NULL, tags='video', vendor_id=v_id, endpoints='[]', status=0, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='cp-seedance-2.0' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'cp-seedance-2.0', 'Seedance 2.0 文/图生视频。时长仅支持 5 / 8 / 10 / 12 / 15 秒（共 5 档固定档位）；4、6、7、9、11、13、14 秒及 15 秒以上均不支持——非上述档位会直接失败，不会自动取整到最近档位。6 档比例（16:9 / 9:16 / 1:1 / 3:4 / 4:3 / 21:9），画质 480p / 720p，按秒计费：480p ¥0.15/秒、720p ¥0.30/秒。参考素材：支持图片与音频（≤30 张参考图），不支持参考视频。', NULL, 'video', v_id, '[]', 0, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='cp-seedance-2.0' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='Seedance 2.0 Fast 文/图生视频。时长仅支持 5 / 8 / 10 / 12 / 15 秒（共 5 档固定档位）；4、6、7、9、11、13、14 秒及 15 秒以上均不支持——非上述档位会直接失败，不会自动取整到最近档位。6 档比例（16:9 / 9:16 / 1:1 / 3:4 / 4:3 / 21:9），画质 480p / 720p，按秒计费：480p ¥0.08/秒、720p ¥0.12/秒。参考素材：支持图片与音频（≤30 张参考图），不支持参考视频。', icon=NULL, tags='video', vendor_id=v_id, endpoints='[]', status=0, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='cp-seedance-2.0fast' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'cp-seedance-2.0fast', 'Seedance 2.0 Fast 文/图生视频。时长仅支持 5 / 8 / 10 / 12 / 15 秒（共 5 档固定档位）；4、6、7、9、11、13、14 秒及 15 秒以上均不支持——非上述档位会直接失败，不会自动取整到最近档位。6 档比例（16:9 / 9:16 / 1:1 / 3:4 / 4:3 / 21:9），画质 480p / 720p，按秒计费：480p ¥0.08/秒、720p ¥0.12/秒。参考素材：支持图片与音频（≤30 张参考图），不支持参考视频。', NULL, 'video', v_id, '[]', 0, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='cp-seedance-2.0fast' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='Seedance 2.0 Mini 文/图生视频。时长仅支持 5 / 8 / 10 / 12 / 15 秒（共 5 档固定档位）；4、6、7、9、11、13、14 秒及 15 秒以上均不支持——非上述档位会直接失败，不会自动取整到最近档位。6 档比例（16:9 / 9:16 / 1:1 / 3:4 / 4:3 / 21:9），画质 480p / 720p，按秒计费：480p ¥0.05/秒、720p ¥0.075/秒。参考素材：支持图片与音频（≤30 张参考图），不支持参考视频。', icon=NULL, tags='video', vendor_id=v_id, endpoints='["openai-video"]', status=0, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='cp-seedance-2.0mini' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'cp-seedance-2.0mini', 'Seedance 2.0 Mini 文/图生视频。时长仅支持 5 / 8 / 10 / 12 / 15 秒（共 5 档固定档位）；4、6、7、9、11、13、14 秒及 15 秒以上均不支持——非上述档位会直接失败，不会自动取整到最近档位。6 档比例（16:9 / 9:16 / 1:1 / 3:4 / 4:3 / 21:9），画质 480p / 720p，按秒计费：480p ¥0.05/秒、720p ¥0.075/秒。参考素材：支持图片与音频（≤30 张参考图），不支持参考视频。', NULL, 'video', v_id, '["openai-video"]', 0, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='cp-seedance-2.0mini' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='Seedance 2.5 满血 文/图生视频。时长仅支持 5 / 8 / 10 / 12 / 15 / 30 秒（共 6 档固定档位）；4、6、7、9、11、13、14 秒，以及 16–29 秒和 30 秒以上均不支持——非上述档位会直接失败，不会自动取整到最近档位。6 档比例（16:9 / 9:16 / 1:1 / 3:4 / 4:3 / 21:9），画质 480p / 720p，按秒计费：480p ¥0.20/秒、720p ¥0.40/秒。参考素材：支持图片与音频（≤30 张参考图），不支持参考视频。', icon=NULL, tags='video', vendor_id=v_id, endpoints='["openai-video"]', status=0, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='cp-seedance-2.5' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'cp-seedance-2.5', 'Seedance 2.5 满血 文/图生视频。时长仅支持 5 / 8 / 10 / 12 / 15 / 30 秒（共 6 档固定档位）；4、6、7、9、11、13、14 秒，以及 16–29 秒和 30 秒以上均不支持——非上述档位会直接失败，不会自动取整到最近档位。6 档比例（16:9 / 9:16 / 1:1 / 3:4 / 4:3 / 21:9），画质 480p / 720p，按秒计费：480p ¥0.20/秒、720p ¥0.40/秒。参考素材：支持图片与音频（≤30 张参考图），不支持参考视频。', NULL, 'video', v_id, '["openai-video"]', 0, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='cp-seedance-2.5' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='Seedance 2.5 Flash 文/图生视频。时长仅支持 5 / 8 / 10 / 12 / 15 / 30 秒（共 6 档固定档位）；4、6、7、9、11、13、14 秒，以及 16–29 秒和 30 秒以上均不支持——非上述档位会直接失败，不会自动取整到最近档位。6 档比例（16:9 / 9:16 / 1:1 / 3:4 / 4:3 / 21:9），画质 480p / 720p，按秒计费：480p ¥0.15/秒、720p ¥0.30/秒。参考素材：支持图片与音频（≤30 张参考图），不支持参考视频。', icon=NULL, tags='video', vendor_id=v_id, endpoints='["openai-video"]', status=0, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='cp-seedance-2.5flash' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'cp-seedance-2.5flash', 'Seedance 2.5 Flash 文/图生视频。时长仅支持 5 / 8 / 10 / 12 / 15 / 30 秒（共 6 档固定档位）；4、6、7、9、11、13、14 秒，以及 16–29 秒和 30 秒以上均不支持——非上述档位会直接失败，不会自动取整到最近档位。6 档比例（16:9 / 9:16 / 1:1 / 3:4 / 4:3 / 21:9），画质 480p / 720p，按秒计费：480p ¥0.15/秒、720p ¥0.30/秒。参考素材：支持图片与音频（≤30 张参考图），不支持参考视频。', NULL, 'video', v_id, '["openai-video"]', 0, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='cp-seedance-2.5flash' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = 'Google';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('Google', 'Gemini.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = 'Google';
  END IF;
  UPDATE models SET description='Gemini Omni Flash 1.2（ElevenLabs 图像和视频创作台）。3–15 秒、360p/720p，按秒计费（360p ¥0.25/秒，720p ¥0.50/秒）；只收参考图（最多 30 张），不支持参考视频/音频与音频开关。', icon=NULL, tags='gemini,elevenlabs', vendor_id=v_id, endpoints=NULL, status=1, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='el-gemini-1.2flash' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'el-gemini-1.2flash', 'Gemini Omni Flash 1.2（ElevenLabs 图像和视频创作台）。3–15 秒、360p/720p，按秒计费（360p ¥0.25/秒，720p ¥0.50/秒）；只收参考图（最多 30 张），不支持参考视频/音频与音频开关。', NULL, 'gemini,elevenlabs', v_id, NULL, 1, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='el-gemini-1.2flash' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='Seedance 2.0 Mini（ElevenLabs 图像和视频创作台）。4–15 秒、480p/720p，按秒计费（480p ¥0.25/秒，720p ¥0.50/秒）；支持参考图 / 参考视频 / 参考音频。', icon=NULL, tags='video,seedance,elevenlabs', vendor_id=v_id, endpoints=NULL, status=1, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='el-seedance-2.0mini' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'el-seedance-2.0mini', 'Seedance 2.0 Mini（ElevenLabs 图像和视频创作台）。4–15 秒、480p/720p，按秒计费（480p ¥0.25/秒，720p ¥0.50/秒）；支持参考图 / 参考视频 / 参考音频。', NULL, 'video,seedance,elevenlabs', v_id, NULL, 1, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='el-seedance-2.0mini' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='Seedance 2.5 Flash（ElevenLabs 图像和视频创作台）。4–30 秒、480p/720p，按秒计费（480p ¥0.25/秒，720p ¥0.50/秒）；支持参考图 / 参考视频 / 参考音频。', icon=NULL, tags='video,seedance,elevenlabs', vendor_id=v_id, endpoints=NULL, status=1, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='el-seedance-2.5flash' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'el-seedance-2.5flash', 'Seedance 2.5 Flash（ElevenLabs 图像和视频创作台）。4–30 秒、480p/720p，按秒计费（480p ¥0.25/秒，720p ¥0.50/秒）；支持参考图 / 参考视频 / 参考音频。', NULL, 'video,seedance,elevenlabs', v_id, NULL, 1, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='el-seedance-2.5flash' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = 'OpenAI';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('OpenAI', 'OpenAI', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = 'OpenAI';
  END IF;
  UPDATE models SET description='GPT-6 轻量文本模型，高性价比档', icon='OpenAI', tags=NULL, vendor_id=v_id, endpoints=NULL, status=1, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='gpt-6-luna' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'gpt-6-luna', 'GPT-6 轻量文本模型，高性价比档', 'OpenAI', NULL, v_id, NULL, 1, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='gpt-6-luna' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = 'OpenAI';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('OpenAI', 'OpenAI', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = 'OpenAI';
  END IF;
  UPDATE models SET description='GPT-6 旗舰文本模型，复杂推理档', icon='OpenAI', tags=NULL, vendor_id=v_id, endpoints=NULL, status=1, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='gpt-6-sol' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'gpt-6-sol', 'GPT-6 旗舰文本模型，复杂推理档', 'OpenAI', NULL, v_id, NULL, 1, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='gpt-6-sol' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = 'OpenAI';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('OpenAI', 'OpenAI', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = 'OpenAI';
  END IF;
  UPDATE models SET description='GPT-6.1 旗舰文本模型，复杂推理档（与 gpt-6-sol 同价）', icon='OpenAI', tags=NULL, vendor_id=v_id, endpoints=NULL, status=0, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='gpt-6.1-sol' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'gpt-6.1-sol', 'GPT-6.1 旗舰文本模型，复杂推理档（与 gpt-6-sol 同价）', 'OpenAI', NULL, v_id, NULL, 0, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='gpt-6.1-sol' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = 'OpenAI';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('OpenAI', 'OpenAI', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = 'OpenAI';
  END IF;
  UPDATE models SET description='图片生成，2K 按张计费', icon='OpenAI', tags='image', vendor_id=v_id, endpoints=NULL, status=0, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='gpt-image-2.5-官方' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'gpt-image-2.5-官方', '图片生成，2K 按张计费', 'OpenAI', 'image', v_id, NULL, 0, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='gpt-image-2.5-官方' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = 'MiniMax';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('MiniMax', 'Minimax.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = 'MiniMax';
  END IF;
  UPDATE models SET description='MiniMax H3-Image 图片生成（文生图 / 图生图同一模型）。同步 POST /v1/images/generations，返回图片直链；只给 prompt 即文生图，附带 image 参考图（data URI）即自动按图生图处理，由服务端自行判断，无需切换模型。计费 ¥0.05/张；显式指定 4K（size=4k、4096×4096 或 3840×2160）按 ¥0.10/张；n 张按倍数累计。6 步蒸馏采样，默认出 2K 级静帧，原生最高 4096×4096。⚠️ H3-Image 没有负向提示词输入，negative_prompt / steps / cfg 会被忽略。', icon=NULL, tags='image', vendor_id=v_id, endpoints=NULL, status=1, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='h3-image' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'h3-image', 'MiniMax H3-Image 图片生成（文生图 / 图生图同一模型）。同步 POST /v1/images/generations，返回图片直链；只给 prompt 即文生图，附带 image 参考图（data URI）即自动按图生图处理，由服务端自行判断，无需切换模型。计费 ¥0.05/张；显式指定 4K（size=4k、4096×4096 或 3840×2160）按 ¥0.10/张；n 张按倍数累计。6 步蒸馏采样，默认出 2K 级静帧，原生最高 4096×4096。⚠️ H3-Image 没有负向提示词输入，negative_prompt / steps / cfg 会被忽略。', NULL, 'image', v_id, NULL, 1, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='h3-image' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('阿里巴巴', 'Qwen.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  END IF;
  UPDATE models SET description='Wan3.0 视频生成（WAN3 满血满参）。按秒计费：480P ¥0.18/秒、720P ¥0.25/秒、1080P ¥0.48/秒（基准档 480P），参考视频不额外收费。时长 4-30 整数秒逐秒可选（不传默认 5 秒）。画幅支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / auto（不传默认 16:9）。支持纯文生、首帧 / 首尾帧（尾帧需首帧，且与普通参考素材互斥）和多素材参考：参考图最多 10、参考视频最多 5、参考音频最多 5，合计最多 20 项，均须公网 http(s) URL。成片默认带音轨，参考音频用于给声音定调；带参考视频时生成时长与参考视频合计均不超过 15 秒。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=1, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='hk-wan3' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'hk-wan3', 'Wan3.0 视频生成（WAN3 满血满参）。按秒计费：480P ¥0.18/秒、720P ¥0.25/秒、1080P ¥0.48/秒（基准档 480P），参考视频不额外收费。时长 4-30 整数秒逐秒可选（不传默认 5 秒）。画幅支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / auto（不传默认 16:9）。支持纯文生、首帧 / 首尾帧（尾帧需首帧，且与普通参考素材互斥）和多素材参考：参考图最多 10、参考视频最多 5、参考音频最多 5，合计最多 20 项，均须公网 http(s) URL。成片默认带音轨，参考音频用于给声音定调；带参考视频时生成时长与参考视频合计均不超过 15 秒。', NULL, 'video', v_id, NULL, 1, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='hk-wan3' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='视频反解（输出提示词）', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=0, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='jimeng-video-reverse' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'jimeng-video-reverse', '视频反解（输出提示词）', NULL, 'video', v_id, NULL, 0, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='jimeng-video-reverse' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = 'MiniMax';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('MiniMax', 'Minimax.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = 'MiniMax';
  END IF;
  UPDATE models SET description='FastH3-Omni 多模态参考视频模型：最多 9 张设定参考图 + 3 个参考视频 + 3 个参考音频（首尾帧可另用 metadata.first_frame / last_frame 锚定，与设定参考同请求共存）。出片快：768P 约 1-2 分钟出片，3 分钟内可交付。按秒计费 ¥0.02（480P）/ ¥0.04（768p，720P 档）/ ¥0.06（1080P），时长 1-15 秒可选（默认 5 秒），支持 9 档画面比例（480p/768p/1080p × 横屏/竖屏/1:1）。⚠️ 带参考视频时计费秒数 = 生成秒数 + 参考视频总秒数（每条按实际时长四舍五入到整秒后相加，与生成同价）；例：768p 生成 5 秒 + 11 秒参考视频 = ¥0.04 × 16 秒 = ¥0.64。参考图片与参考音频不额外计费。参考视频须为可解析时长的 MP4/MOV，解析失败在提交阶段 400 拒单（不建单不扣费）。prompt 中用 <Picture 1> / <Video 1> / <Audio 1> 依次引用对应素材。⚠️ 三种创作模式按素材自动分派：① 纯文生视频＝只传 prompt（零素材）；② 图生视频 / 首尾帧＝只传 metadata.first_frame（+可选 metadata.last_frame），不带参考视频/音频，images 是设定参考不是首帧；③ 多参考＝带设定参考图 / 参考视频 / 参考音频。带参考视频或音频时属多参考档，必须同时至少一张设定参考图，只给尾帧不给首帧同样在提交阶段 400（不建单不扣费）。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=1, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='minimax_h3_omni' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'minimax_h3_omni', 'FastH3-Omni 多模态参考视频模型：最多 9 张设定参考图 + 3 个参考视频 + 3 个参考音频（首尾帧可另用 metadata.first_frame / last_frame 锚定，与设定参考同请求共存）。出片快：768P 约 1-2 分钟出片，3 分钟内可交付。按秒计费 ¥0.02（480P）/ ¥0.04（768p，720P 档）/ ¥0.06（1080P），时长 1-15 秒可选（默认 5 秒），支持 9 档画面比例（480p/768p/1080p × 横屏/竖屏/1:1）。⚠️ 带参考视频时计费秒数 = 生成秒数 + 参考视频总秒数（每条按实际时长四舍五入到整秒后相加，与生成同价）；例：768p 生成 5 秒 + 11 秒参考视频 = ¥0.04 × 16 秒 = ¥0.64。参考图片与参考音频不额外计费。参考视频须为可解析时长的 MP4/MOV，解析失败在提交阶段 400 拒单（不建单不扣费）。prompt 中用 <Picture 1> / <Video 1> / <Audio 1> 依次引用对应素材。⚠️ 三种创作模式按素材自动分派：① 纯文生视频＝只传 prompt（零素材）；② 图生视频 / 首尾帧＝只传 metadata.first_frame（+可选 metadata.last_frame），不带参考视频/音频，images 是设定参考不是首帧；③ 多参考＝带设定参考图 / 参考视频 / 参考音频。带参考视频或音频时属多参考档，必须同时至少一张设定参考图，只给尾帧不给首帧同样在提交阶段 400（不建单不扣费）。', NULL, 'video', v_id, NULL, 1, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='minimax_h3_omni' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='图生视频 · 2.0 标准档。时长 4–15 秒（逐秒可选），分辨率固定 720p，画面比例固定 9:16 竖屏，≤4 张参考图，按秒计费 ¥0.22/秒。', icon=NULL, tags='video', vendor_id=v_id, endpoints='[]', status=1, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='sd-seedance-2.0' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'sd-seedance-2.0', '图生视频 · 2.0 标准档。时长 4–15 秒（逐秒可选），分辨率固定 720p，画面比例固定 9:16 竖屏，≤4 张参考图，按秒计费 ¥0.22/秒。', NULL, 'video', v_id, '[]', 1, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='sd-seedance-2.0' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='图生视频 · 2.0 高速档。时长 4–15 秒（逐秒可选），分辨率固定 720p，画面比例固定 9:16 竖屏，≤4 张参考图，按秒计费 ¥0.10/秒。', icon=NULL, tags='video', vendor_id=v_id, endpoints='[]', status=1, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='sd-seedance-2.0fast' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'sd-seedance-2.0fast', '图生视频 · 2.0 高速档。时长 4–15 秒（逐秒可选），分辨率固定 720p，画面比例固定 9:16 竖屏，≤4 张参考图，按秒计费 ¥0.10/秒。', NULL, 'video', v_id, '[]', 1, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='sd-seedance-2.0fast' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='Seedance 2.0 标准版 文/图生视频。4–15 秒逐秒可选，固定 720p，¥0.25/秒；7 档比例（含 auto），≤10 张参考图', icon=NULL, tags='video,seedance,doubao', vendor_id=v_id, endpoints=NULL, status=1, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='seedance-2-0' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'seedance-2-0', 'Seedance 2.0 标准版 文/图生视频。4–15 秒逐秒可选，固定 720p，¥0.25/秒；7 档比例（含 auto），≤10 张参考图', NULL, 'video,seedance,doubao', v_id, NULL, 1, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='seedance-2-0' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='Seedance 2.0 Fast 文/图生视频。5–15 秒逐秒可选，¥0.10/秒；7 档比例（含 auto），≤10 张参考图', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=1, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='seedance-2-0-fast' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'seedance-2-0-fast', 'Seedance 2.0 Fast 文/图生视频。5–15 秒逐秒可选，¥0.10/秒；7 档比例（含 auto），≤10 张参考图', NULL, 'video', v_id, NULL, 1, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='seedance-2-0-fast' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='Seedance 2.5 文/图生视频。5/10/15/30 秒四档，¥3.50/次；7 档比例（含 auto），≤10 张参考图', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=1, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='seedance-2-5' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'seedance-2-5', 'Seedance 2.5 文/图生视频。5/10/15/30 秒四档，¥3.50/次；7 档比例（含 auto），≤10 张参考图', NULL, 'video', v_id, NULL, 1, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='seedance-2-5' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='Seedance 2.0 文/图生视频，5–15 秒逐秒可选（默认 15 秒），6 档比例，≤10 张参考图，卡人脸', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=0, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='seedance-2.0' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'seedance-2.0', 'Seedance 2.0 文/图生视频，5–15 秒逐秒可选（默认 15 秒），6 档比例，≤10 张参考图，卡人脸', NULL, 'video', v_id, NULL, 0, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='seedance-2.0' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='高清视频生成（促销档）。按次计费 ¥4.50/条，固定 720P 输出，价格与生成时长无关。时长 5-30 秒可选（不传默认 30 秒）。参考素材仅支持参考图：公网 http(s) URL、最多 10 张、短边 ≥720px；不支持参考视频 / 参考音频 / 首尾帧。特点：价格固定、计费可预期，适合批量出片。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=0, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='seedance2.5-活动' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'seedance2.5-活动', '高清视频生成（促销档）。按次计费 ¥4.50/条，固定 720P 输出，价格与生成时长无关。时长 5-30 秒可选（不传默认 30 秒）。参考素材仅支持参考图：公网 http(s) URL、最多 10 张、短边 ≥720px；不支持参考视频 / 参考音频 / 首尾帧。特点：价格固定、计费可预期，适合批量出片。', NULL, 'video', v_id, NULL, 0, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='seedance2.5-活动' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('字节跳动', 'Doubao.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '字节跳动';
  END IF;
  UPDATE models SET description='高清视频生成。按分辨率分档：720P ¥0.20/秒、1080P ¥0.30/秒，时长 5-30 秒逐秒可选（30 秒自动由 Prime 增强版执行）。参考素材仅支持参考图：公网 http(s) URL、最多 10 张、短边 ≥720px；不支持参考视频 / 参考音频 / 首尾帧。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=1, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='seedance2.5-特惠' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'seedance2.5-特惠', '高清视频生成。按分辨率分档：720P ¥0.20/秒、1080P ¥0.30/秒，时长 5-30 秒逐秒可选（30 秒自动由 Prime 增强版执行）。参考素材仅支持参考图：公网 http(s) URL、最多 10 张、短边 ≥720px；不支持参考视频 / 参考音频 / 首尾帧。', NULL, 'video', v_id, NULL, 1, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='seedance2.5-特惠' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('阿里巴巴', 'Qwen.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  END IF;
  UPDATE models SET description='按条计费：720P ¥5.00/条、1080P ¥7.00/条，价格与生成时长无关。Wan3.0 Pro 高清视频生成（独立上游模型，非 Wan3 标准版别名）。时长 4-30 秒整数秒可选（不传默认 5 秒）。画幅仅支持 16:9 / 9:16，输出尺寸 1280x720 / 720x1280 / 1920x1080 / 1080x1920。参考素材支持图片（最多 10）、视频（最多 5）、音频（最多 5），合计最多 20 项，均须公网 http(s) URL；平台校验参考图短边 ≥720px、参考视频合计 ≤15 秒、参考音频合计 ≤15 秒、有参考视频时生成时长 ≤15 秒。计费可预期，适合批量出片。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=0, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='wan-3.0-pro' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'wan-3.0-pro', '按条计费：720P ¥5.00/条、1080P ¥7.00/条，价格与生成时长无关。Wan3.0 Pro 高清视频生成（独立上游模型，非 Wan3 标准版别名）。时长 4-30 秒整数秒可选（不传默认 5 秒）。画幅仅支持 16:9 / 9:16，输出尺寸 1280x720 / 720x1280 / 1920x1080 / 1080x1920。参考素材支持图片（最多 10）、视频（最多 5）、音频（最多 5），合计最多 20 项，均须公网 http(s) URL；平台校验参考图短边 ≥720px、参考视频合计 ≤15 秒、参考音频合计 ≤15 秒、有参考视频时生成时长 ≤15 秒。计费可预期，适合批量出片。', NULL, 'video', v_id, NULL, 0, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='wan-3.0-pro' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('阿里巴巴', 'Qwen.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  END IF;
  UPDATE models SET description='Wan3.0 视频生成（有赞标准版）。按秒计费：720P ¥0.20/秒、1080P ¥0.25/秒（分档价，基准档 720P）。时长 10-30 整数秒逐秒可选（不传默认 10 秒）。画幅仅 16:9 / 9:16，输出尺寸 1280x720 / 720x1280 / 1920x1080 / 1080x1920。参考素材走顶层 images / videos / audios 数组（图片最多 10、视频最多 5、音频最多 5，合计最多 20 项），均须公网 http(s) URL。⚠️ 对外公开名 wan-3.0-yz，上游模型 ID 是 wan-3.0 —— 调用时请用带 yz 的公开名。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=0, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='wan-3.0-yz' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'wan-3.0-yz', 'Wan3.0 视频生成（有赞标准版）。按秒计费：720P ¥0.20/秒、1080P ¥0.25/秒（分档价，基准档 720P）。时长 10-30 整数秒逐秒可选（不传默认 10 秒）。画幅仅 16:9 / 9:16，输出尺寸 1280x720 / 720x1280 / 1920x1080 / 1080x1920。参考素材走顶层 images / videos / audios 数组（图片最多 10、视频最多 5、音频最多 5，合计最多 20 项），均须公网 http(s) URL。⚠️ 对外公开名 wan-3.0-yz，上游模型 ID 是 wan-3.0 —— 调用时请用带 yz 的公开名。', NULL, 'video', v_id, NULL, 0, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='wan-3.0-yz' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('阿里巴巴', 'Qwen.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  END IF;
  UPDATE models SET description='Wan3.0 视频生成，固定输出 720P。支持文生视频、图生视频与多模态参考（图 / 视频 / 音频），时长 2-30 秒。计费按秒 ¥0.20，单一价位，不随分辨率变化。⚠️ 带参考视频时按「生成秒数 + 参考视频总秒数（四舍五入）」× ¥0.20 计费，例如 10 秒参考视频 + 生成 10 秒 = ¥4.00（不传参考视频只要 ¥2.00）。参考视频单条最长 15 秒。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=1, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='wan3.0-720p-官网' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'wan3.0-720p-官网', 'Wan3.0 视频生成，固定输出 720P。支持文生视频、图生视频与多模态参考（图 / 视频 / 音频），时长 2-30 秒。计费按秒 ¥0.20，单一价位，不随分辨率变化。⚠️ 带参考视频时按「生成秒数 + 参考视频总秒数（四舍五入）」× ¥0.20 计费，例如 10 秒参考视频 + 生成 10 秒 = ¥4.00（不传参考视频只要 ¥2.00）。参考视频单条最长 15 秒。', NULL, 'video', v_id, NULL, 1, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='wan3.0-720p-官网' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('阿里巴巴', 'Qwen.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  END IF;
  UPDATE models SET description='Wan3.0 官方模型，原生 720P 输出，固定时长 30 秒。支持文生视频、图生视频与多张参考图（≤9 张），6 档画面比例（自适应 / 16:9 / 9:16 / 1:1 / 4:3 / 3:4）。特点：按次计费 ¥4.50/次，不随时长与分辨率变化；支持稳定高并发，适合批量出片。⚠️ 不支持负向提示词（negativePrompt）与 seed 参数。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=0, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='wan3.0-720p-按次' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'wan3.0-720p-按次', 'Wan3.0 官方模型，原生 720P 输出，固定时长 30 秒。支持文生视频、图生视频与多张参考图（≤9 张），6 档画面比例（自适应 / 16:9 / 9:16 / 1:1 / 4:3 / 3:4）。特点：按次计费 ¥4.50/次，不随时长与分辨率变化；支持稳定高并发，适合批量出片。⚠️ 不支持负向提示词（negativePrompt）与 seed 参数。', NULL, 'video', v_id, NULL, 0, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='wan3.0-720p-按次' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('阿里巴巴', 'Qwen.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  END IF;
  UPDATE models SET description='Wan3.0 智能调度视频生成。支持 480P（¥0.28/s）、720P（¥0.28/s）、1080P（¥0.30/s），时长 2-30 秒。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=0, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='wan3.0-smart' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'wan3.0-smart', 'Wan3.0 智能调度视频生成。支持 480P（¥0.28/s）、720P（¥0.28/s）、1080P（¥0.30/s），时长 2-30 秒。', NULL, 'video', v_id, NULL, 0, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='wan3.0-smart' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('阿里巴巴', 'Qwen.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  END IF;
  UPDATE models SET description='Wan3 标准版视频生成。720P / 1080P 同价 ¥0.23/秒，时长 10-30 秒逐秒可选（不传默认 10 秒）。参考素材仅支持参考图：公网 http(s) URL、最多 10 张、短边 ≥720px、长边 ≤1920px；不支持参考视频 / 参考音频 / 首尾帧。7 档画面比例（自适应 / 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9）。480P 已下架，显式传 480P 按 720P 执行。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=0, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='wan3.0-video' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'wan3.0-video', 'Wan3 标准版视频生成。720P / 1080P 同价 ¥0.23/秒，时长 10-30 秒逐秒可选（不传默认 10 秒）。参考素材仅支持参考图：公网 http(s) URL、最多 10 张、短边 ≥720px、长边 ≤1920px；不支持参考视频 / 参考音频 / 首尾帧。7 档画面比例（自适应 / 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9）。480P 已下架，显式传 480P 按 720P 执行。', NULL, 'video', v_id, NULL, 0, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='wan3.0-video' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('阿里巴巴', 'Qwen.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  END IF;
  UPDATE models SET description='Wan3 Prime 增强画质版（独立上游）。仅 720P 输出 ¥0.25/秒，时长仅接受 30 秒（其他时长会被拒绝）。参考素材仅支持参考图：公网 http(s) URL、最多 10 张、短边 ≥720px；不支持参考视频 / 参考音频 / 首尾帧。输出尺寸 1280x720 / 720x1280。1080P 已下架，显式请求会被拒绝。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=0, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='wan3.0-video-prime' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'wan3.0-video-prime', 'Wan3 Prime 增强画质版（独立上游）。仅 720P 输出 ¥0.25/秒，时长仅接受 30 秒（其他时长会被拒绝）。参考素材仅支持参考图：公网 http(s) URL、最多 10 张、短边 ≥720px；不支持参考视频 / 参考音频 / 首尾帧。输出尺寸 1280x720 / 720x1280。1080P 已下架，显式请求会被拒绝。', NULL, 'video', v_id, NULL, 0, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='wan3.0-video-prime' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('阿里巴巴', 'Qwen.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  END IF;
  UPDATE models SET description='Wan3.0 特价满血档视频生成。按秒计费：480P ¥0.25/秒、720P ¥0.48/秒、1080P ¥0.96/秒。时长 2-30 秒整数秒可选（不传默认 5 秒）。画幅支持 16:9 / 9:16 / 4:3 / 3:4 / 1:1。支持文生视频、首尾帧生视频与多图参考生视频；参考素材支持图片（最多 10，jpg/jpeg/png/bmp/webp）、视频（最多 5，mp4/mov）、音频（最多 5，wav/mp3），均须公网 http(s) URL。首尾帧与参考类素材不可混用。满血算力档，画质与稳定性优于标准档。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=1, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='wan3.0-video-prime-tj' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'wan3.0-video-prime-tj', 'Wan3.0 特价满血档视频生成。按秒计费：480P ¥0.25/秒、720P ¥0.48/秒、1080P ¥0.96/秒。时长 2-30 秒整数秒可选（不传默认 5 秒）。画幅支持 16:9 / 9:16 / 4:3 / 3:4 / 1:1。支持文生视频、首尾帧生视频与多图参考生视频；参考素材支持图片（最多 10，jpg/jpeg/png/bmp/webp）、视频（最多 5，mp4/mov）、音频（最多 5，wav/mp3），均须公网 http(s) URL。首尾帧与参考类素材不可混用。满血算力档，画质与稳定性优于标准档。', NULL, 'video', v_id, NULL, 1, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='wan3.0-video-prime-tj' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('阿里巴巴', 'Qwen.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  END IF;
  UPDATE models SET description='Wan3.0 特价档视频生成。按秒计费：480P ¥0.15/秒、720P ¥0.25/秒、1080P ¥0.552/秒。时长 2-30 秒整数秒可选（不传默认 5 秒）。画幅支持 16:9 / 9:16 / 4:3 / 3:4 / 1:1。支持文生视频、首尾帧生视频与多图参考生视频；参考素材支持图片（最多 10，jpg/jpeg/png/bmp/webp）、视频（最多 5，mp4/mov）、音频（最多 5，wav/mp3），均须公网 http(s) URL。首尾帧与参考类素材不可混用。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=1, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='wan3.0-video-tj' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'wan3.0-video-tj', 'Wan3.0 特价档视频生成。按秒计费：480P ¥0.15/秒、720P ¥0.25/秒、1080P ¥0.552/秒。时长 2-30 秒整数秒可选（不传默认 5 秒）。画幅支持 16:9 / 9:16 / 4:3 / 3:4 / 1:1。支持文生视频、首尾帧生视频与多图参考生视频；参考素材支持图片（最多 10，jpg/jpeg/png/bmp/webp）、视频（最多 5，mp4/mov）、音频（最多 5，wav/mp3），均须公网 http(s) URL。首尾帧与参考类素材不可混用。', NULL, 'video', v_id, NULL, 1, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='wan3.0-video-tj' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('阿里巴巴', 'Qwen.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  END IF;
  UPDATE models SET description='Wan3.0 视频生成。支持 480P（¥0.20/s）、720P（¥0.45/s）、1080P（¥0.80/s），时长 2-30 秒。⚠️ 带参考视频时按「生成秒数 + 参考视频总秒数（四舍五入）」× 同档单价计费，例如 10 秒参考视频 + 生成 10 秒 480P = ¥4.00（不传参考视频只要 ¥2.00）。参考视频单条最长 15 秒。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=1, sync_official=1, name_rule=0, updated_time=1795340886 WHERE model_name='wan3.0-video-官网' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'wan3.0-video-官网', 'Wan3.0 视频生成。支持 480P（¥0.20/s）、720P（¥0.45/s）、1080P（¥0.80/s），时长 2-30 秒。⚠️ 带参考视频时按「生成秒数 + 参考视频总秒数（四舍五入）」× 同档单价计费，例如 10 秒参考视频 + 生成 10 秒 480P = ¥4.00（不传参考视频只要 ¥2.00）。参考视频单条最长 15 秒。', NULL, 'video', v_id, NULL, 1, 1, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='wan3.0-video-官网' AND x.deleted_at IS NULL);
  SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  IF v_id IS NULL THEN
    INSERT INTO vendors (name, icon, status, created_time, updated_time) VALUES ('阿里巴巴', 'Qwen.Color', 1, 1795340886, 1795340886);
    SELECT id INTO v_id FROM vendors WHERE name = '阿里巴巴';
  END IF;
  UPDATE models SET description='Wan3.0 视频生成，480P / 720P / 1080P 同价，按次计费 ¥3.50/次，不随时长与分辨率变化。支持文生视频、首帧控制、首尾帧控制与多模态参考（参考图 ≤10 张 / 音频 ≤5 段 / 视频 ≤5 段），时长 2–30 秒可选，7 档画面比例（自适应 / 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9）。特点：批次调度出片，提交后需等待调度，出片等待时间明显长于常规通道，适合不赶时间的批量出片。', icon=NULL, tags='video', vendor_id=v_id, endpoints=NULL, status=0, sync_official=0, name_rule=0, updated_time=1795340886 WHERE model_name='wan3.0-班车' AND deleted_at IS NULL;
  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time) SELECT 'wan3.0-班车', 'Wan3.0 视频生成，480P / 720P / 1080P 同价，按次计费 ¥3.50/次，不随时长与分辨率变化。支持文生视频、首帧控制、首尾帧控制与多模态参考（参考图 ≤10 张 / 音频 ≤5 段 / 视频 ≤5 段），时长 2–30 秒可选，7 档画面比例（自适应 / 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9）。特点：批次调度出片，提交后需等待调度，出片等待时间明显长于常规通道，适合不赶时间的批量出片。', NULL, 'video', v_id, NULL, 0, 0, 0, 1795340886, 1795340886 WHERE NOT EXISTS (SELECT 1 FROM models x WHERE x.model_name='wan3.0-班车' AND x.deleted_at IS NULL);
END $do$;

COMMIT;

-- ── 验证 ──
-- SELECT model||' | '||coalesce(v.name,'-')||' | st='||m.status||' | tags='||coalesce(m.tags,'-')
--   FROM models m LEFT JOIN vendors v ON v.id=m.vendor_id WHERE m.deleted_at IS NULL ORDER BY 1;
-- SELECT model, "group", channel_id, enabled FROM abilities ORDER BY model;
