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
import { Gift, ExternalLink, Loader2, Receipt, WalletCards } from 'lucide-react'
import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import {
  formatLocalMoney,
  getDiscountLabel,
  getPaymentIcon,
  getMinTopupAmount,
  calculatePresetPricing,
} from '../lib'
import type {
  PaymentMethod,
  PresetAmount,
  TopupInfo,
  CreemProduct,
  WaffoPayMethod,
} from '../types'
import { CreemProductsSection } from './creem-products-section'

interface RechargeFormCardProps {
  topupInfo: TopupInfo | null
  presetAmounts: PresetAmount[]
  selectedPreset: number | null
  onSelectPreset: (preset: PresetAmount) => void
  topupAmount: number
  onTopupAmountChange: (amount: number) => void
  paymentAmount: number
  calculating: boolean
  onPaymentMethodSelect: (method: PaymentMethod) => void
  paymentLoading: string | null
  redemptionCode: string
  onRedemptionCodeChange: (code: string) => void
  onRedeem: () => void
  redeeming: boolean
  topupLink?: string
  loading?: boolean
  priceRatio?: number
  usdExchangeRate?: number
  onOpenBilling?: () => void
  creemProducts?: CreemProduct[]
  enableCreemTopup?: boolean
  onCreemProductSelect?: (product: CreemProduct) => void
  enableWaffoTopup?: boolean
  waffoPayMethods?: WaffoPayMethod[]
  waffoMinTopup?: number
  onWaffoMethodSelect?: (method: WaffoPayMethod, index: number) => void
  enableWaffoPancakeTopup?: boolean
}

