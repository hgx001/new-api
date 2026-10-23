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
 * 计费方式。
 *
 * - `prepaid`：包月预付（半年 / 一年 / 三年），一次付清，到期前一直可用
 * - `hourly`：按小时按量（预付 N 小时），买的时长用完实例自动释放
 *
 * 为什么要两种：GPU 算力的真实选型场景里，临时跑一次训练/推理用按小时，
 * 长期服务才用包月。只给包月会把「试一下」的用户挡在门外。
 */
export type BillingMode = 'prepaid' | 'hourly'

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
 * 按小时计费时可购买的时长档。
 *
 * 阶梯折扣的理由与 AutoDL 一致：租得越久单价越低（同一台机器长期占用
 * 对平台是更稳定的需求），所以时长越长给一点折扣，而不是全都一个价。
 */
export interface HourlyDuration {
  id: string
  /** 租用小时数 */
  hours: number
  /** i18n key: 时长展示名，例如「24 小时」 */
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
  term: TermId
  /**
   * 计费方式（缺省按 `prepaid` 处理）。
   *
   * 做成可选是为了兼容本地已存的旧订单/实例记录 —— 那是包月时代写下的，
   * 没有这个字段，读出来时补默认值即可。
   */
  billing?: BillingMode
  /** 按小时时的租用时长（小时，缺省取最短档） */
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
