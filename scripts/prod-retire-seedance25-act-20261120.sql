-- 2026-11-20  seedance2.5-活动 下架（运营口径：停售，请求方直接被告知换用特惠）
--
-- 为什么不是「删模型」：
--   models 行要留着 —— 历史任务、消费日志、ModelPrice 都按这个名字记账，
--   删了等于把过去的账单对不上账。下架的正确动作是 status=0（广场隐藏）
--   + abilities 不启用 + 应用层拦截提示，三者各管一段。
--
-- 执行范围（实测）：
--   · 老站 119.29.253.97：models.status 本来就是 0，abilities 里**没有**这条路由，
--     options.Seedance25TeHuiRoute30s.source_models 只有特惠 → 数据面无需改动，
--     只靠代码拦截（e7a6c724d）返回提示。
--   · 新站 114.132.252.150：models.status=0，但 abilities 仍有 ch17 一条 enabled 路由，
--     且策略 source_models 里带着活动版（会静默改派到 seedance-2-5）→ 需要下面 1~3 步。
--
-- 应用层拦截见 middleware/retired_model.go（retiredModelReplacements），
-- 命中即在选渠道之前返回：「模型已下线，请使用 seedance2.5-特惠」。
-- 因此本脚本属于「双保险」：即使哪天拦截被摘掉，也不会回到静默改派。

\pset pager off

-- 1) 备份将被改动的路由行（新站执行结果为 1 行）
drop table if exists bak_20261120_abilities_act;
create table bak_20261120_abilities_act as
select * from abilities where model = 'seedance2.5-活动';

-- 2) 下架路由
update abilities set enabled = false where model = 'seedance2.5-活动';

-- 3) 视频路由策略里摘掉活动源，只留特惠
--    原值备份在执行机 /tmp/opt_before.json（本次为新站 /root 会话内生成）。
--    注意：改 options 只改库，new-api 的内存策略要重启后才重建，
--    所以本次执行后对新站容器做了一次 docker restart。
update options
set value = jsonb_set(
        value::jsonb,
        '{source_models}',
        coalesce(
          (select jsonb_agg(elem)
             from jsonb_array_elements(value::jsonb -> 'source_models') elem
            where elem #>> '{}' <> 'seedance2.5-活动'),
          '[]'::jsonb
        )
      )::text
where key = 'Seedance25TeHuiRoute30s';

-- 4) 复核
select channel_id, model, enabled, "group" from abilities where model = 'seedance2.5-活动';
select key, value::jsonb -> 'source_models' as source_models
  from options where key = 'Seedance25TeHuiRoute30s';
