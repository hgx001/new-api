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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'

import {
  createFinanceCost,
  updateFinanceCost,
  type FinanceChannelOption,
  type FinanceCost,
  type FinanceCostCategory,
  type FinanceCostInput,
} from '../api'
import {
  parseDateInput,
  timestampToDateInput,
  todayDateInput,
} from '../lib/format'

const CATEGORIES: FinanceCostCategory[] = [
  'topup',
  'api_prepay',
  'bandwidth',
  'other',
  'refund',
]

const CATEGORY_LABEL_KEYS: Record<FinanceCostCategory, string> = {
  topup: 'Top-up',
  api_prepay: 'API prepay',
  bandwidth: 'Bandwidth',
  other: 'Other',
  refund: 'Refund',
}

interface CostDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  cost: FinanceCost | null
  channels: FinanceChannelOption[]
  onSaved: () => void
}

function toInput(cost: FinanceCost | null): {
  channelId: number
  amount: string
  date: string
  category: FinanceCostCategory
  referenceNo: string
  note: string
} {
  if (!cost) {
    return {
      channelId: 0,
      amount: '',
      date: todayDateInput(),
      category: 'topup',
      referenceNo: '',
      note: '',
    }
  }
  return {
    channelId: cost.channel_id,
    amount: cost.amount,
    date: timestampToDateInput(cost.occurred_at),
    category: cost.category,
    referenceNo: cost.reference_no,
    note: cost.note,
  }
}

export function CostDialog(props: CostDialogProps) {
  const { t } = useTranslation()
  const [form, setForm] = useState(() => toInput(props.cost))
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (props.open) setForm(toInput(props.cost))
  }, [props.open, props.cost])

  const update = (patch: Partial<typeof form>) => {
    setForm((current) => ({ ...current, ...patch }))
  }

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const amount = form.amount.trim()
    if (!amount || !form.date) return

    // 固定落在当天中午，避免浏览器/服务器时区差把日期推到前一天或后一天。
    const noonOffset = 12 * 3600
    const input: FinanceCostInput = {
      channel_id: Number(form.channelId) || 0,
      amount,
      occurred_at: parseDateInput(form.date) + noonOffset,
      category: form.category,
      reference_no: form.referenceNo.trim(),
      note: form.note.trim(),
    }
    setSubmitting(true)
    try {
      if (props.cost) {
        await updateFinanceCost(props.cost.id, input)
        toast.success(t('Cost updated'))
      } else {
        const result = await createFinanceCost(input)
        toast.success(t('Cost saved'))
        if (result?.duplicate_ids?.length) {
          toast(
            t('A cost with the same channel, amount and day already exists')
          )
        }
      }
      props.onSaved()
      props.onOpenChange(false)
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t('Failed to save cost')
      toast.error(message)
    } finally {
      setSubmitting(false)
    }
  }

  const fieldClass =
    'border-input bg-background h-9 w-full rounded-md border px-3 text-sm'

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>
            {props.cost ? t('Edit cost') : t('Add cost')}
          </DialogTitle>
        </DialogHeader>
        <p className='text-muted-foreground text-xs leading-relaxed'>
          {t(
            'Record an upstream payment. The full amount counts on the selected day.'
          )}
        </p>
        <form
          className='space-y-3'
          onSubmit={(event) => void handleSubmit(event)}
        >
          <div className='space-y-1'>
            <label
              className='text-sm font-medium'
              htmlFor='finance-cost-channel'
            >
              {t('Channel')}
            </label>
            <select
              id='finance-cost-channel'
              className={fieldClass}
              value={form.channelId}
              onChange={(event) =>
                update({ channelId: Number(event.target.value) })
              }
            >
              <option value={0}>{t('Unallocated')}</option>
              {props.channels.map((channel) => (
                <option key={channel.id} value={channel.id}>
                  {channel.name || `#${channel.id}`} (#{channel.id})
                </option>
              ))}
            </select>
            <p className='text-muted-foreground text-xs'>
              {t('Leave the channel unassigned for shared costs.')}
            </p>
          </div>

          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
            <div className='space-y-1'>
              <label
                className='text-sm font-medium'
                htmlFor='finance-cost-amount'
              >
                {t('Amount (CNY)')}
              </label>
              <Input
                id='finance-cost-amount'
                type='text'
                inputMode='decimal'
                placeholder='0.00'
                value={form.amount}
                onChange={(event) => update({ amount: event.target.value })}
                required
              />
            </div>
            <div className='space-y-1'>
              <label
                className='text-sm font-medium'
                htmlFor='finance-cost-date'
              >
                {t('Date')}
              </label>
              <Input
                id='finance-cost-date'
                type='date'
                value={form.date}
                onChange={(event) => update({ date: event.target.value })}
                required
              />
            </div>
          </div>
          <p className='text-muted-foreground text-xs'>
            {t('Use up to two decimals. Negative values record refunds.')}
          </p>

          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
            <div className='space-y-1'>
              <label
                className='text-sm font-medium'
                htmlFor='finance-cost-category'
              >
                {t('Category')}
              </label>
              <select
                id='finance-cost-category'
                className={fieldClass}
                value={form.category}
                onChange={(event) =>
                  update({
                    category: event.target.value as FinanceCostCategory,
                  })
                }
              >
                {CATEGORIES.map((category) => (
                  <option key={category} value={category}>
                    {t(CATEGORY_LABEL_KEYS[category])}
                  </option>
                ))}
              </select>
            </div>
            <div className='space-y-1'>
              <label
                className='text-sm font-medium'
                htmlFor='finance-cost-reference'
              >
                {t('Reference no.')}
              </label>
              <Input
                id='finance-cost-reference'
                value={form.referenceNo}
                onChange={(event) =>
                  update({ referenceNo: event.target.value })
                }
              />
            </div>
          </div>

          <div className='space-y-1'>
            <label className='text-sm font-medium' htmlFor='finance-cost-note'>
              {t('Note')}
            </label>
            <textarea
              id='finance-cost-note'
              className='border-input bg-background min-h-16 w-full rounded-md border px-3 py-2 text-sm'
              value={form.note}
              onChange={(event) => update({ note: event.target.value })}
            />
          </div>

          <div className='flex justify-end gap-2 pt-1'>
            <Button
              type='button'
              variant='outline'
              onClick={() => props.onOpenChange(false)}
            >
              {t('Cancel')}
            </Button>
            <Button type='submit' disabled={submitting}>
              {submitting ? t('Loading') : t('Save')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
