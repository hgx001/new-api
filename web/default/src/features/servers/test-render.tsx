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
import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'

import en from '../../i18n/locales/en.json'
import zh from '../../i18n/locales/zh.json'

/**
 * 服务器租赁组件的 SSR 渲染助手。
 *
 * 为什么用 SSR 而不是 jsdom + testing-library：仓库前端测试基线是
 * node:test（bun test 跑），没有 DOM 测试环境；react-dom/server 不需要
 * DOM，能直接渲染出真实组件的输出用于断言文本，不引入新基建。
 */
export async function renderWithI18n(
  node: React.ReactNode,
  locale: 'en' | 'zh' = 'en'
) {
  const i18n = createInstance()
  await i18n.use(initReactI18next).init({
    lng: locale,
    fallbackLng: 'en',
    resources: {
      en: { translation: en.translation },
      zh: { translation: zh.translation },
    },
    interpolation: { escapeValue: false },
  })
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>{node}</I18nextProvider>
  )
}

/** 统计子串在 HTML 中出现的次数 */
export function countOccurrences(html: string, needle: string): number {
  return html.split(needle).length - 1
}
