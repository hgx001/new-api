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
  /**
   * 数据盘 GB（可选）。
   *
   * 为什么和 disk 分开：真实机器的盘不是一个数 —— 系统盘 30 GB 装系统，
   * 数据盘 50 GB 放数据集，扩容时也是只扩数据盘。合成一个数字会
   * 让「总共 80 GB」看起来像单个盘，和真实交付对不上。
   */
  dataDisk?: number
  /**
   * 数据盘可扩容上限 GB（可选）。
   *
   * 「数据盘 50 GB，可扩容 23 GB」= 当前 50 GB，最多能扩到 73 GB。
   */
  dataDiskMax?: number
  /** CPU 型号，例如 Xeon(R) Gold 6430 */
  cpuModel?: string
  /** 规格价格系数（相对该型号基准档） */
  multiplier: number
  /**
   * 两个口径各自独立的**机型价**，填了就覆盖「基准价 × multiplier」，
   * 不填才回落到系数推导。后端 ServerSpec 有同名字段（snake_case）。
   *
   * 为什么需要显式定价：按量与包月是两套独立的价格体系，不能互推。
   * GPU 三档就是典型 —— 用户指定 4090 ¥5.60 / 5090 ¥7.80 /
   * PRO 6000 ¥19.90 每小时，除以基准时价 ¥2.50 会得到 2.24 / 3.12 /
   * 7.96 这种没人看得懂的系数；同理 PRO 6000 包月 ¥6,580 也对不上。
   *
   * 语义：hourlyPrice 只在按量口径生效，monthlyPrice 只在包月口径生效，
   * 互不污染。multiplier 仍供未显式定价的档位与历史数据展示使用。
   */
  hourlyPrice?: number
  /** 包月口径的机型月价（¥/月），可选 */
  monthlyPrice?: number
  /**
   * 显卡那部分的加价（¥/小时、¥/月），与两个口径一一对应。
   *
   * 为什么要拆出来：GPU 机器值钱就值在那张卡上，但价格只给一个总数时，
   * 用户看到的是「24 vCPU / 96 GB 的主机收了 ¥17.40/小时」—— 主机的加价
   * 比显卡还显眼，贵得没有道理，也没法判断换一张卡要加多少钱。
   * 拆开后摘要里显卡单独一行、占大头，主机只收成本价。
   *
   * 拆的是展示口径，不改变总价：hourlyPrice 仍是该档总机型价，
   * gpuHourlyPrice 是它的子项，配置行拿差值，三行相加恒等于机型价。
   */
  gpuHourlyPrice?: number
  /** 包月口径的显卡加价（¥/月），可选 */
  gpuMonthlyPrice?: number
  /**
   * 选中该规格时自动套用的带宽档（Mbps）。
   *
   * 大卡机器默认给 5 Mbps 没意义：拉一个 30 GB 的模型权重要跑一整天。
   * 所以 Pro 6000 这类旗舰档默认 100 Mbps，用户想省钱再自己往下调。
   */
  defaultMbps?: number
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
  /** 规格加价（当前口径）：机型 × 规格系数 − 裸机型基准价 − 显卡价；基准规格为 0 */
  specPrice: number
  /**
   * 显卡加价（当前口径）：机型价里显卡那部分，没买卡的机型为 0。
   *
   * 套餐 + 配置 + 显卡 恒等于机型价 —— 摘要就是按这个加法展示的，
   * 读者能自己把账加出来。
   */
  gpuPrice: number
  /** 带宽加价（当前口径）：包月档是月加价，按量档是折算后的小时加价 */
  bandwidthPrice: number
  /** 地区系数（上海 1.08，广州 1） */
  regionFactor: number
}

export type ServerStatus =
  | 'pending'
  /** 已付款，等管理员分配 IP 与登录凭据；此阶段不计时 */
  | 'provisioning'
  | 'running'
  | 'stopped'
  | 'expired'
  | 'released'

/** 服务端报价：与前端 computePrice 同口径，但金额以下单时服务端返回为准 */
export interface ServerQuoteResult {
  billing: BillingMode
  unit: 'month' | 'hour'
  units: number
  unit_price: number
  discount: number
  list_total: number
  total: number
  saved: number
  monthly_equivalent: number
  plan_price: number
  spec_price: number
  gpu_price: number
  bandwidth_price: number
  region_factor: number
  period_seconds: number
  region: string
  plan: string
  spec_id: string
  mbps: number
  duration_id: string
}

/**
 * 实例规格快照。
 *
 * 不能再按 specId 去目录里查：目录会改价、会下架旧档，而实例是**按购买时
 * 的规格**交付的。服务端在下单时把规格冻结进实例记录，控制台只读这份快照。
 */
