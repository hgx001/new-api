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
/**
 * 代码示例生成器 —— 从 model-details-api.tsx 抽出的**纯函数**层。
 *
 * 为什么抽出来：示例代码是客户**直接照抄**的东西，一旦与后端校验口径
 * 不一致，客户照抄就吃 400 —— 而且报的还是一个跟参数毫不相干的错。
 * 抽成纯函数后就能被单测直接断言字符串内容，不用渲染 React 组件。
 *
 * ⚠️ 本文件**不允许**有副作用（不发请求、不读全局状态），全部输入
 * 来自 SampleContext。
 */
import { getWan3SampleResolution } from './model-helpers'
import { getSeedance25SampleSpec } from './seedance25-family'

export type Lang = 'curl' | 'python' | 'typescript' | 'javascript'

export const LANG_LABELS: Record<Lang, string> = {
  curl: 'cURL',
  python: 'Python',
  typescript: 'TypeScript',
  javascript: 'JavaScript',
}

export type SampleContext = {
  baseUrl: string
  apiKeyEnv: string
  modelName: string
  endpointType: string
  endpointPath: string
}

export function buildChatSample(lang: Lang, ctx: SampleContext): string {
  const url = `${ctx.baseUrl}${ctx.endpointPath}`
  const isResponses = ctx.endpointType === 'openai-response'
  const isReasoning = /^o[1-4]|reasoning|thinking|deepseek-r/i.test(
    ctx.modelName
  )
  const userMessage = 'Explain quantum entanglement in one paragraph.'

  const bodyJson = isResponses
    ? JSON.stringify({ model: ctx.modelName, input: userMessage }, null, 2)
    : JSON.stringify(
        {
          model: ctx.modelName,
          messages: [{ role: 'user', content: userMessage }],
          ...(isReasoning ? {} : { temperature: 0.7 }),
        },
        null,
        2
      )

  const fnCall = isResponses ? 'responses.create' : 'chat.completions.create'

  if (lang === 'curl') {
    return [
      `curl ${url} \\`,
      `  -H "Authorization: Bearer $${ctx.apiKeyEnv}" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '${bodyJson.replaceAll('\n', '\n     ')}'`,
    ].join('\n')
  }

  if (lang === 'python') {
    return [
      'from openai import OpenAI',
      '',
      'client = OpenAI(',
      `    base_url="${ctx.baseUrl}/v1",`,
      `    api_key="<YOUR_API_KEY>",`,
      ')',
      '',
      isResponses
        ? `response = client.${fnCall}(\n    model="${ctx.modelName}",\n    input="${userMessage}",\n)\n\nprint(response.output_text)`
        : `completion = client.${fnCall}(\n    model="${ctx.modelName}",\n    messages=[\n        {"role": "user", "content": "${userMessage}"}\n    ],\n)\n\nprint(completion.choices[0].message.content)`,
    ].join('\n')
  }

  if (lang === 'typescript') {
    return [
      `import OpenAI from 'openai'`,
      '',
      `const client = new OpenAI({`,
      `  baseURL: '${ctx.baseUrl}/v1',`,
      `  apiKey: process.env.${ctx.apiKeyEnv},`,
      `})`,
      '',
      isResponses
        ? `const response = await client.${fnCall}({\n  model: '${ctx.modelName}',\n  input: '${userMessage}',\n})\n\nconsole.log(response.output_text)`
        : `const completion = await client.${fnCall}({\n  model: '${ctx.modelName}',\n  messages: [{ role: 'user', content: '${userMessage}' }],\n})\n\nconsole.log(completion.choices[0].message.content)`,
    ].join('\n')
  }

  return [
    `const response = await fetch('${url}', {`,
    `  method: 'POST',`,
    `  headers: {`,
    `    Authorization: \`Bearer \${process.env.${ctx.apiKeyEnv}}\`,`,
    `    'Content-Type': 'application/json',`,
    `  },`,
    `  body: JSON.stringify(${bodyJson}),`,
    `})`,
    '',
    `const data = await response.json()`,
    `console.log(data)`,
  ].join('\n')
}

