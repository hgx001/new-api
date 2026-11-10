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
import { Loader2, ShieldAlert } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  clearVideoRouteFailoverUnavailable,
  getVideoRouteFailover,
  markVideoRouteFailoverUnavailable,
  setVideoRouteFailoverEnabled,
  type VideoRouteFailoverState,
} from '../api'
import { buildFailoverView } from '../lib/failover-view'

type FailoverSwitchCardProps = {
  /**
   * 当前渠道绑定的模型（逗号分隔）。
   *
   * 只在本渠道承载**源模型**时渲染：故障转移改派的是「请求 seedance-2-5
   * 时的去向」，与承载备胎模型的渠道无关。在别的渠道抽屉里显示它，
   * 会让人以为按了会停掉那条渠道。
   */
  channelModels: string
}

type BusyKind = 'toggle' | 'mark' | 'clear' | null

/**
 * 「故障转移」开关（规则五）。
 *
 * 源链路额度用完 / 渠道被关 → 请求自动改派到备胎模型（wan3.0-班车）。
 *
 * 与「跨模型改派」（规则一~四）分开成一张卡，因为语义完全不同：
 * 前者是「省钱的调度偏好」，这里是「保出片的兜底」。混在一起时，
 * 运营为了关掉一次成本优化，会把兜底也一起关掉 —— 然后上游一挂，
 * 全站这个模型直接不可用。
 *
 * 界面必须分开表达三件事：
 *   - 开关开着吗（failover_enabled）；
 *   - 源链路现在好不好使（failover_unavailable）；
 *   - 人工标记 / 自动标记（人工标记不会被一次成功自动解除）。
 */
export function FailoverSwitchCard({ channelModels }: FailoverSwitchCardProps) {
  const { t } = useTranslation()
  const [state, setState] = useState<VideoRouteFailoverState | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<BusyKind>(null)

  const load = useCallback(async () => {
    try {
      setState(await getVideoRouteFailover())
    } catch {
      setState(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const sourceModel = state?.failover_source_model ?? ''
  const isRelevant = useMemo(() => {
    if (!sourceModel) return false
    return channelModels
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
      .includes(sourceModel)
  }, [channelModels, sourceModel])

  const apply = useCallback(
    async (
      kind: BusyKind,
      action: () => Promise<VideoRouteFailoverState>,
      successText: string
    ) => {
      setBusy(kind)
      try {
        setState(await action())
        toast.success(successText)
      } catch (error) {
        toast.error(
          (error as { response?: { data?: { message?: string } } })?.response
            ?.data?.message ?? t('Failed to update routing policy')
        )
      } finally {
        setBusy(null)
      }
    },
    [t]
  )

  const handleToggle = useCallback(
    (nextOn: boolean) => {
      void apply(
        'toggle',
        () => setVideoRouteFailoverEnabled(nextOn),
        nextOn ? t('Failover is on') : t('Failover is off')
      )
    },
    [apply, t]
  )

  const handleMark = useCallback(() => {
    void apply(
      'mark',
      () => markVideoRouteFailoverUnavailable(),
      t('Source marked unavailable until quota reset')
    )
  }, [apply, t])

  const handleClear = useCallback(() => {
    void apply(
      'clear',
      () => clearVideoRouteFailoverUnavailable(),
      t('Source marked available again')
    )
  }, [apply, t])

  if (loading || !state || !isRelevant) {
    return null
  }

  const view = buildFailoverView(state)
  const busyNow = busy !== null

  return (
    <div
      className={cn(
        'rounded-lg border p-3 transition-colors',
        view.tone === 'on'
          ? 'border-emerald-500/40 bg-emerald-500/[0.05]'
          : view.tone === 'off'
            ? 'border-amber-500/50 bg-amber-500/[0.07]'
            : 'border-border bg-muted/30'
      )}
    >
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-2'>
          <span
            className={cn(
              'mt-0.5 shrink-0',
              view.tone === 'on'
                ? 'text-emerald-600'
                : view.tone === 'off'
                  ? 'text-amber-600'
                  : 'text-muted-foreground'
            )}
          >
            <ShieldAlert className='h-4 w-4' />
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('Failover routing')}
            </h4>
            <p className='text-muted-foreground mt-0.5 text-xs leading-relaxed'>
              {t(view.detailKey, view.detailParams)}
            </p>
          </div>
        </div>

        <div className='flex shrink-0 items-center gap-2'>
          <span
            className={cn(
              'text-xs font-semibold whitespace-nowrap',
              view.tone === 'on'
                ? 'text-emerald-600'
                : view.tone === 'off'
                  ? 'text-amber-600'
                  : 'text-muted-foreground'
            )}
          >
            {t(view.statusKey)}
          </span>
          <Switch
            checked={view.on}
            disabled={busyNow || !state.failover_configured}
            onCheckedChange={handleToggle}
            className='h-6! w-11! data-checked:bg-emerald-600! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('Failover routing')}
          />
          {busyNow ? (
            <Loader2 className='text-muted-foreground h-3.5 w-3.5 animate-spin' />
          ) : null}
        </div>
      </div>

      {/* 源链路手动标记 / 解除。
          自动标记（额度探针）在源链路成功出片后自己就解除了，
          只有「上游不回报额度用完」这种情况才需要人来点。 */}
      {state.failover_configured ? (
        <div className='mt-2 flex flex-wrap items-center gap-2'>
          {view.sourceDown ? (
            <>
              <Button
                type='button'
                size='sm'
                variant='outline'
                disabled={busyNow}
                onClick={handleClear}
              >
                {t('Mark source as available')}
              </Button>
              {view.manuallyMarked ? (
                <span className='text-muted-foreground text-[11px]'>
                  {t(
                    'Manually marked. It will not be cleared automatically by a successful task.'
                  )}
                </span>
              ) : null}
            </>
          ) : (
            <Button
              type='button'
              size='sm'
              variant='ghost'
              disabled={busyNow}
              onClick={handleMark}
            >
              {t('Mark source unavailable until quota reset')}
            </Button>
          )}
          {!view.targetReady ? (
            <span className='text-[11px] text-amber-600'>
              {t('The backup model has no available channel.')}
            </span>
          ) : null}
        </div>
      ) : null}

      <p className='text-muted-foreground mt-2 text-[11px]'>
        {t(
          'Pricing is unchanged: customers are still billed for the model they requested.'
        )}
      </p>
    </div>
  )
}
