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
import { Loader2, ShieldCheck } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRoutePolicy,
  setShortYouzanEnabled,
  type VideoRoutePolicy,
} from '../api'

type ShortYouzanCardProps = {
  /**
   * 当前渠道绑定的模型（逗号分隔）。
   *
   * 与心笔卡同口径：只在承载有赞标准版目标模型（wan3.0-video）的渠道
   * 抽屉里渲染 —— 别的渠道里显示这个开关会让人以为按了会停掉整条渠道。
   */
  channelModels: string
}

/**
 * 「有赞标准版兜底」开关 —— 一个开关 = 心笔接不住时短请求有没有退路。
 *
 * 规则六把 ≤阈值（默认 10 秒）的短请求改派心笔内链，但心笔会因为余额耗尽、
 * AutoBan 或契约不兼容接不住；这时原本会回落到源链路 prime，撞上
 * PRIME_DURATION_TOO_SHORT 直接失败。打开本开关后，短请求改派有赞标准版
 * wan3.0-video（同一渠道同一 adaptor，只换 model 并钉住分辨率档）。
 *
 * 优先级：心笔内链开关 > 本开关；两个都关闭 = 不路由，走原链路。
 */
export function ShortYouzanCard({ channelModels }: ShortYouzanCardProps) {
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

  const targetModel = policy?.short_youzan_target_model ?? ''
  const isRelevant = useMemo(() => {
    if (!targetModel) return false
    return channelModels
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
      .includes(targetModel)
  }, [channelModels, targetModel])

  const handleToggle = useCallback(
    async (nextOn: boolean) => {
      setBusy(true)
      try {
        setPolicy(await setShortYouzanEnabled(nextOn))
        toast.success(
          t(nextOn ? 'Youzan fallback is on' : 'Youzan fallback is off')
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

  const on = policy.short_youzan_enabled
  const toneClasses = on
    ? 'border-sky-500/40 bg-sky-500/[0.05]'
    : 'border-slate-500/40 bg-slate-500/[0.05]'
  const toneText = on ? 'text-sky-600' : 'text-slate-600'
  const detail = on
    ? t(
        'When the Xinbi link cannot take it, requests up to {{seconds}}s route to {{target}}; the Xinbi link takes priority when both are on',
        {
          seconds: policy.short_trigger_seconds,
          target: targetModel,
        }
      )
    : t(
        'Short requests (up to {{seconds}}s) stay on the source chain when the Xinbi link cannot take them; turn on to reroute them to {{target}}',
        {
          seconds: policy.short_trigger_seconds,
          target: targetModel,
        }
      )

  return (
    <div className={cn('rounded-lg border p-3 transition-colors', toneClasses)}>
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-2'>
          <span className={cn('mt-0.5 shrink-0', toneText)}>
            <ShieldCheck className='h-4 w-4' />
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('Youzan standard fallback')}
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
            {t(on ? 'Youzan fallback is on' : 'Youzan fallback is off')}
          </span>
          <Switch
            checked={on}
            disabled={busy}
            onCheckedChange={handleToggle}
            className='h-6! w-11! data-checked:bg-sky-600! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('Youzan standard fallback')}
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
    </div>
  )
}
