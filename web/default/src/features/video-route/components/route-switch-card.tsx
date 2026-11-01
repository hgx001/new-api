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
import { Loader2, Route } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRoutePolicy,
  pauseVideoRoute,
  resumeVideoRoute,
  type VideoRoutePolicy,
} from '../api'
import { buildSwitchView, LONG_OFF_SECONDS } from '../lib/switch-view'

type RouteSwitchCardProps = {
  /**
   * 当前渠道绑定的模型（逗号分隔）。
   *
   * 改派只跟承载**目标模型**的渠道有关：渠道 20 上还挂着另外 7 个与豆包
   * 无关的模型，在它们的抽屉里显示这个开关会让人以为按了会停掉整条渠道。
   */
  channelModels: string
}

type BusyKind = 'toggle' | 'longOff' | null

/**
 * 「跨模型改派」开关。
 *
 * 主控件就是一个开关，不是三个按钮 —— 之前做成「暂停到今天结束 / 暂停到
 * 指定时间」，运营找不到「关」在哪。开关背后仍然是「暂停到某个时刻」，
 * 只是默认值取今天结束，把「什么时候自己恢复」写在状态行里，既好按，
 * 又不会关掉之后没人记得开。
 */
export function RouteSwitchCard({ channelModels }: RouteSwitchCardProps) {
  const { t } = useTranslation()
  const [policy, setPolicy] = useState<VideoRoutePolicy | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<BusyKind>(null)

  const load = useCallback(async () => {
    try {
      setPolicy(await getVideoRoutePolicy())
    } catch {
      // 拉不到就不显示这张卡：与其显示一个不知道状态的开关，不如不显示。
      setPolicy(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const targetModel = policy?.target_model ?? ''
  const isRelevant = useMemo(() => {
    if (!targetModel) return false
    return channelModels
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
      .includes(targetModel)
  }, [channelModels, targetModel])

  const applyPolicy = useCallback(
    async (
      kind: BusyKind,
      action: () => Promise<VideoRoutePolicy>,
      successText: string
    ) => {
      setBusy(kind)
      try {
        setPolicy(await action())
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
      if (nextOn) {
        void applyPolicy('toggle', () => resumeVideoRoute(), t('Routing is on'))
        return
      }
      // 关掉 = 暂停到今天结束，明天 0 点自动接回。
      void applyPolicy('toggle', () => pauseVideoRoute(), t('Routing is off'))
    },
    [applyPolicy, t]
  )

  const handleLongOff = useCallback(() => {
    const until = Math.floor(Date.now() / 1000) + LONG_OFF_SECONDS
    void applyPolicy(
      'longOff',
      () => pauseVideoRoute(until),
      t('Routing is off for 30 days')
    )
  }, [applyPolicy, t])

  if (loading || !policy || !isRelevant) {
    return null
  }

  const view = buildSwitchView(policy)
  const busyNow = busy !== null

  return (
    <div
      className={cn(
        'rounded-lg border p-3 transition-colors',
        view.on
          ? 'border-emerald-500/40 bg-emerald-500/[0.05]'
          : 'border-amber-500/50 bg-amber-500/[0.07]'
      )}
    >
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-2'>
          <span
            className={cn(
              'mt-0.5 shrink-0',
              view.on ? 'text-emerald-600' : 'text-amber-600'
            )}
          >
            <Route className='h-4 w-4' />
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('Cross-model routing')}
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
              view.on ? 'text-emerald-600' : 'text-amber-600'
            )}
          >
            {t(view.statusKey)}
          </span>
          <Switch
            checked={view.on}
            disabled={busyNow}
            onCheckedChange={handleToggle}
            className='h-6! w-11! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('Cross-model routing')}
          />
          {busyNow ? (
            <Loader2 className='text-muted-foreground h-3.5 w-3.5 animate-spin' />
          ) : null}
        </div>
      </div>

      {!view.on ? (
        <div className='mt-2 flex flex-wrap items-center gap-2'>
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={busyNow}
            onClick={() => handleToggle(true)}
          >
            {t('Turn on now')}
          </Button>
          {view.showLongOff ? (
            <Button
              type='button'
              size='sm'
              variant='ghost'
              disabled={busyNow}
              onClick={handleLongOff}
            >
              {busy === 'longOff' ? (
                <Loader2 className='mr-1.5 h-3.5 w-3.5 animate-spin' />
              ) : null}
              {t('Keep it off for 30 days')}
            </Button>
          ) : null}
        </div>
      ) : null}

      {/* 抽屉是表单，保存才生效；这个开关是即时 POST。不写清楚，
          运营按完开关会再去找保存按钮。 */}
      <p className='text-muted-foreground mt-2 text-[11px]'>
        {t('Takes effect immediately. No need to save.')}
      </p>
    </div>
  )
}
