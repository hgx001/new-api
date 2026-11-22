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
import { Loader2, Repeat } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRoutePolicy,
  setWan3HkWan3TjEnabled,
  type VideoRoutePolicy,
} from '../api'

type Wan3HkWan3TjCardProps = {
  /**
   * 当前渠道绑定的模型（逗号分隔）。
   *
   * 只在承载源模型（hk-wan3，渠道 17）或目标模型（wan3.0-video-tj，
   * 渠道 32）的渠道抽屉里渲染 —— 与其它路由卡同口径。
   */
  channelModels: string
}

/**
 * 「hk-wan3 → 客美 tj」手动换货开关（2026-11-22）。
 *
 * 一个开关 = hk-wan3 这个模型由谁来跑：
 *
 *	打开：不带参考视频的 hk-wan3 请求内部改名为 wan3.0-video-tj
 *	      （渠道 32 客美）执行，计费仍按客户请求的 hk-wan3；
 *	关闭：留在 hk-wan3，行为与加这个开关之前完全一致。
 *
 * 带参考视频的请求**不改派**：客美会把参考视频秒数并入计费，
 * 而 hk-wan3 不收这部分钱。
 */
export function Wan3HkWan3TjCard({ channelModels }: Wan3HkWan3TjCardProps) {
  const { t } = useTranslation()
  const [policy, setPolicy] = useState<VideoRoutePolicy | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)

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

  const sourceModel = policy?.wan3_hk_wan3_tj_source_model ?? ''
  const targetModel = policy?.wan3_hk_wan3_tj_target_model ?? ''
  const isRelevant = useMemo(() => {
    if (!sourceModel || !targetModel) return false
    const models = channelModels
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
    return models.includes(sourceModel) || models.includes(targetModel)
  }, [channelModels, sourceModel, targetModel])

  const handleToggle = useCallback(
    async (nextOn: boolean) => {
      setBusy(true)
      try {
        setPolicy(await setWan3HkWan3TjEnabled(nextOn))
        toast.success(
          t(nextOn ? 'Rerouted to Kemeitv' : 'Staying on hk-wan3')
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

  if (loading || !policy || !isRelevant) {
    return null
  }

  const on = policy.wan3_hk_wan3_tj_enabled
  const toneClasses = on
    ? 'border-emerald-500/40 bg-emerald-500/[0.05]'
    : 'border-slate-500/40 bg-slate-500/[0.05]'
  const toneText = on ? 'text-emerald-600' : 'text-slate-600'
  const detail = on
    ? t(
        'Requests to {{source}} run on {{target}}; customers are still billed for {{source}}',
        { source: sourceModel, target: targetModel }
      )
    : t(
        'Requests to {{source}} stay on hk-wan3; turn on to run them on {{target}}',
        { source: sourceModel, target: targetModel }
      )

  return (
    <div className={cn('rounded-lg border p-3 transition-colors', toneClasses)}>
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-2'>
          <span className={cn('mt-0.5 shrink-0', toneText)}>
            <Repeat className='h-4 w-4' />
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('HK-wan3 → Kemeitv reroute')}
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
            {t(on ? 'Rerouted to Kemeitv' : 'Staying on hk-wan3')}
          </span>
          <Switch
            checked={on}
            disabled={busy}
            onCheckedChange={handleToggle}
            className='h-6! w-11! data-checked:bg-emerald-600! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('HK-wan3 → Kemeitv reroute')}
          />
          {busy ? (
            <Loader2 className='text-muted-foreground h-3.5 w-3.5 animate-spin' />
          ) : null}
        </div>
      </div>

      {/* 抽屉是表单，保存才生效；这个开关是即时 POST。不写清楚，
          运营按完开关会再去找保存按钮。 */}
      <p className='text-muted-foreground mt-2 text-[11px]'>
        {t('Takes effect immediately. No need to save.')}
      </p>
      <p className='text-muted-foreground mt-1 text-[11px]'>
        {t(
          'Pricing is unchanged: customers are still billed for the model they requested.'
        )}
      </p>
      <p className='text-muted-foreground mt-1 text-[11px]'>
        {t(
          'Requests carrying a reference video stay on hk-wan3: Kemeitv bills reference video seconds.'
        )}
      </p>
      <p className='text-muted-foreground mt-1 text-[11px]'>
        {t(
          'Adaptive aspect ratios, out-of-range durations, and requests the target cannot accept stay on hk-wan3.'
        )}
      </p>
    </div>
  )
}
