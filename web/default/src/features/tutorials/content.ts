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
      id: 'platform-access',
      title: '平台接入',
      level: 2,
      content: [
        {
          type: 'paragraph',
          children: ['前置条件：'],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              '一个本平台的 ',
              { type: 'link', href: '/keys', text: '密钥/令牌（key）', external: false },
            ],
            ['已安装任意支持 OpenAI 格式调用的客户端或命令行工具（如 curl、Python、Node.js）'],
          ],
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '获取 API 密钥' }],
        },
        {
          type: 'paragraph',
          children: [
            '登录平台后，进入',
            { type: 'link', href: '/keys', text: '密钥/令牌', external: false },
            '页面，点击“新建密钥”即可生成一个 API Key。该密钥用于调用本平台的统一 API 接口。',
          ],
        },
        {
          type: 'hint',
          children: [
            '安全提示：API Key 等同于您的账户密码，请勿泄露给他人或在公开仓库中提交。',
          ],
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '配置 API 端点与密钥' }],
        },
        {
          type: 'paragraph',
          children: [
            '本平台提供与 OpenAI 兼容的 API 格式。接入时通常需要配置两个变量：',
          ],
        },
        {
          type: 'list',
          ordered: false,
          items: [
            [
              { type: 'code', value: 'OPENAI_API_KEY' },
              '：您在平台生成的 API Key',
            ],
            [
              { type: 'code', value: 'OPENAI_BASE_URL' },
              '：本平台的 API 基础地址，例如 ',
              { type: 'link', href: url, text: url, external: true },
            ],
          ],
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '方式一：使用命令行（适合 macOS / Linux / WSL）' }],
        },
        {
          type: 'paragraph',
          children: [
            '在终端中编辑您的 shell 配置文件（如 ~/.bashrc、~/.zshrc），添加以下内容：',
          ],
        },
        {
          type: 'codeBlock',
          value: `export OPENAI_API_KEY="sk-..."
export OPENAI_BASE_URL="${url}/v1"`,
          lang: 'bash',
        },
        {
          type: 'paragraph',
          children: ['保存后执行以下命令使配置生效：'],
        },
        {
          type: 'codeBlock',
          value: 'source ~/.bashrc  # 或 source ~/.zshrc',
          lang: 'bash',
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '方式二：使用 Windows 命令行（CMD / PowerShell）' }],
        },
        {
          type: 'paragraph',
          children: [
            'Windows 命令提示符（CMD）和 PowerShell 都可以使用 setx 命令永久设置环境变量。setx 会将变量写入注册表，影响未来打开的所有终端窗口。',
          ],
        },
        {
          type: 'hint',
          children: [
            '重要提示：setx 不会影响当前已经打开的窗口。设置完成后，请关闭现有终端并重新打开一个新窗口。',
          ],
        },
        {
          type: 'paragraph',
          children: ['操作步骤（在 CMD 中执行）：'],
        },
        {
          type: 'paragraph',
          children: ['设置 API Key：'],
        },
        {
          type: 'codeBlock',
          value: 'setx OPENAI_API_KEY "sk-..."',
          lang: 'bash',
        },
        {
          type: 'paragraph',
          children: ['设置 API Base URL：'],
        },
        {
          type: 'codeBlock',
          value: `setx OPENAI_BASE_URL "${url}/v1"`,
          lang: 'bash',
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '方式三：使用 Windows 图形用户界面（GUI）' }],
        },
        {
          type: 'paragraph',
          children: [
            '这是最直观、最不容易出错的方法，推荐给所有 Windows 用户。',
          ],
        },
        {
          type: 'paragraph',
          children: [
            { type: 'strong', value: '打开“系统属性”' },
            '按键盘上的 Windows 键 + R 键，打开“运行”对话框。输入 sysdm.cpl 然后按回车。',
          ],
        },
        {
          type: 'codeBlock',
          value: 'sysdm.cpl',
          lang: 'bash',
        },
        {
          type: 'paragraph',
          children: [
            { type: 'strong', value: '进入“环境变量”设置' },
            '在打开的“系统属性”窗口中，切换到“高级”选项卡。点击右下角的“环境变量...”按钮。',
          ],
        },
        {
          type: 'paragraph',
          children: [
            { type: 'strong', value: '添加新的用户变量' },
            '在弹出的“环境变量”窗口中，上半部分是“（您的用户名） 的用户变量”。点击“新建(N)...”按钮。变量名(N): OPENAI_API_KEY，变量值(V): sk-...（你的密钥），点击“确定”。',
          ],
        },
        {
          type: 'codeBlock',
          value: 'OPENAI_API_KEY\nsk-...',
          lang: 'text',
        },
        {
          type: 'paragraph',
          children: [
            { type: 'strong', value: '重复步骤添加第二个变量' },
            '再次点击“新建(N)...”。变量名(N): OPENAI_BASE_URL，变量值(V): ',
            { type: 'link', href: `${url}/v1`, text: `${url}/v1`, external: true },
            '，点击“确定”。',
          ],
        },
        {
          type: 'codeBlock',
          value: `OPENAI_BASE_URL\n${url}/v1`,
          lang: 'text',
        },
        {
          type: 'paragraph',
          children: [
            { type: 'strong', value: '保存并关闭' },
            '在“环境变量”窗口点击“确定”。在“系统属性”窗口点击“确定”。',
          ],
        },
        {
          type: 'hint',
          children: [
            '重要：通过 GUI 设置完成后，请关闭所有已经打开的 CMD 或 PowerShell 窗口，然后重新打开一个新窗口，这样新的环境变量才会生效。',
          ],
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '开始使用：curl 示例' }],
        },
        {
          type: 'paragraph',
          children: [
            '打开一个新终端，执行以下命令测试接口是否正常工作：',
          ],
        },
        {
          type: 'codeBlock',
          value: `curl ${url}/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer sk-..." \\
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "你好"}]
  }'`,
          lang: 'bash',
        },
        {
          type: 'paragraph',
          children: [{ type: 'strong', value: '开始使用：Python 示例' }],
        },
        {
          type: 'codeBlock',
          value: `from openai import OpenAI

client = OpenAI(
    api_key="sk-...",
    base_url="${url}/v1"
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "你好"}]
)
print(response.choices[0].message.content)`,
          lang: 'python',
        },
      ],
    },
    {
      id: 'platform-can-do',
      title: '平台能为您做什么',
      level: 2,
      content: [
        {
          type: 'list',
          ordered: false,
          items: [
            [
              { type: 'strong', value: '统一接入多家大模型' },
              '：一次接入即可调用 OpenAI、Claude、Gemini、Azure、AWS Bedrock 等 40 余家上游供应商的模型，无需为每家单独适配。',
            ],
            [
              { type: 'strong', value: '灵活的密钥与额度管理' },
              '：支持创建多个 API Key，分别设置额度、速率限制和可用模型，方便团队协作与成本控制。',
            ],
            [
              { type: 'strong', value: '计费与审计' },
              '：实时记录每次调用的 token 消耗、费用和响应时间，提供明细账单与用量统计。',
            ],
            [
              { type: 'strong', value: '渠道负载均衡与容灾' },
              '：自动在多个上游渠道间分发请求，单个渠道异常时自动切换，提升服务稳定性。',
            ],
          ],
        },
      ],
    },
    {
      id: 'why-developers-love',
      title: '为什么开发者喜欢本平台',
      level: 2,
      content: [
        {
          type: 'list',
          ordered: false,
          items: [
            [
              { type: 'strong', value: '与 OpenAI 兼容' },
              '：现有使用 OpenAI SDK 的项目只需修改 base_url 和 api_key 即可迁移，无需重写业务代码。',
            ],
            [
              { type: 'strong', value: '开箱即用' },
              '：提供 Docker 一键部署、详细的安装文档和管理后台，几分钟即可搭建私有化 API 网关。',
            ],
            [
              { type: 'strong', value: '开源可定制' },
              '：基于 AGPL-3.0 开源，代码透明，可根据团队需求二次开发或扩展新的上游渠道。',
            ],
            [
              { type: 'strong', value: '企业级能力' },
              '：支持用户体系、分组策略、模型计费表达式、日志审计和多语言前端，满足生产环境需求。',
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
    { title: '平台接入', href: '#platform-access' },
    { title: '平台能为您做什么', href: '#platform-can-do' },
    { title: '为什么开发者喜欢本平台', href: '#why-developers-love' },
  ]
}