export function buildAnthropicSample(lang: Lang, ctx: SampleContext): string {
  const url = `${ctx.baseUrl}${ctx.endpointPath}`
  const userMessage = 'Explain quantum entanglement in one paragraph.'

  if (lang === 'curl') {
    const body = JSON.stringify(
      {
        model: ctx.modelName,
        max_tokens: 1024,
        messages: [{ role: 'user', content: userMessage }],
      },
      null,
      2
    )
    return [
      `curl ${url} \\`,
      `  -H "x-api-key: $${ctx.apiKeyEnv}" \\`,
      `  -H "anthropic-version: 2023-06-01" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '${body.replaceAll('\n', '\n     ')}'`,
    ].join('\n')
  }
  if (lang === 'python') {
    return [
      'import anthropic',
      '',
      'client = anthropic.Anthropic(',
      `    base_url="${ctx.baseUrl}",`,
      `    api_key="<YOUR_API_KEY>",`,
      ')',
      '',
      `message = client.messages.create(`,
      `    model="${ctx.modelName}",`,
      `    max_tokens=1024,`,
      `    messages=[{"role": "user", "content": "${userMessage}"}],`,
      ')',
      '',
      'print(message.content[0].text)',
    ].join('\n')
  }
  if (lang === 'typescript') {
    return [
      `import Anthropic from '@anthropic-ai/sdk'`,
      '',
      `const client = new Anthropic({`,
      `  baseURL: '${ctx.baseUrl}',`,
      `  apiKey: process.env.${ctx.apiKeyEnv},`,
      `})`,
      '',
      `const message = await client.messages.create({`,
      `  model: '${ctx.modelName}',`,
      `  max_tokens: 1024,`,
      `  messages: [{ role: 'user', content: '${userMessage}' }],`,
      `})`,
      '',
      `console.log(message.content[0].text)`,
    ].join('\n')
  }
  return [
    `const response = await fetch('${url}', {`,
    `  method: 'POST',`,
    `  headers: {`,
    `    'x-api-key': \`\${process.env.${ctx.apiKeyEnv}}\`,`,
    `    'anthropic-version': '2023-06-01',`,
    `    'Content-Type': 'application/json',`,
    `  },`,
    `  body: JSON.stringify({`,
    `    model: '${ctx.modelName}',`,
    `    max_tokens: 1024,`,
    `    messages: [{ role: 'user', content: '${userMessage}' }],`,
    `  }),`,
    `})`,
    '',
    `const data = await response.json()`,
    `console.log(data.content[0].text)`,
  ].join('\n')
}

export function buildGeminiSample(lang: Lang, ctx: SampleContext): string {
  const url = `${ctx.baseUrl}${ctx.endpointPath}?key=$${ctx.apiKeyEnv}`
  const userMessage = 'Explain quantum entanglement in one paragraph.'

  if (lang === 'curl') {
    const body = JSON.stringify(
      { contents: [{ parts: [{ text: userMessage }] }] },
      null,
      2
    )
    return [
      `curl '${url}' \\`,
      `  -H 'Content-Type: application/json' \\`,
      `  -d '${body.replaceAll('\n', '\n     ')}'`,
    ].join('\n')
  }
  if (lang === 'python') {
    return [
      'import google.generativeai as genai',
      '',
      `genai.configure(api_key="<YOUR_API_KEY>")`,
      '',
      `model = genai.GenerativeModel("${ctx.modelName}")`,
      `response = model.generate_content("${userMessage}")`,
      '',
      `print(response.text)`,
    ].join('\n')
  }
  if (lang === 'typescript') {
    return [
      `import { GoogleGenerativeAI } from '@google/generative-ai'`,
      '',
      `const genAI = new GoogleGenerativeAI(process.env.${ctx.apiKeyEnv}!)`,
      `const model = genAI.getGenerativeModel({ model: '${ctx.modelName}' })`,
      '',
      `const result = await model.generateContent('${userMessage}')`,
      `console.log(result.response.text())`,
    ].join('\n')
  }
  return [
    `const response = await fetch('${url}', {`,
    `  method: 'POST',`,
    `  headers: { 'Content-Type': 'application/json' },`,
    `  body: JSON.stringify({`,
    `    contents: [{ parts: [{ text: '${userMessage}' }] }],`,
    `  }),`,
    `})`,
    '',
    `const data = await response.json()`,
    `console.log(data.candidates[0].content.parts[0].text)`,
  ].join('\n')
}

