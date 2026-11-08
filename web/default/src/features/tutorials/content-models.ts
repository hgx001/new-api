/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { TutorialSection } from './content'

// ----------------------------------------------------------------------------
// 下游模型对接文档（模型总览 / 文本 / 图片 / 视频 / 计费 / 错误码）。
// 价格与参数取自网关当前生产配置，调整定价或模型上下架时需同步本文件。
// ----------------------------------------------------------------------------

const MODEL_OVERVIEW_ITEMS = [
  'MiniMax-M3｜文本｜输入 ¥0.22 / 1M，输出 ¥0.91 / 1M',
  'MiniMax-M3.1｜文本｜输入 ¥0.22 / 1M，输出 ¥0.91 / 1M（与 MiniMax-M3 同价，实际转发到上游 MiniMax-M3）',
  'gpt-6-luna｜文本｜输入 ¥0.94 / 1M，输出 ¥7.50 / 1M',
  'gpt-6-sol｜文本｜输入 ¥18.75 / 1M，输出 ¥150.00 / 1M',
  'gpt-6.1-sol｜文本｜输入 ¥18.75 / 1M，输出 ¥150.00 / 1M（与 gpt-6-sol 同价）',
  'gpt-image-2.5-官方｜图片｜¥0.15 / 张',
  'wan3.0-video｜视频（按秒）｜480P / 720P / 1080P 同价 ¥0.23/秒，时长 2–30 秒，参考视频按时长并入（生成秒数+参考视频秒数）',
  'wan3.0-video-prime｜视频（按秒）｜720P ¥0.25/s、1080P ¥0.30/s，时长 2–30 秒，参考视频按时长并入',
  'seedance2.5-特惠｜视频（按秒）｜720P ¥0.20/s、1080P ¥0.30/s，时长 2–30 秒（带参考视频时 ≤15 秒），参考视频按时长并入',
  'wan3.0-video-官网｜视频（按秒）｜480P ¥0.20/s、720P ¥0.45/s、1080P ¥0.80/s，参考视频按时长同价并入（生成秒数+参考视频秒数）',
  'wan3.0-720p-特惠｜视频（按秒）｜720P ¥0.20/s，时长 2–30 秒，参考视频按时长并入',
  'minimax_h3_v2｜视频（按秒）｜480P ¥0.03/s、768P ¥0.05/s，时长 1–15 秒，文生 / 首帧图生 / 首尾帧三合一，最多 2 张关键帧',
  'minimax_h3_omni｜视频（按秒）｜480P ¥0.02/s、768P ¥0.04/s、1080P ¥0.06/s，设定参考最多 9 图 + 3 视频 + 3 音频；⚠️ 参考视频秒数并入计费（生成秒数 + 参考视频秒数），图/音频不额外计费，768P 约 1–2 分钟出片',
  'seedance-2.0｜视频（按次）｜¥1.00 / 次，支持 5–15 秒逐秒',
  'seedance-2-0-fast｜视频（按秒）｜¥0.10 / 秒，支持 5–15 秒逐秒',
  'seedance-2-5｜视频（按次）｜¥3.50 / 次，5/10/15/30 秒同价',
  'Nano Banana Pro｜图片（按张）｜¥0.30 / 张，异步任务，走 /v1/videos 提交与轮询',
  'jimeng-video-reverse｜提示词（按次）｜¥1.00 / 次',
]

const COMPAT_MODEL_ITEMS = [
  'gpt-5.6-luna：等价 gpt-6-luna，同价（老 Key 无需改代码）',
  'gpt-5.6-sol：等价 gpt-6-sol，同价',
  'MiniMax-M2.7：输入价与 MiniMax-M3 相同，输出价较低（¥0.09 / 1M）；2026-10-31 起 MiniMax-M3 / M3.1 输出价已上调 10 倍，两者不再同价',
  'wan3.0-720p-官网：wan3.0-720p-特惠 的旧名（2026-10-30 改名），同价同链路，仅为老代码兼容而保留',
  'minimax_h3-文生 / minimax_h3-图生：2026-11-07 起退役为隐藏别名，模型广场与 /v1/models 都不再展示，老代码仍可路由；新接入请改用 minimax_h3_v2（文生/图生/首尾帧已合并为一个模型）',
]

const VIDEO_FLOW_ITEMS = [
  'POST /v1/videos：提交任务，立即返回任务 id',
  'GET /v1/videos/{id}：轮询状态，建议 5–10 秒一次',
  'GET /v1/videos/{id}/content：任务完成后下载成片',
]

const VIDEO_COMMON_FIELDS = [
  'model：必填，模型名',
  'prompt：必填，文本描述；引用素材可用 @图片1 / @视频1 / @音频1（也支持 Image 1 等英文写法）',
  'seconds / duration：时长（秒），duration 传整数（推荐）；seconds 为字符串（OpenAI 风格，如 "5"），传数字会报 invalid_json；两者都传时以 duration 为准',
  'resolution：分辨率档位，大小写均可（720p / 720P）。⚠️ minimax_h3_v2 / minimax_h3_omni 用「档位+方向」格式（480p横 / 768p竖 / 1080p(1:1)），大小写敏感，见各自小节',
  'ratio / aspect_ratio：画幅比例（官网渠道请放在 metadata.ratio，或用 size）',
  'images / input_reference / image：参考图，字符串或数组，需公网 HTTPS 直链。⚠️ 这几个字段在有赞 wan3.0-video / wan3.0-video-prime 上一律被标记为 reference_image（往里塞 .mp4 会被当图片送上游），只有 wan3.0-video-官网 会按 URL 后缀推断类型',
  '参考图字段名（漫屋链路模型：Nano Banana Pro、seedance-2.0、seedance-2-5、seedance-2-0-fast）：input_reference / inputReference / images / image / image_url / imageUrl / image_urls / imageUrls / reference_image / referenceImage / reference_images / referenceImages / input_images / inputImages 都能识别。⚠️ 只取**第一个有值的字段**，不会合并 —— 同时传 images 和 image_urls 时只认 images，另一个被丢弃',
  '⚠️ 提示词引用校验（漫屋链路模型）：提示词里写了「图1 / 图片1 / Image 1」这类标记时，标记数量不能超过实际传入张数，否则提交前直接 400（reason=reference_image_coverage_mismatch），不建单不扣费。多传图不报错，少传图必报错',
  'media：结构化素材数组，元素形如 {"type":"reference_image","url":"..."}，type 可取 reference_image / reference_video / reference_audio / first_frame（首帧）/ last_frame（尾帧）。⚠️ 有赞 wan3.0-video / wan3.0-video-prime / 官网 传参考视频、参考音频只有这一条路',
  'media 的数量上限（wan3 系）：参考图 ≤10、参考视频 ≤5、参考音频 ≤5，合计 ≤20；first_frame 与 last_frame 各最多 1 张，且**不能与参考图/参考视频/参考音频混用**（混用直接 400，不消耗费用）。⚠️ 有赞 wan3.0-video / wan3.0-video-prime / seedance2.5-特惠 的上游已取消首尾帧，传入直接 400，请改用 reference_image；wan3.0-video-官网 与 wan3.0-720p-特惠 仍支持首尾帧',
  '顶层 audios / audio：⚠️ 只有 minimax_h3_omni 读取（最多 3 条）。wan3 系不读这两个字段，传了会被静默丢弃——不报错、不计入上限、任务照跑，请改用 media 数组；minimax_h3_v2 没有音频槽位，传入直接 400（需要音频参考请改用 minimax_h3_omni）',
  'metadata：兜底传参，支持 metadata.resolution、metadata.ratio、metadata.media。⚠️ metadata.audio（是否生成音轨）只对 wan3.0-video-官网 生效，有赞 wan3.0-video / wan3.0-video-prime 不读这个字段（传了被静默丢弃）',
  '⚠️ 「传图片给视频模型」的三种写法（任选其一，优先级 media > metadata.media > 旧字段）：① media 里 type=reference_image；② metadata.media（同样格式）；③ images / input_reference / image。后者会被一律标记为参考图，不会按后缀推断类型；minimax_h3_v2 的 images 可选（不传=纯文生，1 张=首帧，2 张=首尾帧，第 3 张直接 400），minimax_h3_omni 的 images 是「设定参考」（≤9 张），首尾帧另走 metadata.first_frame / metadata.last_frame',
  '⚠️ 首尾帧是图片的一种特殊类型：在 media 里用 type=first_frame / last_frame，各最多 1 张。⚠️ 有赞 wan3.0-video / wan3.0-video-prime 的上游已取消首尾帧，传入直接 400（不消耗费用），请改用 reference_image；官网渠道仍支持，且首尾帧不能与普通参考图/视频/音频混用（混用直接 400，不消耗费用）',
]

