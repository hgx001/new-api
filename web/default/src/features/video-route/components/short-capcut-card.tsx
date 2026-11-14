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
import { Loader2, Zap } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRoutePolicy,
  setShortCapcutEnabled,
  type VideoRoutePolicy,
} from '../api'

type ShortCapcutCardProps = {
  /**
   * 当前渠道绑定的模型（逗号分隔）。
   *
   * 与心笔卡 / 有赞卡同口径：只在承载 CapCut 目标模型
   * （cp-seedance-2.0fast）的渠道抽屉里渲染 —— 别的渠道里显示这个
   * 开关会让人以为按了会停掉整条渠道。
   */
  channelModels: string
}

/**
 * 「CapCut 快速档」开关 —— 规则六的第三条短时长分支。
 *
 * 规则六把 ≤阈值（默认 10 秒）的短请求改派出去，三个可选出口：
 *   1. 心笔内链（wan3.0-720p-特惠-内链）—— 480P 生成 + 超分 720P；
 *   2. 有赞标准版（wan3.0-video）—— 绕开 prime 的 <10 秒保护；
 *   3. CapCut 快速档（cp-seedance-2.0fast）—— 本卡片。
 *
 * ⚠️ **三条同时只能有一条生效**：它们是同一个时长区间的三个出口。
 * 同时开着时，「落到哪一条」必须是**明确的经营意图**，而不是
 * 「谁写在配置里靠前」—— 两条都开着，运营看着两个按钮都亮着，
 * 却只有一条真正生效，这是最难排查的静默失效型故障。
 *
 * 所以打开本条会**自动关掉**另外两条（后端写入口保证），读侧还有一层
 * 按「心笔 > 有赞 > CapCut」的优先级兜底。关掉本条时不动另外两条 ——
 * 运营关掉一条往往只是想换一条，不是想全停。
 */
export function ShortCapcutCard({ channelModels }: ShortCapcutCardProps) {
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

  const targetModel = policy?.short_capcut_target_model ?? ''
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
        setPolicy(await setShortCapcutEnabled(nextOn))
        toast.success(
          t(
            nextOn
              ? 'CapCut fast lane is on'
              : 'CapCut fast lane is off'
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

  if (loading || !policy || !isRelevant) {
    return null
  }

  const on = policy.short_capcut_enabled
  const toneClasses = on
    ? 'border-amber-500/40 bg-amber-500/[0.05]'
    : 'border-slate-500/40 bg-slate-500/[0.05]'
  const toneText = on ? 'text-amber-600' : 'text-slate-600'

  // 三条分支的当前生效方：运营必须一眼看出「现在短请求到底走哪条」。
  const activeTarget = policy.short_active_target || ''
  const detail = on
    ? t(
        'Requests up to {{seconds}}s route to {{target}}; the Xinbi link and the Youzan fallback are turned off automatically',
        {
          seconds: policy.short_trigger_seconds,
          target: targetModel,
        }
      )
    : t(
        'Turn on to route requests up to {{seconds}}s to {{target}} when the Xinbi link cannot take them',
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
            <Zap className='h-4 w-4' />
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('CapCut fast lane (short requests)')}
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
            {t(on ? 'CapCut fast lane is on' : 'CapCut fast lane is off')}
          </span>
          <Switch
            checked={on}
            disabled={busy}
            onCheckedChange={handleToggle}
            className='h-6! w-11! data-checked:bg-amber-600! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('CapCut fast lane (short requests)')}
          />
          {busy ? (
            <Loader2 className='text-muted-foreground h-3.5 w-3.5 animate-spin' />
          ) : null}
        </div>
      </div>

      {/* 三条分支互斥：把「现在哪条在生效」明说出来。
          否则运营看到三个按钮各亮各的，却不知道短请求实际走哪条。 */}
      <p className='text-muted-foreground mt-2 text-[11px]'>
        {t('Only one short-request lane can be active. Currently active: {{target}}', {
          target: activeTarget || t('none (short requests stay on the source chain)'),
        })}
      </p>

      {/* 抽屉是表单，保存才生效；这个开关是即时 POST。 */}
      <p className='text-muted-foreground mt-1 text-[11px]'>
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
