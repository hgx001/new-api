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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import en from './locales/en.json'
import fr from './locales/fr.json'
import ja from './locales/ja.json'
import ru from './locales/ru.json'
import vi from './locales/vi.json'
import zh from './locales/zh.json'

/**
 * 语言包「串味」检测。
 *
 * 2026-11 出过一次事故：补文案的脚本把 ENTRIES 的列位用错了一位，
 * 六种语言整体错开 —— en 写成中文、fr 写成日文、ja 写成俄文、ru 写成越南语。
 * 界面上一半法语一半中文，但构建、类型检查、单测全绿，没人发现，
 * 直到用户截图来问。
 *
 * 这里用「该语言不该出现的字符集」当作哨兵：只要某个值的文字体系对不上
 * 语言，就说明它被别的语言污染了。比逐个 key 对比期望值便宜得多，
 * 也不会因为文案微调而频繁失败。
 */

const SCRIPTS = {
  cjk: /[\u4e00-\u9fff]/,
  kana: /[\u3040-\u30ff]/,
  // 越南语专属拉丁扩展：法语用不到这些字符，可以安全区分。
  vietnamese: /[đĐơƠưƯ\u1ea0-\u1ef9]/,
  cyrillic: /[\u0400-\u04ff]/,
} as const

type ScriptName = keyof typeof SCRIPTS

/**
 * 唯一允许在非中文语言里保留汉字的键。
 *
 * 公众号关键词「验证码」是用户要照着回复的原文，翻译掉就收不到验证码了。
 */
const CJK_ALLOWED_KEYS = new Set([
  'Scan the QR code to follow the official account and reply with “验证码” to receive your verification code.',
])

const RULES: Record<string, ScriptName[]> = {
  en: ['cjk', 'kana', 'cyrillic'],
  zh: ['kana', 'cyrillic', 'vietnamese'],
  fr: ['cjk', 'kana', 'cyrillic', 'vietnamese'],
  ja: ['cyrillic', 'vietnamese'],
  ru: ['cjk', 'kana', 'vietnamese'],
  vi: ['cjk', 'kana', 'cyrillic'],
}

const LOCALES: Record<string, { translation: Record<string, unknown> }> = {
  en,
  zh,
  fr,
  ja,
  ru,
  vi,
}

describe('语言包纯净度', () => {
  for (const [lang, scripts] of Object.entries(RULES)) {
    test(`${lang} 的文案不该混入其它文字体系`, () => {
      const table = LOCALES[lang].translation
      const offenders: string[] = []
      for (const [key, value] of Object.entries(table)) {
        if (typeof value !== 'string') continue
        for (const script of scripts) {
          if (!SCRIPTS[script].test(value)) continue
          if (script === 'cjk' && CJK_ALLOWED_KEYS.has(key)) break
          offenders.push(`${key} => ${value.slice(0, 60)}`)
          break
        }
      }
      assert.deepEqual(
        offenders.slice(0, 5),
        [],
        `${lang}.json 有 ${offenders.length} 条文案疑似写成了别的语言`
      )
    })
  }

  test('六种语言的 key 集合一致', () => {
    const reference = new Set(Object.keys(LOCALES.en.translation))
    for (const [lang, bundle] of Object.entries(LOCALES)) {
      if (lang === 'en') continue
      const keys = new Set(Object.keys(bundle.translation))
      const missing = [...reference].filter((key) => !keys.has(key))
      const extra = [...keys].filter((key) => !reference.has(key))
      assert.deepEqual(
        { missing: missing.slice(0, 5), extra: extra.slice(0, 5) },
        { missing: [], extra: [] },
        `${lang}.json 与 en.json 的 key 集合不一致`
      )
    }
  })
})
