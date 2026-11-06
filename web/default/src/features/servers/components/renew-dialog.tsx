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

import { Loader2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

import { quoteServer } from '../api'
import { DURATIONS } from '../constants'
import { formatAmount } from '../lib/pricing'
import type { ServerInstance, ServerQuoteResult } from '../types'

interface RenewDialogProps {
  instance: ServerInstance
  onClose: () => void
  onConfirm: (durationId: string) => Promise<void>
}

/**
 * 续费弹窗。
 *
 * 只列出与原实例**同一计费口径**的时长：按量实例不能续一年，
 * 包月实例也不能按小时续 —— 两种口径的单价含义不同，混用会差一个数量级。
 * 价格以服务端报价为准；报价失败（历史规格已下架）时用实例记录的单价估算，
 * 实际扣款仍由服务端决定。
 */
export function RenewDialog(props: RenewDialogProps) {
  const { t } = useTranslation()
  const durations = useMemo(
    () => DURATIONS.filter((item) => item.pricing === props.instance.billing),
    [props.instance.billing]
  )
  const [durationId, setDurationId] = useState(
    durations.find((item) => item.id === props.instance.config.durationId)?.id ??
      durations[0]?.id ??
      ''
  )
  const [quote, setQuote] = useState<ServerQuoteResult | null>(null)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!durationId) return
    let cancelled = false
    setQuote(null)
    quoteServer({ ...props.instance.config, durationId })
      .then((result) => {
        if (!cancelled) setQuote(result)
      })
      .catch(() => {
        if (!cancelled) setQuote(null)
      })
    return () => {
      cancelled = true
    }
  }, [durationId, props.instance.config])

  const duration = durations.find((item) => item.id === durationId)
  const fallbackTotal = duration
    ? Math.round(
        props.instance.unit_price *
          duration.amount *
          duration.discount *
          100
      ) / 100
    : 0
  const total = quote?.total ?? fallbackTotal
  // 按量档换算成天（方便和包月档的「延长多少天」对照）
  let days = 0
  if (duration) {
    days =
      duration.pricing === 'hourly'
        ? Math.round((duration.amount / 24) * 10) / 10
        : duration.amount * 30
  }

  const handleConfirm = async () => {
    if (!durationId) return
    setSubmitting(true)
    try {
      await props.onConfirm(durationId)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Renew instance')}</DialogTitle>
          <DialogDescription>
            {t(
              'The new period starts from the later of now and the current expiry time.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-3'>
          <div className='grid grid-cols-2 gap-2'>
            {durations.map((item) => (
              <button
                key={item.id}
                type='button'
                onClick={() => setDurationId(item.id)}
                className={cn(
                  'rounded-xl border px-3 py-2.5 text-left text-sm transition-colors',
                  durationId === item.id
                    ? 'border-[#7dd3c0] bg-[#7dd3c0]/[0.08] font-medium'
                    : 'border-border hover:border-[#7dd3c0]/60'
                )}
              >
                <div>{t(item.nameKey)}</div>
                {item.discount < 1 && (
                  <div className='mt-0.5 text-[11px] text-emerald-600 dark:text-emerald-400'>
                    {t('Save {{percent}}%', {
                      percent: Math.round((1 - item.discount) * 100),
                    })}
                  </div>
                )}
              </button>
            ))}
          </div>

          <div className='bg-muted/50 rounded-xl p-3.5'>
            <div className='text-muted-foreground text-xs'>
              {t('Renewal amount')}
            </div>
            <div className='mt-0.5 text-xl font-bold text-foreground'>
              {formatAmount(total)}
            </div>
            <div className='text-muted-foreground mt-1 text-[11px]'>
              {t('Extends by about {{days}} days', { days })}
            </div>
            {!quote && (
              <div className='mt-1 text-[11px] text-amber-600'>
                {t(
                  'Estimated with the price you paid; the server confirms the final amount.'
                )}
              </div>
            )}
          </div>
        </div>

        <DialogFooter>
          <Button variant='outline' disabled={submitting} onClick={props.onClose}>
            {t('Cancel')}
          </Button>
          <Button
            disabled={submitting || !durationId}
            onClick={handleConfirm}
            className='bg-[#3a8f7a] text-white hover:bg-[#2f7a66]'
          >
            {submitting ? (
              <>
                <Loader2 className='mr-2 size-4 animate-spin' />
                {t('Processing...')}
              </>
            ) : (
              t('Pay {{amount}}', { amount: formatAmount(total) })
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