export function buildEmbeddingSample(lang: Lang, ctx: SampleContext): string {
  const url = `${ctx.baseUrl}${ctx.endpointPath}`
  const text = 'The food was delicious and the waiter…'

  if (lang === 'curl') {
    const body = JSON.stringify({ model: ctx.modelName, input: text }, null, 2)
    return [
      `curl ${url} \\`,
      `  -H "Authorization: Bearer $${ctx.apiKeyEnv}" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '${body.replaceAll('\n', '\n     ')}'`,
    ].join('\n')
  }
  if (lang === 'python') {
    return [
      'from openai import OpenAI',
      '',
      `client = OpenAI(base_url="${ctx.baseUrl}/v1", api_key="<YOUR_API_KEY>")`,
      '',
      'response = client.embeddings.create(',
      `    model="${ctx.modelName}",`,
      `    input="${text}",`,
      ')',
      '',
      'print(response.data[0].embedding[:8])',
    ].join('\n')
  }
  if (lang === 'typescript') {
    return [
      `import OpenAI from 'openai'`,
      '',
      `const client = new OpenAI({`,
      `  baseURL: '${ctx.baseUrl}/v1',`,
      `  apiKey: process.env.${ctx.apiKeyEnv},`,
      `})`,
      '',
      `const response = await client.embeddings.create({`,
      `  model: '${ctx.modelName}',`,
      `  input: '${text}',`,
      `})`,
      '',
      `console.log(response.data[0].embedding.slice(0, 8))`,
    ].join('\n')
  }
  return [
    `const response = await fetch('${url}', {`,
    `  method: 'POST',`,
    `  headers: {`,
    `    Authorization: \`Bearer \${process.env.${ctx.apiKeyEnv}}\`,`,
    `    'Content-Type': 'application/json',`,
    `  },`,
    `  body: JSON.stringify({`,
    `    model: '${ctx.modelName}',`,
    `    input: '${text}',`,
    `  }),`,
    `})`,
    '',
    `const data = await response.json()`,
    `console.log(data.data[0].embedding.slice(0, 8))`,
  ].join('\n')
}

export function buildAutoDLAudioVideoSample(
  lang: Lang,
  ctx: SampleContext
): string {
  const url = ctx.baseUrl + ctx.endpointPath
  const modelName = ctx.modelName
  const isSync = modelName === 'autodl:minimax-h3-lipsync'
  const isMulti =
    modelName === 'autodl:minimax-h3-u24' ||
    modelName === 'autodl:minimax-h3-u08' ||
    modelName === 'autodl:minimax-h3-image-audio-10s' ||
    modelName === 'autodl:minimax-h3-image-audio-15s'
  const durationField = isSync ? 'audio_duration' : 'seconds'
  const resolution =
    modelName === 'autodl:minimax-h3-image-audio-10s' ||
    modelName === 'autodl:minimax-h3-lipsync'
      ? '1080p竖'
      : '768p竖'

  if (lang === 'curl') {
    const fields = [
      `curl ${url}`,
      `  -H "Authorization: Bearer $${ctx.apiKeyEnv}"`,
      `  -F "model=${modelName}"`,
    ]
    if (!isSync) {
      fields.push('  -F "prompt=A cinematic scene at sunrise"')
    }
    fields.push(
      `  -F "${durationField}=5"`,
      `  -F "resolution=${resolution}"`,
      '  -F "images=https://example.com/reference-1.png"'
    )
    if (isMulti) {
      fields.push('  -F "images=https://example.com/reference-2.png"')
    }
    fields.push('  -F "audios=https://example.com/reference.mp3"')
    if (isMulti) {
      fields.push('  -F "seed=212238359716024"')
    }
    return fields.join(' ')
  }

  if (lang === 'python') {
    const data = [
      `    ("model", "${modelName}"),`,
      ...(!isSync ? ['    ("prompt", "A cinematic scene at sunrise"),'] : []),
      `    ("${durationField}", "5"),`,
      `    ("resolution", "${resolution}"),`,
      '    ("images", "https://example.com/reference-1.png"),',
      ...(isMulti
        ? ['    ("images", "https://example.com/reference-2.png"),']
        : []),
      '    ("audios", "https://example.com/reference.mp3"),',
      ...(isMulti ? ['    ("seed", "212238359716024"),'] : []),
    ]
    return [
      'import os',
      'import requests',
      '',
      `url = "${url}"`,
      'headers = {"Authorization": "Bearer " + os.environ["NEW_API_KEY"]}',
      'data = [',
      ...data,
      ']',
      'response = requests.post(url, headers=headers, data=data)',
      'response.raise_for_status()',
      'print(response.json())',
    ].join('\n')
  }

  const formLines = [`form.append('model', '${modelName}')`]
  if (!isSync) {
    formLines.push("form.append('prompt', 'A cinematic scene at sunrise')")
  }
  formLines.push(
    `form.append('${durationField}', '5')`,
    `form.append('resolution', '${resolution}')`,
    "form.append('images', 'https://example.com/reference-1.png')"
  )
  if (isMulti) {
    formLines.push(
      "form.append('images', 'https://example.com/reference-2.png')"
    )
  }
  formLines.push("form.append('audios', 'https://example.com/reference.mp3')")
  if (isMulti) {
    formLines.push("form.append('seed', '212238359716024')")
  }
  return [
    'const form = new FormData()',
    ...formLines,
    '',
    `const response = await fetch('${url}', {`,
    `  method: 'POST',`,
    `  headers: { Authorization: "Bearer " + process.env.${ctx.apiKeyEnv} },`,
    `  body: form,`,
    `})`,
    '',
    'const data = await response.json()',
    'console.log(data)',
  ].join('\n')
}

