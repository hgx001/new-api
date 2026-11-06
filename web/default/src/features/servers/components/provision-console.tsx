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

import { useNavigate } from '@tanstack/react-router'
import {
  CheckCircle2,
  ClipboardList,
  Clock3,
  Loader2,
  RefreshCcw,
  ServerCog,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import { formatDiskBreakdown, formatGpu } from '../constants'
import { useServerProvisionOrders } from '../hooks/use-servers'
import { formatDateTime } from '../lib/instance-runtime'
import { formatAmount } from '../lib/pricing'
import type { ServerProvisionOrder } from '../types'
import { AssignDialog } from './assign-dialog'

type ProvisionTab = 'awaiting' | 'assigned'

/** 等待时长的颜色：10 分钟开始提醒，30 分钟算超时。 */
function waitTone(seconds: number): string {
  if (seconds >= 1800) return 'text-red-600 dark:text-red-400'
  if (seconds >= 600) return 'text-amber-600 dark:text-amber-400'
  return 'text-muted-foreground'
}

function waitLabel(
  seconds: number,
  t: (key: string, options?: Record<string, unknown>) => string
): string {
  if (seconds < 60) return t('Just paid')
  if (seconds < 3600) {
    return t('{{count}} min waiting', { count: Math.floor(seconds / 60) })
  }
  return t('{{count}} h waiting', { count: Math.floor(seconds / 3600) })
}

/**
 * 服务器分配（管理端）。
 *
 * 用户付完款到机器可用之间有一个真实的人工环节：选机器、装系统、配网络、
 * 记录凭据。这个页面就是那条流水线的工作台 —— 队列在最上面，点开填凭据，
 * 点「分配」才真正交付给用户。
 *
 * 三个设计取舍：
 *  1. **先付的先办**：待办按支付时间升序，不会因为订单金额大就插队；
 *  2. **等待时长可视化**：超过 10 分钟转黄、30 分钟转红，积压自己会说话；
 *  3. **分配即计时**：到期时间从分配那一刻起算，用户排队的时间不计费。
 */
export function ProvisionConsole() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const query = useServerProvisionOrders(true)
  const [tab, setTab] = useState<ProvisionTab>('awaiting')
  const [assignTarget, setAssignTarget] = useState<ServerProvisionOrder | null>(
    null
  )

  const orders = query.data?.orders ?? []
  const awaiting = orders.filter(
    (order) => order.provision_status === 'awaiting'
  )
  const assigned = orders.filter(
    (order) => order.provision_status === 'assigned'
  )
  const list = tab === 'awaiting' ? awaiting : assigned
  const oldest = query.data?.oldest_wait_seconds ?? 0
  const waitingTab = tab === 'awaiting'

  const stats = useMemo(
    () => [
      {
        icon: ClipboardList,
        label: t('Waiting for assignment'),
        value: String(awaiting.length),
        tone: 'text-blue-600 dark:text-blue-400',
      },
      {
        icon: CheckCircle2,
        label: t('Delivered'),
        value: String(assigned.length),
        tone: 'text-emerald-600 dark:text-emerald-400',
      },
      {
        icon: Clock3,
        label: t('Longest wait'),
        value: oldest > 0 ? waitLabel(oldest, t) : '—',
        tone: waitTone(oldest),
      },
    ],
    [assigned.length, awaiting.length, oldest, t]
  )

  // 列表主体先算出来：三层嵌套三元既过不了 lint，也很难读。
  const listBody =
    list.length === 0 ? (
      <div className='bg-card/60 rounded-2xl border border-dashed px-6 py-16 text-center'>
        <ClipboardList className='text-muted-foreground mx-auto size-6' />
        <div className='text-foreground mt-2 text-sm font-medium'>
          {waitingTab
            ? t('No orders waiting for assignment')
            : t('Nothing delivered yet')}
        </div>
        <div className='text-muted-foreground mt-1 text-xs'>
          {waitingTab
            ? t('New paid orders will show up here automatically.')
            : t('Delivered machines are listed here for your records.')}
        </div>
      </div>
    ) : (
      <div className='border-border/70 bg-card overflow-hidden rounded-2xl border'>
        <div className='text-muted-foreground border-border/70 hidden grid-cols-[minmax(0,1.5fr)_minmax(0,1.1fr)_minmax(0,0.7fr)_minmax(0,1fr)_minmax(0,0.9fr)_auto] gap-3 border-b px-4 py-2.5 text-[11px] font-medium xl:grid'>
          <span>{t('Order and user')}</span>
          <span>{t('Configuration')}</span>
          <span>{t('Amount paid')}</span>
          <span>{t('Payment time')}</span>
          <span>{t('Status')}</span>
          <span className='text-right'>{t('Actions')}</span>
        </div>
        {list.map((order) => (
          <ProvisionRow
            key={order.trade_no}
            order={order}
            onAssign={() => setAssignTarget(order)}
            onOpenInstances={() => navigate({ to: '/console/servers' })}
          />
        ))}
      </div>
    )

  return (
    <div className='flex flex-col'>
      <div className='border-border/70 bg-card border-b px-6 py-5'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div>
            <h1 className='text-foreground flex items-center gap-2 text-xl font-bold'>
              <ServerCog className='size-5' />
              {t('Server assignment')}
            </h1>
            <p className='text-muted-foreground mt-1 text-sm'>
              {t(
                'Paid orders wait here until you hand over credentials. The rental clock starts at assignment.'
              )}
            </p>
          </div>
          <Button
            variant='outline'
            size='sm'
            onClick={() => query.refetch()}
            disabled={query.isFetching}
          >
            <RefreshCcw
              className={cn(
                'mr-1.5 size-3.5',
                query.isFetching && 'animate-spin'
              )}
            />
            {t('Refresh')}
          </Button>
        </div>
      </div>

      <div className='space-y-4 p-6'>
        <div className='grid gap-3 sm:grid-cols-3'>
          {stats.map((stat) => (
            <div
              key={stat.label}
              className='border-border/70 bg-card flex items-center gap-3 rounded-xl border px-4 py-3'
            >
              <span className={cn('shrink-0', stat.tone)}>
                <stat.icon className='size-4' />
              </span>
              <div className='min-w-0'>
                <div className='text-muted-foreground truncate text-[11px]'>
                  {stat.label}
                </div>
                <div className={cn('truncate text-base font-bold', stat.tone)}>
                  {stat.value}
                </div>
              </div>
            </div>
          ))}
        </div>

        <div className='border-border/70 bg-card flex items-center gap-2 rounded-xl border px-3 py-2.5'>
          <TabButton
            active={waitingTab}
            onClick={() => setTab('awaiting')}
            label={t('Waiting for assignment')}
            count={awaiting.length}
          />
          <TabButton
            active={!waitingTab}
            onClick={() => setTab('assigned')}
            label={t('Delivered')}
            count={assigned.length}
          />
        </div>

        {query.isLoading ? (
          <div className='text-muted-foreground flex items-center justify-center gap-2 py-16 text-sm'>
            <Loader2 className='size-4 animate-spin' />
            {t('Loading')}
          </div>
        ) : (
          listBody
        )}
      </div>

      {assignTarget && (
        <AssignDialog
          order={assignTarget}
          onClose={() => setAssignTarget(null)}
          onDone={() => query.refetch()}
        />
      )}
    </div>
  )
}

