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

import { Loader2, Receipt } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

import { getDuration, getPlan, getSpec } from '../constants'
import { useServerOrders } from '../hooks/use-servers'
import { formatDateTime } from '../lib/instance-runtime'
import { formatAmount } from '../lib/pricing'
import type { ServerOrderRecord } from '../types'

interface OrderHistoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** 一笔流水的标题：购买 / 续费 / 退款 */
function typeKeyOf(order: ServerOrderRecord): string {
  if (order.type === 'renew') return 'Renewal'
  if (order.type === 'refund') return 'Refund'
  return 'Purchase'
}

/**
 * 配置摘要：用目录把 id 翻成人话。
 *
 * 实例可能已经释放、规格可能已经下架，所以这里只用订单自己记下的
 * 快照字段（plan / spec_id / mbps / duration_id），不去查实例表。
 */
function useConfigSummary(order: ServerOrderRecord) {
  const { t } = useTranslation()
  const plan = getPlan(order.plan)
  const spec = getSpec(plan, order.spec_id)
  const duration = getDuration(order.duration_id)
  return `${t(plan.nameKey)} · ${spec.cpu} vCPU / ${spec.memory} GB · ${order.mbps} Mbps · ${t(duration.nameKey)}`
}

function OrderRow({ order }: { order: ServerOrderRecord }) {
  const { t } = useTranslation()
  const summary = useConfigSummary(order)
  const refund = order.type === 'refund'

  return (
    <li className='flex flex-wrap items-start justify-between gap-3 py-3'>
      <div className='min-w-0'>
        <div className='flex items-center gap-2'>
          <span
            className={cn(
              'rounded-md px-1.5 py-0.5 text-[11px] font-semibold',
              refund
                ? 'bg-emerald-500/15 text-emerald-600'
                : 'bg-emerald-500/12 text-foreground'
            )}
          >
            {t(typeKeyOf(order))}
          </span>
          <span className='truncate text-sm font-medium text-foreground'>
            {order.instance_name || `${t('Instance ID')} #${order.instance_id}`}
          </span>
        </div>
        <div className='text-muted-foreground mt-1 truncate text-xs'>
          {summary}
        </div>
        <div className='text-muted-foreground mt-0.5 text-[11px]'>
          {order.trade_no} · {formatDateTime(order.create_time)}
        </div>
      </div>
      <div className='shrink-0 text-right'>
        <div
          className={cn(
            'text-sm font-semibold tabular-nums',
            refund ? 'text-emerald-600' : 'text-foreground'
          )}
        >
          {refund ? '+' : '−'}
          {formatAmount(Math.abs(order.amount))}
        </div>
        <div className='text-muted-foreground mt-0.5 text-[11px]'>
          {t('Balance')} {order.quota_delta > 0 ? '+' : '−'}
          {Math.abs(order.quota_delta).toLocaleString()}
        </div>
      </div>
    </li>
  )
}

/**
 * 账单流水。
 *
 * 购买、续费、退款都会在这里留一条记录 —— 用户能自己核对「扣了多少、
 * 退了多少」，而不是只相信页面上的一句「支付成功」。金额以订单为准，
 * 不从实例的 paid_amount 反推（续费多次后那个数只是累计值）。
 */
export function OrderHistoryDialog({
  open,
  onOpenChange,
}: OrderHistoryDialogProps) {
  const { t } = useTranslation()
  const ordersQuery = useServerOrders(open)
  const orders = ordersQuery.data?.items ?? []

  // 三种状态各一段 UI，平铺开比嵌套三元好读
  let body: ReactNode
  if (ordersQuery.isLoading) {
    body = (
      <div className='text-muted-foreground flex items-center justify-center gap-2 py-10 text-sm'>
        <Loader2 className='size-4 animate-spin' />
        {t('Loading...')}
      </div>
    )
  } else if (orders.length === 0) {
    body = (
      <div className='text-muted-foreground py-10 text-center text-sm'>
        {t('No billing records yet')}
      </div>
    )
  } else {
    body = (
      <ul className='divide-border/70 max-h-[60vh] divide-y overflow-y-auto'>
        {orders.map((order) => (
          <OrderRow key={order.id} order={order} />
        ))}
      </ul>
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <Receipt className='size-4' />
            {t('Billing history')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Every purchase, renewal and refund is recorded here. Amounts are charged from your account balance.'
            )}
          </DialogDescription>
        </DialogHeader>

        {body}

        <div className='flex justify-end'>
          <Button
            variant='outline'
            className='rounded-lg'
            disabled={ordersQuery.isFetching}
            onClick={() => void ordersQuery.refetch()}
          >
            {ordersQuery.isFetching && (
              <Loader2 className='size-4 animate-spin' />
            )}
            {t('Refresh')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
