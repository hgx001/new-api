/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY, without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/
import { ChevronRight, Copy } from 'lucide-react'
import { memo } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import { DEFAULT_TOKEN_UNIT } from '../constants'
import {
  getDynamicDisplayGroupRatio,
  getDynamicPricingSummary,
  type DynamicPricingSummary,
} from '../lib/dynamic-price'
import { parseTags } from '../lib/filters'
import { isTokenBasedModel, getModelDisplayName, getModelDescriptionKey } from '../lib/model-helpers'
import { formatPrice, formatRequestPrice } from '../lib/price'
import type { PricingModel, TokenUnit } from '../types'
import type { ModelPerfBadgeData } from './model-perf-badge'

export interface ModelCardProps {
  model: PricingModel
  onClick: () => void
  priceRate?: number
  usdExchangeRate?: number
  tokenUnit?: TokenUnit
  showRechargePrice?: boolean
  perf?: ModelPerfBadgeData
}

function ModelCardPrice(props: {
  model: PricingModel
  dynamicSummary: DynamicPricingSummary | null
  isTokenBased: boolean
  tokenUnit: TokenUnit
  showRechargePrice: boolean
  priceRate: number
  usdExchangeRate: number
}) {
  const { t } = useTranslation()
  const tokenUnitLabel = props.tokenUnit === 'K' ? '1K' : '1M'
  const dynamicSummary = props.dynamicSummary

  if (dynamicSummary) {
    if (dynamicSummary.isSpecialExpression) {
      return (
        <span className='inline-flex items-center rounded-full bg-amber-500/10 px-2 py-0.5 font-medium text-amber-700 dark:text-amber-300'>
          {t('Special billing expression')}
        </span>
      )
    }

    if (dynamicSummary.primaryEntries.length > 0) {
      return (
        <>
          {dynamicSummary.primaryEntries.map((entry) => (
            <span
              key={entry.key}
              className='inline-flex items-center gap-1 rounded-full bg-muted/60 px-2 py-0.5'
            >
              <span className='text-muted-foreground'>
                {t(entry.shortLabel)}
              </span>
              <span className='font-mono font-semibold text-foreground'>
                {entry.formatted}
              </span>
              <span className='text-muted-foreground/70'>
                /{tokenUnitLabel}
              </span>
            </span>
          ))}
        </>
      )
    }

    return (
      <span className='inline-flex items-center rounded-full bg-amber-500/10 px-2 py-0.5 font-medium text-amber-700 dark:text-amber-300'>
        {t('Dynamic Pricing')}
      </span>
    )
  }

  if (props.isTokenBased) {
    return (
      <>
        <span className='inline-flex items-center gap-1 rounded-full bg-muted/60 px-2 py-0.5'>
          <span className='text-muted-foreground'>{t('Input')}</span>
          <span className='font-mono font-semibold text-foreground'>
            {formatPrice(
              props.model,
              'input',
              props.tokenUnit,
              props.showRechargePrice,
              props.priceRate,
              props.usdExchangeRate
            )}
          </span>
          <span className='text-muted-foreground/70'>/{tokenUnitLabel}</span>
        </span>
        <span className='inline-flex items-center gap-1 rounded-full bg-muted/60 px-2 py-0.5'>
          <span className='text-muted-foreground'>{t('Output')}</span>
          <span className='font-mono font-semibold text-foreground'>
            {formatPrice(
              props.model,
              'output',
              props.tokenUnit,
              props.showRechargePrice,
              props.priceRate,
              props.usdExchangeRate
            )}
          </span>
          <span className='text-muted-foreground/70'>/{tokenUnitLabel}</span>
        </span>
        {props.model.cache_ratio != null && (
          <span className='inline-flex items-center gap-1 rounded-full bg-muted/40 px-2 py-0.5'>
            <span className='text-muted-foreground/70'>{t('Cached')}</span>
            <span className='font-mono text-muted-foreground'>
              {formatPrice(
                props.model,
                'cache',
                props.tokenUnit,
                props.showRechargePrice,
                props.priceRate,
                props.usdExchangeRate
              )}
            </span>
          </span>
        )}
      </>
    )
  }

  return (
    <span className='inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 font-semibold text-emerald-600 dark:text-emerald-400'>
      <span className='font-mono'>
        {formatRequestPrice(
          props.model,
          props.showRechargePrice,
          props.priceRate,
          props.usdExchangeRate
        )}
      </span>
      <span>/ {t('second')}</span>
    </span>
  )
}

