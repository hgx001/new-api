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
// 测试用：让 Node 解析无扩展名的相对导入（./foo → ./foo.ts）。
//
// 为什么不直接 node --test：源码里的 import 不带 .ts 后缀，
// Node 的 ESM 解析器要求完整路径。这只影响本地跑测试，不进产物。
import { existsSync } from 'node:fs'
import { dirname, resolve as resolvePath } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

export async function resolve(specifier, context, nextResolve) {
  if (specifier.startsWith('.') || specifier.startsWith('/')) {
    const parentURL = context.parentURL
    if (parentURL) {
      const base = dirname(fileURLToPath(parentURL))
      const candidate = resolvePath(base, specifier)
      for (const ext of ['.ts', '.tsx', '/index.ts', '/index.tsx']) {
        if (existsSync(candidate + ext)) {
          return nextResolve(pathToFileURL(candidate + ext).href, context)
        }
      }
    }
  }
  return nextResolve(specifier, context)
}
