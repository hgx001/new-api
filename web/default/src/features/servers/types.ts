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

export interface ServerConfig {
  region: RegionId
  plan: PlanId
  specId: string
  mbps: number
  term: TermId
}

export interface PriceBreakdown {
  /** 型号 + 规格的月价 */
  baseMonthly: number
  /** 带宽加价月值 */
  bandwidthMonthly: number
  /** 地区系数之后的月价合计 */
  monthly: number
  /** 购买月数 */
  months: number
  /** 周期折扣 */
  discount: number
  /** 不打折的总价 */
  listTotal: number
  /** 实付总价 */
  total: number
  /** 优惠金额 */
  saved: number
  /** 折合每月 */
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
