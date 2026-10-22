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

import { create } from 'zustand'
import { persist } from 'zustand/middleware'

import type { OwnedServer, ServerOrder } from './types'

const STORAGE_KEY = 'heibaidao-servers'

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
        set((state) => ({ orders: [order, ...state.orders] })),

      settleOrder: (orderId, server) =>
        set((state) => ({
          orders: state.orders.map((o) =>
            o.id === orderId
              ? { ...o, status: 'success' as const, paidAt: Date.now() }
              : o
          ),
          servers: [server, ...state.servers],
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