export function RechargeFormCard({
  topupInfo,
  presetAmounts,
  selectedPreset,
  onSelectPreset,
  topupAmount,
  onTopupAmountChange,
  paymentAmount,
  calculating,
  onPaymentMethodSelect,
  paymentLoading,
  redemptionCode,
  onRedemptionCodeChange,
  onRedeem,
  redeeming,
  topupLink,
  loading,
  priceRatio = 1,
  usdExchangeRate = 1,
  onOpenBilling,
  creemProducts,
  enableCreemTopup,
  onCreemProductSelect,
  enableWaffoTopup,
  waffoPayMethods,
  waffoMinTopup,
  onWaffoMethodSelect,
  enableWaffoPancakeTopup,
}: RechargeFormCardProps) {
  const { t } = useTranslation()
  // The whole topup surface is presented in renminbi so users never see a raw
  // USD figure. The API still takes whole USD units, so the value is snapped to
  // the nearest multiple of the exchange rate when the user finishes typing.
  // What they see is exactly what they pay and exactly what lands in the wallet
  // (1:1, no rounding surprise), and the snapped figure stays visible.
  const [localAmount, setLocalAmount] = useState('')

  const cnyToUsd = (cny: number) =>
    usdExchangeRate > 0 ? Math.max(1, Math.round(cny / usdExchangeRate)) : 0

  const usdToCny = (usd: number) =>
    Number((usd * usdExchangeRate).toFixed(2))

  const handleAmountChange = (value: string) => {
    setLocalAmount(value)
    const cnyValue = parseFloat(value)
    if (Number.isFinite(cnyValue) && cnyValue > 0) {
      onTopupAmountChange(cnyToUsd(cnyValue))
    }
  }

  // Snap the displayed amount to the payable multiple once the user stops
  // typing, so the field never displays a figure the backend cannot honour.
  const handleAmountBlur = () => {
    const cnyValue = parseFloat(localAmount)
    if (Number.isFinite(cnyValue) && cnyValue > 0) {
      setLocalAmount(String(usdToCny(cnyToUsd(cnyValue))))
    }
  }

  const hasConfigurableTopup =
    topupInfo?.enable_online_topup ||
    topupInfo?.enable_stripe_topup ||
    enableWaffoTopup ||
    enableWaffoPancakeTopup
  const hasAnyTopup = hasConfigurableTopup || enableCreemTopup
  const hasStandardPaymentMethods =
    Array.isArray(topupInfo?.pay_methods) && topupInfo.pay_methods.length > 0
  const hasWaffoPaymentMethods =
    Array.isArray(waffoPayMethods) && waffoPayMethods.length > 0
  const minTopup = getMinTopupAmount(topupInfo)
  const minTopupCny = minTopup * usdExchangeRate
  const redemptionEnabled = topupInfo?.enable_redemption !== false

  // Keep the field in sync when a preset is picked (the parent owns topupAmount).
  // Only real preset/reset changes propagate here; while the user is typing the
  // field is authoritative, so the snapped value we already sent matches.
  useEffect(() => {
    if (topupAmount > 0) {
      setLocalAmount((prev) => {
        const asCny = parseFloat(prev)
        if (Number.isFinite(asCny) && usdToCny(cnyToUsd(asCny)) === asCny) {
          return prev
        }
        return String(usdToCny(topupAmount))
      })
    }
  }, [topupAmount, usdExchangeRate])

  if (loading) {
    return (
      <Card data-card-hover='false' className='gap-0 overflow-hidden py-0'>
        <CardHeader className='border-b p-3 !pb-3 sm:p-5 sm:!pb-5'>
          <Skeleton className='h-6 w-32' />
          <Skeleton className='mt-2 h-4 w-48' />
        </CardHeader>
        <CardContent className='space-y-4 p-3 sm:space-y-6 sm:p-5'>
          <div className='space-y-4 sm:space-y-6'>
            {/* Preset Amounts Skeleton */}
            <div className='space-y-3'>
              <Skeleton className='h-3 w-16' />
              <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
                {Array.from({ length: 8 }).map((_, i) => (
                  <Skeleton key={i} className='h-[72px] rounded-lg' />
                ))}
              </div>
            </div>

            {/* Custom Amount Input Skeleton */}
            <div className='space-y-3'>
              <Skeleton className='h-3 w-28' />
              <Skeleton className='h-[42px] w-full' />
            </div>

            {/* Payment Methods Skeleton */}
            <div className='space-y-3'>
              <Skeleton className='h-3 w-32' />
              <div className='flex flex-wrap gap-3'>
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className='h-10 w-24 rounded-lg' />
                ))}
              </div>
            </div>
          </div>

          {/* Redemption Code Section Skeleton */}
          <div className='space-y-3 border-t pt-8'>
            <Skeleton className='h-3 w-24' />
            <div className='flex gap-2'>
              <Skeleton className='h-10 flex-1' />
              <Skeleton className='h-10 w-20' />
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <TitledCard
      title={t('Add Funds')}
      description={t('Choose an amount and payment method')}
      icon={<WalletCards className='h-4 w-4' />}
      disableHoverEffect
      action={
        onOpenBilling ? (
          <Button
            variant='outline'
            size='sm'
            onClick={onOpenBilling}
            className='w-full gap-2 sm:w-auto'
          >
            <Receipt className='h-4 w-4' />
            {t('Order History')}
          </Button>
        ) : null
      }
      contentClassName='space-y-4 sm:space-y-6'
    >
      {/* Online Topup Section */}
      {hasAnyTopup ? (
        <div className='space-y-4 sm:space-y-6'>
          {hasConfigurableTopup && (
            <>
              {presetAmounts.length > 0 && (
                <div className='space-y-2.5 sm:space-y-3'>
                  <Label className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
                    {t('Amount')}
                  </Label>
                  <div className='grid grid-cols-2 gap-1.5 sm:gap-3 md:grid-cols-4'>
                    {presetAmounts.map((preset, index) => {
                      const discount =
                        preset.discount ||
                        topupInfo?.discount?.[preset.value] ||
                        1.0
                      const {
                        displayValue,
                        actualPrice,
                        savedAmount,
                        hasDiscount,
                      } = calculatePresetPricing(
                        preset.value,
                        priceRatio,
                        discount,
                        usdExchangeRate
                      )
                      return (
                        <Button
                          key={index}
                          variant='outline'
                          className={cn(
                            'flex min-h-16 flex-col items-start rounded-lg px-3 py-2.5 text-left whitespace-normal sm:min-h-[72px] sm:p-4',
                            selectedPreset === preset.value
                              ? 'border-foreground bg-foreground/5 dark:border-foreground dark:bg-foreground/10'
                              : 'border-muted'
                          )}
                          onClick={() => onSelectPreset(preset)}
                        >
                          <div className='flex w-full items-center justify-between'>
                            <div className='text-base font-semibold sm:text-lg'>
                              {formatLocalMoney(displayValue)}
                            </div>
                            {hasDiscount && (
                              <div className='text-xs font-medium text-green-600'>
                                {getDiscountLabel(discount)}
                              </div>
                            )}
                          </div>
                          <div className='text-muted-foreground mt-1.5 w-full text-xs sm:mt-2'>
                            {hasDiscount ? (
                              <>
                                {t('Actual Amount')}{' '}
                                <span className='text-green-600'>
                                  {formatLocalMoney(actualPrice)}
                                </span>
                                {savedAmount > 0 && (
                                  <span className='text-green-600'>
                                    {' '}
                                    • {t('You save')}{' '}
                                    {formatLocalMoney(savedAmount)}
                                  </span>
                                )}
                              </>
                            ) : (
                              t('Credited as {{amount}}', {
                                amount: formatLocalMoney(displayValue),
                              })
                            )}
                          </div>
                        </Button>
                      )
                    })}
                  </div>
                </div>
              )}

              <div className='space-y-2.5 sm:space-y-3'>
                <Label
                  htmlFor='topup-amount'
                  className='text-muted-foreground text-xs font-medium tracking-wider uppercase'
                >
                  {t('Custom Amount')}
                </Label>
                <div className='grid grid-cols-[minmax(0,1fr)_minmax(110px,0.55fr)] gap-2 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center'>
                  <div className='relative'>
                    <span className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-sm font-semibold'>
                      ¥
                    </span>
                    <Input
                      id='topup-amount'
                      type='number'
                      inputMode='decimal'
                      value={localAmount}
                      onChange={(e) => handleAmountChange(e.target.value)}
                      onBlur={handleAmountBlur}
                      min={minTopupCny}
                      step='any'
                      placeholder={`${formatLocalMoney(minTopupCny)} 起`}
                      className='h-9 pl-7 text-base sm:h-10 sm:text-lg'
                    />
                  </div>
                  <div className='bg-muted/30 flex min-h-9 items-center justify-between gap-2 rounded-md border px-3 lg:min-w-52'>
                    <span className='text-muted-foreground truncate text-xs'>
                      {t('Actual Amount')}
                    </span>
                    {calculating ? (
                      <Skeleton className='h-5 w-16' />
                    ) : (
                      <span className='text-sm font-semibold'>
                        {formatLocalMoney(paymentAmount)}
                      </span>
                    )}
                  </div>
                </div>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Enter the renminbi amount you want to recharge. The credited amount equals the amount paid, with no conversion or extra charge.'
                  )}
                </p>
              </div>

              <div className='space-y-2.5 sm:space-y-3'>
                <Label className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
                  {t('Payment Method')}
                </Label>
                {hasStandardPaymentMethods ? (
                  <div className='grid grid-cols-2 gap-1.5 sm:gap-3 lg:grid-cols-3'>
                    {topupInfo?.pay_methods?.map((method) => {
                      const minTopup = method.min_topup || 0
                      const disabled = minTopup > topupAmount
                      const disabledReason = disabled
                        ? t('Minimum topup amount: {{amount}}', {
                            amount: minTopup,
                          })
                        : undefined
                      const disabledLabel = disabled
                        ? `${t('Minimum:')} ${minTopup}`
                        : undefined

                      const button = (
                        <Button
                          key={method.type}
                          variant='outline'
                          onClick={() => onPaymentMethodSelect(method)}
                          disabled={disabled || !!paymentLoading}
                          title={disabledReason}
                          aria-label={
                            disabledReason
                              ? `${method.name}. ${disabledReason}`
                              : method.name
                          }
                          className='min-h-14 min-w-0 justify-start gap-2 rounded-lg px-3 py-2 text-left'
                        >
                          {paymentLoading === method.type ? (
                            <Loader2 className='h-4 w-4 animate-spin' />
                          ) : (
                            getPaymentIcon(
                              method.type,
                              'h-4 w-4',
                              method.icon,
                              method.name
                            )
                          )}
                          <span className='flex min-w-0 flex-col items-start gap-0.5'>
                            <span className='max-w-full truncate'>
                              {method.name}
                            </span>
                            {disabledLabel && (
                              <span className='text-muted-foreground max-w-full truncate text-[11px] leading-4 font-normal'>
                                {disabledLabel}
                              </span>
                            )}
                          </span>
                        </Button>
                      )

                      return disabled ? (
                        <TooltipProvider key={method.type}>
                          <Tooltip>
                            <TooltipTrigger render={button}></TooltipTrigger>
                            <TooltipContent>{disabledReason}</TooltipContent>
                          </Tooltip>
                        </TooltipProvider>
                      ) : (
                        button
                      )
                    })}
                  </div>
                ) : hasWaffoPaymentMethods ? null : (
                  <Alert>
                    <AlertDescription>
                      {t(
                        'No payment methods available. Please contact administrator.'
                      )}
                    </AlertDescription>
                  </Alert>
                )}
              </div>

              {enableWaffoTopup &&
                hasWaffoPaymentMethods &&
                onWaffoMethodSelect && (
                  <div className='space-y-2.5 sm:space-y-3'>
                    <Label className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
                      {t('Waffo Payment')}
                    </Label>
                    <div className='grid grid-cols-2 gap-1.5 sm:gap-3 lg:grid-cols-3'>
                      {waffoPayMethods?.map((method, index) => {
                        const loadingKey = `waffo-${index}`
                        const waffoMin = waffoMinTopup || 0
                        const belowMin = waffoMin > topupAmount
                        const disabledReason = belowMin
                          ? t('Minimum topup amount: {{amount}}', {
                              amount: waffoMin,
                            })
                          : undefined
                        const disabledLabel = belowMin
                          ? `${t('Minimum:')} ${waffoMin}`
                          : undefined

                        const button = (
                          <Button
                            key={`${method.name}-${index}`}
                            variant='outline'
                            onClick={() => onWaffoMethodSelect(method, index)}
                            disabled={belowMin || !!paymentLoading}
                            title={disabledReason}
                            aria-label={
                              disabledReason
                                ? `${method.name}. ${disabledReason}`
                                : method.name
                            }
                            className='min-h-14 min-w-0 justify-start gap-2 rounded-lg px-3 py-2 text-left'
                          >
                            {paymentLoading === loadingKey ? (
                              <Loader2 className='h-4 w-4 animate-spin' />
                            ) : method.icon ? (
                              <img
                                src={method.icon}
                                alt={method.name}
                                className='h-4 w-4 object-contain'
                              />
                            ) : (
                              getPaymentIcon('waffo')
                            )}
                            <span className='flex min-w-0 flex-col items-start gap-0.5'>
                              <span className='max-w-full truncate'>
                                {method.name}
                              </span>
                              {disabledLabel && (
                                <span className='text-muted-foreground max-w-full truncate text-[11px] leading-4 font-normal'>
                                  {disabledLabel}
                                </span>
                              )}
                            </span>
                          </Button>
                        )

                        return belowMin ? (
                          <TooltipProvider key={`${method.name}-${index}`}>
                            <Tooltip>
                              <TooltipTrigger render={button}></TooltipTrigger>
                              <TooltipContent>{disabledReason}</TooltipContent>
                            </Tooltip>
                          </TooltipProvider>
                        ) : (
                          button
                        )
                      })}
                    </div>
                  </div>
                )}
            </>
          )}
        </div>
      ) : (
        <Alert>
          <AlertDescription>
            {t(
              'Online topup is not enabled. Please use redemption code or contact administrator.'
            )}
          </AlertDescription>
        </Alert>
      )}

      {/* Creem Products Section */}
      {enableCreemTopup &&
        Array.isArray(creemProducts) &&
        creemProducts.length > 0 &&
        onCreemProductSelect && (
          <div className='space-y-2.5 border-t pt-4 sm:space-y-3 sm:pt-6'>
            <Label className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
              {t('Creem Payment')}
            </Label>
            <CreemProductsSection
              products={creemProducts}
              onProductSelect={onCreemProductSelect}
            />
          </div>
        )}

      {/* Redemption Code Section */}
      {redemptionEnabled ? (
        <div className='space-y-2.5 border-t pt-4 sm:space-y-3 sm:pt-6'>
          <div className='flex items-center gap-2'>
            <Gift className='text-muted-foreground h-4 w-4' />
            <Label
              htmlFor='redemption-code'
              className='text-muted-foreground text-xs font-medium tracking-wider uppercase'
            >
              {t('Have a Code?')}
            </Label>
          </div>
          <div className='grid grid-cols-[minmax(0,1fr)_auto] gap-2'>
            <Input
              id='redemption-code'
              value={redemptionCode}
              onChange={(e) => onRedemptionCodeChange(e.target.value)}
              placeholder={t('Enter your redemption code')}
              className='h-9 min-w-0'
            />
            <Button
              onClick={onRedeem}
              disabled={redeeming}
              variant='outline'
              className='h-9 px-4'
            >
              {redeeming && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
              {t('Redeem')}
            </Button>
          </div>
          {topupLink && (
            <p className='text-muted-foreground text-xs'>
              {t('Need a redemption code?')}{' '}
              <a
                href={topupLink}
                target='_blank'
                rel='noopener noreferrer'
                className='inline-flex items-center gap-1 underline-offset-4 hover:underline'
              >
                {t('Get one here')}
                <ExternalLink className='h-3 w-3' />
              </a>
            </p>
          )}
        </div>
      ) : (
        <Alert className='border-t'>
          <AlertDescription>
            {t(
              'Redemption codes are disabled until the administrator confirms compliance terms.'
            )}
          </AlertDescription>
        </Alert>
      )}
    </TitledCard>
  )
}
