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

export type RegionId = 'guangzhou' | 'shanghai'

export type PlanId = 'lite' | 'general' | 'compute' | 'memory' | 'gpu'

export type TermId = 'half' | 'year' | 'triple'

/**
 * 计费口径。
 *
 * - `hourly`：按量（预付 N 小时/天），时长用完实例自动释放
 * - `prepaid`：包月预付（一次付清若干个月），整个周期内一直可用
 */
export type BillingMode = 'hourly' | 'prepaid'

export type ServerStatus = 'running' | 'pending' | 'expired'

export interface Region {
  id: RegionId
  /** i18n key: region display name */
  nameKey: string
  /** i18n key: one-line region description */
  descriptionKey: string
  /** 机房线路标签，例如 CN2 GIA */
  line: string
  /** 延迟标签，例如 12ms */
  latency: string
  /** 地区价格系数 */
  priceFactor: number
}

export interface ServerGpu {
  /** 显卡型号，例如 RTX 4090 */
  model: string
  /** 单卡显存（GB） */
  vram: number
  /** 卡数 */
  count: number
}

export interface ServerSpec {
  id: string
  /** vCPU 核数 */
  cpu: number
  /** 内存 GB */
  memory: number
  /** 系统盘 GB */
  disk: number
  /** 规格价格系数（相对该型号基准档） */
  multiplier: number
  /**
   * 显卡配置（只有 GPU 型套餐才有）。
   *
   * 为什么必须有：显卡型号 + 显存是 GPU 算力选型的**第一决策依据**
   * （显存决定模型装不装得下），没有它用户只能看到 vCPU/内存/硬盘，
   * 选了 GPU 型也不知道拿到的是什么卡。
   */
  gpu?: ServerGpu
}

export interface ServerPlan {
  id: PlanId
  /** i18n key: plan display name */
  nameKey: string
  /** i18n key: plan short tagline */
  taglineKey: string
  /** i18n key: plan description */
  descriptionKey: string
  /** 型号基准月价（¥/月，含 5M 带宽，不含地区系数与周期折扣） */
  basePricePerMonth: number
  /**
   * 型号基准小时价（¥/小时，含 5M 带宽，不含地区系数与时长折扣）。
   *
   * 为什么单独配而不从月价反推：按量与包月是两种计价口径 —— 包月含长期
   * 折扣与摊薄的固定成本，按量要为停机/闲置付费。反推会把包月的优惠
   * 错误地摊进按小时单价。
   */
  basePricePerHour: number
  /** 可选规格 */
  specs: ServerSpec[]
  /** 角标标签 i18n key（可为空） */
  badgeKey?: string
}

export interface BandwidthOption {
  /** Mbps */
  mbps: number
  /** 带宽加价（¥/月） */
  pricePerMonth: number
  /** 面向客户的描述，例如「独享 20 Mbps」 */
  descriptionKey: string
}

export interface TermOption {
  id: TermId
  /** i18n key: term display name */
  nameKey: string
  /** 购买月数 */
  months: number
  /** 支付折扣，1 表示不打折 */
  discount: number
  /** 优惠角标 i18n key（可为空） */
  badgeKey?: string
}

/**
 * 可购买的租用时长档。
 *
 * 设计要点：**时长是唯一的时间维度**。
 * 之前做成「计费方式（按小时按量 / 包月预付）+ 购买周期（半年 / 一年 / 三年）」
 * 两层选择器是冗余的 —— 「包月」只是后面那组档位的统称，把它单列成一个
 * 开关，用户要先想口径、再想周期，页面右侧也凭空多出一层。
 * 现在按 AutoDL 的口径合成一份平铺列表：从 1 小时到 3 年，选中即决定
 * 用哪套计价模型。
 */
