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

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRouteFailover,
  getVideoRoutePolicy,
  setDoubaoLinkEnabled,
  setDoubaoLinkMaxTasks,
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
  // 接单上限的输入值：与「已保存的上限」分开存 ——
  // 输入框里改到一半还没点保存时，卡片不能显示成「已改」。
  const [maxTasksInput, setMaxTasksInput] = useState('')

  const load = useCallback(async () => {
    try {
      const [nextPolicy, nextFailover] = await Promise.all([
        getVideoRoutePolicy(),
        getVideoRouteFailover(),
      ])
      setPolicy(nextPolicy)
      setFailover(nextFailover)
      setMaxTasksInput(
        nextPolicy.doubao_link_max_tasks > 0
          ? String(nextPolicy.doubao_link_max_tasks)
          : ''
      )
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
   * 保存「最多接单数」。
   *
   * 与 handleToggle 分开：那个开关表达「豆包通不通」，这个表达「这批额度接几单」。
   * 共用一个处理函数会让「改额度」顺带把开关拨了。
   *
   * 空值 = 不限（上限 0）。清空输入框再保存即取消限额。
   */
  const handleMaxTasksSave = useCallback(async () => {
    const trimmed = maxTasksInput.trim()
    let next = 0
    if (trimmed !== '') {
      next = Number(trimmed)
      if (!Number.isInteger(next) || next < 0) {
        toast.error(
          t('Enter a non-negative integer, or leave it empty for no limit')
        )
        return
      }
    }
    setBusy(true)
    try {
      const nextPolicy = await setDoubaoLinkMaxTasks(next)
      setPolicy(nextPolicy)
      setMaxTasksInput(next > 0 ? String(next) : '')
      toast.success(t('Doubao link task limit saved'))
    } catch (error) {
      toast.error(
        (error as { response?: { data?: { message?: string } } })?.response
          ?.data?.message ?? t('Failed to update routing policy')
      )
    } finally {
      setBusy(false)
    }
  }, [maxTasksInput, t])

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
          <span className={cn('mt-0.5 shrink-0', toneText[view.tone])}>
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

      {/* 接单上限（2026-11-13 老板定）：豆包链路上游给的是运营自己掌握的一小批
          额度 —— 「这批只接 3 单，接满就关掉，我手动再打开时又来 3 单」。

          为什么单独一行而不是并进上面的开关：开关表达「豆包通不通」，
          这个表达「这批额度有多大」。关着的时候额度还在、只是暂时不接 ——
          两个动作混在一起，运营会以为「关掉开关 = 额度作废」。 */}
      <div className='border-border/50 mt-3 flex items-start gap-2 border-t pt-3'>
        <div className='min-w-0 flex-1'>
          <Label
            htmlFor='doubao-link-max-tasks'
            className='text-foreground text-xs font-semibold'
          >
            {t('Max tasks accepted from this link')}
          </Label>
          <p className='text-muted-foreground mt-0.5 text-[11px] leading-relaxed'>
            {t(
              'Set a quota for the doubao link: it closes itself after accepting this many tasks. Reopening the switch gives you a fresh batch. Leave empty for no limit.'
            )}
          </p>

          <div className='mt-2 flex items-center gap-2'>
            <Input
              id='doubao-link-max-tasks'
              type='number'
              min={0}
              step={1}
              inputMode='numeric'
              value={maxTasksInput}
              disabled={busy}
              onChange={(event) => setMaxTasksInput(event.target.value)}
              placeholder={t('No limit')}
              className='h-8 w-28'
            />
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={busy}
              onClick={() => void handleMaxTasksSave()}
            >
              {busy ? <Loader2 className='h-3.5 w-3.5 animate-spin' /> : null}
              {t('Save')}
            </Button>
          </div>

          {/* 已接/剩余：运营决定「要不要再给一批」的唯一依据。 */}
          {view.limitConfigured ? (
            <p className='text-muted-foreground mt-2 text-[11px]'>
              {t('Accepted {{accepted}}/{{max}} tasks, {{remaining}} left', {
                accepted: view.acceptedTasks,
                max: view.maxTasks,
                remaining: view.remaining,
              })}
            </p>
          ) : null}

          {/* 限额接满的提示与「人工关闭」不同：没有自动恢复时间，
              必须由人再给一批额度。 */}
          {view.offReason === 'limit' ? (
            <p className='mt-1 text-[11px] text-amber-600'>
              {t(
                'Task limit reached: this link stopped accepting tasks. Raise the limit or reopen the switch to accept more.'
              )}
            </p>
          ) : null}
        </div>
      </div>

      {/* 可选比例条件：勾上才要求满足该比例，不勾则不限比例。
          与上面的链路开关是两个独立动作，所以单独一行、单独一个接口。 */}
      <div className='border-border/50 mt-3 flex items-start gap-2 border-t pt-3'>
        <Checkbox
          id='seconds-rule-ratio-gate'
          checked={view.ratioGateOn}
          disabled={busy}
          onCheckedChange={(value) =>
            void handleRatioGateToggle(value === true)
          }
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
