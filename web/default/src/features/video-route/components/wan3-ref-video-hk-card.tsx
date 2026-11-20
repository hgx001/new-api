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
import { Loader2, Video } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRoutePolicy,
  setWan3RefVideoHkEnabled,
  type VideoRoutePolicy,
} from '../api'

type Wan3RefVideoHkCardProps = {
  /**
   * 当前渠道绑定的模型（逗号分隔）。
   *
   * 只在承载源模型（官网 / tj / prime-tj）或目标模型（hk-wan3）的渠道抽屉里
   * 渲染 —— 与其它路由卡同口径：在无关渠道里显示这个开关，会让人以为按了
   * 会动整条渠道。
   */
  channelModels: string
}

/**
 * 「带参考视频的万相请求 → hk-wan3」开关（2026-11-20，**默认开启**）。
 *
 * 三条万相链路对参考视频的处置完全不同：
 *
 *	官网     适配器只下发 prompt/规格，参考图/视频/音频**整段丢弃**；
 *	tj       2026-11-20 起参考视频秒数**并入计费**（多收钱）；
 *	hk-wan3  **不收参考视频的钱**，且真的收音视频。
 *
 * 所以带参考视频的请求走 hk-wan3：素材真正生效，也不为参考视频多付钱。
 * 关掉这个开关就退回现状（官网丢素材 / tj 多收秒数）。
 */
export function Wan3RefVideoHkCard({ channelModels }: Wan3RefVideoHkCardProps) {
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

  const targetModel = policy?.wan3_ref_video_hk_target_model ?? ''
  const sourceModels = useMemo(
    () => policy?.wan3_ref_video_hk_source_models ?? [],
    [policy]
  )
  const isRelevant = useMemo(() => {
    if (!targetModel) return false
    const models = channelModels
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
    return (
      models.includes(targetModel) ||
      sourceModels.some((m) => models.includes(m))
    )
  }, [channelModels, sourceModels, targetModel])

  const handleToggle = useCallback(
    async (nextOn: boolean) => {
      setBusy(true)
      try {
        setPolicy(await setWan3RefVideoHkEnabled(nextOn))
        toast.success(
          t(
            nextOn
              ? 'Reference videos run on hk-wan3'
              : 'Reference videos stay on the source chain'
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

  const on = policy.wan3_ref_video_hk_enabled
  const toneClasses = on
    ? 'border-violet-500/40 bg-violet-500/[0.05]'
    : 'border-slate-500/40 bg-slate-500/[0.05]'
  const toneText = on ? 'text-violet-600' : 'text-slate-600'
  const sources = sourceModels.join(' / ')
  // 时段边界由后端给值（旧版本后端没有这两个字段时回退到 4/15）。
  const minSeconds = policy.wan3_ref_video_hk_min_seconds ?? 4
  const maxSeconds = policy.wan3_ref_video_hk_max_seconds ?? 15
  const detail = on
    ? t(
        'Requests to {{sources}} with a reference video run on {{target}} ({{min}}–{{max}} seconds); {{target}} does not charge for reference video, and billing stays on the requested model',
        { sources, target: targetModel, min: minSeconds, max: maxSeconds }
      )
    : t(
        'Requests to {{sources}} with a reference video stay on the source chain: the official channel drops the media and tj bills the reference video seconds',
        { sources }
      )

  return (
    <div className={cn('rounded-lg border p-3 transition-colors', toneClasses)}>
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-2'>
          <span className={cn('mt-0.5 shrink-0', toneText)}>
            <Video className='h-4 w-4' />
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('Reference video → hk-wan3 reroute')}
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
                ? 'Reference videos run on hk-wan3'
                : 'Reference videos stay on the source chain'
            )}
          </span>
          <Switch
            checked={on}
            disabled={busy}
            onCheckedChange={handleToggle}
            className='h-6! w-11! data-checked:bg-violet-600! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('Reference video → hk-wan3 reroute')}
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
          'Not rerouted when the duration is outside 4–15 seconds, the aspect ratio is adaptive, frames are used, media exceeds the limits, or the target has no available channel.'
        )}
      </p>
    </div>
  )
}
