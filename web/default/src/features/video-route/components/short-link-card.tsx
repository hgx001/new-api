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
import { Loader2, Scissors } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRoutePolicy,
  setShortLinkEnabled,
  type VideoRoutePolicy,
} from '../api'

type ShortLinkCardProps = {
  /**
   * 当前渠道绑定的模型（逗号分隔）。
   *
   * 与豆包链路开关同口径：开关只跟承载规则六目标模型（心笔内链）的渠道
   * 有关 —— 其它渠道的抽屉里显示这个开关，会让人以为按了会停掉整条渠道。
   */
  channelModels: string
}

/**
 * 「短时长分流」开关 —— 一个开关 = 心笔内链通不通。
 *
 * 规则六把 ≤阈值（默认 10 秒）的短请求改派到心笔内链（480P 生成 +
 * 超分交付 720P），绕开上游 prime 的 PRIME_DURATION_TOO_SHORT。
 * 打开：短请求路由到心笔；关闭：不路由，短请求走原链路。
 *
 * 与豆包链路开关的差异：心笔没有按天重置的日额度，所以是纯手动开关 ——
 * 没有自动恢复时间、没有接单计数、没有强制降级，开了就恢复、关了就停。
 */
export function ShortLinkCard({ channelModels }: ShortLinkCardProps) {
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

  const targetModel = policy?.short_target_model ?? ''
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
        setPolicy(await setShortLinkEnabled(nextOn))
        toast.success(
          t(nextOn ? 'Short link is on' : 'Short link is off')
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

  const on = policy.short_link_enabled
  const toneClasses = on
    ? 'border-emerald-500/40 bg-emerald-500/[0.05]'
    : 'border-amber-500/50 bg-amber-500/[0.07]'
  const toneText = on ? 'text-emerald-600' : 'text-amber-600'
  const detail = on
    ? t(
        'Requests up to {{seconds}}s route to {{target}}; switch off to keep them on the source chain',
        {
          seconds: policy.short_trigger_seconds,
          target: targetModel,
        }
      )
    : t(
        'Short requests (up to {{seconds}}s) stay on the source chain; turn on to reroute them to {{target}}',
        {
          seconds: policy.short_trigger_seconds,
          target: targetModel,
        }
      )

  return (
    <div
      className={cn('rounded-lg border p-3 transition-colors', toneClasses)}
    >
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-2'>
          <span className={cn('mt-0.5 shrink-0', toneText)}>
            <Scissors className='h-4 w-4' />
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('Short-duration split link')}
            </h4>
            <p className='text-muted-foreground mt-0.5 text-xs leading-relaxed'>
              {detail}
            </p>
          </div>
        </div>

        <div className='flex shrink-0 items-center gap-2'>
          <span
            className={cn(
              'text-xs font-semibold whitespace-nowrap',
              toneText
            )}
          >
            {t(on ? 'Short link is on' : 'Short link is off')}
          </span>
          <Switch
            checked={on}
            disabled={busy}
            onCheckedChange={handleToggle}
            // 默认的 data-checked 跟随主题 primary（亮色下是黑的），
            // 在一个彩色卡片里看着像「没开」。这里强制绿/灰两色。
            className='h-6! w-11! data-checked:bg-emerald-600! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('Short-duration split link')}
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
