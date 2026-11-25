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
import { Loader2, Sparkles } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRoutePolicy,
  setPromoShortOwnEnabled,
  type VideoRoutePolicy,
} from '../api'

type PromoShortOwnCardProps = {
  /** 当前渠道绑定的模型（逗号分隔）。 */
  channelModels: string
}

/**
 * 促销族「自营短档」开关（2026-11-23）。
 *
 * 打开：`seedance2.5-特惠` 收到「≤15 秒 + 9:16」（不带参考音视频/首尾帧、
 * 参考图 ≤9 张）的请求时改派 **sd-seedance-2.0fast**（漫屋 ch20，自营
 * 720p/9:16）；关闭（默认）：这类请求走豆包 Fast（seedance-2-0-fast）。
 *
 * 为什么只在本渠道承载目标模型时渲染：这张卡回答的是「9:16 的短单
 * 由谁来跑」，与源渠道（有赞）无关；在有赞抽屉里显示会让人以为
 * 按了会停掉源链路。
 */
export function PromoShortOwnCard({ channelModels }: PromoShortOwnCardProps) {
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

  // 与满血档卡联动：打开一个会关掉另一个（三选一互斥），两张卡在同一抽屉
  // 并列显示，不重新拉一次就会一个亮一个灭地对不上（实际只有一个生效）。
  useEffect(() => {
    const refresh = () => {
      void load()
    }
    window.addEventListener('video-route-policy-changed', refresh)
    return () =>
      window.removeEventListener('video-route-policy-changed', refresh)
  }, [load])

  const targetModel = policy?.promo_short_own_model ?? ''
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
        setPolicy(await setPromoShortOwnEnabled(nextOn))
        window.dispatchEvent(new Event('video-route-policy-changed'))
        toast.success(
          t(nextOn ? 'Own short tier is on' : 'Own short tier is off')
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

  const on = Boolean(policy.promo_short_own_enabled)
  const ratio = policy.promo_short_own_ratio ?? '9:16'
  // 参考图上限由后端给（上游物理上限 9 张，与模型广场描述同源）。
  // 前端不写死：写死一个旧数字会让客户按上限准备素材却被静静路由走。
  const maxImages = policy.promo_short_own_max_images ?? 9
  const toneClasses = on
    ? 'border-violet-500/40 bg-violet-500/[0.05]'
    : 'border-slate-500/40 bg-slate-500/[0.05]'
  const toneText = on ? 'text-violet-600' : 'text-slate-600'
  const fullOn = Boolean(policy.promo_short_full_enabled)
  let detail = t(
    'Requests up to {{seconds}}s in {{ratio}} stay on Doubao Fast ({{target}}); turn on to reroute them to {{own}}',
    {
      seconds: policy.short_trigger_seconds,
      ratio,
      target: 'seedance-2-0-fast',
      own: targetModel,
    }
  )
  if (on) {
    detail = t(
      'Requests up to {{seconds}}s in {{ratio}} (no reference video/audio or first/last frame, at most {{images}} images) route to {{target}}; other short requests still go to Doubao Fast',
      {
        seconds: policy.short_trigger_seconds,
        ratio,
        images: maxImages,
        target: targetModel,
      }
    )
  } else if (fullOn) {
    // 三选一：满血档开着时，短请求（含 9:16）归满血档，本档实际不生效。
    detail = t('Full tier is on: short requests are routed to {{full}}', {
      full: policy.promo_short_full_model ?? 'seedance-2-0',
    })
  }

  return (
    <div className={cn('rounded-lg border p-3 transition-colors', toneClasses)}>
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-2'>
          <span className={cn('mt-0.5 shrink-0', toneText)}>
            <Sparkles className='h-4 w-4' />
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('Own short tier (9:16)')}
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
            {t(on ? 'Own short tier is on' : 'Own short tier is off')}
          </span>
          <Switch
            checked={on}
            disabled={busy}
            onCheckedChange={handleToggle}
            className='h-6! w-11! data-checked:bg-violet-600! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('Own short tier (9:16)')}
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
        {t('Only one short tier can be on; enabling one turns the other off.')}
      </p>
    </div>
  )
}
