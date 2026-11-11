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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'

import {
  exportFinanceCosts,
  getFinanceChannelOptions,
  getFinanceChannels,
  getFinanceCosts,
  getFinanceSummary,
  getFinanceTrend,
  voidFinanceCost,
  type FinanceCost,
  type FinanceTrendPoint,
} from './api'
import { CostDialog } from './components/cost-dialog'
import {
  buildFinanceRange,
  formatCny,
  formatPercent,
  formatTimestamp,
  todayDateInput,
  type FinanceRangePreset,
} from './lib/format'

const PAGE_SIZE = 10

const PRESETS: FinanceRangePreset[] = [
  'this_month',
  'last_month',
  'last_30_days',
  'this_year',
  'custom',
]

const PRESET_LABEL_KEYS: Record<FinanceRangePreset, string> = {
  this_month: 'This month',
  last_month: 'Last month',
  last_30_days: 'Last 30 days',
  this_year: 'This year',
  custom: 'Custom range',
}

const CATEGORY_LABEL_KEYS: Record<string, string> = {
  topup: 'Top-up',
  api_prepay: 'API prepay',
  bandwidth: 'Bandwidth',
  other: 'Other',
  refund: 'Refund',
}

const selectClass =
  'border-input bg-background h-9 rounded-md border px-3 text-sm'

function metricValueClass(tone?: 'positive' | 'negative'): string {
  const base = 'text-2xl font-semibold tabular-nums'
  if (tone === 'positive') {
    return `${base} text-emerald-600 dark:text-emerald-400`
  }
  if (tone === 'negative') {
    return `${base} text-rose-600 dark:text-rose-400`
  }
  return base
}

function MetricCard(props: {
  label: string
  value: string
  hint?: string
  tone?: 'positive' | 'negative'
}) {
  return (
    <Card>
      <CardContent className='space-y-1 pt-6'>
        <p className='text-muted-foreground text-xs'>{props.label}</p>
        <p className={metricValueClass(props.tone)}>{props.value}</p>
        {props.hint ? (
          <p className='text-muted-foreground text-xs'>{props.hint}</p>
        ) : null}
      </CardContent>
    </Card>
  )
}

function TrendBars(props: { points: FinanceTrendPoint[] }) {
  const { t } = useTranslation()
  const max = Math.max(
    1,
    ...props.points.map((point) =>
      Math.max(Number(point.revenue), Number(point.cost), 0)
    )
  )
  if (props.points.length === 0) {
    return (
      <p className='text-muted-foreground py-6 text-center text-sm'>
        {t('No data in this range')}
      </p>
    )
  }
  return (
    <div className='space-y-2'>
      <div className='flex h-32 items-end gap-1.5'>
        {props.points.map((point) => {
          const revenueHeight = Math.max(2, (Number(point.revenue) / max) * 100)
          const costHeight = Math.max(2, (Number(point.cost) / max) * 100)
          return (
            <div
              key={point.period}
              className='flex min-w-0 flex-1 items-end gap-0.5'
              title={`${point.period} ${t('Revenue')}: ${formatCny(point.revenue)} / ${t('Cost')}: ${formatCny(point.cost)}`}
            >
              <span
                className='flex-1 rounded-t-sm bg-sky-500/70'
                style={{ height: `${revenueHeight}%` }}
              />
              <span
                className='flex-1 rounded-t-sm bg-amber-500/70'
                style={{ height: `${costHeight}%` }}
              />
            </div>
          )
        })}
      </div>
      <div className='text-muted-foreground flex flex-wrap items-center justify-between gap-2 text-xs'>
        <span className='flex items-center gap-3'>
          <span className='flex items-center gap-1'>
            <span className='inline-block size-2 rounded-sm bg-sky-500/70' />
            {t('Cash revenue')}
          </span>
          <span className='flex items-center gap-1'>
            <span className='inline-block size-2 rounded-sm bg-amber-500/70' />
            {t('Cost')}
          </span>
        </span>
        <span>
          {props.points[0].period} - {props.points.at(-1)?.period ?? ''}
        </span>
      </div>
    </div>
  )
}

