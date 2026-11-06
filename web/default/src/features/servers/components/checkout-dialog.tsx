import {
  CheckCircle2,
  Info,
  Loader2,
  ShieldCheck,
  Wallet,
} from 'lucide-react'
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
import { cn } from '@/lib/utils'

import {
  getBandwidth,
  getDuration,
  getPlan,
  getRegion,
  getSpec,
  formatGpu,
  formatSpec,
} from '../constants'
import {
  computePrice,
  discountPercent,
  formatAmount,
  resolveBilling,
  resolveDurationId,
} from '../lib/pricing'
import type {
  ServerConfig,
  ServerInstance,
  ServerPayMethod,
  ServerQuoteResult,
} from '../types'
import { CredentialsPanel } from './credentials-panel'

interface CheckoutDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  config: ServerConfig
  paying: boolean
  /** 支付成功后的实例；为空表示尚未支付成功 */
  paidServer: ServerInstance | null
  /** 服务端报价（权威金额）；为空时退回本地计算值展示 */
  serverQuote: ServerQuoteResult | null
  /** 账户余额（人民币） */
  balance: number
  /** 可用的支付方式（余额 + 服务端下发的第三方渠道） */
  payMethods: ServerPayMethod[]
  /** 当前选中的支付方式 */
  selectedMethod: string
  onSelectMethod: (method: string) => void
  onPay: () => void
  onDone: () => void
}

interface OrderRow {
  label: string
  value: string
  /** 这一行在单价里的贡献（文案已本地化） */
  price?: string
}

