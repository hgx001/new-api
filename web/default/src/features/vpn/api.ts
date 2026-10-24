import { api } from '@/lib/api'

export interface VpnPayMethod {
  name: string
  icon?: string
  type: string
}

export interface TopupInfo {
  enable_online_topup?: boolean
  pay_methods?: VpnPayMethod[]
}

export interface VpnOrder {
  id: number
  trade_no: string
  plan: string
  money: number
  payment_method: string
  payment_provider?: string
  status: 'pending' | 'success' | 'failed' | 'expired'
  create_time: number
  complete_time: number
}

export interface VpnKeyData {
  trade_no: string
  plan: string
  pay_time: number
  expire_time?: number
  expired?: boolean
  key?: string
}

export interface VpnKeyResult {
  success: boolean
  message?: string
  data?: VpnKeyData
}

/** 获取支付方式（复用钱包充值信息接口） */
export async function getTopupInfo(): Promise<TopupInfo | null> {
  try {
    const res = await api.get('/api/user/topup/info', {
      skipBusinessError: true,
    })
    return (res.data?.data as TopupInfo) ?? null
  } catch {
    return null
  }
}

/**
 * 支付拉起方式：
 * - `form`：易支付网关，浏览器要 POST 一张表单过去（submitPaymentForm）
 * - `redirect`：支付宝官方直连，整页跳转到支付宝收银台即可
 */
export type VpnPayMode = 'form' | 'redirect'

export interface VpnOrderResult {
  success: boolean
  message?: string
  url?: string
  params?: Record<string, unknown>
  payMode?: VpnPayMode
}

/** 创建 VPN 年费订单并拉起支付 */
export async function createVpnOrder(
  paymentMethod: string
): Promise<VpnOrderResult> {
  try {
    const res = await api.post(
      '/api/user/vpn/order',
      { payment_method: paymentMethod },
      { skipBusinessError: true }
    )
    if (res.data?.message === 'success' && res.data?.url) {
      const payMode: VpnPayMode =
        res.data?.pay_mode === 'redirect' ? 'redirect' : 'form'
      // redirect 模式下 data 里只有 { url, trade_no }，url 在顶层已经给过；
      // form 模式下 data 是网关要提交的表单字段。
      const params =
        payMode === 'redirect'
          ? undefined
          : ((res.data?.data as Record<string, unknown>) ?? {})
      return {
        success: true,
        url: res.data.url,
        params,
        payMode,
      }
    }
    return { success: false, message: res.data?.data || '拉起支付失败' }
  } catch {
    return { success: false, message: '网络错误，请稍后重试' }
  }
}

/** 获取我的 VPN 订单列表 */
export async function getMyVpnOrders(): Promise<VpnOrder[]> {
  try {
    const res = await api.get('/api/user/vpn/orders', {
      skipBusinessError: true,
    })
    return (res.data?.data as VpnOrder[]) ?? []
  } catch {
    return []
  }
}

/** 获取已购买套餐的节点密钥（仅支付成功且未过期时服务端才会下发密钥） */
export async function getMyVpnKey(): Promise<VpnKeyResult | null> {
  try {
    const res = await api.get('/api/user/vpn/key', {
      skipBusinessError: true,
    })
    return {
      success: !!res.data?.success,
      message: res.data?.message as string | undefined,
      data: res.data?.data as VpnKeyData | undefined,
    }
  } catch {
    return null
  }
}