/**
 * 实例上的规格快照。
 *
 * 字段名与后端 JSON 一致（snake_case）—— 实例接口直接透传后端结构，
 * 这里不做命名转换，避免多一层映射出错。
 */
export interface ServerInstanceSpec {
  id: string
  cpu: number
  memory: number
  disk: number
  /** 数据盘 GB */
  data_disk?: number
  /** 数据盘可扩容上限 GB */
  data_disk_max?: number
  /** CPU 型号 */
  cpu_model?: string
  multiplier: number
  /** 包月机型价（¥/月）；后端 snake_case，与实例快照其余字段一致 */
  monthly_price?: number
  /** 显卡那部分的月价（¥/月） */
  gpu_monthly_price?: number
  gpu?: ServerGpu
}

/** 一笔资金流水：购买 / 续费 / 退款 */
export interface ServerOrderRecord {
  id: number
  trade_no: string
  type: 'purchase' | 'renew' | 'refund'
  instance_id: number
  instance_name: string
  plan: string
  spec_id: string
  region: string
  mbps: number
  duration_id: string
  billing: BillingMode
  /** 人民币金额；退款为负数 */
  amount: number
  /** 站内额度变动；退款为正数、扣款为负数 */
  quota_delta: number
  payment_method: string
  /** 支付渠道：balance（站内余额）/ alipay（官方直连）；历史订单可能为空 */
  payment_provider?: string
  status: string
  /** 购买订单的交付进度：awaiting 待分配 / assigned 已分配 */
  provision_status?: 'awaiting' | 'assigned' | ''
  provisioned_at?: number
  description: string
  create_time: number
  complete_time: number
}

/** 收银台上的一个支付方式 */
export interface ServerPayMethod {
  name: string
  type: string
  icon?: string
}

/** 服务端下发的实例视图（控制台唯一数据源） */
export interface ServerInstance {
  id: number
  name: string
  status: ServerStatus
  ip: string
  username: string
  password: string
  password_readable: boolean
  /** SSH 端口：人工开台时可能是非标端口，缺省按 22 处理 */
  ssh_port?: number
  /** 管理员填写的私钥明文；未填写为空串 */
  private_key?: string
  private_key_ready?: boolean
  /** 管理员分配时写给用户的备注 */
  admin_note?: string
  /** 分配时间；未分配为 0 */
  assigned_at?: number
  config: ServerConfig
  spec: ServerInstanceSpec
  region: string
  plan: string
  mbps: number
  billing: BillingMode
  unit_price: number
  paid_amount: number
  refunded_amount: number
  /** simulated = 未接入真实开台，控制台必须如实标注 */
  provision_mode: 'simulated' | 'live'
  expire_at: number
  created_at: number
  released_at: number
  remaining_seconds: number
  order_id: number
}

export interface ServerMetricPoint {
  label: string
  value: number
}

/** 服务端下发的监控数据（与实例规格自洽：无卡不出 GPU、网络不超带宽） */
export interface ServerMetrics {
  cpu: ServerMetricPoint[]
  memory: ServerMetricPoint[]
  network: ServerMetricPoint[]
  disk: number
  gpu: number
  temperature: number
  has_gpu: boolean
  bandwidth_mbps: number
  range: string
  generated_at: number
  status: ServerStatus
}

export interface ServerInstanceEvent {
  id: number
  instance_id: number
  user_id: number
  action: string
  detail: string
  created_at: number
}

/* ---------------------------------------------------------------------------
 * 人工开台（管理端「服务器分配」）
 * ------------------------------------------------------------------------ */

/** 一行待分配/已分配的购买订单：订单 + 用户 + 配置快照 */
export interface ServerProvisionOrder {
  trade_no: string
  user_id: number
  username: string
  email: string
  plan: string
  spec_id: string
  region: string
  mbps: number
  duration_id: string
  billing: BillingMode
  amount: number
  status: string
  provision_status: 'awaiting' | 'assigned'
  /** 支付完成时间 */
  paid_at: number
  provisioned_at: number
  /** 已等待秒数；已分配为 0 */
  waited_seconds: number
  instance_id: number
  instance_name: string
  config: ServerConfig
  spec: ServerInstanceSpec
}

export interface ServerProvisionListResult {
  orders: ServerProvisionOrder[]
  awaiting: number
  oldest_wait_seconds: number
}

export interface ServerProvisionSummary {
  awaiting: number
  oldest_wait_seconds: number
}

/** 分配表单：IP / 端口 / 账号 / 密码或私钥 */
export interface ServerAssignPayload {
  trade_no: string
  ip: string
  ssh_port: number
  username: string
  password: string
  private_key: string
  note: string
}
