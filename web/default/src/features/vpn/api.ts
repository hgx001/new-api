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

/** 创建 VPN 年费订单并拉起支付 */
export async function createVpnOrder(
  paymentMethod: string
): Promise<{ success: boolean; message?: string; url?: string; params?: unknown }> {
  try {
    const res = await api.post(
      '/api/user/vpn/order',
      { payment_method: paymentMethod },
      { skipBusinessError: true }
    )
    if (res.data?.message === 'success' && res.data?.url) {
      return { success: true, url: res.data.url, params: res.data.data }
    }
    return { success: false, message: res.data?.data || '拉起支付失败' }
  } catch (e) {
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