const VIDEO_STATUS_ITEMS = [
  'queued：排队中',
  'in_progress：生成中，progress 为 0–100',
  'completed：完成，取 metadata.url',
  'failed：失败，读 error.message，费用已自动退还',
]

const MEDIA_RULE_ITEMS = [
  '图片有三种传法：① 内联 base64（data URL）；② 公网 http(s) 直链；③ 上游若还支持文件上传则走 multipart。⚠️ base64 只有 wan3 系（wan3.0-video / wan3.0-video-prime / wan3.0-video-官网 / wan3.0-720p-特惠 / seedance2.5-特惠）与 H3 系（minimax_h3_v2 / minimax_h3_omni）支持；漫屋链路模型（seedance-2.0 / seedance-2-5 / seedance-2-0-fast / Nano Banana Pro / jimeng-video-reverse）只收公网 http(s) URL，传 base64 直接 400（不建单、不扣费）',
  'H3 系（minimax_h3_v2 / minimax_h3_omni）的参考素材两种写法都收：内联 data URI，或公网 http(s) 直链（网关代下载后转成 data URI 再下发，单份下载超时 60 秒）。data URI 的 MIME 前缀必须与素材类型一致（data:image/… / data:video/… / data:audio/…）；单份体积上限 图 10MB / 视频 50MB / 音频 30MB。格式或体积不符在提交阶段 400，不建单不扣费',
  '内联 base64（推荐首选）：格式 data:{MIME_type};base64,{base64_data}，例如 data:image/png;base64,iVBORw0KGgo...。不需要任何公网图床，也不受图床可达性与速度影响，是最稳的写法',
  'base64 图片限制（通用）：单张解码后 ≤20MB，宽高 [240, 8000] 像素，宽高比 1:8 ~ 8:1；格式 PNG / JPG / BMP / WEBP；base64 约为原图的 4/3，请求体上限默认 128MB。⚠️ H3 系（minimax_h3_v2 / minimax_h3_omni）以各自小节的 10MB / 50MB / 30MB 上限与 MIME 前缀校验为准',
  '公网 URL：必须是公网可访问的 http(s) 直链，不接受内网地址、localhost、本地路径；带鉴权、防盗链或会跳转到登录页的地址一律无效',
  '图片：PNG / JPG / BMP / WEBP，单张 ≤20MB，单边 240–8000 像素。⚠️ 上游不接受带透明通道的 PNG（渲染会出问题）：有赞 wan3 系会在上传前自动压白底，wan3.0-video-官网 / wan3.0-720p-特惠 不会转换格式，请自行先转成 RGB 再提交',
  '参考视频（wan3 系）：单个 1–15 秒、合计 ≤15 秒，官网与有赞渠道都在本地强制校验，超限直接 400（不建单、不扣费）；带参考视频时「生成时长」另有上限，见各模型条目。⚠️ minimax_h3_omni 不设单条 / 合计秒数上限，但参考视频按实际秒数并入计费，见该模型小节',
  '参考视频时长由网关下载文件头（MP4/MOV 的 mvhd）解析：解析不出时长的素材（URL 打不开、不是真正的 MP4/MOV、鉴权失效）会被直接拒绝（不建单、不扣费），因为按 0 秒计费会漏收。wan3 系与 minimax_h3_omni 都按此口径 —— webm 等无 mvhd 的容器无法计费，会一并被拒',
  '参考音频：单个 1–15 秒、合计 ≤15 秒、单条 ≤15MB（音频时长不在本地校验，由上游拒绝）',
  '⚠️ 2026-11-01 起，wan3 系提交前由网关代下载并转存图片：传入的公网图片 URL 会先被网关拉下来、转存到网关自带的暂存图床，再把自家地址交给上游。上游因此不再需要访问你的图床，只要网关能打开你的地址即可',
  '代下载有 30 秒单图上限，超时即拒单（400，指明是哪张图拉不到）。实测部分境外免费图床达不到这个要求：tmpfiles.org 连不上、litter.catbox.moe 单次要 88 秒 —— 请换用国内可直连的图床，或直接改用 base64',
  '转存失败在提交阶段就返回 400，不再出现「提交成功、生成阶段才失败、白等 1~3 分钟」的情况；转存发生在预扣费之前，拒单不扣费',
  '漫屋链路模型不走代下载转存：图片 URL 由 ArcReel 侧在任务提交后拉取，素材 URL 需在提交后至少 90 秒内保持可访问',
  '图床防盗链或签名过期会导致任务失败，建议使用稳定的公网地址',
]

