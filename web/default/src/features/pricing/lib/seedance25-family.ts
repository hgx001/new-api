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
 * seedance2.5 促销族 —— **前端侧**的能力定义。
 *
 * 后端的唯一来源是 `constant/seedance25_family.go`（能力组成员）+
 * `relay/channel/task/youzanwan3/constants.go`（固定档常量）。
 * 前端这份是它的**镜像**：用于「示例代码 / 参数表 / 计费单位」三处，
 * 它们必须与后端校验口径一致，否则客户照抄示例就吃 400。
 *
 * ⚠️ 前端无法直接 import Go 源码，所以这里有一道**交叉校验单测**
 * （seedance25-family-crosscheck.test.ts）直接读后端 Go 源码比对常量值。
 * 两边一旦漂移，单测当场变红。
 */

/** 促销族成员。 */
export const SEEDANCE25_TEHUI = 'seedance2.5-特惠'
export const SEEDANCE25_HUODONG = 'seedance2.5-活动'

/**
 * 固定档规格。
 *
 * `seconds` = 该模型唯一合法的生成时长（后端对其它值直接 400）。
 * `resolution` = 该模型唯一合法的分辨率。
 *
 * 只有**按次档**（活动版）是固定档：按次定价只有在时长固定时才不会出现
 * 「长片亏本」，而母本是按秒计费、时长 2—30 秒可选。
 */
export type Seedance25SampleSpec = {
  seconds: number
  resolution: string
}

/**
 * 促销族的**固定档**表。
 *
 * 母本（seedance2.5-特惠）按秒计费、时长 2—30 秒可选、分辨率 720P/1080P
 * 可选 → **不是固定档**，示例沿用通用的 5 秒 / 720P。
 *
 * 活动版（seedance2.5-活动）按次计费、**固定 30 秒**、**固定 720P**
 * → 是固定档，示例必须写实值。
 */
const SEEDANCE25_FIXED_TIER_SPECS: Record<string, Seedance25SampleSpec> = {
  [SEEDANCE25_HUODONG]: {
    seconds: 30,
    resolution: '720P',
  },
}

/**
 * 取该模型的固定档规格，非固定档返回 null。
 *
 * ⚠️ 母本刻意**不在**这张表里：它按秒计费、时长可选，示例写 5 秒是对的。
 * 如果把母本也标成固定 30 秒，客户想跑 5 秒就被示例误导了。
 */
export function getSeedance25SampleSpec(
  modelName: string
): Seedance25SampleSpec | null {
  const trimmed = (modelName ?? '').trim()
  return SEEDANCE25_FIXED_TIER_SPECS[trimmed] ?? null
}

/**
 * 该模型是否是「固定档（时长锁死）」模型。
 *
 * 后端对这类模型会**显式拒绝**非档位的时长（400），不做就近吸附。
 */
export function isFixedTierModel(modelName: string): boolean {
  return getSeedance25SampleSpec(modelName) !== null
}

/**
 * 促销族成员集合。
 *
 * ⚠️ 用**精确**集合，不用前缀：站内还有 seedance-2-5 / seedance-2.5 /
 * seedance2.5特惠版 等一堆相近的 seedance 模型名，前缀匹配会误命中别家
 * 渠道的枚举表（后端 constant/seedance25_family.go 里已经踩过这个坑并写明）。
 */
const SEEDANCE25_FAMILY_MEMBERS = new Set<string>([
  SEEDANCE25_TEHUI,
  SEEDANCE25_HUODONG,
])

/**
 * 该模型是否是 seedance2.5 促销族成员。
 */
export function isSeedance25FamilyMember(modelName: string): boolean {
  return SEEDANCE25_FAMILY_MEMBERS.has((modelName ?? '').trim())
}

/**
 * 促销族成员的**示例分辨率**。
 *
 * 与固定档表分开，因为口径不同：
 *   - 母本：720P / 1080P **都合法**，示例取 720P（最低档、最便宜）；
 *   - 活动版：只有 720P 合法（1080P 会被 400）。
 *
 * 两者示例恰好都是 720P，但**原因不同** —— 母本是「取默认档」，活动版是
 * 「唯一档位」。混为一谈会导致哪天母本改价改档时把活动版一起带偏。
 */
const SEEDANCE25_SAMPLE_RESOLUTIONS: Record<string, string> = {
  [SEEDANCE25_TEHUI]: '720P',
  [SEEDANCE25_HUODONG]: '720P',
}

/**
 * 取促销族成员的示例分辨率，非族内成员返回 null。
 */
export function getSeedance25SampleResolution(
  modelName: string
): string | null {
  const trimmed = (modelName ?? '').trim()
  return SEEDANCE25_SAMPLE_RESOLUTIONS[trimmed] ?? null
}