export function CheckoutDialog({
  open,
  onOpenChange,
  config,
  paying,
  paidServer,
  serverQuote,
  balance,
  payMethods,
  selectedMethod,
  onSelectMethod,
  onPay,
  onDone,
}: CheckoutDialogProps) {
  const { t } = useTranslation()

  const plan = getPlan(config.plan)
  const spec = getSpec(plan, config.specId)
  const region = getRegion(config.region)
  const bandwidth = getBandwidth(config.mbps)
  const duration = getDuration(resolveDurationId(config))
  const price = computePrice(config)
  const isHourly = resolveBilling(config) === 'hourly'

  // 金额以服务端报价为准；本地算出来的只用于「报价还没回来」时的占位展示。
  // 两者若不一致，说明前端常量落后于后端目录，以服务端为准才是对的。
  const total = serverQuote?.total ?? price.total
  const listTotal = serverQuote?.list_total ?? price.listTotal
  const saved = serverQuote?.saved ?? price.saved
  const quoteMismatch =
    serverQuote !== null && Math.abs(serverQuote.total - price.total) > 0.01

  // 余额支付要求余额充足；第三方支付直接付现金，没有余额门槛。
  const isBalanceMethod = selectedMethod === 'balance'
  const insufficient = isBalanceMethod && balance + 0.005 < total
  const balanceAfter = Math.max(0, balance - total)

  // 与购买摘要同一套拆解：各行相加 = 单价，单价 × 时长 = 总价
  const formatUnitPrice = (value: number) =>
    isHourly
      ? t('{{price}} / hour', { price: formatAmount(value) })
      : t('{{price}} / month', { price: formatAmount(value) })

  const formatUnitSurcharge = (value: number) =>
    isHourly
      ? t('+{{price}} / hour', { price: formatAmount(value) })
      : t('+{{price}} / month', { price: formatAmount(value) })

  const rows: OrderRow[] = [
    {
      label: t('Region'),
      value: t(region.nameKey),
      price: price.regionFactor !== 1 ? `×${price.regionFactor}` : undefined,
    },
    {
      label: t('Plan'),
      value: t(plan.nameKey),
      price: formatUnitPrice(price.planPrice),
    },
    {
      label: t('Specifications'),
      value: formatSpec(spec, t),
      price:
        price.specPrice > 0
          ? formatUnitSurcharge(price.specPrice)
          : t('Base spec'),
    },
    // 显卡单列一行：确认订单时也要看得见卡型与显存
    ...(spec.gpu
      ? [{ label: t('Graphics card'), value: formatGpu(spec.gpu) }]
      : []),
    {
      label: t('Bandwidth'),
      value: `${bandwidth.mbps} Mbps`,
      price:
        price.bandwidthPrice > 0
          ? formatUnitSurcharge(price.bandwidthPrice)
          : t('Included'),
    },
    {
      // 订单里只有一行时间维度：选了多久 + 按什么口径计价
      label: t('Rental duration'),
      value: `${t(duration.nameKey)} · ${
        isHourly ? t('Pay-as-you-go') : t('Prepaid')
      }`,
      price:
        price.discount < 1
          ? t('Save {{percent}}%', {
              percent: discountPercent(price.discount),
            })
          : undefined,
    },
  ]

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='gap-0 overflow-hidden p-0 sm:max-w-lg'>
        {paidServer ? (
          <div className='space-y-5 p-6'>
            <div className='flex items-start gap-3.5'>
              <div className='flex size-11 shrink-0 items-center justify-center rounded-full bg-emerald-500/10'>
                <CheckCircle2 className='size-6 text-emerald-500' />
              </div>
              <div className='min-w-0 flex-1'>
                <DialogTitle className='text-lg font-semibold'>
                  {t('Payment successful')}
                </DialogTitle>
                <DialogDescription className='mt-1'>
                  {t(
                    'Your server is ready. Use the credentials below to log in right away.'
                  )}
                </DialogDescription>
              </div>
            </div>

            {/* 凭据面板：付款那一刻用户最关心「怎么连上去」，
                所以直接把 IP / 用户名 / 密码摆出来，而不是让用户再去「我的服务器」里翻 */}
            <div className='rounded-2xl border border-[#d9ece6] bg-[#f5f9f7] p-4'>
              <CredentialsPanel server={paidServer} />
            </div>

            <Button
              className='w-full rounded-xl bg-[#3a8f7a] text-white hover:bg-[#2f7a66]'
              onClick={onDone}
            >
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
                {t('Review the configuration before paying.')}
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
                    <span className='text-right'>
                      <span className='font-medium'>{row.value}</span>
                      {row.price && (
                        <span className='text-muted-foreground ml-2 text-xs'>
                          {row.price}
                        </span>
                      )}
                    </span>
                  </div>
                ))}
              </div>

              <Separator />

              {/* 支付方式：站内余额（同步开通）或第三方收银台（异步开通）。
                  两种方式的门槛不同 —— 余额付要余额充足，第三方付不需要。 */}
              <div className='space-y-2.5'>
                <div className='text-sm font-semibold'>
                  {t('Payment Method')}
                </div>
                <div className='space-y-2'>
                  {payMethods.map((method) => {
                    const active = method.type === selectedMethod
                    const methodInsufficient =
                      method.type === 'balance' && balance + 0.005 < total
                    return (
                      <button
                        key={method.type}
                        type='button'
                        disabled={paying}
                        onClick={() => onSelectMethod(method.type)}
                        className={cn(
                          'flex w-full items-center gap-3 rounded-xl border px-3.5 py-3 text-left transition-colors',
                          active
                            ? 'border-[#7dd3c0] bg-[#7dd3c0]/[0.08]'
                            : 'border-border hover:bg-muted/40',
                          methodInsufficient && 'border-destructive/40'
                        )}
                      >
                        <span className='flex size-8 shrink-0 items-center justify-center rounded-lg bg-[#e8f4f0]'>
                          <Wallet className='size-4 text-[#3a8f7a]' />
                        </span>
                        <div className='min-w-0 flex-1'>
                          <div className='text-sm font-medium'>
                            {method.type === 'balance'
                              ? t('Account balance')
                              : method.name}
                          </div>
                          <div className='text-muted-foreground text-xs'>
                            {method.type === 'balance'
                              ? t('Available: {{amount}}', {
                                  amount: formatAmount(balance),
                                })
                              : t('Pay directly with {{method}}', {
                                  method: method.name,
                                })}
                          </div>
                        </div>
                        <span
                          className={cn(
                            'shrink-0 text-sm font-semibold',
                            methodInsufficient
                              ? 'text-destructive'
                              : 'text-[#1a4a3f]'
                          )}
                        >
                          {method.type === 'balance'
                            ? formatAmount(balance)
                            : formatAmount(total)}
                        </span>
                      </button>
                    )
                  })}
                </div>

                {insufficient && (
                  <div className='border-destructive/30 bg-destructive/5 text-destructive flex items-start gap-2 rounded-xl border px-3.5 py-3 text-xs leading-relaxed'>
                    <Info className='mt-0.5 size-4 shrink-0' />
                    <span>
                      {t(
                        'Insufficient balance. Top up {{amount}} more to place this order.',
                        { amount: formatAmount(total - balance) }
                      )}
                    </span>
                  </div>
                )}

                {isBalanceMethod && !insufficient && (
                  <p className='text-muted-foreground text-[11px] leading-relaxed'>
                    {t('Balance after payment: {{amount}}', {
                      amount: formatAmount(balanceAfter),
                    })}
                  </p>
                )}

                {!isBalanceMethod && (
                  <p className='text-muted-foreground text-[11px] leading-relaxed'>
                    {t(
                      'You will be redirected to the payment page. The server is provisioned once the payment is confirmed.'
                    )}
                  </p>
                )}

                {quoteMismatch && (
                  <p className='text-[11px] leading-relaxed text-amber-600'>
                    {t(
                      'The price has been updated by the server; the amount below is the final one.'
                    )}
                  </p>
                )}
              </div>

              <div className='bg-muted/50 rounded-xl p-3.5'>
                <div className='flex items-end justify-between gap-3'>
                  <div>
                    <div className='text-muted-foreground text-xs'>
                      {t('You pay')}
                    </div>
                    <div className='text-2xl font-bold text-[#1a4a3f]'>
                      {formatAmount(total)}
                    </div>
                  </div>
                  {saved > 0 && (
                    <div className='text-muted-foreground text-right text-xs'>
                      <div className='line-through'>
                        {formatAmount(listTotal)}
                      </div>
                      <div className='font-medium text-[#3a8f7a]'>
                        {t('Save {{amount}}', {
                          amount: formatAmount(saved),
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
                {isBalanceMethod
                  ? t('Deducted from your balance instantly')
                  : t('Payment is confirmed by the payment gateway')}
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
                    paying ||
                    insufficient ||
                    payMethods.length === 0 ||
                    !selectedMethod
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
                        amount: formatAmount(total),
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