const BILLING_ITEMS = [
  '文本：输入、输出 token 分别计价',
  '图片：按张计费，gpt-image-2.5-官方 ¥0.15 / 张（n=2 扣两次）；Nano Banana Pro ¥0.30 / 张，走 /v1/videos 异步任务，成图读 metadata.url',
  '视频（按秒）：单价 × 秒数 × 分辨率档位倍率；seedance-2-0-fast 无分辨率档位，固定 ¥0.10 / 秒',
  '视频（按秒）+ 参考视频：秒数 = 生成秒数 + 参考视频总秒数（四舍五入后相加），再乘单价。例：wan3.0-video 生成 2 秒 + 3 秒参考视频 = 0.23 × 5 = ¥1.15。不带参考视频就只按生成秒数收。⚠️ minimax_h3_omni 同样按此口径，只是还要再乘分辨率档位倍率（见下一条与该模型小节）',
  '视频（按秒）+ 分辨率倍率：minimax_h3_v2 / minimax_h3_omni 按档位乘倍率 —— v2 以 ¥0.03 为基准（480P 1.0 / 768P 1.667），omni 以 ¥0.02 为基准（480P 1.0 / 768P 2.0 / 1080P 3.0，1080P 仅 omni）。v2 的参考图不额外计费；omni 的参考图 / 参考音频不额外计费，但参考视频按上一条并入计费秒数。例：v2 768p横 生成 5 秒 = ¥0.05 × 5 = ¥0.25；omni 768p横 生成 5 秒（无参考视频）= ¥0.04 × 5 = ¥0.20；omni 768p横 生成 5 秒 + 11 秒参考视频 = ¥0.04 × 16 = ¥0.64',
  '视频（按次）：素材不额外计费，秒数只影响上游是否接单',
  '视频（按次）：seedance-2.0 ¥1.00 / 次，seedance-2-5 ¥3.50 / 次，jimeng-video-reverse 固定 ¥1.00 / 次',
  '任务失败：自动全额退还，无需申请',
  '内容审核：提示词或素材触发上游审核导致失败时，失败原因为「内容审核不通过」，费用自动退还',
]

const QUOTA_QUERY_ITEMS = [
  '接口：GET /v1/dashboard/billing/quota；使用 Authorization: Bearer <API_KEY>，不需要 query 参数或 request body。身份只由当前 API Key 决定。',
  '查询为只读操作：不扣费、不记模型调用日志；响应带 Cache-Control: no-store。',
  'quota.remain_user 是用户钱包剩余 quota；quota.remain_token 是令牌剩余额度，不限量令牌为 null。quota.effective_remain 是下游应使用的可用余额：有限额令牌取 min(remain_user, remain_token)，不限量令牌取 remain_user。',
  '程序判断和告警请使用 effective_remain（quota）；display_remain 仅供展示，受站点币种与汇率设置影响。quota_per_unit 表示 500000 quota = $1。',
  '响应可能因批量落库最多滞后 5 秒；建议每 10 秒或更长时间轮询一次。限流为每用户 6 次 / 60 秒；429 不带 Retry-After，等下一轮再请求。',
  'HTTP 状态：200 成功（余额低也返回 200）；401 API Key 无效/缺失；403 用户封禁或调用权限不足；429 触发限流；500 查询失败。错误体为 OpenAI 风格 error 对象。',
]

const QUOTA_QUERY_EXAMPLE = (
  url: string
) => `curl ${url}/v1/dashboard/billing/quota \\
  -H "Authorization: Bearer $NEW_API_KEY"`

const ERROR_ITEMS = [
  '400：参数错误（时长/比例/分辨率非法、缺少必填素材、首尾帧与参考素材混用等），按 message 修正后重试',
  '401：Key 缺失 / 无效 / 过期，检查 Authorization 头',
  '403：配额不足（insufficient_user_quota），或该 Key 无此模型权限',
  '404：任务不存在或不属于该账号',
  '408：提交结果未知（超时），不要立即重复提交，先查询任务状态',
  '429：触发限流或并发上限，建议指数退避重试',
  '500：请求构建失败（build_request_failed）或内部错误，检查参数后重试',
  '502 / 503：上游暂时不可用或模型不可用（model_not_found），稍后重试或改用其它模型',
]

const FAQ_ITEMS = [
  '轮询与超时：建议 5–10 秒轮询一次，客户端超时建议 ≥ 300 秒；实测耗时（H3 两模型为 2026-11-07 真机实测，其余为近 30 天生产均值）——Nano Banana Pro 约 1 分钟，minimax_h3_v2 约 1 分钟，minimax_h3_omni 约 1–1.5 分钟，wan3.0-video-prime 约 2.5 分钟，seedance2.5-特惠 约 3 分钟，wan3.0-720p-特惠 约 3 分钟，wan3.0-video 约 4 分钟，wan3.0-video-官网 约 8 分钟，jimeng-video-reverse 30 秒视频约 90 秒，seedance-2.0 约 4–40 分钟（波动大）',
  '成片转存：metadata.url 与 /content 都可能有时效，成功后请立即下载转存到自己的存储',
  '素材拉不到时先看报错：wan3 系若报「first frame #1 ... is not usable」或「download failed」，是图片 URL 网关打不开（提交阶段就会拒单、不扣费），请换国内可直连图床或改用 base64 内联；漫屋链路模型若在生成阶段报拉取失败，说明素材 URL 在提交后 90 秒内失效，需要换更稳定的图床',
  '不要重复提交：提交报超时或网络错误时先查询任务状态，重复提交会产生两次费用',
  '提示词审核：含敏感内容会被上游拦截，失败会自动退款，改写提示词即可',
  '参数容错：分辨率大小写均可；时长超范围会被就近取档或截断（固定档位模型除外）',
  '参考视频上限分三套口径：有赞渠道（wan3.0-video / wan3.0-video-prime / seedance2.5-特惠）为「单条 ≤15 秒、合计 ≤15 秒，且带参考视频时生成时长 ≤15 秒」；官网渠道（wan3.0-video-官网 / wan3.0-720p-特惠）为「单条 ≤15 秒、合计 ≤15 秒，且输入 + 输出合计 ≤30 秒」；minimax_h3_omni 为「最多 3 段、不设单条 / 合计秒数上限，但按实际总秒数并入计费」。超限与时长解析失败都在提交阶段 400，不建单、不扣费',
  '模型清单以 GET /v1/models 为准，请勿在代码里写死；模型上下架会通过该接口体现。该接口按你的分组与令牌白名单过滤，只返回当前可用的模型；但模型可用性与额度、渠道健康状态相关，首次接入仍建议对关键模型发一次最小请求探活',
]

