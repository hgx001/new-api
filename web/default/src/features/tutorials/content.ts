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
export type TextNode =
  | string
  | { type: 'link'; href: string; text: string; external?: boolean }
  | { type: 'code'; value: string }
  | { type: 'strong'; value: string }

export type ContentNode =
  | { type: 'paragraph'; children: TextNode[] }
  | { type: 'heading'; level: 3 | 4; id: string; children: TextNode[] }
  | { type: 'list'; ordered: boolean; items: TextNode[][] }
  | { type: 'codeBlock'; value: string; lang?: string }
  | { type: 'image'; src: string; alt: string }
  | { type: 'hint'; children: TextNode[] }

export type TutorialSection = {
  id: string
  title: string
  level: 1 | 2
  children?: TutorialSection[]
  content?: ContentNode[]
}

export function getTutorials(platformUrl: string): TutorialSection[] {
  const url = platformUrl

  return [
    {
      id: 'introduction',
      title: '平台简介',
      level: 2,
      content: [
        {
          type: 'paragraph',
          children: [
            'New API 是一站式 AI API 管理网关，面向企业与开发者提供统一的 API 接入、密钥管理、计费审计和多渠道容灾能力。通过本平台，您无需逐一接入不同厂商的接口，即可调用 OpenAI、Claude、Gemini、Azure、AWS Bedrock 等 40 余家上游供应商的大模型。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'core-features',
          children: [{ type: 'strong', value: '核心能力' }],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              { type: 'strong', value: '统一接口' },
              '：所有模型均使用与 OpenAI 兼容的接口，切换模型只需修改 model 参数，无需改动业务逻辑。',
            ],
            [
              { type: 'strong', value: '多模型支持' },
              '：支持 GPT、Claude、Gemini、DeepSeek 等主流模型，上游渠道持续扩展。',
            ],
            [
              { type: 'strong', value: '弹性计费' },
              '：按量计费，用量与余额实时可查，支持分组策略与计费表达式。',
            ],
            [
              { type: 'strong', value: '团队协作' },
              '：多用户管理、API Key 隔离、额度与模型权限灵活分配。',
            ],
            [
              { type: 'strong', value: '实时监控' },
              '：提供详细调用日志、数据看板和成本追踪，帮助优化 API 使用。',
            ],
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'platform-advantages',
          children: [{ type: 'strong', value: '平台优势' }],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              { type: 'strong', value: '开箱即用' },
              '：注册并创建 API Key 后即可开始调用，无需复杂配置。',
            ],
            [
              { type: 'strong', value: '高可用' },
              '：多渠道负载均衡与自动故障切换，保障 API 请求稳定响应。',
            ],
            [
              { type: 'strong', value: '开发者友好' },
              '：兼容 OpenAI SDK，支持 Python、Node.js、cURL 等调用方式，降低接入成本。',
            ],
          ],
        },
      ],
    },
    {
      id: 'quickstart',
      title: '快速开始',
      level: 2,
      content: [
        {
          type: 'heading',
          level: 3,
          id: 'create-api-key',
          children: [{ type: 'strong', value: '创建 API Key' }],
        },
        {
          type: 'list',
          ordered: true,
          items: [
            [
              '登录平台后，进入左侧菜单的',
              { type: 'link', href: '/keys', text: '密钥/令牌', external: false },
              '页面。',
            ],
            ['点击右上角“新建密钥”按钮。'],
            ['在弹窗中填写密钥名称，并选择过期时间。'],
            ['展开“高级设置”，在模型限制中选择该 Key 允许调用的模型。'],
            ['点击“保存”——生成的密钥将显示在列表中。'],
          ],
        },
        {
          type: 'hint',
          children: [
            '安全提示：API Key 等同于账户密码，弹窗关闭后将无法再次查看完整密钥。如遗失，请删除后重新创建。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'configure-base-url',
          children: [{ type: 'strong', value: '配置 Base URL' }],
        },
        {
          type: 'paragraph',
          children: [
            '本平台提供与 OpenAI 完全兼容的 API 接口，只需将 Base URL 替换为以下地址：',
          ],
        },
        {
          type: 'codeBlock',
          value: `${url}/v1`,
          lang: 'text',
        },
        {
          type: 'paragraph',
          children: [
            '兼容接口包括：',
            { type: 'code', value: '/v1/chat/completions' },
            '、',
            { type: 'code', value: '/v1/models' },
            '、',
            { type: 'code', value: '/v1/embeddings' },
            ' 等。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'code-examples',
          children: [{ type: 'strong', value: '代码调用示例' }],
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: 'cURL' }],
        },
        {
          type: 'codeBlock',
          value: `curl ${url}/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer sk-YOUR_API_KEY" \\
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "你好"}],
    "max_tokens": 1000
  }'`,
          lang: 'bash',
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: 'Python（使用 OpenAI SDK）' }],
        },
        {
          type: 'codeBlock',
          value: `from openai import OpenAI

client = OpenAI(
    api_key="sk-YOUR_API_KEY",
    base_url="${url}/v1"
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "你好"}],
    max_tokens=1000
)

print(response.choices[0].message.content)`,
          lang: 'python',
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: 'Node.js（使用 OpenAI SDK）' }],
        },
        {
          type: 'codeBlock',
          value: `import OpenAI from "openai";

const client = new OpenAI({
  apiKey: "sk-YOUR_API_KEY",
  baseURL: "${url}/v1",
});

const response = await client.chat.completions.create({
  model: "gpt-4o",
  messages: [{ role: "user", content: "你好" }],
  max_tokens: 1000,
});

console.log(response.choices[0].message.content);`,
          lang: 'javascript',
        },
        {
          type: 'hint',
          children: [
            '模型名称请替换为您在平台中实际启用并允许该 Key 调用的模型，例如 gpt-4o、claude-3-5-sonnet-latest、deepseek-chat、gemini-1.5-pro 等。',
          ],
        },
      ],
    },
    {
      id: 'ai-client-config',
      title: 'AI 客户端配置',
      level: 2,
      content: [
        {
          type: 'paragraph',
          children: [
            '以下介绍常见 AI 编程工具与本平台对接的方式。所有示例中的 API Key、Base URL 和模型名请按实际情况替换。',
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'claude-code',
          children: [{ type: 'strong', value: 'Claude Code CLI' }],
        },
        {
          type: 'paragraph',
          children: [
            'Claude Code 是 Anthropic 推出的终端 AI 编程助手。通过修改其配置文件，可将其后端指向本平台提供的兼容接口。',
          ],
        },
        {
          type: 'paragraph',
          children: ['在 ', { type: 'code', value: '~/.claude/settings.json' }, ' 中添加如下配置：'],
        },
        {
          type: 'codeBlock',
          value: `{
  "env": {
    "ANTHROPIC_API_KEY": "sk-YOUR_API_KEY",
    "ANTHROPIC_BASE_URL": "${url}",
    "ANTHROPIC_MODEL": "claude-3-5-sonnet-latest",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "claude-3-5-sonnet-latest",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "claude-3-5-sonnet-latest",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "claude-3-5-sonnet-latest",
    "ANTHROPIC_REASONING_MODEL": "claude-3-5-sonnet-latest",
    "API_TIMEOUT_MS": "3000000",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"
  }
}`,
          lang: 'json',
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '说明' }],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              { type: 'code', value: 'ANTHROPIC_API_KEY' },
              '：替换为您在本平台创建的 API Key。',
            ],
            [
              { type: 'code', value: 'ANTHROPIC_BASE_URL' },
              '：填写本平台地址，',
              { type: 'strong', value: '不要' },
              '带 ',
              { type: 'code', value: '/v1' },
              ' 后缀。Claude Code 实际会请求 ',
              { type: 'code', value: `${url}/v1/messages` },
              '，该端点已在本平台开放。',
            ],
            [
              '模型名请替换为您实际启用的 Claude 兼容模型，例如 claude-3-5-sonnet-latest。',
            ],
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'opencode',
          children: [{ type: 'strong', value: 'OpenCode TUI' }],
        },
        {
          type: 'paragraph',
          children: [
            'OpenCode 是一款终端式 AI 编码工具，支持通过 JSON 配置文件接入 OpenAI 兼容服务商。',
          ],
        },
        {
          type: 'paragraph',
          children: [
            '在 ',
            { type: 'code', value: '~/.config/opencode/opencode.json' },
            ' 中添加如下配置：',
          ],
        },
        {
          type: 'codeBlock',
          value: `{
  "providers": {
    "newapi": {
      "name": "New API",
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "apiKey": "sk-YOUR_API_KEY",
        "baseURL": "${url}/v1"
      },
      "models": {
        "gpt-4o": { "name": "GPT-4o" },
        "deepseek-chat": { "name": "DeepSeek Chat" }
      }
    }
  }
}`,
          lang: 'json',
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '说明' }],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              { type: 'code', value: 'baseURL' },
              '：OpenAI 兼容端点需要带 ',
              { type: 'code', value: '/v1' },
              ' 后缀。',
            ],
            [
              { type: 'code', value: 'models' },
              '：填写您希望在该工具中使用的模型名称与展示名。',
            ],
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'cline',
          children: [{ type: 'strong', value: 'Cline（VS Code 扩展）' }],
        },
        {
          type: 'paragraph',
          children: [
            'Cline 是 VS Code 的一款 AI 编程扩展，支持 OpenAI 兼容接口。',
          ],
        },
        {
          type: 'paragraph',
          children: ['在 Cline 扩展设置中，按以下方式填写：'],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              { type: 'strong', value: 'API Provider' },
              '：选择 ',
              { type: 'code', value: 'OpenAI Compatible' },
            ],
            [
              { type: 'strong', value: 'Base URL' },
              '：',
              { type: 'code', value: `${url}/v1` },
            ],
            [
              { type: 'strong', value: 'API Key' },
              '：',
              { type: 'code', value: 'sk-YOUR_API_KEY' },
            ],
            [
              { type: 'strong', value: 'Model' },
              '：',
              { type: 'code', value: 'gpt-4o' },
            ],
          ],
        },
        {
          type: 'heading',
          level: 3,
          id: 'generic-config',
          children: [{ type: 'strong', value: '通用配置（OpenAI 兼容客户端）' }],
        },
        {
          type: 'paragraph',
          children: [
            '任何支持 OpenAI 兼容接口的客户端或 SDK，均可使用以下通用参数：',
          ],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              { type: 'strong', value: 'Base URL' },
              '：',
              { type: 'code', value: `${url}/v1` },
            ],
            [
              { type: 'strong', value: 'API Key' },
              '：您在本平台创建的 API Key',
            ],
            [
              { type: 'strong', value: '模型' },
              '：在平台模型广场中查询当前可用模型，例如 gpt-4o、claude-3-5-sonnet-latest、deepseek-chat、gemini-1.5-pro',
            ],
          ],
        },
      ],
    },
  ]
}

export function getPageTitle(): string {
  return 'New API 接入教程'
}

export function getPagination() {
  return {
    prev: {
      title: '平台接入',
      href: '/tutorials',
      disabled: true,
    },
    next: {
      title: '模型广场使用指南',
      href: '/tutorials',
      disabled: true,
    },
  }
}

export function getDocIndex() {
  return [
    { title: '平台简介', href: '#introduction' },
    { title: '核心能力', href: '#core-features' },
    { title: '平台优势', href: '#platform-advantages' },
    { title: '快速开始', href: '#quickstart' },
    { title: '创建 API Key', href: '#create-api-key' },
    { title: '配置 Base URL', href: '#configure-base-url' },
    { title: '代码调用示例', href: '#code-examples' },
    { title: 'AI 客户端配置', href: '#ai-client-config' },
    { title: 'Claude Code CLI', href: '#claude-code' },
    { title: 'OpenCode TUI', href: '#opencode' },
    { title: 'Cline（VS Code 扩展）', href: '#cline' },
    { title: '通用配置', href: '#generic-config' },
  ]
}