// SeetaCloud（H3）视频样例。
//
// 与其它 openai-video 模型的差异：
// 1. resolution 是「档位+方向」格式（480p横 / 768p横），不是 autoDL 的 768p竖；
// 2. 2026-11-07 起对外模型改为 minimax_h3_v2（文生/图生/首尾帧三合一）与
//    minimax_h3_omni（多模态设定参考 + 1080p）。两个模型都**可以不带参考图**，
//    所以示例统一给最简的纯文生调用；参考图/视频/音频的写法见 /tutorials；
// 3. 时长上限 15 秒，示例写 5 秒（缺省值，安全）。
const SEETA_MODEL_NAMES = new Set(['minimax_h3_v2', 'minimax_h3_omni'])

export function buildSeetaVideoSample(lang: Lang, ctx: SampleContext): string {
  const url = `${ctx.baseUrl}${ctx.endpointPath}`
  const modelName = ctx.modelName
  // 两个新模型都能纯文生，示例不带上传文件，客户照抄即可跑通。
  const includeReferenceImage = !SEETA_MODEL_NAMES.has(modelName)
  const resolution = '480p横'

  if (lang === 'typescript' || lang === 'javascript') {
    return buildSeetaVideoFetchSample(lang, ctx)
  }

  if (lang === 'curl') {
    const fields = [
      `curl ${url} \\`,
      `  -H "Authorization: Bearer $${ctx.apiKeyEnv}" \\`,
      `  -F "model=${modelName}" \\`,
      `  -F "prompt=A cinematic scene at sunrise" \\`,
      `  -F "seconds=5" \\`,
      `  -F "resolution=${resolution}"`,
    ]
    if (includeReferenceImage) {
      fields[fields.length - 1] += ' \\'
      fields.push('  -F "input_reference=@reference.png"')
    }
    return fields.join('\n')
  }

  if (lang === 'python') {
    const data = [
      `    ("model", "${modelName}"),`,
      '    ("prompt", "A cinematic scene at sunrise"),',
      '    ("seconds", "5"),',
      `    ("resolution", "${resolution}"),`,
    ]
    if (includeReferenceImage) {
      data.push('    ("input_reference", open("reference.png", "rb")),')
    }
    return [
      'import os',
      'import requests',
      '',
      `url = "${url}"`,
      'headers = {"Authorization": "Bearer " + os.environ["NEW_API_KEY"]}',
      'data = [',
      ...data,
      ']',
      'response = requests.post(url, headers=headers, data=data)',
      'response.raise_for_status()',
      'print(response.json())',
    ].join('\n')
  }

  const formLines = [
    `form.append('model', '${modelName}')`,
    `form.append('prompt', 'A cinematic scene at sunrise')`,
    `form.append('seconds', '5')`,
    `form.append('resolution', '${resolution}')`,
  ]
  if (includeReferenceImage) {
    formLines.push(`form.append('input_reference', referenceFile)`)
  }
  formLines.push(
    '',
    'response = requests.post(url, headers=headers, data=form)',
    'response.raise_for_status()',
    'print(response.json())'
  )
  return [
    'import os',
    'import requests',
    '',
    `url = "${url}"`,
    'headers = {"Authorization": "Bearer " + os.environ["NEW_API_KEY"]}',
    "with open('reference.png', 'rb') as referenceFile:",
    '    form = {',
    ...formLines.map((line) => `    ${line}`),
    '    ]',
    'response = requests.post(url, headers=headers, data=form)',
    'response.raise_for_status()',
    'print(response.json())',
  ].join('\n')
}