export function getModelGuideSections(platformUrl: string): TutorialSection[] {
  const url = platformUrl
  const videoUrl = `${url}/v1/videos`

  return [
    {
      id: 'models-overview',
      title: '可用模型总览',
      level: 2,
      content: [
        {
          type: 'paragraph',
          children: [
            '下列模型为当前对外开放的全部模型。每把 API Key 可单独配置可见模型，',
            '实际可用清单请以 ',
            { type: 'code', value: 'GET /v1/models' },
            ' 返回为准，请勿在代码中写死模型名。',
          ],
        },
        {
          type: 'list',
          ordered: false,
          items: MODEL_OVERVIEW_ITEMS.map((t) => [t]),
        },
        {
          type: 'heading',
          level: 3,
          id: 'compat-models',
          children: [{ type: 'strong', value: '兼容与隐藏模型' }],
        },
        {
          type: 'paragraph',
          children: ['以下模型可正常调用，但不在模型广场展示：'],
        },
        {
          type: 'list',
          ordered: false,
          items: COMPAT_MODEL_ITEMS.map((t) => [t]),
        },
      ],
    },
    {
      id: 'text-api',
      title: '文本对话接口',
      level: 2,
      content: [
        {
          type: 'paragraph',
          children: [
            '接口：',
            { type: 'code', value: 'POST /v1/chat/completions' },
            '，与 OpenAI 完全一致，temperature、max_tokens、tools、stream 等参数原样透传。',
          ],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `curl ${url}/v1/chat/completions \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-6-luna",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": false
  }'`,
        },
        {
          type: 'paragraph',
          children: [
            '计费示例：gpt-6-luna 输入 5,000 token + 输出 800 token ≈ ',
            { type: 'code', value: '¥0.011' },
            '。',
          ],
        },
      ],
    },
    {
      id: 'image-api',
      title: '图片生成接口',
      level: 2,
      content: [
        {
          type: 'paragraph',
          children: [
            '接口：',
            { type: 'code', value: 'POST /v1/images/generations' },
            '，模型 gpt-image-2.5-官方，¥0.15 / 张，与尺寸无关。',
          ],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            ['model：gpt-image-2.5-官方'],
            ['prompt：必填，图片描述'],
            ['size：如 1024x1024，不支持时自动回落模型默认尺寸，不报错'],
            ['n：出图张数，默认 1，n=2 扣两次 ¥0.15'],
            ['response_format：url（默认）或 b64_json'],
            [
              '其余 OpenAI 图片字段（quality / style / background / output_format 等）会被透传给上游，本平台不校验、不保证生效',
            ],
          ],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `curl ${url}/v1/images/generations \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-image-2.5-官方",
    "prompt": "一只橘猫，写实摄影风格",
    "size": "1024x1024",
    "n": 1
  }'`,
        },
        {
          type: 'hint',
          children: [
            '图片生成耗时约 20–60 秒，请把客户端超时设置到 120 秒以上。',
          ],
        },
        {
          type: 'paragraph',
          children: [
            { type: 'strong', value: 'Nano Banana Pro 不走这个接口' },
            '。它是异步任务模型，只挂在 ',
            { type: 'code', value: '/v1/videos' },
            ' 上，对 ',
            { type: 'code', value: '/v1/images/generations' },
            ' 提交会返回 404。正确姿势见「各视频模型参数 → Nano Banana Pro」：',
            { type: 'code', value: 'POST /v1/videos' },
            ' 提交 → 轮询 → 从 ',
            { type: 'code', value: 'metadata.url' },
            ' 取图或走 ',
            { type: 'code', value: '/content' },
            ' 下载。',
          ],
        },
      ],
    },
    {
      id: 'video-api',
      title: '视频生成接口',
      level: 2,
      content: [
        {
          type: 'heading',
          level: 3,
          id: 'video-flow',
          children: [{ type: 'strong', value: '三步流程' }],
        },
        {
          type: 'list',
          ordered: true,
          items: VIDEO_FLOW_ITEMS.map((t) => [t]),
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '① 提交任务' }],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `curl ${videoUrl} \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "wan3.0-video",
    "prompt": "一只橘猫在窗台晒太阳，午后暖光",
    "duration": 5,
    "resolution": "720P",
    "ratio": "16:9"
  }'`,
        },
        {
          type: 'paragraph',
          children: ['返回：'],
        },
        {
          type: 'codeBlock',
          lang: 'json',
          value: `{"created_at": 1790475484, "id": "task_xxx", "object": "video", "model": "wan3.0-video", "progress": 0, "status": "queued"}`,
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '② 轮询状态' }],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `curl ${videoUrl}/task_xxx \\
  -H "Authorization: Bearer $NEW_API_KEY"`,
        },
        {
          type: 'list',
          ordered: false,
          items: VIDEO_STATUS_ITEMS.map((t) => [t]),
        },
        {
          type: 'codeBlock',
          lang: 'json',
          value: `{
  "id": "task_xxx",
  "object": "video",
  "model": "wan3.0-video",
  "status": "completed",
  "progress": 100,
  "metadata": {"url": "https://..."}
}`,
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '③ 下载成片（推荐）' }],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `curl -L ${videoUrl}/task_xxx/content \\
  -H "Authorization: Bearer $NEW_API_KEY" -o out.mp4`,
        },
        {
          type: 'paragraph',
          children: [
            '返回成片字节流。任务未完成时返回 ',
            { type: 'code', value: '400' },
            '，任务不属于该 Key 的账号时返回 ',
            { type: 'code', value: '404' },
            '。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'video-common-fields',
          children: [{ type: 'strong', value: '通用请求字段' }],
        },
        {
          type: 'list',
          ordered: false,
          items: VIDEO_COMMON_FIELDS.map((t) => [t]),
        },
        {
          type: 'hint',
          children: [
            '视频接口同时支持 application/json 与 multipart/form-data 两种提交方式，按需选择。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'video-media-examples',
          children: [
            { type: 'strong', value: '传递参考素材（图片 / 视频 / 音频）' },
          ],
        },
        {
          type: 'paragraph',
          children: [
            'wan3 系（smart / 官网 / prime）传入图片、视频、音频推荐使用顶层 ',
            { type: 'code', value: 'media' },
            ' 数组，元素结构为 ',
            { type: 'code', value: '{"type":"...","url":"..."}' },
            '，type 可取 reference_image（参考图）/ reference_video（参考视频）/ reference_audio（参考音频）/ first_frame（首帧）/ last_frame（尾帧）。提示词中用 ',
            { type: 'code', value: '@图片1 / @视频1 / @音频1' },
            ' 引用对应素材，编号按 media 数组顺序从 1 开始。',
          ],
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '图生视频（锁定首帧）' }],
        },
        {
          type: 'hint',
          children: [
            '⚠️ 首尾帧仅 wan3.0-video-官网 / wan3.0-720p-特惠 支持。有赞 wan3.0-video / wan3.0-video-prime / seedance2.5-特惠 的上游已取消首尾帧，传入直接 400（不消耗费用），请改用 reference_image 做图生视频。',
          ],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `curl ${videoUrl} \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "wan3.0-video-官网",
    "prompt": "让画面里的橘猫跳下窗台走向镜头",
    "duration": 5,
    "resolution": "720P",
    "media": [{"type": "first_frame", "url": "https://your-cdn.com/cat.png"}]
  }'`,
        },
        {
          type: 'paragraph',
          children: [
            { type: 'strong', value: '图片内联 base64（首选，免图床）' },
          ],
        },
        {
          type: 'paragraph',
          children: [
            'wan3 系支持把图片以 ',
            { type: 'code', value: 'data:{MIME_type};base64,{base64_data}' },
            ' 直接内联在 ',
            { type: 'code', value: 'media' },
            ' 的 url 字段里，',
            '不需要任何公网图床，也不受图床可达性与速度影响。',
            '这是上游官方支持的正规用法，不是变通。',
          ],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `curl ${videoUrl} \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "wan3.0-video-官网",
    "prompt": "让画面里的橘猫跳下窗台走向镜头",
    "duration": 5,
    "resolution": "720P",
    "media": [{"type": "first_frame", "url": "data:image/png;base64,iVBORw0KGgo..."}]
  }'`,
        },
        {
          type: 'hint',
          children: [
            'base64 图片限制：单张解码后 ≤20MB，宽高 [240, 8000] 像素，格式 PNG / JPG / BMP / WEBP。base64 约为原图的 4/3，请求体上限默认 128MB。⚠️ base64 只有 wan3 系支持，漫屋链路模型（seedance / Nano Banana Pro 等）只收公网 http(s) URL，传 base64 直接 400。',
          ],
        },
        {
          type: 'paragraph',
          children: [
            { type: 'strong', value: '多模态参考（图 + 视频 + 音频）' },
          ],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `curl ${videoUrl} \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "wan3.0-video",
    "prompt": "参考 @图片1 的角色形象与 @视频1 的运镜，配音跟随 @音频1 的节奏",
    "duration": 10,
    "resolution": "720P",
    "media": [
      {"type": "reference_image", "url": "https://your-cdn.com/character.png"},
      {"type": "reference_video", "url": "https://your-cdn.com/camera-move.mp4"},
      {"type": "reference_audio", "url": "https://your-cdn.com/narration.mp3"}
    ]
  }'`,
        },
        {
          type: 'hint',
          children: [
            '有赞 wan3.0-video / wan3.0-video-prime 与官网渠道的参考视频、参考音频只能走 media（或 metadata.media，格式相同）这一条路，顶层 audios / audio 会被静默丢弃；images 旧字段一律按参考图处理。素材 URL 须为公网 http(s) 直链，要求见「参考素材要求」。',
          ],
        },
        {
          type: 'hint',
          children: [
            '⚠️ 2026-11-01 起，wan3 系提交前由网关代下载并转存图片：传入的公网图片 URL 会先被网关拉下来、转存到网关自带的暂存图床，再把自家地址交给上游 —— 上游因此不再需要访问你的图床。代下载有 30 秒单图上限，超时即拒单（400）；转存失败也在提交阶段就拒单，不会出现「提交成功、生成阶段才失败」的情况。仍建议优先用 base64。',
          ],
        },
      ],
    },
    {
      id: 'video-model-params',
      title: '各视频模型参数',
      level: 2,
      content: [
        {
          type: 'paragraph',
          children: [
            '全部视频模型与出图模型统一用 ',
            { type: 'code', value: 'POST /v1/videos' },
            ' 提交，参数口径见下表，逐项说明见后文各模型小节。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'video-params-cheatsheet',
          children: [{ type: 'strong', value: '参数速查表' }],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              'wan3.0-video｜2–30 秒｜480P / 720P / 1080P（三档同价）｜图 ≤10、视频 ≤5、音频 ≤5，合计 ≤20｜¥0.23/秒 + 参考视频秒数',
            ],
            [
              'wan3.0-video-prime｜2–30 秒｜720P / 1080P｜图 ≤10、视频 ≤5、音频 ≤5，合计 ≤20｜720P ¥0.25/秒、1080P ¥0.30/秒 + 参考视频秒数',
            ],
            [
              'seedance2.5-特惠｜2–30 秒（带参考视频时 ≤15 秒）｜720P / 1080P｜图 ≤10、视频 ≤5、音频 ≤5，合计 ≤20｜720P ¥0.20/秒、1080P ¥0.30/秒 + 参考视频秒数',
            ],
            [
              'wan3.0-video-官网｜2–30 秒｜480P / 720P / 1080P｜图 ≤10、视频 ≤5、音频 ≤5、首尾帧各 1｜¥0.20 / ¥0.45 / ¥0.80 每秒 + 参考视频秒数',
            ],
            [
              'wan3.0-720p-特惠｜2–30 秒｜固定 720P｜同 wan3.0-video-官网｜¥0.20/秒 + 参考视频秒数',
            ],
            [
              'minimax_h3_v2｜1–15 秒｜480p / 768p × 横 / 竖 / (1:1)｜关键帧 ≤2（不传=文生，1 张=首帧，2 张=首尾帧），不支持视频/音频参考｜¥0.03 / ¥0.05 每秒',
            ],
            [
              'minimax_h3_omni｜1–15 秒｜480p / 768p / 1080p × 横 / 竖 / (1:1)｜设定参考 ≤9 图 + ≤3 视频 + ≤3 音频，首尾帧走 metadata｜¥0.02 / ¥0.04 / ¥0.06 每秒；参考视频秒数并入计费，图/音频不额外计费（768P 约 1–2 分钟出片）',
            ],
            ['seedance-2.0｜5–15 秒逐秒｜无档位｜图 ≤10｜¥1.00/次'],
            ['seedance-2-0-fast｜5–15 秒逐秒｜不支持分辨率｜图 ≤10｜¥0.10/秒'],
            [
              'seedance-2-5｜5 / 10 / 15 / 30 秒四档｜不支持分辨率｜图 ≤10｜¥3.50/次',
            ],
            [
              'jimeng-video-reverse｜不适用（输入是视频）｜不适用｜视频恰 1 个、≤100MB｜¥1.00/次',
            ],
            [
              'Nano Banana Pro｜不适用（出图）｜按 ratio 出图｜图 ≤10｜¥0.30/张 × 张数',
            ],
          ],
        },
        {
          type: 'hint',
          children: [
            '「+ 参考视频秒数」= 计费秒数为「生成秒数 + 参考视频总秒数（四舍五入）」；不带参考视频时只按生成秒数计费。按次计费的模型不因参考素材加价。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-wan30-video',
          children: [{ type: 'strong', value: 'wan3.0-video（有赞标准版）' }],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            ['resolution：480P（默认）/ 720P / 1080P'],
            ['seconds：2–30，默认 5'],
            ['ratio：adaptive（默认）/ 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9'],
            ['参考素材：图 ≤10 张、视频 ≤5 个、音频 ≤5 条，三类合计 ≤20 项'],
            ['计费：按秒 ¥0.23（480P / 720P / 1080P 三档同价）'],
            [
              '参考视频计费：带 reference_video 时按「生成秒数 + 参考视频总秒数（四舍五入）」× ¥0.23 计费，例如 3 秒参考视频生成 2 秒 = 0.23 × 5 = ¥1.15；无法解析时长的参考视频会被拒绝（不建单不扣费）',
            ],
            [
              '⚠️ 参考视频时长约束：单条 ≤15 秒、合计 ≤15 秒，且有参考视频时生成时长也须 ≤15 秒。超限直接拒绝（不建单不扣费），错误提示会写明当前秒数与上限',
            ],
          ],
        },
        {
          type: 'hint',
          children: [
            '支持纯文生视频、图生视频与多模态参考。首尾帧（first_frame / last_frame）已被上游移除，传入会直接 400（不消耗费用），请改用参考图。旧名 wan3.0-smart 已下线、不再可路由，请改用 wan3.0-video。带参考视频时计费秒数会并入参考视频时长，单条与合计均不得超过 15 秒。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-wan30-video-prime',
          children: [
            {
              type: 'strong',
              value: 'wan3.0-video-prime（有赞 Prime 增强画质）',
            },
          ],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              'resolution：720P（默认）/ 1080P（无 480P 档；传 480P 会按 720P 执行，不会静默升到 1080P）',
            ],
            ['seconds：2–30，默认 5'],
            ['ratio：adaptive（默认）/ 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9'],
            ['参考素材：图 ≤10 张、视频 ≤5 个、音频 ≤5 条，三类合计 ≤20 项'],
            ['计费：按秒 ¥0.25（720P）/ ¥0.30（1080P）'],
            [
              '参考视频计费：带 reference_video 时按「生成秒数 + 参考视频总秒数（四舍五入）」× 所选分辨率单价计费，例如 3 秒参考视频生成 2 秒 720P = 0.25 × 5 = ¥1.25；无法解析时长的参考视频会被拒绝（不建单不扣费）',
            ],
            [
              '⚠️ 参考视频时长约束：单条 ≤15 秒、合计 ≤15 秒，且有参考视频时生成时长也须 ≤15 秒。超限直接拒绝（不建单不扣费）',
            ],
          ],
        },
        {
          type: 'hint',
          children: [
            'Prime 按分辨率分档按秒计费（不再是固定 1080P / 30 秒的按次计费）。参考素材上限与标准版一致。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-seedance25-tehui',
          children: [
            { type: 'strong', value: 'seedance2.5-特惠（高清版，按秒计费）' },
          ],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              'resolution：720P（默认）/ 1080P（无 480P 档；传 480P 会按 720P 执行，不会静默升到 1080P）',
            ],
            ['seconds：2–30，默认 5。⚠️ 但带参考视频时生成时长上限 15 秒'],
            ['ratio：adaptive（默认）/ 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9'],
            ['参考素材：图 ≤10 张、视频 ≤5 个、音频 ≤5 条，三类合计 ≤20 项'],
            ['计费：按秒 ¥0.20（720P）/ ¥0.30（1080P）'],
            [
              '参考视频计费：带 reference_video 时按「生成秒数 + 参考视频总秒数（四舍五入）」× 所选分辨率单价计费，例如 3 秒参考视频生成 2 秒 720P = 0.20 × 5 = ¥1.00；无法解析时长（非 MP4/MOV 或取不到文件）的参考视频会被拒绝（不建单不扣费）',
            ],
            [
              '⚠️ 参考视频时长约束：单条 ≤15 秒、多条合计 ≤15 秒；且有参考视频时生成时长也须 ≤15 秒。超限直接拒绝（不建单不扣费），错误提示会写明当前秒数与上限',
            ],
          ],
        },
        {
          type: 'hint',
          children: [
            '按分辨率分档按秒计费，无参考视频时时长 2–30 秒。参考素材上限与标准版一致。参考视频与生成时长同价计秒，不额外打折。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-wan30-guanwang',
          children: [
            { type: 'strong', value: 'wan3.0-video-官网（官方渠道标准版）' },
          ],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            ['resolution：480P（默认）/ 720P / 1080P'],
            ['seconds：2–30，默认 5'],
            [
              'ratio：metadata.ratio 传 adaptive / 16:9 / 4:3 / 1:1 / 3:4 / 9:16，也可用 size（960x540 / 1280x720 / 1920x1080）',
            ],
            [
              '参考素材：图 ≤10 张、视频 ≤5 个（单个 1–15 秒、合计 ≤15 秒）、音频 ≤5 条（合计 ≤15 秒）、首尾帧各 1 张',
            ],
            [
              'metadata.audio：是否生成音轨，默认 true；关掉传 false（别名 generate_audio）',
            ],
            ['计费：按秒 ¥0.20 / ¥0.45 / ¥0.80（480P / 720P / 1080P）'],
            [
              '参考视频计费：带 reference_video 时按「生成秒数 + 参考视频总秒数（四舍五入）」× 所选分辨率单价计费，例如 10 秒参考视频生成 10 秒 480P = 0.20 × 20 = ¥4.00；无法解析时长的参考视频会被拒绝（不建单不扣费）',
            ],
            [
              '⚠️ 时长约束（有参考视频时）：「输入视频总时长 + 输出视频时长 ≤ 30 秒」，本地强制，超限直接 400（不建单不扣费）。无参考视频时生成时长为 2–30 秒',
            ],
          ],
        },
        {
          type: 'hint',
          children: [
            '本渠道同样支持图生视频与多模态参考，与有赞 wan3.0-video 走相同的 media 解析逻辑；唯一区别是分辨率倍率（1:2:4，720P、1080P 更贵）且没有 adaptive 比例之外的智能调度。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-wan30-720p-tehui',
          children: [
            { type: 'strong', value: 'wan3.0-720p-特惠（720P 成片档）' },
          ],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            ['输出分辨率：固定 720P（只有一个档位，不接受 480P / 1080P 参数）'],
            ['seconds：2–30，默认 5'],
            [
              'ratio：metadata.ratio 传 adaptive / 16:9 / 4:3 / 1:1 / 3:4 / 9:16，也可用 size（960x540 / 1280x720 / 1920x1080）',
            ],
            [
              '参考素材：图 ≤10 张、视频 ≤5 个（单个 1–15 秒、合计 ≤15 秒）、音频 ≤5 条（合计 ≤15 秒）、首尾帧各 1 张',
            ],
            [
              'metadata.audio：是否生成音轨，默认 true；关掉传 false（别名 generate_audio）',
            ],
            ['计费：按秒 ¥0.20，单一价位，不乘分辨率倍率'],
            [
              '参考视频计费：带 reference_video 时按「生成秒数 + 参考视频总秒数（四舍五入）」× ¥0.20 计费，例如 10 秒参考视频生成 10 秒 = 0.20 × 20 = ¥4.00；无法解析时长的参考视频会被拒绝（不建单不扣费）',
            ],
            [
              '⚠️ 时长约束（有参考视频时）：「输入视频总时长 + 输出视频时长 ≤ 30 秒」，本地强制，超限直接 400（不建单不扣费）。无参考视频时生成时长为 2–30 秒',
            ],
          ],
        },
        {
          type: 'hint',
          children: [
            '本档位固定输出 720P，价格比 wan3.0-video-官网 的 720P 档更低（¥0.25/s 对 ¥0.45/s），适合只要 720P 的场景。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-minimax-h3-v2',
          children: [
            {
              type: 'strong',
              value: 'minimax_h3_v2（文生 / 图生 / 首尾帧三合一）',
            },
          ],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            ['prompt：必填，不能是空串'],
            [
              'seconds / duration：1–15 秒，默认 5。⚠️ 越界不会报错，网关会收敛到边界值（传 20 按 15 秒计费），需要精确时长请在客户端先校验',
            ],
            [
              'resolution：480p横 / 480p竖 / 480p(1:1) / 768p横 / 768p竖 / 768p(1:1)，默认 480p横；也可用 size（如 1280x720）按短边推档。⚠️ 档位与方向必须写全且区分大小写，裸 480p、1080p 一律 400（1080p 仅 minimax_h3_omni 支持）',
            ],
            ['seed：可选，整数'],
            [
              'images / input_reference / image / media（type 含 image）：可选，最多 2 张。不传=纯文生视频，1 张=首帧图生，2 张=首尾帧驱动（第 1 张首帧、第 2 张尾帧）。⚠️ 第 3 张直接 400，上游不会报错但会静默忽略，所以由网关拦下',
            ],
            [
              '参考素材写法：data URI 或公网 http(s) 直链都收，网关会代下载并转成 data URI 下发；图 ≤10MB、MIME 前缀须为 data:image/…',
            ],
            [
              '参考视频 / 参考音频：不支持，传入直接 400（改用 minimax_h3_omni），不会被静默丢弃',
            ],
            [
              '计费：按秒 480p ¥0.03、768p ¥0.05；参考素材不额外计费。例：768p竖 生成 5 秒 = ¥0.25',
            ],
            [
              '产物：完成后从 metadata.url 取站内代理地址，或直接 GET /v1/videos/{id}/content 下载；实测约 1 分钟出片',
            ],
          ],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `# ① 提交（不带 images 即纯文生；带 1 张为首帧图生）
curl ${videoUrl} \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "minimax_h3_v2",
    "prompt": "平静的湖面日出，微风吹皱水面，写实镜头",
    "resolution": "480p横",
    "seconds": "5"
  }'

# ② 轮询到 status=completed
curl ${videoUrl}/task_xxx \\
  -H "Authorization: Bearer $NEW_API_KEY"

# ③ 下载成片
curl -L ${videoUrl}/task_xxx/content \\
  -H "Authorization: Bearer $NEW_API_KEY" -o out.mp4`,
        },
        {
          type: 'hint',
          children: [
            'v2 把原来的文生、图生、首尾帧合并成同一个模型，不再需要按场景切换模型名。旧名 minimax_h3-文生 / minimax_h3-图生 仍可路由但已不在模型广场展示，新接入请直接用 minimax_h3_v2。提交回执里的 size 会回显实际生效的档位。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-minimax-h3-omni',
          children: [
            {
              type: 'strong',
              value: 'minimax_h3_omni（多模态设定参考 + 1080P + 快速出片）',
            },
          ],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              'prompt：必填；引用设定参考用 <Picture 1> / <Video 1> / <Audio 1>，按类型独立从 1 开始计数，对应下面素材数组的顺序',
            ],
            ['seconds / duration：1–15 秒，默认 5；越界会被收敛到边界值'],
            [
              'resolution：v2 的六档 + 1080p横 / 1080p竖 / 1080p(1:1)，默认 480p横。1080p 是 768p 采样后潜空间放大的档位，¥0.06/秒',
            ],
            ['seed：可选，整数'],
            [
              'images / input_reference / image / media（type 含 image）：可选，设定参考最多 9 张（对应 <Picture i>）',
            ],
            [
              'media 中 type 含 video 的条目：参考视频最多 3 个（对应 <Video k>），按实际时长并入计费秒数，须为可解析时长的 MP4/MOV；type 含 audio 的条目或顶层 audios / audio：参考音频最多 3 条（对应 <Audio j>），不额外计费',
            ],
            [
              'metadata.first_frame / metadata.last_frame：可选首尾帧各 1 张，可与设定参考在同一个请求里共存（这是与 v2 的关键区别：v2 的 images 就是关键帧，omni 的 images 是设定参考）',
            ],
            [
              '参考素材写法：data URI 或公网 http(s) 直链都收，网关代下载后转成 data URI 下发；单份上限 图 10MB / 视频 50MB / 音频 30MB，MIME 前缀须与类型一致',
            ],
            [
              '计费：按秒 480p ¥0.02、768p ¥0.04、1080p ¥0.06；⚠️ 带参考视频时计费秒数 = 生成秒数 + 参考视频总秒数（每条按实际时长四舍五入到整秒后相加，与生成同价），参考图片与参考音频不额外计费。例：1080p横 生成 5 秒 = ¥0.30，768p横 生成 5 秒 = ¥0.20；768p横 生成 5 秒 + 11 秒参考视频 = ¥0.04 × 16 = ¥0.64。参考视频须为可解析时长的 MP4/MOV，解析失败在提交阶段 400（不建单不扣费）',
            ],
            [
              '速度：出片快，768p 实测约 1–2 分钟即可交付（3 分钟内）；480p 与 1080p 相近',
            ],
            [
              '产物：轮询与取片方式与 v2 相同（GET /v1/videos/{id} → /content）',
            ],
          ],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `# 设定参考 1 图 + 1 音频，同时用首帧锚定（三种素材可共存）
# 素材也可直接内联 data:image/png;base64,... / data:audio/wav;base64,...
curl ${videoUrl} \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "minimax_h3_omni",
    "prompt": "<Picture 1> 中的场景缓缓展开，配上 <Audio 1> 的氛围",
    "resolution": "768p横",
    "duration": 5,
    "images": ["https://your-domain.com/ref-1.png"],
    "audios": ["https://your-domain.com/ambience.wav"],
    "metadata": { "first_frame": "https://your-domain.com/first.png" }
  }'`,
        },
        {
          type: 'hint',
          children: [
            'omni 的 9 图 / 3 视频 / 3 音频是「设定参考」，不是关键帧；需要首尾帧驱动时请同时传 metadata.first_frame / metadata.last_frame。素材超限、格式不符、分辨率不在白名单内，都会在提交阶段直接 400，不建单不扣费。⚠️ 参考视频按实际秒数并入计费，提交前请按「生成秒数 + 参考视频秒数」估算余额。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-seedance-2',
          children: [{ type: 'strong', value: 'seedance-2.0' }],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              'seconds / duration：5–15 秒逐秒，默认 15；区间外（含 4、16、30）直接 400 invalid_duration',
            ],
            [
              'ratio：16:9（默认）/ 9:16 / 1:1 / 4:3 / 3:4 / 21:9，也可用 size 传比例字符串',
            ],
            ['input_reference / images：可选，≤10 张，必须是 http/https URL'],
            ['计费：按次 ¥1.00，与时长无关'],
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-seedance-2-0-fast',
          children: [{ type: 'strong', value: 'seedance-2-0-fast' }],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              'seconds / duration：5–15 秒逐秒，默认 5；区间外（含 4、16、30）直接 400',
            ],
            [
              'ratio：auto（默认）/ 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9；不传则保持官网当前档位',
            ],
            ['input_reference / images：可选，≤10 张，必须是 http/https URL'],
            ['resolution：不支持，传入直接 400（官网无分辨率控件）'],
            ['计费：按秒 ¥0.10 × 秒数，无分辨率倍率'],
          ],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `curl ${videoUrl} \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "seedance-2-0-fast",
    "prompt": "一只橘猫在窗台晒太阳，午后暖光",
    "duration": 9
  }'`,
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-seedance-2-5',
          children: [{ type: 'strong', value: 'seedance-2-5' }],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              'seconds / duration：5 / 10 / 15 / 30 四档，默认 5；非档位值直接 400',
            ],
            [
              'ratio：auto（默认）/ 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9；不传则保持官网当前档位',
            ],
            ['input_reference / images：可选，≤10 张，必须是 http/https URL'],
            ['resolution：不支持，传入直接 400'],
            ['计费：按次 ¥3.50，与时长无关'],
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-nano-banana-pro',
          children: [{ type: 'strong', value: 'Nano Banana Pro' }],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            ['prompt：必填，图片描述'],
            [
              'n / count：出图张数，1–10，默认 1；超出区间直接 400（invalid_count）',
            ],
            [
              'ratio：16:9（默认）/ 9:16 / 1:1 / 4:3 / 3:4 / 21:9，也可用 size 传比例字符串',
            ],
            [
              'input_reference / images：可选，≤10 张 http/https URL；字段名与「提示词引用校验」规则同上文通用字段',
            ],
            ['产物：metadata.url，metadata.media_type = image'],
            [
              '⚠️ 本模型走 /v1/videos 异步任务（不是 /v1/images/generations），需轮询后从 metadata.url 取图；对 /v1/images/generations 发请求会返回 404',
            ],
            ['计费：按张 ¥0.30 × 张数（n=2 扣两次）'],
            [
              '实测：约 1 分钟出图（3 次真机样本 52–55 秒），输出 PNG、宽度 1024，客户端超时请设 ≥180 秒',
            ],
          ],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `# ① 提交（注意是 /v1/videos，不是 /v1/images/generations）
curl ${videoUrl} \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "Nano Banana Pro",
    "prompt": "一只橘猫在窗台晒太阳，写实摄影风格",
    "n": 1,
    "ratio": "16:9"
  }'

# ② 轮询到 status=completed 后，从 metadata.url 取图
curl ${videoUrl}/task_xxx \\
  -H "Authorization: Bearer $NEW_API_KEY"

# ③ 也可以直接下载图片字节流
curl -L ${videoUrl}/task_xxx/content \\
  -H "Authorization: Bearer $NEW_API_KEY" -o out.png`,
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-jimeng-video-reverse',
          children: [
            { type: 'strong', value: 'jimeng-video-reverse（视频反解）' },
          ],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              'input_reference（或 video / videos）：必填，恰好 1 个公网 http/https 视频 URL（服务端代下载，mp4/mov/webm，≤100MB）',
            ],
            ['prompt：可选，作为附加指令'],
            ['ratio / seconds / images：不支持，传入直接 400'],
            [
              '产物：metadata.prompt（文本），metadata.media_type = text；无媒体产物',
            ],
            ['计费：按次 ¥1.00；30 秒视频实测约 90 秒出结果，上限 5 分钟'],
          ],
        },
      ],
    },
    {
      id: 'video-media-rules',
      title: '参考素材要求',
      level: 2,
      content: [
        {
          type: 'list',
          ordered: false,
          items: MEDIA_RULE_ITEMS.map((t) => [t]),
        },
      ],
    },
    {
      id: 'quota-query',
      title: '余额查询接口',
      level: 2,
      content: [
        {
          type: 'paragraph',
          children: [
            '下游可使用只读接口查询当前 API Key 对应账户的余额。接口不接受其他用户或令牌标识，身份完全由 API Key 决定。',
          ],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: QUOTA_QUERY_EXAMPLE(url),
        },
        {
          type: 'list',
          ordered: false,
          items: QUOTA_QUERY_ITEMS.map((t) => [t]),
        },
        {
          type: 'paragraph',
          children: [
            '核心判断字段：',
            { type: 'code', value: 'data.quota.effective_remain' },
            '（quota）。不限量令牌的 ',
            { type: 'code', value: 'remain_token' },
            ' 为 null；不要仅用它判断是否还有余额。',
          ],
        },
      ],
    },
    {
      id: 'billing-refund',
      title: '计费与退款',
      level: 2,
      content: [
        { type: 'list', ordered: false, items: BILLING_ITEMS.map((t) => [t]) },
        {
          type: 'paragraph',
          children: [
            '计费在提交时预扣、按实际用量结算；任务失败自动全额退还，无需申请。',
          ],
        },
      ],
    },
    {
      id: 'api-errors',
      title: '错误码',
      level: 2,
      content: [
        {
          type: 'paragraph',
          children: [
            '对话与图片接口返回 OpenAI 风格错误体 ',
            {
              type: 'code',
              value: '{"error": {"message": "...", "type": "..."}}',
            },
            '；视频接口返回 ',
            { type: 'code', value: '{"code": "...", "message": "..."}' },
            '。',
          ],
        },
        { type: 'list', ordered: false, items: ERROR_ITEMS.map((t) => [t]) },
      ],
    },
    {
      id: 'integration-faq',
      title: '对接注意事项',
      level: 2,
      content: [
        { type: 'list', ordered: false, items: FAQ_ITEMS.map((t) => [t]) },
        {
          type: 'hint',
          children: [
            '需要新增模型、调整额度或排查具体任务，请提供 task id、请求时间与错误信息。',
          ],
        },
      ],
    },
  ]
}