export const ModelCard = memo(function ModelCard(props: ModelCardProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  const tokenUnit = props.tokenUnit ?? DEFAULT_TOKEN_UNIT
  const priceRate = props.priceRate ?? 1
  const usdExchangeRate = props.usdExchangeRate ?? 1
  const showRechargePrice = props.showRechargePrice ?? false
  const isTokenBased = isTokenBasedModel(props.model)
  const tags = parseTags(props.model.tags)
  const groups = props.model.enable_groups || []
  const endpoints = props.model.supported_endpoint_types || []
  const modelIconKey = props.model.icon || props.model.vendor_icon
  const modelIcon = modelIconKey ? getLobeIcon(modelIconKey, 28) : null
  const displayName = getModelDisplayName(props.model)
  const descriptionKey = getModelDescriptionKey(props.model)
  const initial = displayName.charAt(0).toUpperCase() || '?'
  const isDynamicPricing =
    props.model.billing_mode === 'tiered_expr' &&
    Boolean(props.model.billing_expr)
  const dynamicSummary = isDynamicPricing
    ? getDynamicPricingSummary(props.model, {
        tokenUnit,
        showRechargePrice,
        priceRate,
        usdExchangeRate,
        groupRatioMultiplier: getDynamicDisplayGroupRatio(props.model),
      })
    : null

  const primaryGroup = groups[0]
  const bottomTags = [...endpoints.slice(0, 2), ...tags.slice(0, 2)]
  const hiddenCount =
    Math.max(groups.length - 1, 0) +
    Math.max(endpoints.length - 2, 0) +
    Math.max(tags.length - 2, 0)

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation()
    copyToClipboard(props.model.model_name || '')
  }

  return (
    <div
      className={cn(
        'group relative flex flex-col overflow-hidden rounded-2xl border border-border/60 bg-card p-4',
        'transition-all duration-200 hover:-translate-y-0.5 hover:border-primary/30',
        'hover:shadow-lg hover:shadow-primary/5 sm:p-5'
      )}
    >
      {/* 顶部渐变高亮条 */}
      <div className='pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-primary/50 to-transparent opacity-70' />

      {/* Header: icon + name + price + actions */}
      <div className='flex items-start justify-between gap-3'>
        <div className='flex min-w-0 items-start gap-3'>
          <div className='relative flex size-11 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-gradient-to-br from-primary/15 to-primary/5 ring-1 ring-inset ring-primary/10 sm:size-12'>
            {modelIcon || (
              <span className='text-base font-bold text-primary/80'>
                {initial}
              </span>
            )}
          </div>
          <div className='min-w-0'>
            <h3 className='truncate font-mono text-[15px] leading-tight font-bold text-foreground'>
              {displayName}
            </h3>
            <div className='mt-1.5 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-xs'>
              <ModelCardPrice
                model={props.model}
                dynamicSummary={dynamicSummary}
                isTokenBased={isTokenBased}
                tokenUnit={tokenUnit}
                showRechargePrice={showRechargePrice}
                priceRate={priceRate}
                usdExchangeRate={usdExchangeRate}
              />
            </div>
          </div>
        </div>

        <div className='flex shrink-0 items-center gap-1.5'>
          <button
            type='button'
            onClick={props.onClick}
            className='text-muted-foreground hover:text-foreground hover:bg-muted inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs transition-colors sm:px-2.5 sm:py-1.5'
          >
            {t('Details')}
            <ChevronRight className='size-3.5' />
          </button>
          <button
            type='button'
            onClick={handleCopy}
            className='text-muted-foreground hover:text-foreground hover:bg-muted rounded-md border p-1.5 transition-colors'
            title={t('Copy')}
          >
            <Copy className='size-3.5' />
          </button>
        </div>
      </div>

      {/* Description */}
      <p className='text-muted-foreground mt-3 line-clamp-1 flex-1 text-[13px] leading-relaxed sm:mt-4 sm:line-clamp-2 sm:min-h-[2.5rem]'>
        {props.model.description ||
          (descriptionKey ? t(descriptionKey) : null) ||
          t('No description available.')}
      </p>

      {/* Footer: metadata pills */}
      <div className='mt-3 flex flex-wrap items-center gap-1.5 sm:mt-4'>
        {primaryGroup && (
          <span className='inline-flex items-center rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary'>
            {primaryGroup} {t('Groups')}
          </span>
        )}
        <span className='inline-flex items-center rounded-full bg-muted/60 px-2 py-0.5 text-xs font-medium text-muted-foreground'>
          {isTokenBased ? t('Token-based') : t('Per Second')}
        </span>
        {isDynamicPricing && (
          <StatusBadge
            label={t('Dynamic Pricing')}
            variant='warning'
            copyable={false}
            size='sm'
          />
        )}
        {bottomTags.map((item) => (
          <span
            key={item}
            className='inline-flex items-center rounded-full bg-muted/40 px-2 py-0.5 text-xs text-muted-foreground/70'
          >
            {item}
          </span>
        ))}
        {hiddenCount > 0 && (
          <span className='inline-flex items-center rounded-full bg-muted/40 px-2 py-0.5 text-xs text-muted-foreground/50'>
            +{hiddenCount}
          </span>
        )}
      </div>
    </div>
  )
})
