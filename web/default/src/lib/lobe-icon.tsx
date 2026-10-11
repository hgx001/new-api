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
 * LobeHub Icon Loader
 * Renders icons from @lobehub/icons by name.
 *
 * Supports:
 * - Basic: "OpenAI", "OpenAI.Color"
 * - Chained properties: "OpenAI.Avatar.type={'platform'}"
 * - Size parameter: getLobeIcon("OpenAI", 20)
 *
 * Icons are imported explicitly rather than via `import * as LobeIcons`.
 * The namespace form defeats tree-shaking (the lookup key is dynamic, so the
 * bundler must retain all 320 icons / ~6.8 MB of SVG path data in a shared
 * chunk), which made every client-side navigation pay for the whole library.
 * The explicit registry below keeps only the icons this app actually renders.
 *
 * To add an icon: import it and add it to LOBE_ICONS. Names come from
 * `getChannelTypeIcon` (channel types), `defaultVendorIcons`
 * (model/pricing_default.go), and the provider maps in usage-logs /
 * rankings / pricing.
 */
import {
  Ai360,
  Aws,
  Azure,
  AzureAI,
  Baichuan,
  Baidu,
  Claude,
  Cloudflare,
  Cohere,
  Coze,
  DeepSeek,
  Dify,
  Doubao,
  FastGPT,
  Gemini,
  Google,
  Grok,
  Hunyuan,
  InternLM,
  Jimeng,
  Jina,
  Kling,
  Meta,
  Midjourney,
  Minimax,
  Mistral,
  Moonshot,
  NewAPI,
  Ollama,
  OpenAI,
  OpenRouter,
  Perplexity,
  Qwen,
  Replicate,
  SiliconCloud,
  Spark,
  Stepfun,
  Suno,
  Vidu,
  Volcengine,
  Wenxin,
  XAI,
  XiaomiMiMo,
  Xinference,
  Yi,
  Zhipu,
} from '@lobehub/icons'

type LobeIconComponent = React.ComponentType<Record<string, unknown>>

// Each icon is a compounded component (Mono + .Color/.Avatar/.Text/.Combine),
// so the registry values keep their sub-components for chained lookups.
const LOBE_ICONS: Record<string, unknown> = {
  Ai360,
  Aws,
  Azure,
  AzureAI,
  Baichuan,
  Baidu,
  Claude,
  Cloudflare,
  Cohere,
  Coze,
  DeepSeek,
  Dify,
  Doubao,
  FastGPT,
  Gemini,
  Google,
  Grok,
  Hunyuan,
  InternLM,
  Jimeng,
  Jina,
  Kling,
  Meta,
  Midjourney,
  Minimax,
  Mistral,
  Moonshot,
  NewAPI,
  Ollama,
  OpenAI,
  OpenRouter,
  Perplexity,
  Qwen,
  Replicate,
  SiliconCloud,
  Spark,
  Stepfun,
  Suno,
  Vidu,
  Volcengine,
  Wenxin,
  XAI,
  XiaomiMiMo,
  Xinference,
  Yi,
  Zhipu,
}

/**
 * Parse a property value from string to appropriate type
 * @param raw - Raw string value
 * @returns Parsed value (boolean, number, or string)
 */
function parseValue(raw: string | undefined | null): string | number | boolean {
  if (raw == null) return true

  let v = String(raw).trim()

  // Remove curly braces
  if (v.startsWith('{') && v.endsWith('}')) {
    v = v.slice(1, -1).trim()
  }

  // Remove quotes
  if (
    (v.startsWith('"') && v.endsWith('"')) ||
    (v.startsWith("'") && v.endsWith("'"))
  ) {
    return v.slice(1, -1)
  }

  // Boolean
  if (v === 'true') return true
  if (v === 'false') return false

  // Number
  if (/^-?\d+(?:\.\d+)?$/.test(v)) return Number(v)

  // Return as string
  return v
}

/**
 * Get LobeHub icon component by name
 * @param iconName - Icon name/description (e.g., "OpenAI", "OpenAI.Color", "Claude.Avatar")
 * @param size - Icon size (default: 20)
 * @returns Icon component or fallback
 *
 * @example
 * getLobeIcon("OpenAI", 24)
 * getLobeIcon("OpenAI.Color", 20)
 * getLobeIcon("Claude.Avatar.type={'platform'}", 32)
 */
export function getLobeIcon(
  iconName: string | undefined | null,
  size: number = 20
): React.ReactNode {
  const renderFallback = (letter: string) => (
    <div
      className='bg-muted text-muted-foreground flex items-center justify-center rounded-full text-xs font-medium'
      style={{ width: size, height: size }}
    >
      {letter}
    </div>
  )

  if (!iconName || typeof iconName !== 'string') {
    return renderFallback('?')
  }

  const trimmedName = iconName.trim()
  if (!trimmedName) {
    return renderFallback('?')
  }

  // Parse component path and chained properties
  const segments = trimmedName.split('.')
  const baseKey = segments[0]
  const BaseIcon = LOBE_ICONS[baseKey] as Record<string, unknown> | undefined

  let IconComponent: LobeIconComponent | undefined
  let propStartIndex: number

  if (BaseIcon && segments.length > 1 && BaseIcon[segments[1]]) {
    IconComponent = BaseIcon[segments[1]] as LobeIconComponent
    propStartIndex = 2
  } else {
    IconComponent = LOBE_ICONS[baseKey] as LobeIconComponent | undefined
    propStartIndex = segments.length > 1 && /^[A-Z]/.test(segments[1]) ? 2 : 1
  }

  // Fallback if icon not found
  if (
    !IconComponent ||
    (typeof IconComponent !== 'function' && typeof IconComponent !== 'object')
  ) {
    if (import.meta.env.DEV) {
      // eslint-disable-next-line no-console
      console.warn(
        `[lobe-icon] "${trimmedName}" is not in LOBE_ICONS; add it to src/lib/lobe-icon.tsx to render the real icon.`
      )
    }
    return renderFallback(trimmedName.charAt(0).toUpperCase())
  }

  // Parse chained properties (e.g., "type={'platform'}", "shape='square'")
  const props: Record<string, string | number | boolean> = {}

  for (let i = propStartIndex; i < segments.length; i++) {
    const seg = segments[i]
    if (!seg) continue

    const eqIdx = seg.indexOf('=')
    if (eqIdx === -1) {
      props[seg.trim()] = true
      continue
    }

    const key = seg.slice(0, eqIdx).trim()
    const valRaw = seg.slice(eqIdx + 1).trim()
    props[key] = parseValue(valRaw)
  }

  // Set size if not explicitly specified in the string
  if (props.size == null && size != null) {
    props.size = size
  }

  return <IconComponent {...props} />
}
