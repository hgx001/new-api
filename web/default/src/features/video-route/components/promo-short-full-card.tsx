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
import { Crown, Loader2 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRoutePolicy,
  setPromoShortFullEnabled,
  type VideoRoutePolicy,
} from '../api'

type PromoShortFullCardProps = {
  /** 当前渠道绑定的模型（逗号分隔）。 */
  channelModels: string
}

/**
 * 促销族「满血档」开关（2026-11-25 老板口径）。
 *
 * 打开：`seedance2.5-特惠` 收到「≤15 秒、不带参考音视频/首尾帧、720P 或未
 * 指定分辨率」的请求时改派 **seedance-2-0**（漫屋 ch20，豆包官网标准版，
 * 固定 720p）；关闭（默认）：这批请求走自营短档（若勾选）或豆包 Fast。
 *
 * ⚠️ 与自营短档**三选一**：最多勾一个（勾一个自动关另一个），
 * 都不勾 → 豆包 Fast。写入口互斥由后端保证，读侧自营档先判。
 *
 * 为什么只在本渠道承载目标模型时渲染：这张卡回答的是「≤15 秒的短单由谁
 * 来跑」，与源渠道（有赞）无关；在有赞抽屉里显示会让人误以为按了会停掉
 * 源链路。
 */
export function PromoShortFullCard(props: PromoShortFullCardProps) {
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

  // 与自营短档卡联动：打开一个会关掉另一个，两张卡在同一抽屉并列显示，
  // 不重新拉一次就会一个亮一个灭地对不上（实际只有一个生效）。
  useEffect(() => {
    const refresh = () => {
      void load()
    }
    window.addEventListener('video-route-policy-changed', refresh)
    return () =>
      window.removeEventListener('video-route-policy-changed', refresh)
  }, [load])

  const targetModel = policy?.promo_short_full_model ?? ''
  const isRelevant = useMemo(() => {
    if (!targetModel) return false
    return props.channelModels
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
      .includes(targetModel)
  }, [props.channelModels, targetModel])

  const handleToggle = useCallback(
    async (nextOn: boolean) => {
      setBusy(true)
      try {
        setPolicy(await setPromoShortFullEnabled(nextOn))
        window.dispatchEvent(new Event('video-route-policy-changed'))
        toast.success(t(nextOn ? 'Full tier is on' : 'Full tier is off'))
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

  const on = Boolean(policy.promo_short_full_enabled)
  const ownOn = Boolean(policy.promo_short_own_enabled)
  // 时长上界由后端给（标准版只到 15 秒），前端不写死。
  const maxSeconds = policy.promo_short_full_max_seconds ?? 15

  let detail = t(
    'Requests up to {{seconds}}s stay on Doubao Fast ({{target}}); turn on to reroute them to {{full}}',
    { seconds: maxSeconds, target: 'seedance-2-0-fast', full: targetModel }
  )
  if (on) {
    detail = t(
      'Requests up to {{seconds}}s (no reference video/audio or first/last frame, 720P) route to {{target}}; other short requests still go to Doubao Fast',
      { seconds: maxSeconds, target: targetModel }
    )
  } else if (ownOn) {
    detail = t(
      'Own short tier is on: 9:16 short requests are routed to {{own}}',
      {
        own: policy.promo_short_own_model ?? 'sd-seedance-2.0fast',
      }
    )
  }

  const toneClasses = on
    ? 'border-amber-500/40 bg-amber-500/[0.05]'
    : 'border-slate-500/40 bg-slate-500/[0.05]'
  const toneText = on ? 'text-amber-600' : 'text-slate-600'

  return (
    <div className={cn('rounded-lg border p-3 transition-colors', toneClasses)}>
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-2'>
          <span className={cn('mt-0.5 shrink-0', toneText)}>
            <Crown className='h-4 w-4' />
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('Full tier (seedance-2-0)')}
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
            {t(on ? 'Full tier is on' : 'Full tier is off')}
          </span>
          <Switch
            checked={on}
            disabled={busy}
            onCheckedChange={handleToggle}
            className='h-6! w-11! data-checked:bg-amber-600! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('Full tier (seedance-2-0)')}
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