// SeetaCloud 的 TypeScript / JavaScript 样例。
//
// 用 fetch + FormData 而不是 OpenAI SDK：OpenAI SDK 的 videos.create 不接受
// resolution 这类非标准字段，而 SeetaCloud 的分辨率枚举是「档位+方向」格式，
// 必须原样传给上游。
export function buildSeetaVideoFetchSample(
  _lang: Lang,
  ctx: SampleContext
): string {
  const url = `${ctx.baseUrl}${ctx.endpointPath}`
  const modelName = ctx.modelName
  // 与 curl / python 样例保持同一口径：两个新模型都给最简纯文生调用。
  const includeReferenceImage = !SEETA_MODEL_NAMES.has(modelName)
  const resolution = '480p横'

  const formLines = [
    `form.append('model', '${modelName}')`,
    `form.append('prompt', 'A cinematic scene at sunrise')`,
    `form.append('seconds', '5')`,
    `form.append('resolution', '${resolution}')`,
  ]
  if (includeReferenceImage) {
    formLines.push(`form.append('input_reference', referenceFile)`)
  }

  return [
    'const form = new FormData()',
    ...formLines,
    '',
    `const response = await fetch('${url}', {`,
    `  method: 'POST',`,
    `  headers: {`,
    `    Authorization: \`Bearer \${process.env.${ctx.apiKeyEnv}}\`,`,
    `  },`,
    `  body: form,`,
    `})`,
    '',
    'const data = await response.json()',
    'console.log(data)',
  ].join('\n')
}