export function getModelGuideIndex(): { title: string; href: string }[] {
  return [
    { title: '可用模型总览', href: '#models-overview' },
    { title: '文本对话接口', href: '#text-api' },
    { title: '图片生成接口', href: '#image-api' },
    { title: '视频生成接口', href: '#video-api' },
    {
      title: '传递参考素材（图片 / 视频 / 音频）',
      href: '#video-media-examples',
    },
    { title: '各视频模型参数', href: '#video-model-params' },
    { title: '参数速查表', href: '#video-params-cheatsheet' },
    { title: '余额查询接口', href: '#quota-query' },
    { title: 'wan3.0-video', href: '#model-wan30-video' },
    { title: 'wan3.0-video-prime', href: '#model-wan30-video-prime' },
    { title: 'seedance2.5-特惠', href: '#model-seedance25-tehui' },
    { title: 'wan3.0-video-官网', href: '#model-wan30-guanwang' },
    { title: 'wan3.0-720p-特惠', href: '#model-wan30-720p-tehui' },
    { title: 'minimax_h3_v2', href: '#model-minimax-h3-v2' },
    { title: 'minimax_h3_omni', href: '#model-minimax-h3-omni' },
    { title: 'seedance-2.0', href: '#model-seedance-2' },
    { title: 'seedance-2-0-fast', href: '#model-seedance-2-0-fast' },
    { title: 'seedance-2-5', href: '#model-seedance-2-5' },
    { title: 'Nano Banana Pro', href: '#model-nano-banana-pro' },
    { title: 'jimeng-video-reverse', href: '#model-jimeng-video-reverse' },
    { title: '参考素材要求', href: '#video-media-rules' },
    { title: '计费与退款', href: '#billing-refund' },
    { title: '错误码', href: '#api-errors' },
    { title: '对接注意事项', href: '#integration-faq' },
  ]
}
