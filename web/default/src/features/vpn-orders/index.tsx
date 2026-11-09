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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'

import { getVpnAdminOrders, type VpnOrderStatus } from './api'
import {
  formatVpnMoney,
  formatVpnTime,
  vpnPageRange,
  vpnProviderLabel,
  vpnStatusLabelKey,
} from './lib/format'

const PAGE_SIZE = 10

const STATUS_OPTIONS: Array<VpnOrderStatus | ''> = [
  '',
  'success',
  'pending',
  'failed',
  'expired',
]

function statusClassName(status: VpnOrderStatus): string {
  switch (status) {
    case 'success':
      return 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400'
    case 'pending':
      return 'bg-amber-500/15 text-amber-600 dark:text-amber-400'
    case 'failed':
      return 'bg-rose-500/15 text-rose-600 dark:text-rose-400'
    default:
      return 'bg-muted text-muted-foreground'
  }
}

function StatCard(props: { label: string; value: string; hint?: string }) {
  return (
    <Card>
      <CardContent className='space-y-1 pt-6'>
        <p className='text-muted-foreground text-xs'>{props.label}</p>
        <p className='text-2xl font-semibold tabular-nums'>{props.value}</p>
        {props.hint ? (
          <p className='text-muted-foreground text-xs'>{props.hint}</p>
        ) : null}
      </CardContent>
    </Card>
  )
}

export function VpnOrders() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<VpnOrderStatus | ''>('')
  const [keywordInput, setKeywordInput] = useState('')
  const [keyword, setKeyword] = useState('')

  const query = useQuery({
    queryKey: ['vpn-admin-orders', page, status, keyword],
    queryFn: () =>
      getVpnAdminOrders({ p: page, page_size: PAGE_SIZE, keyword, status }),
  })

  const data = query.data
  const stats = data?.stats
  const total = data?.total ?? 0
  const [from, to] = vpnPageRange(page, PAGE_SIZE, total)
  const hasNext = page * PAGE_SIZE < total

  const runSearch = () => {
    setPage(1)
    setKeyword(keywordInput.trim())
  }

  return (
    <div className='space-y-6 p-6'>
      <div className='space-y-1'>
        <h1 className='text-2xl font-semibold'>{t('VPN Orders')}</h1>
        <p className='text-muted-foreground text-sm'>
          {t(
            'VPN revenue is settled separately from API quota. These orders do not add user quota or create top-up records.'
          )}
        </p>
      </div>

      <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
        <StatCard
          label={t('Total paid')}
          value={formatVpnMoney(stats?.paid_amount ?? '0.00')}
          hint={`${t('Paid orders')}: ${stats?.paid_count ?? 0}`}
        />
        <StatCard
          label={t("Today's paid")}
          value={formatVpnMoney(stats?.today_paid_amount ?? '0.00')}
          hint={t('Today is based on the server timezone')}
        />
        <StatCard
          label={t('Pending orders')}
          value={String(stats?.pending_count ?? 0)}
        />
        <StatCard
          label={t('By payment channel')}
          value={String(stats?.by_provider.length ?? 0)}
          hint={(stats?.by_provider ?? [])
            .map(
              (row) =>
                `${vpnProviderLabel(row.provider)} ${formatVpnMoney(row.paid_amount)}`
            )
            .join(' · ')}
        />
      </div>

      <Card>
        <CardContent className='space-y-4 pt-6'>
          <div className='flex flex-wrap items-center gap-2'>
            <Input
              className='max-w-sm'
              placeholder={t('Search by trade no, user ID or username')}
              value={keywordInput}
              onChange={(event) => setKeywordInput(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') runSearch()
              }}
            />
            <select
              className='border-input bg-background h-9 rounded-md border px-3 text-sm'
              value={status}
              onChange={(event) => {
                setPage(1)
                setStatus(event.target.value as VpnOrderStatus | '')
              }}
              aria-label={t('Status')}
            >
              {STATUS_OPTIONS.map((option) => (
                <option key={option || 'all'} value={option}>
                  {option === ''
                    ? t('All statuses')
                    : t(vpnStatusLabelKey(option))}
                </option>
              ))}
            </select>
            <Button variant='outline' onClick={runSearch}>
              {t('Search')}
            </Button>
            <Button
              variant='ghost'
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              {t('Refresh')}
            </Button>
          </div>

          {query.isLoading ? (
            <p className='text-muted-foreground py-8 text-center text-sm'>
              {t('Loading')}
            </p>
          ) : null}
          {query.isError ? (
            <p className='py-8 text-center text-sm text-rose-500'>
              {t('Failed to load VPN orders')}
            </p>
          ) : null}
          {!query.isLoading &&
          !query.isError &&
          (data?.items.length ?? 0) === 0 ? (
            <p className='text-muted-foreground py-8 text-center text-sm'>
              {t('No VPN orders found')}
            </p>
          ) : null}

          {(data?.items.length ?? 0) > 0 ? (
            <div className='overflow-x-auto'>
              <table className='w-full text-left text-sm'>
                <thead className='text-muted-foreground border-b text-xs'>
                  <tr>
                    <th className='py-2 pr-4 font-medium'>{t('Buyer')}</th>
                    <th className='py-2 pr-4 font-medium'>{t('Trade No')}</th>
                    <th className='py-2 pr-4 font-medium'>{t('Plan')}</th>
                    <th className='py-2 pr-4 font-medium'>{t('Amount')}</th>
                    <th className='py-2 pr-4 font-medium'>
                      {t('Payment Channel')}
                    </th>
                    <th className='py-2 pr-4 font-medium'>{t('Status')}</th>
                    <th className='py-2 pr-4 font-medium'>{t('Created At')}</th>
                    <th className='py-2 pr-4 font-medium'>{t('Paid At')}</th>
                  </tr>
                </thead>
                <tbody>
                  {data?.items.map((order) => (
                    <tr key={order.id} className='border-b last:border-0'>
                      <td className='py-2 pr-4'>
                        <span className='font-medium'>
                          {order.username || `#${order.user_id}`}
                        </span>
                        <span className='text-muted-foreground ml-1 text-xs'>
                          #{order.user_id}
                        </span>
                      </td>
                      <td
                        className='text-muted-foreground max-w-[220px] truncate py-2 pr-4 font-mono text-xs'
                        title={order.trade_no}
                      >
                        {order.trade_no}
                      </td>
                      <td className='py-2 pr-4'>{order.plan}</td>
                      <td className='py-2 pr-4 font-medium tabular-nums'>
                        {formatVpnMoney(order.money)}
                      </td>
                      <td className='py-2 pr-4'>
                        {vpnProviderLabel(order.payment_provider)}
                      </td>
                      <td className='py-2 pr-4'>
                        <span
                          className={`rounded px-2 py-0.5 text-xs ${statusClassName(order.status)}`}
                        >
                          {t(vpnStatusLabelKey(order.status))}
                        </span>
                      </td>
                      <td className='text-muted-foreground py-2 pr-4 text-xs'>
                        {formatVpnTime(order.create_time)}
                      </td>
                      <td className='text-muted-foreground py-2 pr-4 text-xs'>
                        {formatVpnTime(order.complete_time)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}

          <div className='flex items-center justify-between gap-2'>
            <p className='text-muted-foreground text-xs tabular-nums'>
              {from}-{to} / {total}
            </p>
            <div className='flex gap-2'>
              <Button
                variant='outline'
                size='sm'
                disabled={page <= 1}
                onClick={() => setPage((current) => current - 1)}
              >
                {t('Previous page')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                disabled={!hasNext}
                onClick={() => setPage((current) => current + 1)}
              >
                {t('Next page')}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
