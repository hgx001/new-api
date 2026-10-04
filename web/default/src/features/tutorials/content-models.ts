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
  'MiniMax-M3｜文本｜输入 ¥0.22 / 1M，输出 ¥0.09 / 1M',
  'gpt-6-luna｜文本｜输入 ¥0.94 / 1M，输出 ¥7.50 / 1M',
  'gpt-6-sol｜文本｜输入 ¥18.75 / 1M，输出 ¥150.00 / 1M',
  'gpt-image-2.5-官方｜图片｜¥0.15 / 张',
  'wan3.0-smart｜视频（按秒）｜480P ¥0.28/s、720P ¥0.32/s、1080P ¥0.40/s',
  'wan3.0-video-官网｜视频（按秒）｜480P ¥0.27/s、720P ¥0.53/s、1080P ¥0.85/s',
  'wan3.0-video-prime-1080p｜视频（按次）｜¥8.00 / 次，固定 1080P、30 秒',
  'wan2.7-r2v｜视频（按秒）｜¥0.10/s，720p / 1080p 同价',
  'autodl:minimax-h3-u24｜视频（按秒）｜480p ¥0.10/s、768p ¥0.12/s',
  'dola-seedance-2.5｜视频（按次）｜¥2.50 / 次',
  'gemini-web-video｜视频（按次）｜¥1.00 / 次',
  'Nano Banana Pro｜图片（按张）｜¥0.30 / 张',
  'jimeng-video-reverse｜提示词（按次）｜¥1.00 / 次',
]

const COMPAT_MODEL_ITEMS = [
  'gpt-5.6-luna：等价 gpt-6-luna，同价（老 Key 无需改代码）',
  'gpt-5.6-sol：等价 gpt-6-sol，同价',
  'MiniMax-M2.7：与 MiniMax-M3 同价',
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
  'resolution：分辨率档位，大小写均可（720p / 720P）',
  'ratio / aspect_ratio：画幅比例（官网渠道请放在 metadata.ratio，或用 size）',
  'images / input_reference / image：参考图，字符串或数组，需公网 HTTPS 直链',
  'media：结构化素材数组，元素形如 {"type":"reference_image","url":"..."}，type 可取 reference_image / reference_video / reference_audio / first_frame / last_frame',
  'metadata：兜底传参，支持 metadata.resolution、metadata.ratio、metadata.media、metadata.audio',
]

const VIDEO_STATUS_ITEMS = [
  'queued：排队中',
  'in_progress：生成中，progress 为 0–100',
  'completed：完成，取 metadata.url',
  'failed：失败，读 error.message，费用已自动退还',
]

const MEDIA_RULE_ITEMS = [
  '必须是公网可访问的 HTTPS 直链，不接受内网地址、http://、本地路径与 base64',
  '图片：PNG / JPG / BMP / WEBP，单张 ≤20MB，单边 240–8000 像素；带透明通道会自动压白底',
  '参考视频：单个 1–15 秒；参考音频：单个 ≤15MB',
  '素材 URL 需在提交后至少 90 秒内保持可访问',
  '图床防盗链或签名过期会导致任务失败，建议使用稳定的公网地址',
]