export function buildVideoSample(lang: Lang, ctx: SampleContext): string {
  const url = `${ctx.baseUrl}${ctx.endpointPath}`
  const isAutoDL = ctx.modelName.startsWith('autodl:')
  // SeetaCloud（自建 ComfyUI 实例）走自己的样例：分辨率枚举是「档位+方向」
  // 格式（480p横 / 768p横），与 autoDL 的 768p竖 不是同一套。
  // 旧名 minimax_h3-文生 / minimax_h3-图生 已于 2026-11-07 退役为隐藏别名。
  if (SEETA_MODEL_NAMES.has(ctx.modelName)) {
    return buildSeetaVideoSample(lang, ctx)
  }
  // 参考图行为沿用历史：标准版 wan3.0-video / wan3.0-video-prime 的示例不带参考图。
  const isLegacyWan3 = /^wan3\.0-video(?:-prime)?$/.test(ctx.modelName)
  // wan3 系（官网/有赞）支持 resolution 参数；其余模型无该参数。
  const wan3Resolution = getWan3SampleResolution(ctx.modelName)
  const hasResolution = isAutoDL || wan3Resolution !== null
  const isR2V = ctx.modelName === 'wan2.7-r2v'
  const isAutoDLMultiReference =
    ctx.modelName === 'autodl:minimax-h3-lightx2v-v5' ||
    ctx.modelName === 'autodl:minimax-h3-lightx2v-v5-15s' ||
    ctx.modelName === 'autodl:minimax-h3-b99-12s' ||
    ctx.modelName === 'autodl:minimax-h3-u24' ||
    ctx.modelName === 'autodl:minimax-h3-u08' ||
    ctx.modelName === 'autodl:minimax-h3-image-audio-10s' ||
    ctx.modelName === 'autodl:minimax-h3-image-audio-15s'
  const isAutoDLAudio =
    ctx.modelName === 'autodl:minimax-h3-u24' ||
    ctx.modelName === 'autodl:minimax-h3-u08' ||
    ctx.modelName === 'autodl:minimax-h3-image-audio-10s' ||
    ctx.modelName === 'autodl:minimax-h3-image-audio-15s' ||
    ctx.modelName === 'autodl:minimax-h3-lipsync'
  const isAutoDLText = ctx.modelName === 'autodl:minimax-h3-text-to-video'
  const isAutoDLB99 = ctx.modelName === 'autodl:minimax-h3-b99-12s'
  // 其余 openai-video 模型（含有赞 wan3 系）的示例带一张参考图。
  const showReferenceImage = !isAutoDLText && !isLegacyWan3
  const autoDLResolution = isAutoDLB99 ? '736p竖' : '768p竖'
  const autoDLSeed = '212238359716024'

  // ── 时长：固定档模型必须写真实档位 ─────────────────────────────────
  //
  // seedance2.5-活动 是**固定 30 秒**的按次档：客户端显式传 5 秒会被后端
  // 400 拒掉（后端不做就近吸附 —— 用户要 5 秒却按 30 秒计费，比报错更糟）。
  //
  // 示例是客户**直接照抄**的东西。统一写 seconds=5 的话，客户照抄就吃 400，
  // 而且报错信息是「fixed duration of 30 seconds」，与示例里的 5 秒对不上，
  // 排查成本极高。所以这里必须按模型给出真实档位。
  const fixedTier = getSeedance25SampleSpec(ctx.modelName)
  const secondsValue = fixedTier ? String(fixedTier.seconds) : '5'

  if (isAutoDLAudio) {
    return buildAutoDLAudioVideoSample(lang, ctx)
  }

  if (lang === 'curl') {
    const fields = [
      `curl ${url} \\`,
      `  -H "Authorization: Bearer $${ctx.apiKeyEnv}" \\`,
      `  -F "model=${ctx.modelName}" \\`,
      `  -F "prompt=A cinematic scene at sunrise" \\`,
      `  -F "seconds=${secondsValue}"`,
    ]
    if (isAutoDL || hasResolution) {
      fields[fields.length - 1] += ' \\'
      fields.push(`  -F "resolution=${wan3Resolution ?? autoDLResolution}"`)
    }
    if (isAutoDLMultiReference) {
      fields[fields.length - 1] += ' \\'
      fields.push(
        '  -F "images=https://example.com/reference-1.png" \\',
        '  -F "images=https://example.com/reference-2.png" \\',
        `  -F "seed=${autoDLSeed}"`
      )
    } else if (showReferenceImage) {
      fields[fields.length - 1] += ' \\'
      fields.push('  -F "input_reference=@reference.png"')
    }
    return fields.join('\n')
  }

  if (lang === 'python') {
    if (isAutoDL || hasResolution) {
      const data = [
        `    ("model", "${ctx.modelName}"),`,
        '    ("prompt", "A cinematic scene at sunrise"),',
        `    ("seconds", "${secondsValue}"),`,
      ]
      data.push(`    ("resolution", "${wan3Resolution ?? autoDLResolution}"),`)
      if (isAutoDLMultiReference || isR2V) {
        data.push('    ("images", "https://example.com/reference-1.png"),')
      }
      if (isAutoDLMultiReference) {
        data.push(
          '    ("images", "https://example.com/reference-2.png"),',
          `    ("seed", "${autoDLSeed}"),`
        )
      }
      return [
        'import os',
        'import requests',
        '',
        `url = "${url}"`,
        'headers = {"Authorization": "Bearer " + os.environ["NEW_API_KEY"]}',
        'data = [',
        ...data,
        ']',
        'response = requests.post(url, headers=headers, data=data)',
        'response.raise_for_status()',
        'print(response.json())',
      ].join('\n')
    }

    return [
      'import os',
      'import requests',
      '',
      `url = "${url}"`,
      'headers = {"Authorization": "Bearer " + os.environ["NEW_API_KEY"]}',
      'data = {',
      `    "model": "${ctx.modelName}",`,
      '    "prompt": "A cinematic scene at sunrise",',
      `    "seconds": "${secondsValue}",`,
      '}',
      'with open("reference.png", "rb") as reference:',
      '    response = requests.post(url, headers=headers, data=data, files={"input_reference": reference})',
      'response.raise_for_status()',
      'print(response.json())',
    ].join('\\n')
  }

  const formLines = [
    `form.append('model', '${ctx.modelName}')`,
    `form.append('prompt', 'A cinematic scene at sunrise')`,
    `form.append('seconds', '${secondsValue}')`,
  ]
  if (isAutoDL || hasResolution) {
    formLines.push(
      `form.append('resolution', '${wan3Resolution ?? autoDLResolution}')`
    )
  }
  if (isAutoDLMultiReference) {
    formLines.push(
      `form.append('images', 'https://example.com/reference-1.png')`,
      `form.append('images', 'https://example.com/reference-2.png')`,
      `form.append('seed', '${autoDLSeed}')`
    )
  } else if (showReferenceImage) {
    formLines.push(`form.append('input_reference', referenceFile)`)
  }

  const referenceSetup =
    !isAutoDL && showReferenceImage
      ? [
          `const referenceResponse = await fetch('https://example.com/reference.png')`,
          `const referenceFile = await referenceResponse.blob()`,
        ]
      : []

  return [
    `const form = new FormData()`,
    ...referenceSetup,
    ...formLines,
    '',
    `const response = await fetch('${url}', {`,
    `  method: 'POST',`,
    `  headers: { Authorization: \`Bearer \${process.env.${ctx.apiKeyEnv}}\` },`,
    `  body: form,`,
    `})`,
    '',
    'const data = await response.json()',
    'console.log(data)',
  ].join('\n')
}

