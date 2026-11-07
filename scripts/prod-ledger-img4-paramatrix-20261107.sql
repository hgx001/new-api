-- 生产台账：2026-11-07 图片三模型全参数矩阵（ArcReel 直连 + new-api 用户链路）
-- 授权依据：用户 2026-11-07「配置到 new-api，模拟用户端到端测试可用性；ArcReel 侧每个模型传递的参数都要测试通过」
-- quota 一律 0（真实扣费已在 logs / ArcReel 钱包入账，台账行不重复计入余额统计）
-- 成本真相源：ArcReel wallet_reservations（openapi-service 钱包），settled 合计 640 分 = ¥6.40；
--   失败/取消 10 笔释放（dola 1 / doubao 2 / gemini 7，¥5.20 冻结已退）。
-- 结论：dola 8/8 比例档 + count=2 + 参考图全过；doubao 8/8 比例 + count=2 过、参考图官网无上传入口；
--   gemini 仅 3:4/16:9 比例生效（9:16、1:1 被站点忽略），其余 4 单因账号 needs_relogin 取消。
-- new-api 用户链路：正向 5/9 出图、负向 3/3 返回 400（非法比例 / seconds / data URI 参考图）。

INSERT INTO logs (user_id, created_at, type, content, username, token_name, model_name,
                   quota, prompt_tokens, completion_tokens, use_time, is_stream,
                   channel_id, channel_name, token_id, "group", ip, request_id, other)
VALUES
(28, extract(epoch from now())::bigint, 5,
 '[OPERATOR-COST-ACCOUNTING] Seedream 4.5 全参数矩阵实测（13 单结算，¥2.80；比例 8 档全 MATCH）。证据：matrix 缺省 gen-df4ca5ad11e5f456fa7f03e3(2048x2048/4.7MB)、16:9 gen-afc0046c232b3f7ebbab1da8(2848x1600/6.3MB)、2:3 1664x2496、3:2 2496x1664、1:1 补跑 gen-877cc83feab778aad55e07a4(2048x2048/5.3MB)、count=2 与参考图各 1 单；new-api 用户链路 base/ratio=3:2/ref 3 单全 completed+PNG。风险：9:16(216x384/134KB)、3:4(288x384/196KB)、4:3(384x288/188KB)、ref(384x384/249KB) 交付为低清预览图，疑似取图节点抓到站点预览而非原图，待查。依据：用户 2026-11-07 授权参数矩阵实测。',
 'admin', 'OPERATOR-COST-ACCOUNTING', 'Seedream 4.5', 0, 0, 0, 0, false,
 30, '漫屋ArcReel-image(m3only)', 166, 'm3only', '', '', ''),

(28, extract(epoch from now())::bigint, 5,
 '[OPERATOR-COST-ACCOUNTING] Seedream 5.0 Flash 全参数矩阵实测（11 单结算，¥2.40；比例 8 档全 MATCH，分辨率 1536x1536~2848x1600 稳定）。证据：matrix 缺省 gen-8c4d0987b19424acbe331c77(1728x2304/4.8MB)、16:9 gen-ae444092ca0678532434f288(2848x1600)、9:16 1600x2848、2:3 1664x2496、count=2；new-api 用户链路 base/ratio=3:2 2 单 completed+PNG。唯一失败：参考图 2 单（matrix+new-api）均在官网「无 input[type=file] 上传入口」阶段被 Worker 硬闸门拒单，未提交未扣费（¥0.40 已释放），属豆包官网页面结构变更，与账号无关。依据：用户 2026-11-07 授权参数矩阵实测。',
 'admin', 'OPERATOR-COST-ACCOUNTING', 'Seedream 5.0 Flash', 0, 0, 0, 0, false,
 30, '漫屋ArcReel-image(m3only)', 166, 'm3only', '', '', ''),

(28, extract(epoch from now())::bigint, 5,
 '[OPERATOR-COST-ACCOUNTING] Nano Banana Pro 参数矩阵实测（4 单结算，¥1.20；比例仅部分生效）。证据：16:9 gen-96d9108ad6ba417cd117c5e6 实出 1024x559 MATCH；3:4 实出 765x1024 MATCH；9:16 与 1:1 请求均实出 1024x559（16:9 缺省）→ 比例被忽略。原因：Gemini 网页图片模式的宽高比控件按设计可缺省（Worker `buildFillImageParamsScript` 找不到控件即跳过 image-params-skipped:aspect-ratio-unavailable），非报错。另 4 单（4:3 / 21:9 / count=2 / 参考图）因唯一 Gemini 账号 needs_relogin 无法执行、已取消（¥1.20 已释放）。new-api 用户链路 3 单同因取消。依据：用户 2026-11-07 授权参数矩阵实测。',
 'admin', 'OPERATOR-COST-ACCOUNTING', 'Nano Banana Pro', 0, 0, 0, 0, false,
 30, '漫屋ArcReel-image(m3only)', 166, 'm3only', '', '', '');

\echo '=== 台账行 ==='
SELECT id, model_name, quota, channel_id, left(content, 60) AS head FROM logs
WHERE token_name='OPERATOR-COST-ACCOUNTING' AND created_at > extract(epoch from now())::bigint - 60
ORDER BY id;
