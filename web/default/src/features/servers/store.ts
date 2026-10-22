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

import { useMemo } from 'react'
import { create } from 'zustand'
import { persist } from 'zustand/middleware'

import { useAuthStore } from '@/stores/auth-store'

import type { OwnedServer, ServerOrder } from './types'

const STORAGE_KEY = 'heibaidao-servers'

/**
 * 当前登录账号的标识。
 *
 * 演示态的实例凭据保存在浏览器本地，如果不按账号隔离，同一台电脑上换账号登录
 * 就能看到上一个账号的服务器 IP 与 root 密码，因此每条记录都带上 ownerUid，
 * 读取时只取当前账号自己的数据。
 */
export function currentOwnerId(): string {
  const user = useAuthStore.getState().auth.user
  return user ? String(user.id) : ''
}

interface ServerState {
  orders: ServerOrder[]
  servers: OwnedServer[]
  addOrder: (order: ServerOrder) => void
  settleOrder: (orderId: string, server: OwnedServer) => void
  failOrder: (orderId: string) => void
  updatePassword: (serverId: string, password: string) => void
  removeServer: (serverId: string) => void
  reset: () => void
}

/**
 * 服务器订单与已购实例的本地状态。
 *
 * 当前为**前端演示态**（后端接口未接入），状态持久化在 localStorage，
 * 刷新页面后已购买的服务器与密码仍然可见，方便演示完整闭环。
 * 后端接入后改为以服务端返回为准，这里的本地状态仅作乐观更新。
 */
export const useServerStore = create<ServerState>()(
  persist(
    (set) => ({
      orders: [],
      servers: [],

      addOrder: (order) =>
        set((state) => ({
          orders: [
            { ...order, ownerUid: order.ownerUid ?? currentOwnerId() },
            ...state.orders,
          ],
        })),

      settleOrder: (orderId, server) =>
        set((state) => ({
          orders: state.orders.map((o) =>
            o.id === orderId
              ? { ...o, status: 'success' as const, paidAt: Date.now() }
              : o
          ),
          // 同一订单只结算一次：支付回调重放或「轮询 + 回调」并发时，
          // 不允许同一笔订单生成多台实例（mock 每次都会生成新的随机密码）。
          servers: state.servers.some((s) => s.orderId === orderId)
            ? state.servers
            : [
                { ...server, ownerUid: server.ownerUid ?? currentOwnerId() },
                ...state.servers,
              ],
        })),

      failOrder: (orderId) =>
        set((state) => ({
          orders: state.orders.map((o) =>
            o.id === orderId ? { ...o, status: 'failed' as const } : o
          ),
        })),

      updatePassword: (serverId, password) =>
        set((state) => ({
          servers: state.servers.map((s) =>
            s.id === serverId ? { ...s, password } : s
          ),
        })),

      removeServer: (serverId) =>
        set((state) => ({
          servers: state.servers.filter((s) => s.id !== serverId),
        })),

      reset: () => set({ orders: [], servers: [] }),
    }),
    {
      name: STORAGE_KEY,
      partialize: (state) => ({
        orders: state.orders,
        servers: state.servers,
      }),
    }
  )
)

/**
 * 当前登录账号自己的服务器实例。
 *
 * 同一台电脑上切换账号时，只返回当前账号的实例，避免看到上一个账号的凭据。
 */
export function useOwnedServers(): OwnedServer[] {
  const servers = useServerStore((s) => s.servers)
  const userId = useAuthStore((s) => s.auth.user?.id)
  const ownerId = userId === undefined ? '' : String(userId)

  return useMemo(
    () => servers.filter((s) => (s.ownerUid ?? '') === ownerId),
    [servers, ownerId]
  )
}

/**
 * 清空本地保存的服务器数据（含 localStorage 持久化内容）。
 *
 * 登出时必须调用：实例凭据（公网 IP / root 密码）保存在浏览器本地，
 * 不清理的话，同一台电脑上的下一个登录用户可以直接看到。
 */
export function clearServerData() {
  useServerStore.getState().reset()
  useServerStore.persist.clearStorage()
}