export function buildImageSample(lang: Lang, ctx: SampleContext): string {
  const url = `${ctx.baseUrl}${ctx.endpointPath}`
  const prompt = 'A serene koi pond at sunset, ukiyo-e style.'

  if (lang === 'curl') {
    const body = JSON.stringify(
      { model: ctx.modelName, prompt, size: '1024x1024', n: 1 },
      null,
      2
    )
    return [
      `curl ${url} \\`,
      `  -H "Authorization: Bearer $${ctx.apiKeyEnv}" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '${body.replaceAll('\n', '\n     ')}'`,
    ].join('\n')
  }
  if (lang === 'python') {
    return [
      'from openai import OpenAI',
      '',
      'client = OpenAI(',
      `    base_url="${ctx.baseUrl}/v1",`,
      `    api_key="<YOUR_API_KEY>",`,
      ')',
      '',
      'response = client.images.generate(',
      `    model="${ctx.modelName}",`,
      `    prompt="${prompt}",`,
      `    size="1024x1024",`,
      '    n=1,',
      ')',
      '',
      'print(response.data[0].url)',
    ].join('\n')
  }
  if (lang === 'typescript') {
    return [
      `import OpenAI from 'openai'`,
      '',
      `const client = new OpenAI({`,
      `  baseURL: '${ctx.baseUrl}/v1',`,
      `  apiKey: process.env.${ctx.apiKeyEnv},`,
      `})`,
      '',
      `const response = await client.images.generate({`,
      `  model: '${ctx.modelName}',`,
      `  prompt: '${prompt}',`,
      `  size: '1024x1024',`,
      `  n: 1,`,
      `})`,
      '',
      'console.log(response.data[0].url)',
    ].join('\n')
  }
  return [
    `const response = await fetch('${url}', {`,
    `  method: 'POST',`,
    `  headers: {`,
    `    Authorization: \`Bearer \${process.env.${ctx.apiKeyEnv}}\`,`,
    `    'Content-Type': 'application/json',`,
    `  },`,
    `  body: JSON.stringify({`,
    `    model: '${ctx.modelName}',`,
    `    prompt: '${prompt}',`,
    `    size: '1024x1024',`,
    `    n: 1,`,
    `  }),`,
    `})`,
    '',
    'const data = await response.json()',
    'console.log(data.data[0].url)',
  ].join('\n')
}

export function buildSample(
  lang: Lang,
  endpointType: string,
  ctx: SampleContext
): string {
  if (endpointType === 'anthropic') return buildAnthropicSample(lang, ctx)
  if (endpointType === 'gemini') return buildGeminiSample(lang, ctx)
  if (endpointType === 'embeddings' || endpointType === 'jina-rerank') {
    return buildEmbeddingSample(lang, ctx)
  }
  if (endpointType === 'image-generation') return buildImageSample(lang, ctx)
  if (endpointType === 'openai-video') return buildVideoSample(lang, ctx)
  return buildChatSample(lang, ctx)
}
