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

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'

import { useAuthStore } from '@/stores/auth-store'

import {
  getServerEvents,
  getServerMetrics,
  getServerOrderStatus,
  listServerInstances,
  listServerOrders,
  listServerPayMethods,
  purchaseServer,
  rebootServer,
  releaseServer,
  renewServer,
  requestServerEpayPay,
  resetServerPassword,
  startServer,
  stopServer,
  type PurchaseResult,
  type RenewResult,
  type ServerWalletSnapshot,
} from '../api'
import type { ServerConfig } from '../types'

export const serverKeys = {
  instances: ['server-instances'] as const,
  metrics: (id: number) => ['server-metrics', id] as const,
  events: (id: number) => ['server-events', id] as const,
  orders: ['server-orders'] as const,
  payMethods: ['server-pay-methods'] as const,
  orderStatus: (tradeNo: string) => ['server-order-status', tradeNo] as const,
}

/**
 * 当前账号的实例列表。
 *
 * 60 秒轮询一次：到期时间、开关机状态都是服务端事实，用户可能在另一台
 * 设备上操作过；纯靠本地状态会让两台设备各说各话。
 */
export function useServerInstances(enabled = true) {
  return useQuery({
    queryKey: serverKeys.instances,
    queryFn: () => listServerInstances(false),
    enabled,
    staleTime: 15_000,
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  })
}

export function useServerOrders(enabled = true) {
  return useQuery({
    queryKey: serverKeys.orders,
    queryFn: listServerOrders,
    enabled,
    staleTime: 30_000,
  })
}

export function useServerMetrics(instanceId: number, enabled = true) {
  return useQuery({
    queryKey: serverKeys.metrics(instanceId),
    queryFn: () => getServerMetrics(instanceId),
    enabled: enabled && instanceId > 0,
    refetchInterval: 60_000,
  })
}

export function useServerEvents(instanceId: number, enabled = true) {
  return useQuery({
    queryKey: serverKeys.events(instanceId),
    queryFn: () => getServerEvents(instanceId),
    enabled: enabled && instanceId > 0,
    staleTime: 15_000,
  })
}

/**
 * 可用的支付方式（站内余额 + 服务端下发的第三方渠道）。
 *
 * 渠道列表由服务端决定：网关凭据没配好、合规没确认的渠道不会下发，
 * 免得用户点下去才发现付不了。
 */
export function useServerPayMethods(enabled = true) {
  return useQuery({
    queryKey: serverKeys.payMethods,
    queryFn: listServerPayMethods,
    enabled,
    staleTime: 60_000,
  })
}

/**
 * 轮询订单支付状态。
 *
 * 第三方支付是异步的：用户跳去收银台付款，回来后机器还没开 —— 要等
 * 网关异步回调。前端靠轮询这个接口确认「钱到了、机器开了」。
 */
export function useServerOrderStatus(
  tradeNo: string | null,
  enabled = true
) {
  return useQuery({
    queryKey: serverKeys.orderStatus(tradeNo ?? ''),
    queryFn: () => getServerOrderStatus(tradeNo as string),
    enabled: enabled && !!tradeNo,
    staleTime: 2_000,
    refetchInterval: (query) => {
      // 已支付就停掉轮询：实例开通后不需要再问了。
      const paid = (query.state.data as { paid?: boolean } | undefined)?.paid
      return paid ? false : 3_000
    },
  })
}

/**
 * 把服务端返回的最新余额同步到登录态。
 *
 * 购买/续费/退款都会改余额；不刷新的话，页面顶部的余额与钱包页会对不上，
 * 用户会以为扣错了钱。
 */
function useWalletSync() {
  const setUser = useAuthStore((state) => state.auth.setUser)
  const user = useAuthStore((state) => state.auth.user)

  return useCallback(
    (snapshot?: ServerWalletSnapshot) => {
      if (!snapshot || !user) return
      setUser({ ...user, quota: snapshot.quota })
    },
    [setUser, user]
  )
}

/** 下单 / 续费 / 释放 / 开关机等所有写操作，统一在这里做缓存失效与余额同步 */
export function useServerActions() {
  const queryClient = useQueryClient()
  const syncWallet = useWalletSync()

  const invalidate = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: serverKeys.instances })
    void queryClient.invalidateQueries({ queryKey: serverKeys.orders })
  }, [queryClient])

  const purchase = useMutation({
    mutationFn: (input: {
      config: ServerConfig
      requestId: string
      paymentMethod?: string
    }) =>
      purchaseServer(
        input.config,
        input.requestId,
        input.paymentMethod ?? 'balance'
      ),
    onSuccess: (result: PurchaseResult) => {
      syncWallet(result)
      invalidate()
    },
  })

  /**
   * 拉起第三方收银台。
   *
   * 与余额支付分开成两个 mutation：它们的成功语义完全不同 ——
   * 余额支付返回实例，第三方支付返回的是「跳去哪儿付钱」。
   */
  const launchEpay = useMutation({
    mutationFn: (input: { tradeNo: string; paymentMethod: string }) =>
      requestServerEpayPay(input.tradeNo, input.paymentMethod),
  })

  const renew = useMutation({
    mutationFn: (input: {
      instanceId: number
      durationId: string
      requestId: string
    }) => renewServer(input.instanceId, input.durationId, input.requestId),
    onSuccess: (result: RenewResult) => {
      syncWallet(result)
      invalidate()
      void queryClient.invalidateQueries({
        queryKey: serverKeys.metrics(result.instance.id),
      })
    },
  })

  const release = useMutation({
    mutationFn: (instanceId: number) => releaseServer(instanceId),
    onSuccess: (result) => {
      syncWallet(result)
      invalidate()
    },
  })

  const power = useMutation({
    mutationFn: (input: { instanceId: number; start: boolean }) =>
      input.start ? startServer(input.instanceId) : stopServer(input.instanceId),
    onSuccess: (_, input) => {
      invalidate()
      // 开关机会写一条操作记录，不刷新的话详情里的日志少一条
      void queryClient.invalidateQueries({
        queryKey: serverKeys.events(input.instanceId),
      })
    },
  })

  const reboot = useMutation({
    mutationFn: (instanceId: number) => rebootServer(instanceId),
    onSuccess: (_, instanceId) => {
      invalidate()
      void queryClient.invalidateQueries({
        queryKey: serverKeys.events(instanceId),
      })
    },
  })

  const resetPassword = useMutation({
    mutationFn: (input: { instanceId: number; password: string }) =>
      resetServerPassword(input.instanceId, input.password),
    onSuccess: (_, input) => {
      void queryClient.invalidateQueries({ queryKey: serverKeys.instances })
      void queryClient.invalidateQueries({
        queryKey: serverKeys.events(input.instanceId),
      })
    },
  })

  return { purchase, launchEpay, renew, release, power, reboot, resetPassword }
}
