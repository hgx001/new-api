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
import { AlertTriangle, Loader2, Route } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRouteFailover,
  setFailoverAltEnabled,
  type VideoRouteFailoverState,
} from '../api'

type FailoverAltCardProps = {
  /**
   * 当前渠道绑定的模型（逗号分隔）。
   *
   * 与豆包卡 / 心笔卡不同，这张卡在两个渠道抽屉里都要出现：
   * 一个是豆包源模型（seedance-2-5，运营盯「豆包挂了怎么办」的地方），
   * 一个是 CapCut 目标模型（cp-seedance-2.5flash）。只在目标渠道显示的话，
   * 运营在豆包渠道排查故障时根本看不到这个开关。
   */
  channelModels: string
}

/**
 * 「故障转移备选：CapCut 2.5 Flash」开关 —— 决定豆包挂了走哪条退路。
 *
 * 规则五原本只有一条退路：wan3.0-班车（同上游队列，排队时长不可控）。
 * 打开本开关后，豆包不可用时改派 cp-seedance-2.5flash：向上游要 480p
 * （成本最低），交付前用腾讯 MPS 超分到 720P（对外承诺的档位）。
 *
 * 关闭（默认）仍走班车。两个开关的分工：
 *   - 豆包链路开关：豆包通不通；
 *   - 本开关：豆包不通时改派到班车还是 CapCut。
 *
 * ⚠️ 超分没配置 / CapCut 没渠道 / 模型名被改错时，后端会**静默回落班车**
 * （避免按 720P 的价交付 480P）。所以这里不能只显示「已打开」，
 * 必须把「打开了但实际不会生效」显式提示出来 —— 否则运营以为配好了，
 * 事故发生时才发现退路根本没换。
 */
export function FailoverAltCard({ channelModels }: FailoverAltCardProps) {
  const { t } = useTranslation()
  const [state, setState] = useState<VideoRouteFailoverState | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      setState(await getVideoRouteFailover())
    } catch {
      // 拉不到就不显示：与其显示一个不知道状态的开关，不如不显示。
      setState(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const altTarget = state?.failover_alt_target_model ?? ''
  const isRelevant = useMemo(() => {
    if (!altTarget) return false
    const models = channelModels
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
    // 目标渠道 or 源渠道都显示（见 props 注释）。
    return (
      models.includes(altTarget) ||
      (!!state?.failover_source_model &&
        models.includes(state.failover_source_model))
    )
  }, [channelModels, altTarget, state?.failover_source_model])

  const handleToggle = useCallback(
    async (nextOn: boolean) => {
      setBusy(true)
      try {
        setState(await setFailoverAltEnabled(nextOn))
        toast.success(
          t(
            nextOn
              ? 'Failover alternative is on'
              : 'Failover alternative is off'
          )
        )
      } catch (error) {
        toast.error(
          (error as { response?: { data?: { message?: string } } })?.response
            ?.data?.message ?? t('Failed to update routing policy')
        )
      } finally {
        setBusy(false)
      }
    },
    [t]
  )

  if (loading || !state || !isRelevant) {
    return null
  }

  const on = state.failover_alt_enabled
  // 开关开着但不会生效 —— 这是最需要被看见的状态（见文件头注释）。
  const inert = on && !state.failover_alt_effective
  const toneClasses = inert
    ? 'border-amber-500/40 bg-amber-500/[0.05]'
    : on
      ? 'border-violet-500/40 bg-violet-500/[0.05]'
      : 'border-slate-500/40 bg-slate-500/[0.05]'
  const toneText = inert
    ? 'text-amber-600'
    : on
      ? 'text-violet-600'
      : 'text-slate-600'

  const detail = on
    ? t(
        'When the Doubao link is unavailable, requests route to {{target}}: generated at 480p, upscaled to 720P before delivery',
        { target: altTarget }
      )
    : t(
        'When the Doubao link is unavailable, requests still go to the shuttle queue; turn on to reroute them to {{target}} (480p generation upscaled to 720P)',
        { target: altTarget }
      )

  return (
    <div className={cn('rounded-lg border p-3 transition-colors', toneClasses)}>
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-2'>
          <span className={cn('mt-0.5 shrink-0', toneText)}>
            {inert ? (
              <AlertTriangle className='h-4 w-4' />
            ) : (
              <Route className='h-4 w-4' />
            )}
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('Failover alternative (CapCut 2.5 Flash)')}
            </h4>
            <p className='text-muted-foreground mt-0.5 text-xs leading-relaxed'>
              {detail}
            </p>
          </div>
        </div>

        <div className='flex shrink-0 items-center gap-2'>
          <span
            className={cn('text-xs font-semibold whitespace-nowrap', toneText)}
          >
            {t(
              on
                ? inert
                  ? 'On but not taking effect'
                  : 'Failover alternative is on'
                : 'Failover alternative is off'
            )}
          </span>
          <Switch
            checked={on}
            disabled={busy}
            onCheckedChange={handleToggle}
            className='h-6! w-11! data-checked:bg-violet-600! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('Failover alternative (CapCut 2.5 Flash)')}
          />
          {busy ? (
            <Loader2 className='text-muted-foreground h-3.5 w-3.5 animate-spin' />
          ) : null}
        </div>
      </div>

      {/* 后端算好的状态说明：这四种「开着但没生效」的原因必须原样展示，
          否则运营只会反复点开关而找不到问题。 */}
      {inert ? (
        <p className='mt-2 text-[11px] leading-relaxed font-medium text-amber-700'>
          {state.failover_alt_note}
        </p>
      ) : null}

      {/* 抽屉是表单，保存才生效；这个开关是即时 POST。 */}
      <p className='text-muted-foreground mt-2 text-[11px]'>
        {t('Takes effect immediately. No need to save.')}
      </p>
      <p className='text-muted-foreground mt-1 text-[11px]'>
        {t(
          'Pricing is unchanged: customers are still billed for the model they requested.'
        )}
      </p>
    </div>
  )
}
