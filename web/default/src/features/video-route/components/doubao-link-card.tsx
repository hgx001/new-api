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

import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRouteFailover,
  getVideoRoutePolicy,
  setDoubaoLinkEnabled,
  type VideoRouteFailoverState,
  type VideoRoutePolicy,
} from '../api'
import { buildDoubaoLinkView } from '../lib/doubao-link-view'

type DoubaoLinkCardProps = {
  /**
   * 当前渠道绑定的模型（逗号分隔）。
   *
   * 开关只跟承载 seedance-2-5 的渠道有关：漫屋渠道上还挂着另外 7 个与
   * 豆包无关的模型，在它们的抽屉里显示这个开关会让人以为按了会停掉
   * 整条渠道。
   */
  channelModels: string
}

/**
 * 「豆包链路」开关 —— 一个开关 = 豆包通不通。
 *
 * 合并自两张卡（跨模型改派 + 故障转移）：运营要的其实是一个事实 ——
 * 豆包这条路通不通。开着：超时长请求改派豆包、额度用完自动降级班车；
 * 关着：特惠/活动留在常驻链路、seedance-2-5 全部改派班车，到下一个
 * 北京凌晨 2 点（豆包额度重置点）自动恢复。附属按钮全部去掉 ——
 * 恢复就是把开关拨回来，一个动作。
 */
export function DoubaoLinkCard({ channelModels }: DoubaoLinkCardProps) {
  const { t } = useTranslation()
  const [policy, setPolicy] = useState<VideoRoutePolicy | null>(null)
  const [failover, setFailover] = useState<VideoRouteFailoverState | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      const [nextPolicy, nextFailover] = await Promise.all([
        getVideoRoutePolicy(),
        getVideoRouteFailover(),
      ])
      setPolicy(nextPolicy)
      setFailover(nextFailover)
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

  const handleToggle = useCallback(
    async (nextOn: boolean) => {
      setBusy(true)
      try {
        setPolicy(await setDoubaoLinkEnabled(nextOn))
        // 故障转移状态跟着开关变（旧式人工标记会被清掉），顺手刷新。
        setFailover(await getVideoRouteFailover())
        toast.success(t(nextOn ? 'Doubao link is on' : 'Doubao link is off'))
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

  const view = buildDoubaoLinkView(policy, failover)
  const toneClasses: Record<typeof view.tone, string> = {
    on: 'border-emerald-500/40 bg-emerald-500/[0.05]',
    off: 'border-amber-500/50 bg-amber-500/[0.07]',
    degraded: 'border-sky-500/40 bg-sky-500/[0.05]',
  }
  const toneText: Record<typeof view.tone, string> = {
    on: 'text-emerald-600',
    off: 'text-amber-600',
    degraded: 'text-sky-600',
  }

  return (
    <div
      className={cn(
        'rounded-lg border p-3 transition-colors',
        toneClasses[view.tone]
      )}
    >
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-2'>
          <span
            className={cn('mt-0.5 shrink-0', toneText[view.tone])}
          >
            <Route className='h-4 w-4' />
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('Doubao link')}
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
              toneText[view.tone]
            )}
          >
            {t(view.statusKey)}
          </span>
          <Switch
            checked={view.on}
            disabled={busy}
            onCheckedChange={handleToggle}
            // 默认的 data-checked 跟随主题 primary（亮色下是黑的），
            // 在一个彩色卡片里看着像「没开」。这里强制绿/灰两色。
            className='h-6! w-11! data-checked:bg-emerald-600! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('Doubao link')}
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
