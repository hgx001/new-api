import { CheckCircle2, Info, Loader2, ShieldCheck, Wallet } from 'lucide-react'
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
import { useEffect } from 'react'
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
import { Separator } from '@/components/ui/separator'
import { useSystemConfig } from '@/hooks/use-system-config'
import { cn } from '@/lib/utils'

import {
  getBandwidth,
  getPlan,
  getRegion,
  getTerm,
  formatGpu,
  formatSpec,
} from '../constants'
import { computePrice, formatAmount, resolveBilling } from '../lib/pricing'
import type { PayMethod, ServerConfig } from '../types'

interface CheckoutDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  config: ServerConfig
  payMethods: PayMethod[]
  selectedMethod: string
  onSelectMethod: (type: string) => void
  paying: boolean
  paid: boolean
  onPay: () => void
  onDone: () => void
}

export function CheckoutDialog({
  open,
  onOpenChange,
  config,
  payMethods,
  selectedMethod,
  onSelectMethod,
  paying,
  paid,
  onPay,
  onDone,
}: CheckoutDialogProps) {
  const { t } = useTranslation()
  const { currency } = useSystemConfig()

  const plan = getPlan(config.plan)
  const spec = plan.specs.find((s) => s.id === config.specId) ?? plan.specs[0]
  const region = getRegion(config.region)
  const bandwidth = getBandwidth(config.mbps)
  const term = getTerm(config.term)
  const price = computePrice(config)
  const isHourly = resolveBilling(config) === 'hourly'

  // 后端下发的 min_topup 是「美元单位」，本页价格是人民币，
  // 与钱包页保持一致：按站点汇率换算后再比较门槛。
  const usdExchangeRate =
    currency?.quotaDisplayType === 'USD' ? 1 : currency?.usdExchangeRate || 1
  const selectedIsDemo =
    payMethods.find((m) => m.type === selectedMethod)?.demo ?? false

  useEffect(() => {
    if (open && payMethods.length > 0 && !selectedMethod) {
      onSelectMethod(payMethods[0].type)
    }
  }, [open, payMethods, selectedMethod, onSelectMethod])

  const rows = [
    { label: t('Region'), value: t(region.nameKey) },
    { label: t('Plan'), value: t(plan.nameKey) },
    {
      label: t('Specifications'),
      value: formatSpec(spec),
    },
    // 显卡单列一行：确认订单时也要看得见卡型与显存
    ...(spec.gpu
      ? [{ label: t('Graphics card'), value: formatGpu(spec.gpu) }]
      : []),
    { label: t('Bandwidth'), value: `${bandwidth.mbps} Mbps` },
    {
      label: t('Billing method'),
      value: isHourly ? t('Pay as you go (hourly)') : t('Prepaid monthly'),
    },
    isHourly
      ? {
          label: t('Rental duration'),
          value: `${price.units} ${t('hours')}`,
        }
      : {
          label: t('Term'),
          value: `${t(term.nameKey)} (${price.units} ${t('months')})`,
        },
  ]

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='gap-0 overflow-hidden p-0 sm:max-w-lg'>
        {paid ? (
          <div className='space-y-5 p-8 text-center'>
            <div className='mx-auto flex size-14 items-center justify-center rounded-full bg-emerald-500/10'>
              <CheckCircle2 className='size-7 text-emerald-500' />
            </div>
            <div>
              <DialogTitle className='text-xl font-semibold'>
                {t('Payment successful')}
              </DialogTitle>
              <DialogDescription className='mt-1.5'>
                {t(
                  'Your server is being provisioned. The IP address, username and password are available under My servers.'
                )}
              </DialogDescription>
            </div>
            <Button className='w-full' onClick={onDone}>
              {t('View my servers')}
            </Button>
          </div>
        ) : (
          <>
            <DialogHeader className='space-y-1.5 border-b px-6 py-5 text-left'>
              <DialogTitle className='text-lg font-semibold'>
                {t('Confirm your order')}
              </DialogTitle>
              <DialogDescription>
                {t('Review the configuration and choose a payment method.')}
              </DialogDescription>
            </DialogHeader>

            <div className='space-y-5 px-6 py-5'>
              <div className='space-y-2'>
                {rows.map((row) => (
                  <div
                    key={row.label}
                    className='flex items-start justify-between gap-3 text-sm'
                  >
                    <span className='text-muted-foreground shrink-0'>
                      {row.label}
                    </span>
                    <span className='text-right font-medium'>{row.value}</span>
                  </div>
                ))}
              </div>

              <Separator />

              {/* 支付方式 */}
              <div className='space-y-2.5'>
                <div className='text-sm font-semibold'>
                  {t('Payment Method')}
                </div>
                {payMethods.length === 0 ? (
                  <div className='border-destructive/30 bg-destructive/5 text-destructive flex items-start gap-2 rounded-xl border px-3.5 py-3 text-xs leading-relaxed'>
                    <Info className='mt-0.5 size-4 shrink-0' />
                    <span>
                      {t(
                        'Online payment is not available yet, please contact the administrator'
                      )}
                    </span>
                  </div>
                ) : (
                  <>
                    <div className='grid gap-2 sm:grid-cols-2'>
                      {payMethods.map((method) => {
                        const active = selectedMethod === method.type
                        const isAlipay = method.type === 'alipay'
                        const minTopup = method.min_topup ?? 0
                        const minTopupCny = minTopup * usdExchangeRate
                        const belowMin =
                          minTopup > 0 && minTopupCny > price.total
                        const label = method.nameKey
                          ? t(method.nameKey)
                          : method.name || method.type
                        return (
                          <button
                            key={method.type}
                            type='button'
                            disabled={paying || belowMin}
                            title={
                              belowMin
                                ? t('Minimum topup amount: {{amount}}', {
                                    amount: formatAmount(minTopupCny),
                                  })
                                : undefined
                            }
                            onClick={() => onSelectMethod(method.type)}
                            className={cn(
                              'flex items-center gap-2.5 rounded-xl border px-3.5 py-2.5 text-left text-sm transition-all disabled:opacity-60',
                              active
                                ? 'border-[#7dd3c0] bg-[#7dd3c0]/[0.08] shadow-[0_0_0_3px_rgba(125,211,192,0.16)]'
                                : 'border-border hover:border-[#7dd3c0]/60'
                            )}
                          >
                            {isAlipay ? (
                              <span className='flex size-6 shrink-0 items-center justify-center rounded-md bg-[#1677ff] text-[11px] font-bold text-white'>
                                支
                              </span>
                            ) : (
                              <span className='bg-muted flex size-6 shrink-0 items-center justify-center rounded-md'>
                                <Wallet className='text-muted-foreground size-3.5' />
                              </span>
                            )}
                            <span className='truncate font-medium'>
                              {label}
                            </span>
                          </button>
                        )
                      })}
                    </div>
                    {selectedIsDemo && (
                      <p className='text-muted-foreground text-[11px] leading-relaxed'>
                        {t(
                          'Demo mode: the payment is simulated and no real money is charged.'
                        )}
                      </p>
                    )}
                  </>
                )}
              </div>

              <div className='bg-muted/50 rounded-xl p-3.5'>
                <div className='flex items-end justify-between gap-3'>
                  <div>
                    <div className='text-muted-foreground text-xs'>
                      {t('You pay')}
                    </div>
                    <div className='text-2xl font-bold text-[#1a4a3f]'>
                      {formatAmount(price.total)}
                    </div>
                  </div>
                  {price.saved > 0 && (
                    <div className='text-muted-foreground text-right text-xs'>
                      <div className='line-through'>
                        {formatAmount(price.listTotal)}
                      </div>
                      <div className='font-medium text-[#3a8f7a]'>
                        {t('Save {{amount}}', {
                          amount: formatAmount(price.saved),
                        })}
                      </div>
                    </div>
                  )}
                </div>
              </div>
            </div>

            <DialogFooter className='bg-muted/30 gap-2 border-t px-6 py-4 sm:justify-between'>
              <span className='text-muted-foreground inline-flex items-center gap-1.5 text-[11px]'>
                <ShieldCheck className='size-3.5 text-[#3a8f7a]' />
                {t('Encrypted checkout')}
              </span>
              <div className='flex gap-2'>
                <Button
                  variant='outline'
                  disabled={paying}
                  onClick={() => onOpenChange(false)}
                >
                  {t('Cancel')}
                </Button>
                <Button
                  disabled={
                    paying || !selectedMethod || payMethods.length === 0
                  }
                  onClick={onPay}
                  className='bg-[#3a8f7a] text-white hover:bg-[#2f7a66]'
                >
                  {paying ? (
                    <>
                      <Loader2 className='mr-2 size-4 animate-spin' />
                      {t('Processing payment...')}
                    </>
                  ) : (
                    <>
                      {t('Pay {{amount}}', {
                        amount: formatAmount(price.total),
                      })}
                    </>
                  )}
                </Button>
              </div>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
