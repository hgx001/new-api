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

import { AlertTriangle, Loader2 } from 'lucide-react'
import { useState } from 'react'
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

import { formatAmount } from '../lib/pricing'
import type { ServerInstance } from '../types'

interface ReleaseDialogProps {
  instance: ServerInstance
  onClose: () => void
  onConfirm: () => Promise<void>
}

/**
 * 释放实例的二次确认。
 *
 * 释放是**不可逆**的：机器立刻销毁、IP 回收、密码失效。所以这里必须
 * 说清三件事：会发生什么、能退多少（口径）、以及不可撤销。
 * 退款金额由服务端按「实付 ÷ 已购时长 × 剩余时长」计算，前端不展示
 * 精确数字，避免两边算法漂移造成「说的和退的不一样」。
 */
export function ReleaseDialog(props: ReleaseDialogProps) {
  const { t } = useTranslation()
  const [submitting, setSubmitting] = useState(false)

  const handleConfirm = async () => {
    setSubmitting(true)
    try {
      await props.onConfirm()
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <AlertTriangle className='text-destructive size-5' />
            {t('Release instance')}
          </DialogTitle>
          <DialogDescription>
            {t(
              '{{name}} will be destroyed immediately. This cannot be undone.',
              {
                name: props.instance.name,
              }
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='border-destructive/30 bg-destructive/5 space-y-1.5 rounded-xl border p-3.5 text-xs leading-relaxed'>
          <p className='text-destructive font-medium'>
            {t('The instance, its public IP and its data will be removed.')}
          </p>
          <p className='text-muted-foreground'>
            {t(
              'The unused part of what you paid is refunded to your balance, calculated by the server from the actual amount paid and the remaining time.'
            )}
          </p>
          <p className='text-muted-foreground'>
            {t('Amount paid: {{amount}}', {
              amount: formatAmount(props.instance.paid_amount),
            })}
          </p>
        </div>

        <DialogFooter>
          <Button
            variant='outline'
            disabled={submitting}
            onClick={props.onClose}
          >
            {t('Cancel')}
          </Button>
          <Button
            variant='destructive'
            disabled={submitting}
            onClick={handleConfirm}
          >
            {submitting ? (
              <>
                <Loader2 className='mr-2 size-4 animate-spin' />
                {t('Processing...')}
              </>
            ) : (
              t('Release and refund')
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
