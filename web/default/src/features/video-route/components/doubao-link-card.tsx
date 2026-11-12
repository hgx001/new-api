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

import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRouteFailover,
  getVideoRoutePolicy,
  setDoubaoLinkEnabled,
  setSecondsRuleRatioGate,
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
        toast.success(t(nextOn ? 'Doubao link is on' : 'Doubao link is off'))
        // 故障转移状态跟着开关变（旧式人工标记会被清掉），顺手刷新。
        // 开关已生效，刷新失败只是少更新一格降级状态 —— 与主操作分开
        // try，避免"操作成功却弹错误提示"误导运营重复操作。
        try {
          setFailover(await getVideoRouteFailover())
        } catch {
          // 保留上一次拉到的状态即可。
        }
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

  /**
   * 切换规则一的「可选比例条件」。
   *
   * 与 handleToggle 分开：那个开关表达「豆包通不通」（关闭带凌晨 2 点
   * 自动恢复、还要强制故障转移），这个勾选只收窄「哪些请求有资格改派」。
   * 共用一个处理函数会让「关一下开关」顺带把条件抹掉。
   */
  const handleRatioGateToggle = useCallback(
    async (nextOn: boolean) => {
      setBusy(true)
      try {
        // 接口返回完整策略快照，直接替换本地状态，勾选框立即反映新值。
        setPolicy(await setSecondsRuleRatioGate(nextOn))
        toast.success(
          t(nextOn ? 'Ratio condition enabled' : 'Ratio condition disabled')
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
            // 备胎没配置时关闭开关，seedance-2-5 会静默放行回豆包（改派承诺
            // 落空），此时禁用开关并给配置提示，不给"关了也没用"的机会。
            disabled={busy || !view.fallbackReady}
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

      {/* 可选比例条件：勾上才要求满足该比例，不勾则不限比例。
          与上面的链路开关是两个独立动作，所以单独一行、单独一个接口。 */}
      <div className='border-border/50 mt-3 flex items-start gap-2 border-t pt-3'>
        <Checkbox
          id='seconds-rule-ratio-gate'
          checked={view.ratioGateOn}
          disabled={busy}
          onCheckedChange={(value) => void handleRatioGateToggle(value === true)}
          className='mt-0.5'
        />
        <div className='min-w-0'>
          <Label
            htmlFor='seconds-rule-ratio-gate'
            className='text-foreground cursor-pointer text-xs font-semibold'
          >
            {t('Only route {{ratio}} requests to the reroute target', {
              ratio: view.ratioGateValue,
            })}
          </Label>
          <p className='text-muted-foreground mt-0.5 text-[11px] leading-relaxed'>
            {t(
              'When checked, only requests specifying {{ratio}} are rerouted; unchecked reroutes every request over the duration threshold.',
              { ratio: view.ratioGateValue }
            )}
          </p>
        </div>
      </div>
      {!view.fallbackReady ? (
        // 陈述事实而非指向 UI 中不存在的入口：改派模型只能由管理员在路由
        // 策略里配置，这个抽屉里没有配置界面。
        <p className='mt-1 text-[11px] text-amber-600'>
          {t(
            'Reroute models are not configured ({{source}} → {{target}}); the switch stays disabled until an admin sets them',
            {
              source: failover?.failover_source_model || policy.source_model,
              target: failover?.failover_target_model || '',
            }
          )}
        </p>
      ) : null}
    </div>
  )
}
