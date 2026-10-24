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
  DURATIONS,
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
  resolveDurationId,
} from '../lib/pricing'
import type { ServerConfig } from '../types'

interface SummaryRow {
  label: string
  value: string
  /** 这一行在单价里的贡献（文案已本地化） */
  price?: string
}

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
  const spec = getSpec(plan, config.specId)
  const region = getRegion(config.region)
  const bandwidth = getBandwidth(config.mbps)
  const price = computePrice(config)
  const durationId = resolveDurationId(config)
  const duration = getDuration(durationId)
  const isHourly = duration.pricing === 'hourly'

  // 租用时长是唯一的时间维度：一份从 1 小时排到 3 年的平铺列表，
  // 选中哪一档就决定了用哪套计价模型（不再另设「计费方式」开关）。
  const durationOptions = DURATIONS.map((item) => ({
    id: item.id,
    label: t(item.nameKey),
    badge: item.badgeKey ? t(item.badgeKey) : undefined,
    active: durationId === item.id,
    onSelect: () => onChange({ durationId: item.id }),
  }))

  // 摘要的每一行都带上它在单价里的贡献：读者把各行加起来就能得到单价，
  // 单价 × 时长 × 折扣 = 总价 —— 页面自身可核对，不用相信我们的算法。
  // 裸机型价必须与左侧套餐卡上的数字完全一致，「套餐」行就是它。
  const formatUnitPrice = (value: number) =>
    isHourly
      ? t('{{price}} / hour', { price: formatAmount(value) })
      : t('{{price}} / month', { price: formatAmount(value) })

  const formatUnitSurcharge = (value: number) =>
    isHourly
      ? t('+{{price}} / hour', { price: formatAmount(value) })
      : t('+{{price}} / month', { price: formatAmount(value) })

  const summaryRows: SummaryRow[] = [
    {
      label: t('Region'),
      value: t(region.nameKey),
      // 地区系数是乘性的，单列出来读者才能把「各行之和」乘成单价。
      // 「×」是数学符号，各语言一致，直接渲染，不占 i18n key
      price: price.regionFactor !== 1 ? `×${price.regionFactor}` : undefined,
    },
    {
      label: t('Plan'),
      value: t(plan.nameKey),
      price: formatUnitPrice(price.planPrice),
    },
    {
      label: t('Specifications'),
      value: formatSpec(spec),
      price:
        price.specPrice > 0
          ? formatUnitSurcharge(price.specPrice)
          : t('Base spec'),
    },
    // 显卡单列一行：下单前必须看得见卡型与显存（能不能装下模型全看它）
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
      label: t('Rental duration'),
      value: t(duration.nameKey),
      // 折扣同样是乘性的，单列出来（与时长格子上的「省 15%」一致）
      price:
        price.discount < 1
          ? t('Save {{percent}}%', {
              percent: discountPercent(price.discount),
            })
          : undefined,
    },
  ]

  return (
    <div className='border-border/70 bg-card rounded-2xl border shadow-sm'>
      <div className='space-y-5 p-5 md:p-6'>
        {/* 租用时长：唯一的时间维度 */}
        <div>
          <div className='mb-2.5 text-sm font-semibold'>
            {t('Rental duration')}
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
          <p className='text-muted-foreground mt-2 text-[11px] leading-relaxed'>
            {isHourly
              ? t(
                  'Pay-as-you-go billing: the instance is released automatically when the purchased time runs out. Unused time is not refundable.'
                )
              : t(
                  'Prepaid billing: the instance stays available for the whole period you paid for.'
                )}
          </p>
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
          {/* 明说这套数怎么加，读者才能自己核对，而不是只能相信页面 */}
          <p className='text-muted-foreground mt-1 text-[11px] leading-relaxed'>
            {t(
              'The rows above add up to the unit price; the unit price times the rental duration is the total.'
            )}
          </p>
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
                {/* 小时单价：就是摘要各行相加（× 地区系数）的结果，精确值，不加「约」 */}
                <span>
                  {t('{{price}} / hour', {
                    price: formatAmount(price.unitPrice),
                  })}
                </span>
                {/* 时长直接用档位自己的名字（1 hour / 3 days），
                    不拼「N + hours」—— 拼出来会出现「1 hours」这种量词错误 */}
                <span>· {t(duration.nameKey)}</span>
                {/* 折合每月：这是按 730 小时/月摊出来的**折算值**，
                    用来和包月档的月单价对比，所以必须标「约」 */}
                <span>
                  ·{' '}
                  {t('≈ {{amount}} / month', {
                    amount: formatAmount(price.monthlyEquivalent),
                  })}
                </span>
              </>
            ) : (
              <>
                {/* 月单价：与摘要各行相加（× 地区系数）完全一致，精确值。
                    不用「≈」—— 包月档这个数不是折算出来的。
                    折扣不摊进这里，折扣由时长行的「省 15%」和总价区的
                    划线价体现，读者可以自己乘：月单价 × 月数 × 折扣 = 总价 */}
                <span>
                  {t('{{price}} / month', {
                    price: formatAmount(price.unitPrice),
                  })}
                </span>
                <span>· {t(duration.nameKey)}</span>
              </>
            )}
          </div>
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