export function FinanceDashboard() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [preset, setPreset] = useState<FinanceRangePreset>('this_month')
  const [customStart, setCustomStart] = useState(todayDateInput())
  const [customEnd, setCustomEnd] = useState(todayDateInput())
  const [granularity, setGranularity] = useState<'day' | 'month'>('day')
  const [page, setPage] = useState(1)
  const [keyword, setKeyword] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<FinanceCost | null>(null)
  const [voiding, setVoiding] = useState<FinanceCost | null>(null)

  const range = useMemo(
    () => buildFinanceRange(preset, customStart, customEnd),
    [preset, customStart, customEnd]
  )
  const rangeKey = `${range.start_timestamp}-${range.end_timestamp}`

  const summaryQuery = useQuery({
    queryKey: ['finance', 'summary', rangeKey],
    queryFn: () => getFinanceSummary(range),
  })
  const channelsQuery = useQuery({
    queryKey: ['finance', 'channels', rangeKey],
    queryFn: () => getFinanceChannels(range),
  })
  const trendQuery = useQuery({
    queryKey: ['finance', 'trend', rangeKey, granularity],
    queryFn: () => getFinanceTrend(range, granularity),
  })
  const costsQuery = useQuery({
    queryKey: ['finance', 'costs', rangeKey, page, keyword],
    queryFn: () =>
      getFinanceCosts({
        p: page,
        page_size: PAGE_SIZE,
        keyword: keyword || undefined,
        start_timestamp: range.start_timestamp,
        end_timestamp: range.end_timestamp,
      }),
  })
  const optionsQuery = useQuery({
    queryKey: ['finance', 'channel-options'],
    queryFn: getFinanceChannelOptions,
    staleTime: 5 * 60 * 1000,
  })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['finance'] })
  }

  const voidMutation = useMutation({
    mutationFn: (id: number) => voidFinanceCost(id),
    onSuccess: () => {
      toast.success(t('Cost voided'))
      setVoiding(null)
      invalidate()
    },
    onError: () => toast.error(t('Failed to void cost')),
  })

  const handleExport = async () => {
    try {
      await exportFinanceCosts(range)
    } catch {
      toast.error(t('Failed to export costs'))
    }
  }

  const summary = summaryQuery.data
  const channels = channelsQuery.data ?? []
  const costs = costsQuery.data
  const total = costs?.total ?? 0

  const openCreate = () => {
    setEditing(null)
    setDialogOpen(true)
  }
  const openEdit = (cost: FinanceCost) => {
    setEditing(cost)
    setDialogOpen(true)
  }

  return (
    <div className='space-y-4'>
      <Card>
        <CardContent className='space-y-3 pt-6'>
          <div className='flex flex-wrap items-start justify-between gap-3'>
            <div className='space-y-1'>
              <h2 className='text-lg font-semibold'>{t('Cost and revenue')}</h2>
              <p className='text-muted-foreground max-w-2xl text-xs leading-relaxed'>
                {t(
                  'Cash revenue is paid top-ups. Channel figures use billed consumption. They are two separate measures.'
                )}
              </p>
            </div>
            <div className='flex flex-wrap gap-2'>
              <Button onClick={openCreate}>{t('Add cost')}</Button>
              <Button variant='outline' onClick={() => void handleExport()}>
                {t('Export CSV')}
              </Button>
              <Button
                variant='ghost'
                onClick={invalidate}
                disabled={summaryQuery.isFetching}
              >
                {t('Refresh')}
              </Button>
            </div>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <select
              className={selectClass}
              value={preset}
              onChange={(event) => {
                setPreset(event.target.value as FinanceRangePreset)
                setPage(1)
              }}
              aria-label={t('Time range')}
            >
              {PRESETS.map((item) => (
                <option key={item} value={item}>
                  {t(PRESET_LABEL_KEYS[item])}
                </option>
              ))}
            </select>
            {preset === 'custom' ? (
              <>
                <Input
                  type='date'
                  className='h-9 w-auto'
                  value={customStart}
                  onChange={(event) => setCustomStart(event.target.value)}
                  aria-label={t('Start date')}
                />
                <Input
                  type='date'
                  className='h-9 w-auto'
                  value={customEnd}
                  onChange={(event) => setCustomEnd(event.target.value)}
                  aria-label={t('End date')}
                />
              </>
            ) : null}
            <select
              className={selectClass}
              value={granularity}
              onChange={(event) =>
                setGranularity(event.target.value as 'day' | 'month')
              }
              aria-label={t('Granularity')}
            >
              <option value='day'>{t('Daily')}</option>
              <option value='month'>{t('Monthly')}</option>
            </select>
            <span className='text-muted-foreground text-xs'>
              {t('Dates follow the server local timezone')}
            </span>
          </div>
        </CardContent>
      </Card>

      {summaryQuery.isError ? (
        <p className='text-sm text-rose-500'>
          {t('Failed to load finance data')}
        </p>
      ) : null}

      <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5'>
        <MetricCard
          label={t('Paid revenue')}
          value={formatCny(summary?.revenue)}
          hint={`${t('Paid top-ups')}: ${summary?.revenue_count ?? 0}`}
        />
        <MetricCard
          label={t('Recorded cost')}
          value={formatCny(summary?.cost)}
          hint={`${t('Cost entries')}: ${summary?.cost_count ?? 0}`}
        />
        <MetricCard
          label={t('Net cash')}
          value={formatCny(summary?.net)}
          hint={formatPercent(summary?.margin_rate)}
          tone={Number(summary?.net) >= 0 ? 'positive' : 'negative'}
        />
        <MetricCard
          label={t('Billed consumption')}
          value={formatCny(summary?.billed)}
          hint={`${t('Requests')}: ${summary?.requests ?? 0}`}
        />
        <MetricCard
          label={t('Cash vs billed difference')}
          value={formatCny(summary?.diff)}
          hint={t(
            'Unused prepayments, granted quota, discounts and refunds create this gap.'
          )}
        />
      </div>

      <div className='grid gap-4 lg:grid-cols-2'>
        <Card>
          <CardContent className='space-y-3 pt-6'>
            <h3 className='text-sm font-semibold'>
              {t('Revenue vs cost trend')}
            </h3>
            <TrendBars points={trendQuery.data ?? []} />
          </CardContent>
        </Card>
        <Card>
          <CardContent className='space-y-3 pt-6'>
            <h3 className='text-sm font-semibold'>{t('Channel breakdown')}</h3>
            <div className='max-h-64 overflow-auto'>
              <table className='w-full text-left text-sm'>
                <thead className='text-muted-foreground border-b text-xs'>
                  <tr>
                    <th className='py-2 pr-3 font-medium'>{t('Channel')}</th>
                    <th className='py-2 pr-3 text-right font-medium'>
                      {t('Billed amount')}
                    </th>
                    <th className='py-2 pr-3 text-right font-medium'>
                      {t('Cost')}
                    </th>
                    <th className='py-2 pr-3 text-right font-medium'>
                      {t('Gross profit')}
                    </th>
                    <th className='py-2 pr-3 text-right font-medium'>
                      {t('Margin')}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {channels.map((row) => (
                    <tr key={row.channel_id} className='border-b last:border-0'>
                      <td className='py-2 pr-3'>
                        {row.channel_id === 0
                          ? t('Unallocated')
                          : row.channel_name}
                      </td>
                      <td className='py-2 pr-3 text-right tabular-nums'>
                        {formatCny(row.billed)}
                      </td>
                      <td className='py-2 pr-3 text-right tabular-nums'>
                        {formatCny(row.cost)}
                      </td>
                      <td
                        className={
                          Number(row.gross_profit) >= 0
                            ? 'py-2 pr-3 text-right text-emerald-600 tabular-nums dark:text-emerald-400'
                            : 'py-2 pr-3 text-right text-rose-600 tabular-nums dark:text-rose-400'
                        }
                      >
                        {formatCny(row.gross_profit)}
                      </td>
                      <td className='py-2 pr-3 text-right tabular-nums'>
                        {formatPercent(row.margin_rate)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {channels.length === 0 ? (
                <p className='text-muted-foreground py-6 text-center text-sm'>
                  {t('No data in this range')}
                </p>
              ) : null}
            </div>
            <p className='text-muted-foreground text-xs'>
              {t('Unallocated public cost is excluded from channel margin.')} (
              {formatCny(summary?.unallocated_cost)})
            </p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardContent className='space-y-4 pt-6'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div>
              <h3 className='text-sm font-semibold'>{t('Cost entries')}</h3>
              <p className='text-muted-foreground text-xs'>
                {t('Filtered total')}: {formatCny(costs?.total_amount)}
              </p>
            </div>
            <div className='flex gap-2'>
              <Input
                className='h-9 max-w-56'
                placeholder={t('Search reference or note')}
                value={keyword}
                onChange={(event) => {
                  setKeyword(event.target.value)
                  setPage(1)
                }}
              />
            </div>
          </div>

          <div className='overflow-x-auto'>
            <table className='w-full text-left text-sm'>
              <thead className='text-muted-foreground border-b text-xs'>
                <tr>
                  <th className='py-2 pr-3 font-medium'>{t('Date')}</th>
                  <th className='py-2 pr-3 font-medium'>{t('Channel')}</th>
                  <th className='py-2 pr-3 text-right font-medium'>
                    {t('Amount')}
                  </th>
                  <th className='py-2 pr-3 font-medium'>{t('Category')}</th>
                  <th className='py-2 pr-3 font-medium'>
                    {t('Reference no.')}
                  </th>
                  <th className='py-2 pr-3 font-medium'>{t('Note')}</th>
                  <th className='py-2 pr-3 text-right font-medium'>
                    {t('Actions')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {(costs?.items ?? []).map((cost) => (
                  <tr key={cost.id} className='border-b last:border-0'>
                    <td className='text-muted-foreground py-2 pr-3 text-xs'>
                      {formatTimestamp(cost.occurred_at)}
                    </td>
                    <td className='py-2 pr-3'>
                      {cost.channel_id === 0
                        ? t('Unallocated')
                        : cost.channel_name}
                    </td>
                    <td className='py-2 pr-3 text-right font-medium tabular-nums'>
                      {formatCny(cost.amount)}
                    </td>
                    <td className='py-2 pr-3 text-xs'>
                      {t(CATEGORY_LABEL_KEYS[cost.category] ?? 'Other')}
                    </td>
                    <td className='text-muted-foreground max-w-32 truncate py-2 pr-3 font-mono text-xs'>
                      {cost.reference_no || '-'}
                    </td>
                    <td className='text-muted-foreground max-w-48 truncate py-2 pr-3 text-xs'>
                      {cost.note || '-'}
                    </td>
                    <td className='py-2 pr-3 text-right'>
                      <div className='flex justify-end gap-1'>
                        <Button
                          variant='ghost'
                          size='sm'
                          onClick={() => openEdit(cost)}
                        >
                          {t('Edit')}
                        </Button>
                        <Button
                          variant='ghost'
                          size='sm'
                          onClick={() => setVoiding(cost)}
                        >
                          {t('Void')}
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {(costs?.items.length ?? 0) === 0 ? (
            <p className='text-muted-foreground py-6 text-center text-sm'>
              {t('No cost entries yet')}
            </p>
          ) : null}

          <div className='flex items-center justify-end gap-2'>
            <span className='text-muted-foreground text-xs tabular-nums'>
              {page} / {Math.max(1, Math.ceil(total / PAGE_SIZE))}
            </span>
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
              disabled={page * PAGE_SIZE >= total}
              onClick={() => setPage((current) => current + 1)}
            >
              {t('Next page')}
            </Button>
          </div>
        </CardContent>
      </Card>

      <CostDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        cost={editing}
        channels={optionsQuery.data ?? []}
        onSaved={invalidate}
      />

      <ConfirmDialog
        open={Boolean(voiding)}
        onOpenChange={(open) => {
          if (!open) setVoiding(null)
        }}
        title={t('Void cost?')}
        desc={t('Voided entries no longer count toward any total.')}
        confirmText={t('Void')}
        destructive
        isLoading={voidMutation.isPending}
        handleConfirm={() => {
          if (voiding) voidMutation.mutate(voiding.id)
        }}
      />
    </div>
  )
}
