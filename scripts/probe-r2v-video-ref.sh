#!/usr/bin/env bash
# 【已废弃 —— 不要再跑这个脚本】
#
# 2026-10-06 事故记录：这个脚本里的「对照组」用了一个**能通过校验**的入参
# （wan3.0-smart + 视频参考），于是**真建单并扣费**。我因此连续误建 2 单
# （task 324、325），上游各扣 90 有赞积分，退不回来。
# 这正是 AGENTS.md「生产运维硬约束 → 验证脚本不得用能通过校验的入参」禁止的事，
# 我在写下那条规矩的同一天又犯了两次。
#
# 另一个错误：用 `{"reference_video": "..."}` 探视频参考，但 TaskSubmitReq
# **没有这个字段**（只有 media[] / metadata.media），它被静默忽略，导致：
#   - r2v 报「缺参考图」而不是「不支持视频参考」，把我引向错误的代码修复
#   - 对照组的 wan3.0-smart 根本没拿到视频，生成的片子只用了 prompt，
#     我却据此宣称「wan3.0-smart 视频参考已验证端到端」—— 结论是错的
#
# 正确做法：用 probe-r2v-vidref-correct-shape.sh（零成本，正确 media 形状），
# 付费对照组必须显式 ALLOW_PAID=1 且单独提出，不要藏在默认路径里。
set -uo pipefail
echo "这个脚本已在 2026-10-06 作废，理由见上方注释。"
echo "请改用: /home/ubuntu/probe-r2v-vidref-correct-shape.sh"
exit 1
