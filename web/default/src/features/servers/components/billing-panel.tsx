import { Check, CreditCard, Info, Lock, Sparkles } from 'lucide-react'
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
import { motion } from 'motion/react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'

import {
  BILLING_MODES,
  HOURLY_DURATIONS,
  TERMS,
  getBandwidth,
  getPlan,
  getRegion,
  formatGpu,
  formatSpec,
} from '../constants'
import {
  computePrice,
  formatAmount,
  resolveBilling,
  resolveHours,
} from '../lib/pricing'
import type { ServerConfig } from '../types'

interface BillingPanelProps {
  config: ServerConfig
  onChange: (patch: Partial<ServerConfig>) => void
  onCheckout: () => void
  requiresLogin: boolean
}

export function BillingPanel({
  config,
  onChange,
  onCheckout,
  requiresLogin,
}: BillingPanelProps) {
  const { t } = useTranslation()

  const plan = getPlan(config.plan)
  const spec = plan.specs.find((s) => s.id === config.specId) ?? plan.specs[0]
  const region = getRegion(config.region)
  const bandwidth = getBandwidth(config.mbps)
  const price = computePrice(config)
  const billing = resolveBilling(config)
  const isHourly = billing === 'hourly'
  const hours = resolveHours(config)
  const activeMode =
    BILLING_MODES.find((mode) => mode.id === billing) ?? BILLING_MODES[0]

  // 两种计费方式的选择器形状一样（名称 + 可选优惠角标 + 选中态），
  // 归一成一份再渲染，避免两套几乎相同的 JSX 各自漂移。
  const durationOptions = isHourly
    ? HOURLY_DURATIONS.map((item) => ({
        id: item.id,
        label: t(item.nameKey),
        badge: item.badgeKey ? t(item.badgeKey) : undefined,
        active: hours === item.hours,
        onSelect: () => onChange({ hours: item.hours }),
      }))
    : TERMS.map((item) => ({
        id: item.id,
        label: t(item.nameKey),
        badge: item.badgeKey ? t(item.badgeKey) : undefined,
        active: config.term === item.id,
        onSelect: () => onChange({ term: item.id }),
      }))

  const summaryRows = [
    { label: t('Region'), value: t(region.nameKey) },
    { label: t('Plan'), value: t(plan.nameKey) },
    {
      label: t('Specifications'),
      value: formatSpec(spec),
    },
    // 显卡单列一行：下单前必须看得见卡型与显存（能不能装下模型全看它）
    ...(spec.gpu
      ? [{ label: t('Graphics card'), value: formatGpu(spec.gpu) }]
      : []),
    { label: t('Bandwidth'), value: `${bandwidth.mbps} Mbps` },
  ]

  return (
    <div className='border-border/70 bg-card rounded-2xl border shadow-sm'>
      <div className='space-y-5 p-5 md:p-6'>
        {/* 计费方式 */}
        <div>
          <div className='mb-2.5 text-sm font-semibold'>
            {t('Billing method')}
          </div>
          <div className='bg-muted grid grid-cols-2 gap-1.5 rounded-xl p-1'>
            {BILLING_MODES.map((mode) => {
              const active = billing === mode.id
              return (
                <button
                  key={mode.id}
                  type='button'
                  onClick={() => onChange({ billing: mode.id })}
                  className={cn(
                    'relative rounded-lg px-2 py-2 text-xs font-medium transition-all',
                    active
                      ? 'bg-background text-[#1a4a3f] shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                  )}
                >
                  {t(mode.nameKey)}
                </button>
              )
            })}
          </div>
          <p className='text-muted-foreground mt-2 text-[11px] leading-relaxed'>
            {t(activeMode.descriptionKey)}
          </p>
        </div>

        {/* 周期 / 租用时长 */}
        <div>
          <div className='mb-2.5 text-sm font-semibold'>
            {isHourly ? t('Rental duration') : t('Term')}
          </div>
          <div className='bg-muted grid grid-cols-3 gap-1.5 rounded-xl p-1'>
            {durationOptions.map((option) => (
              <button
                key={option.id}
                type='button'
                onClick={option.onSelect}
                className={cn(
                  'relative rounded-lg px-2 py-2 text-xs font-medium transition-all',
                  option.active
                    ? 'bg-background text-[#1a4a3f] shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                )}
              >
                {option.label}
                {option.badge && (
                  <span className='mt-0.5 block text-[10px] font-semibold text-[#3a8f7a]'>
                    {option.badge}
                  </span>
                )}
              </button>
            ))}
          </div>
        </div>

        <Separator />

        {/* 摘要 */}
        <div className='space-y-2'>
          {summaryRows.map((row) => (
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

        {/* 价格 */}
        <div className='space-y-1.5'>
          <div className='flex items-end justify-between gap-3'>
            <div>
              <div className='text-muted-foreground text-xs'>
                {t('Total price')}
              </div>
              <motion.div
                key={price.total}
                initial={{ opacity: 1, y: -4 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.2 }}
                className='text-3xl font-extrabold tracking-tight text-[#1a4a3f]'
              >
                {formatAmount(price.total)}
              </motion.div>
            </div>
            {price.saved > 0 && (
              <Badge className='mb-1 gap-1 bg-[#e8f4f0] text-[#3a8f7a]'>
                <Sparkles className='size-3' />
                {t('Save {{amount}}', { amount: formatAmount(price.saved) })}
              </Badge>
            )}
          </div>

          <div className='text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
            {price.saved > 0 && (
              <span className='line-through'>
                {formatAmount(price.listTotal)}
              </span>
            )}
            {isHourly ? (
              <>
                <span>
                  {t('≈ {{amount}} / hour', {
                    amount: formatAmount(price.unitPrice),
                  })}
                </span>
                <span>
                  · {price.units} {t('hours')}
                </span>
                {/* 折合每月：让用户直接和包月价对比，判断按量租一周贵不贵 */}
                <span>
                  ·{' '}
                  {t('≈ {{amount}} / month', {
                    amount: formatAmount(price.monthlyEquivalent),
                  })}
                </span>
              </>
            ) : (
              <>
                <span>
                  {t('≈ {{amount}} / month', {
                    amount: formatAmount(price.monthlyEquivalent),
                  })}
                </span>
                <span>
                  · {price.units} {t('months')}
                </span>
              </>
            )}
          </div>

          {isHourly && (
            <p className='text-muted-foreground text-[11px] leading-relaxed'>
              {t(
                'The instance is released automatically when the purchased hours run out. Unused time is not refundable.'
              )}
            </p>
          )}
        </div>

        {/* 支付 */}
        <div className='space-y-3 pt-1'>
          <Button
            size='lg'
            className='w-full rounded-xl bg-[#3a8f7a] text-base text-white hover:bg-[#2f7a66]'
            onClick={onCheckout}
          >
            {requiresLogin ? (
              t('Sign in to order')
            ) : (
              <>
                <Lock className='mr-2 size-4' />
                {t('Buy now · {{amount}}', {
                  amount: formatAmount(price.total),
                })}
              </>
            )}
          </Button>

          <div className='text-muted-foreground flex items-start gap-2 text-[11px] leading-relaxed'>
            <Info className='mt-0.5 size-3.5 shrink-0' />
            <span>
              {t(
                'After the payment succeeds the server is provisioned automatically, and the IP address, username and password appear under My servers.'
              )}
            </span>
          </div>
        </div>
      </div>

      <div className='text-muted-foreground flex items-center gap-2 border-t bg-[#f5f9f7] px-5 py-3 text-[11px] md:px-6'>
        <Check className='size-3.5 text-[#3a8f7a]' />
        {t('Prices shown include taxes and are settled in CNY.')}
        <CreditCard className='ml-auto size-3.5' />
      </div>
    </div>
  )
}