function TabButton({
  active,
  onClick,
  label,
  count,
}: {
  active: boolean
  onClick: () => void
  label: string
  count: number
}) {
  return (
    <button
      type='button'
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors',
        active
          ? 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-400'
          : 'text-muted-foreground hover:bg-muted hover:text-foreground'
      )}
    >
      {label}
      <span
        className={cn(
          'rounded-full px-1.5 text-[10px]',
          active ? 'bg-emerald-500/20' : 'bg-muted'
        )}
      >
        {count}
      </span>
    </button>
  )
}

function ProvisionRow({
  order,
  onAssign,
  onOpenInstances,
}: {
  order: ServerProvisionOrder
  onAssign: () => void
  onOpenInstances: () => void
}) {
  const { t } = useTranslation()
  const spec = order.spec
  const waiting = order.provision_status === 'awaiting'

  return (
    <div className='border-border/70 grid gap-3 border-t px-4 py-3.5 first:border-t-0 sm:grid-cols-2 xl:grid-cols-[minmax(0,1.5fr)_minmax(0,1.1fr)_minmax(0,0.7fr)_minmax(0,1fr)_minmax(0,0.9fr)_auto] xl:items-center'>
      <div className='min-w-0'>
        <div className='text-foreground truncate text-[13px] font-semibold'>
          {order.username || `#${order.user_id}`}
        </div>
        <div className='text-muted-foreground mt-0.5 truncate font-mono text-[11px]'>
          {order.trade_no}
        </div>
      </div>

      <div className='min-w-0'>
        <div className='text-foreground truncate text-[12px]'>
          {spec.id ? `${spec.cpu} vCPU · ${spec.memory} GB` : order.spec_id}
        </div>
        <div className='text-muted-foreground mt-0.5 truncate text-[11px]'>
          {spec.gpu ? formatGpu(spec.gpu) : formatDiskBreakdown(spec, t)}
        </div>
      </div>

      <div className='text-foreground text-[13px] font-semibold'>
        {formatAmount(order.amount)}
      </div>

      <div className='min-w-0'>
        <div className='text-foreground text-[12px]'>
          {formatDateTime(order.paid_at)}
        </div>
        {waiting && (
          <div
            className={cn(
              'mt-0.5 text-[11px] font-medium',
              waitTone(order.waited_seconds)
            )}
          >
            {waitLabel(order.waited_seconds, t)}
          </div>
        )}
      </div>

      <div className='text-[12px]'>
        {waiting ? (
          <span className='inline-flex items-center gap-1.5 rounded-md bg-blue-500/10 px-2 py-1 text-[11px] font-medium text-blue-700 dark:text-blue-400'>
            <Clock3 className='size-3' />
            {t('Waiting for assignment')}
          </span>
        ) : (
          <span className='inline-flex items-center gap-1.5 rounded-md bg-emerald-500/12 px-2 py-1 text-[11px] font-medium text-emerald-700 dark:text-emerald-400'>
            <CheckCircle2 className='size-3' />
            {t('Delivered')}
          </span>
        )}
        {!waiting && order.provisioned_at > 0 && (
          <div className='text-muted-foreground mt-1 text-[11px]'>
            {formatDateTime(order.provisioned_at)}
          </div>
        )}
      </div>

      <div className='flex justify-end gap-2'>
        {waiting ? (
          <Button size='sm' className='h-7 px-3 text-[11px]' onClick={onAssign}>
            {t('Assign')}
          </Button>
        ) : (
          <Button
            size='sm'
            variant='outline'
            className='h-7 px-3 text-[11px]'
            onClick={onOpenInstances}
          >
            {t('View instance')}
          </Button>
        )}
      </div>
    </div>
  )
}