const BILLING_ITEMS = [
  '文本：输入、输出 token 分别计价',
  '图片：按张计费，¥0.15 / 张（n=2 扣两次）',
  '视频（按秒）：单价 × 秒数 × 分辨率档位倍率',
  '视频（按次）：wan3.0-video-prime-1080p 固定 ¥8 / 次，dola-seedance-2.5 ¥2.50 / 次，gemini-web-video ¥1.00 / 次，jimeng-video-reverse 固定 ¥1.00 / 次',
  '任务失败：自动全额退还，无需申请',
  '内容审核：提示词或素材触发上游审核导致失败时，失败原因为「内容审核不通过」，费用自动退还',
]

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
  '轮询与超时：建议 5–10 秒轮询一次，客户端超时建议 ≥ 300 秒；实测耗时——wan3.0-smart / wan2.7-r2v 约 2–5 分钟，wan3.0-video-官网 约 6 分钟，autodl:minimax-h3-u24 约 5–20 分钟，dola-seedance-2.5 约 4–40 分钟（波动大），wan3.0-video-prime-1080p 预计 5–10 分钟',
  '成片转存：metadata.url 与 /content 都可能有时效，成功后请立即下载转存到自己的存储',
  '不要重复提交：提交报超时或网络错误时先查询任务状态，重复提交会产生两次费用',
  '提示词审核：含敏感内容会被上游拦截，失败会自动退款，改写提示词即可',
  '参数容错：分辨率大小写均可；时长超范围会被就近取档或截断（固定档位模型除外）',
  '模型清单以 GET /v1/models 为准，请勿在代码里写死；模型上下架会通过该接口体现',
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
        { type: 'list', ordered: false, items: MODEL_OVERVIEW_ITEMS.map((t) => [t]) },
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
        { type: 'list', ordered: false, items: COMPAT_MODEL_ITEMS.map((t) => [t]) },
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
        { type: 'list', ordered: false, items: [
          ['model：gpt-image-2.5-官方'],
          ['prompt：图片描述'],
          ['size：如 1024x1024，不支持时自动回落模型默认尺寸，不报错'],
          ['n：张数，默认 1'],
          ['response_format：url（默认）或 b64_json'],
        ] },
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
          children: ['图片生成耗时约 20–60 秒，请把客户端超时设置到 120 秒以上。'],
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
        { type: 'list', ordered: true, items: VIDEO_FLOW_ITEMS.map((t) => [t]) },
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
    "model": "wan3.0-smart",
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
          value: `{"created_at": 1790475484, "id": "task_xxx", "object": "video", "model": "wan3.0-smart", "progress": 0, "status": "queued"}`,
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
        { type: 'list', ordered: false, items: VIDEO_STATUS_ITEMS.map((t) => [t]) },
        {
          type: 'codeBlock',
          lang: 'json',
          value: `{
  "id": "task_xxx",
  "object": "video",
  "model": "wan3.0-smart",
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
        { type: 'list', ordered: false, items: VIDEO_COMMON_FIELDS.map((t) => [t]) },
        {
          type: 'hint',
          children: [
            '视频接口同时支持 application/json 与 multipart/form-data 两种提交方式，按需选择。',
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
          type: 'heading',
          level: 3,
          id: 'model-wan30-smart',
          children: [{ type: 'strong', value: 'wan3.0-smart（有赞智能调度）' }],
        },
        { type: 'list', ordered: false, items: [
          ['resolution：480P（默认）/ 720P / 1080P'],
          ['seconds：2–30，默认 5'],
          ['ratio：adaptive（默认）/ 16:9 / 9:16 / 1:1 / 4:3 / 3:4'],
          ['参考素材：图 ≤10 张、视频 ≤5 个、音频 ≤5 个、首尾帧各 1 张'],
          ['计费：按秒 ¥0.28 / ¥0.32 / ¥0.40（480P / 720P / 1080P）'],
        ] },
        {
          type: 'hint',
          children: [
            '首尾帧不能与普通参考图/视频/音频混用，混用会直接报错（不消耗费用）。支持纯文生视频、图生视频与多模态参考。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-wan30-guanwang',
          children: [{ type: 'strong', value: 'wan3.0-video-官网（官方渠道标准版）' }],
        },
        { type: 'list', ordered: false, items: [
          ['resolution：480P（默认）/ 720P / 1080P'],
          ['seconds：2–30，默认 5'],
          ['ratio：metadata.ratio 传 adaptive / 16:9 / 4:3 / 1:1 / 3:4 / 9:16，也可用 size（960x540 / 1280x720 / 1920x1080）'],
          ['计费：按秒 ¥0.27 / ¥0.53 / ¥0.85（480P / 720P / 1080P）'],
        ] },
        {
          type: 'hint',
          children: [
            '该渠道目前仅支持文生视频：传入参考图/视频/音频会被忽略（不会报错）。需要图生视频或多模态参考请改用 wan3.0-smart。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-wan30-prime',
          children: [{ type: 'strong', value: 'wan3.0-video-prime-1080p（满血 30 秒档）' }],
        },
        { type: 'list', ordered: false, items: [
          ['resolution：固定 1080P，传其它值会被忽略'],
          ['seconds：固定 30，传其它值会被忽略'],
          ['ratio：同 wan3.0-smart'],
          ['参考素材：图 ≤8 张、视频 ≤5 个、音频 ≤5 个、首尾帧各 1 张'],
          ['计费：按次 ¥8.00，与时长、分辨率无关'],
        ] },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `curl ${videoUrl} \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model": "wan3.0-video-prime-1080p", "prompt": "海边的日落延时"}'`,
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-wan27-r2v',
          children: [{ type: 'strong', value: 'wan2.7-r2v（参考生视频）' }],
        },
        { type: 'list', ordered: false, items: [
          ['images：必填，1–3 张参考图'],
          ['resolution：720p / 1080p，默认 1080p'],
          ['seconds：仅 5 或 10，传其它值就近取档'],
          ['ratio：16:9（默认）/ 9:16 / 1:1'],
          ['计费：按秒 ¥0.10，两档同价'],
          ['不支持首尾帧、参考视频、参考音频（传了会直接报错，不消耗费用）；不传参考图同样会报错'],
        ] },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `curl ${videoUrl} \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "wan2.7-r2v",
    "prompt": "@图片1 中的人物转身走向镜头",
    "images": ["https://your-cdn.com/girl.png"],
    "duration": 5,
    "resolution": "720P",
    "ratio": "16:9"
  }'`,
        },
        {
          type: 'heading',
          level: 3,
          id: 'model-autodl-h3-u24',
          children: [{ type: 'strong', value: 'autodl:minimax-h3-u24（多图多音频生视频）' }],
        },
        { type: 'list', ordered: false, items: [
          ['prompt：必填，≤10000 字符'],
          ['images：必填，1–9 张'],
          ['audios：可选，≤3 条'],
          ['seconds：1–15，默认 5'],
          ['resolution：480p竖 / 768p竖 / 480p横 / 768p横 / 480p(1:1) / 768p(1:1)，默认 768p竖'],
          ['seed：可选，0 ~ 999999999999999'],
          ['计费：按秒 480p ¥0.10、768p ¥0.12（768p 即上游 720P 档）'],
        ] },
        {
          type: 'heading',
          level: 3,
          id: 'model-dola-seedance',
          children: [{ type: 'strong', value: 'dola-seedance-2.5（漫屋）' }],
        },
        { type: 'list', ordered: false, items: [
          ['seconds：5 / 10 / 15 / 30，默认 30'],
          ['ratio：16:9（默认）/ 9:16 / 1:1 / 4:3 / 3:4 / 21:9，也可用 size 传比例字符串'],
          ['input_reference / images：可选，≤10 张，必须是 http/https URL'],
          ['计费：按次 ¥2.50，与时长无关（30 秒也是 ¥2.50）'],
        ] },
        {
          type: 'heading',
          level: 3,
          id: 'model-gemini-web-video',
          children: [{ type: 'strong', value: 'gemini-web-video（漫屋 Gemini 官网 Veo）' }],
        },
        { type: 'list', ordered: false, items: [
          ['prompt：必填'],
          ['ratio：同 dola 六档，默认 16:9'],
          ['input_reference / images：可选，≤10 张 http/https URL'],
          ['seconds / duration / resolution：不支持，传入直接 400'],
          ['计费：按次 ¥1.00（官网固定时长，不随时长变化）'],
        ] },
        {
          type: 'heading',
          level: 3,
          id: 'model-nano-banana-pro',
          children: [{ type: 'strong', value: 'Nano Banana Pro（漫屋远端图片）' }],
        },
        { type: 'list', ordered: false, items: [
          ['prompt：必填'],
          ['n / count：出图张数，1–10，默认 1'],
          ['ratio：同 dola 六档，默认 16:9'],
          ['input_reference / images：可选，≤10 张 http/https URL'],
          ['产物：metadata.url，metadata.media_type = image'],
          ['计费：按张 ¥0.30 × 张数'],
        ] },
        {
          type: 'heading',
          level: 3,
          id: 'model-jimeng-video-reverse',
          children: [{ type: 'strong', value: 'jimeng-video-reverse（即梦视频反解）' }],
        },
        { type: 'list', ordered: false, items: [
          ['input_reference（或 video / videos）：必填，恰好 1 个公网 http/https 视频 URL（服务端代下载，mp4/mov/webm，≤100MB）'],
          ['prompt：可选，作为给即梦助手的附加指令'],
          ['ratio / seconds / images：不支持，传入直接 400'],
          ['产物：metadata.prompt（文本），metadata.media_type = text；无媒体产物'],
          ['计费：按次 ¥1.00；30 秒视频实测约 90 秒出结果，上限 5 分钟'],
        ] },
      ],
    },
    {
      id: 'video-media-rules',
      title: '参考素材要求',
      level: 2,
      content: [
        { type: 'list', ordered: false, items: MEDIA_RULE_ITEMS.map((t) => [t]) },
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
            { type: 'code', value: '{"error": {"message": "...", "type": "..."}}' },
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
    { title: '各视频模型参数', href: '#video-model-params' },
    { title: 'wan3.0-smart', href: '#model-wan30-smart' },
    { title: 'wan3.0-video-官网', href: '#model-wan30-guanwang' },
    { title: 'wan3.0-video-prime-1080p', href: '#model-wan30-prime' },
    { title: 'wan2.7-r2v', href: '#model-wan27-r2v' },
    { title: 'autodl:minimax-h3-u24', href: '#model-autodl-h3-u24' },
    { title: 'dola-seedance-2.5', href: '#model-dola-seedance' },
    { title: '参考素材要求', href: '#video-media-rules' },
    { title: '计费与退款', href: '#billing-refund' },
    { title: '错误码', href: '#api-errors' },
    { title: '对接注意事项', href: '#integration-faq' },
  ]
}