export interface DurationOption {
  id: string
  /** 计价口径：按量（小时/天）还是包月预付 */
  pricing: BillingMode
  /**
   * 时长量。
   *
   * `pricing='hourly'` 时是**小时数**（3 天档记 72 小时）；
   * `pricing='prepaid'` 时是**月数**。
   */
  amount: number
  /** i18n key: 时长展示名，例如「24 小时」「1 年」 */
  nameKey: string
  /** 时长折扣，1 表示不打折 */
  discount: number
  /** 优惠角标 i18n key（可为空） */
  badgeKey?: string
}

export interface ServerConfig {
  region: RegionId
  plan: PlanId
  specId: string
  mbps: number
  /**
   * 选中的租用时长档 id（唯一的时间维度）。
   *
   * 新建配置一定会有；旧数据（本地已存的订单/实例）没有这个字段，
   * 读出来时由 `resolveDurationId` 按旧的 term/billing/hours 映射到
   * 最接近的档位。所以类型上必须是可选的 —— 同一个 `ServerConfig`
   * 同时描述「新建的配置」和「历史记录」，写成必填会让旧数据无法通过类型检查。
   */
  durationId?: string
  /** @deprecated 旧数据兼容：包月周期 */
  term?: TermId
  /** @deprecated 旧数据兼容：计费方式 */
  billing?: BillingMode
  /** @deprecated 旧数据兼容：小时数 */
  hours?: number
}

export interface PriceBreakdown {
  /** 计费方式 */
  billing: BillingMode
  /** 计费单位：包月=month，按小时=hour */
  unit: 'month' | 'hour'
  /** 数量：包月是月数，按小时是小时数 */
  units: number
  /** 单价（¥/月 或 ¥/小时，已含规格系数、带宽与地区系数） */
  unitPrice: number
  /** 折扣 */
  discount: number
  /** 不打折的总价 */
  listTotal: number
  /** 实付总价 */
  total: number
  /** 优惠金额 */
  saved: number
  /** 折合每月（按小时模式下按 730 小时/月折算，便于与包月价对比） */
  monthlyEquivalent: number
  /**
   * 裸机型基准价（当前口径）：机型基准价 × 1，不含规格系数、不含带宽、不含地区。
   *
   * 这一项必须与左侧套餐卡上标的数字**完全一致**，否则读者无法把
   * 「左边标的价格」和「右边的总价」对上。
   */
  planPrice: number
  /** 规格加价（当前口径）：机型 × 规格系数 − 裸机型基准价；基准规格为 0 */
  specPrice: number
  /** 带宽加价（当前口径）：包月档是月加价，按量档是折算后的小时加价 */
  bandwidthPrice: number
  /** 地区系数（上海 1.08，广州 1） */
  regionFactor: number
}

export interface ServerOrder {
  id: string
  /** 订单号 */
  tradeNo: string
  config: ServerConfig
  /** 应付金额 */
  amount: number
  paymentMethod: string
  status: 'pending' | 'success' | 'failed'
  createdAt: number
  paidAt?: number
  /** 下单账号的 user id（演示态用于多账号隔离） */
  ownerUid?: string
}

export interface OwnedServer {
  id: string
  orderId: string
  config: ServerConfig
  /** 公网 IP */
  ip: string
  /** 登录用户名 */
  username: string
  /** 登录密码 */
  password: string
  status: ServerStatus
  /** 到期时间戳（秒） */
  expireAt: number
  createdAt: number
  /** 是否为演示数据（前端模拟） */
  demo: boolean
  /** 所属账号的 user id（演示态用于多账号隔离） */
  ownerUid?: string
}

export interface PayMethod {
  /** 后端下发的渠道名（可能是中文，前端不翻译） */
  name?: string
  /** 渠道标识，例如 alipay / wechat / balance / demo */
  type: string
  icon?: string
  /** 展示名 i18n key；内置渠道用它替代后端返回的原文 */
  nameKey?: string
  /** 渠道最低金额（后端 min_topup 是字符串，这里已转成数字） */
  min_topup?: number
  /** 是否为演示渠道（不产生真实扣费） */
  demo?: boolean
}
