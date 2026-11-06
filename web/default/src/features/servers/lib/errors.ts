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

import { serverErrorMessageKey } from '../api'

/**
 * 把服务端错误翻成当前语言。
 *
 * 已知错误码走 i18n；未知错误保留服务端原文 —— 直接吞掉会让用户只看到
 * 一句「请求失败」，运维也无从判断是余额、状态还是并发问题。
 */
export function localizedServerError(
  error: unknown,
  t: (key: string) => string
): string {
  const key = serverErrorMessageKey(error)
  if (key) return t(key)
  if (error instanceof Error && error.message) return error.message
  return t('Request failed')
}
