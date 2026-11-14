/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the License,
or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/**
 * CapCut（剪映国际版）五档 —— **前端侧**的能力定义。
 *
 * 后端的唯一来源是 `constant/capcut_family.go`（能力组成员）。
 * 前端这份是它的**镜像**：用于「示例代码 / 参数表 / 计费单位」三处，
 * 它们必须与后端校验口径一致，否则客户照抄示例就吃 400。
 *
 * ⚠️ 前端无法直接 import Go 源码，所以这里有一道**交叉校验单测**
 * （capcut-family-crosscheck.test.ts）直接读后端 Go 源码比对常量值。
 * 两边一旦漂移，单测当场变红。
 */

/** CapCut 五档成员。 */
export const CAPCUT_20_MINI = 'cp-seedance-2.0mini'
export const CAPCUT_20_FAST = 'cp-seedance-2.0fast'
export const CAPCUT_20 = 'cp-seedance-2.0'
export const CAPCUT_25_FLASH = 'cp-seedance-2.5flash'
export const CAPCUT_25 = 'cp-seedance-2.5'

/**
 * CapCut 五档的**参数规格**。
 *
 * `seconds` = 该档的**默认/示例**时长（客户不传时后端按它跑）。
 * `maxSeconds` = 该档的**时长上界**（官网滑杆上限），客户传超出会被 400。
 * `resolution` = 该档的示例分辨率（480P 是所有档的基准价档）。
 * `maxResolution` = 该档**已定价**的最高分辨率 —— 更高的（1080P / 4K）
 *   官网虽然有，但**没有定价**，后端显式 400（不能静默降级交付）。
 */
export type CapcutSampleSpec = {
  seconds: number
  maxSeconds: number
  resolution: string
  maxResolution: string
}

/**
 * 五档的参数表。
 *
 * ⚠️ 2.5 与 2.5 Flash 的时长上界是 **30 秒**（官网 2.5 系列 4–30s），
 *   其余三档是 **15 秒**（官网 4–15s）。
 *
 * ⚠️ 这张表用**精确键**而非前缀 —— 公开名 `cp-seedance-2.5flash` 包含子串
 *   `cp-seedance-2.5`，前缀匹配会把 2.5 Flash 误判成 2.5 档。
 */
const CAPCUT_SPECS: Record<string, CapcutSampleSpec> = {
  [CAPCUT_20_MINI]: {
    seconds: 5,
    maxSeconds: 15,
    resolution: '480P',
    maxResolution: '720P',
  },
  [CAPCUT_20_FAST]: {
    seconds: 5,
    maxSeconds: 15,
    resolution: '480P',
    maxResolution: '720P',
  },
  [CAPCUT_20]: {
    seconds: 5,
    maxSeconds: 15,
    resolution: '480P',
    maxResolution: '720P',
  },
  [CAPCUT_25_FLASH]: {
    seconds: 5,
    maxSeconds: 30,
    resolution: '480P',
    maxResolution: '720P',
  },
  [CAPCUT_25]: {
    seconds: 5,
    maxSeconds: 30,
    resolution: '480P',
    maxResolution: '720P',
  },
}

/**
 * 取该模型的参数规格，非 CapCut 成员返回 null。
 */
export function getCapcutSampleSpec(
  modelName: string
): CapcutSampleSpec | null {
  const trimmed = (modelName ?? '').trim()
  return CAPCUT_SPECS[trimmed] ?? null
}

/**
 * CapCut 五档的**成员集合**。
 *
 * ⚠️ 用**精确**集合，不用前缀：`cp-seedance-2.5flash` 包含子串
 *   `cp-seedance-2.5`，前缀匹配会误伤（后端 constant/capcut_family.go 里
 *   已经踩过这个坑并写明）。
 */
const CAPCUT_FAMILY_MEMBERS = new Set<string>([
  CAPCUT_20_MINI,
  CAPCUT_20_FAST,
  CAPCUT_20,
  CAPCUT_25_FLASH,
  CAPCUT_25,
])

/**
 * 该模型是否是 CapCut 五档成员。
 */
export function isCapcutFamilyMember(modelName: string): boolean {
  return CAPCUT_FAMILY_MEMBERS.has((modelName ?? '').trim())
}
