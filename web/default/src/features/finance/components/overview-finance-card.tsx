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
import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'

import { getFinanceSummary } from '../api'
import { buildFinanceRange, formatCny, formatPercent } from '../lib/format'

/** 概览只放一张轻量卡，完整看板在 /dashboard/finance；仅管理员渲染。 */
export function OverviewFinanceCard() {
  const { t } = useTranslation()
  const range = useMemo(() => buildFinanceRange('this_month', '', ''), [])
  const query = useQuery({
    queryKey: ['finance', 'overview', range.start_timestamp],
    queryFn: () => getFinanceSummary(range),
    staleTime: 5 * 60 * 1000,
  })
  const summary = query.data

  return (
    <Card>
      <CardContent className='space-y-3 pt-6'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div>
            <h3 className='text-sm font-semibold'>{t('Business overview')}</h3>
            <p className='text-muted-foreground text-xs'>
              {t('This month')} · {t('Admins only')}
            </p>
          </div>
          <Button
            variant='outline'
            size='sm'
            render={
              <Link to='/dashboard/$section' params={{ section: 'finance' }} />
            }
          >
            {t('View finance dashboard')}
            <ArrowRight data-icon='inline-end' />
          </Button>
        </div>
        <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
          <div>
            <p className='text-muted-foreground text-xs'>{t('Paid revenue')}</p>
            <p className='font-mono text-lg font-semibold tabular-nums'>
              {formatCny(summary?.revenue)}
            </p>
          </div>
          <div>
            <p className='text-muted-foreground text-xs'>
              {t('Recorded cost')}
            </p>
            <p className='font-mono text-lg font-semibold tabular-nums'>
              {formatCny(summary?.cost)}
            </p>
          </div>
          <div>
            <p className='text-muted-foreground text-xs'>{t('Net cash')}</p>
            <p className='font-mono text-lg font-semibold tabular-nums'>
              {formatCny(summary?.net)}
            </p>
          </div>
          <div>
            <p className='text-muted-foreground text-xs'>{t('Margin rate')}</p>
            <p className='font-mono text-lg font-semibold tabular-nums'>
              {formatPercent(summary?.margin_rate)}
            </p>
          </div>
        </div>
        <p className='text-muted-foreground text-xs'>
          {t('Billed consumption')}: {formatCny(summary?.billed)} ·{' '}
          {t('Cash revenue and channel consumption are separate measures.')}
        </p>
      </CardContent>
    </Card>
  )
}
