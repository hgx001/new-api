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
import { Loader2, ShoppingBag } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  getVideoRoutePolicy,
  setEcommerceConditions,
  setEcommerceEnabled,
  setEcommerceKeywords,
  type VideoRoutePolicy,
} from '../api'

type EcommerceRouteCardProps = {
  /**
   * 当前渠道绑定的模型（逗号分隔）。
   *
   * 口径与 FailoverAltCard 一致：**源渠道和目标渠道都显示**。
   *
   * 源渠道（承载 seedance2.5-特惠 / seedance2.5-活动）是运营做决策的地方 ——
   * 「这些请求要不要改派」本来就是源链路的经营选择；目标渠道
   * （承载 cp-seedance-2.5flash）也要显示，因为运营在那边排查
   * 「为什么有一批请求跑到了这条链路上」时，需要能直接关掉入口。
   */
  channelModels: string
}

/**
 * 「电商内容路由」开关 —— 规则七。
 *
 * 触发口径：**内容命中 或 参数命中**，两者是并列的进料口。
 *
 *   · 内容条件：提示词命中电商/产品推荐关键词（大小写不敏感的子串匹配）。
 *     恒可用 —— 关键词表为空时回落后端默认表。
 *   · 参数条件：勾选的项任意一项命中即可：
 *     - 「9:16 比例」入口（比例由后端给默认值，前端不硬编码）；
 *     - 「时长超过阈值」入口（阈值可配，默认 20 秒）。
 *
 * ⚠️ 三条必须写在界面上的事实（否则运营一定会误判）：
 *
 *  1. **带参考视频的请求永远不路由**，与本开关无关 —— 上游对参考视频的
 *     处理与源链路不一致，改派过去等于给客户换了一种素材语义。
 *  2. **计费不变**：客户仍按源模型（seedance2.5-特惠 / 活动）付费，
 *     改派只是内部调度。
 *  3. **规则七优先于规则一**：勾上「时长超过阈值」后，超长请求会被规则七
 *     先接走（改派 CapCut 快档），不会再走规则一的豆包链路。这不是 bug，
 *     是刻意的优先级 —— 否则「时长超过阈值」这个入口永远轮不到，
 *     后台看着勾着的、代码里是死配置。
 */
export function EcommerceRouteCard({ channelModels }: EcommerceRouteCardProps) {
  const { t } = useTranslation()
  const [policy, setPolicy] = useState<VideoRoutePolicy | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)

  // 阈值输入值与「已保存的阈值」分开存：输入框改到一半还没点保存时，
  // 卡片不能显示成「已改」。
  const [secondsInput, setSecondsInput] = useState('')
  // 关键词编辑框同理：改到一半没保存时，判定用的仍是旧表。
  const [keywordsInput, setKeywordsInput] = useState('')

  const load = useCallback(async () => {
    try {
      const next = await getVideoRoutePolicy()
      setPolicy(next)
      setSecondsInput(String(next.ecommerce_trigger_seconds))
      setKeywordsInput(next.ecommerce_keywords.join('\n'))
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

  const targetModel = policy?.ecommerce_target_model ?? ''

  const isRelevant = useMemo(() => {
    const models = channelModels
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
    if (models.length === 0) return false
    if (targetModel && models.includes(targetModel)) return true
    // 源渠道：承载任一被配置的源模型（特惠 / 活动）。
    return (policy?.source_switches ?? []).some((s) =>
      models.includes(s.source_model)
    )
  }, [channelModels, targetModel, policy?.source_switches])

  /** 统一的错误提示：优先用后端给的中文原因，兜底一句通用文案。 */
  const reportError = useCallback(
    (error: unknown) => {
      toast.error(
        (error as { response?: { data?: { message?: string } } })?.response
          ?.data?.message ?? t('Failed to update routing policy')
      )
    },
    [t]
  )

  const handleToggle = useCallback(
    async (nextOn: boolean) => {
      setBusy(true)
      try {
        setPolicy(await setEcommerceEnabled(nextOn))
        toast.success(
          t(
            nextOn
              ? 'Ecommerce routing is on'
              : 'Ecommerce routing is off'
          )
        )
      } catch (error) {
        reportError(error)
      } finally {
        setBusy(false)
      }
    },
    [reportError, t]
  )

  /**
   * 勾选/取消某个参数入口 —— 即时 POST，与规则一的比例闸门同款交互。
   *
   * 只改勾选状态，不改阈值：阈值有独立的输入框 + 保存按钮。
   */
  const handleConditionToggle = useCallback(
    async (kind: 'ratio' | 'seconds', nextOn: boolean) => {
      setBusy(true)
      try {
        const ratioEnabled =
          kind === 'ratio'
            ? nextOn
            : (policy?.ecommerce_ratio_condition ?? false)
        const secondsEnabled =
          kind === 'seconds'
            ? nextOn
            : (policy?.ecommerce_seconds_condition ?? false)
        setPolicy(
          await setEcommerceConditions(ratioEnabled, secondsEnabled)
        )
        toast.success(t('Ecommerce routing conditions updated'))
      } catch (error) {
        reportError(error)
      } finally {
        setBusy(false)
      }
    },
    [policy?.ecommerce_ratio_condition, policy?.ecommerce_seconds_condition, reportError, t]
  )

  /** 保存时长阈值。 */
  const handleSecondsSave = useCallback(async () => {
    const parsed = Number(secondsInput.trim())
    if (!Number.isInteger(parsed) || parsed <= 0) {
      toast.error(t('Enter a positive integer number of seconds'))
      return
    }
    setBusy(true)
    try {
      setPolicy(
        await setEcommerceConditions(
          policy?.ecommerce_ratio_condition ?? false,
          policy?.ecommerce_seconds_condition ?? false,
          parsed
        )
      )
      toast.success(t('Ecommerce duration threshold saved'))
    } catch (error) {
      reportError(error)
    } finally {
      setBusy(false)
    }
  }, [policy?.ecommerce_ratio_condition, policy?.ecommerce_seconds_condition, reportError, secondsInput, t])

  /** 保存关键词表：一行一个词。 */
  const handleKeywordsSave = useCallback(async () => {
    const keywords = keywordsInput
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)
    setBusy(true)
    try {
      setPolicy(await setEcommerceKeywords(keywords))
      toast.success(t('Ecommerce keywords saved'))
    } catch (error) {
      reportError(error)
    } finally {
      setBusy(false)
    }
  }, [keywordsInput, reportError, t])

  /** 恢复后端默认关键词表。 */
  const handleKeywordsReset = useCallback(async () => {
    setBusy(true)
    try {
      setPolicy(await setEcommerceKeywords([]))
      toast.success(t('Ecommerce keywords reset to defaults'))
    } catch (error) {
      reportError(error)
    } finally {
      setBusy(false)
    }
  }, [reportError, t])

  if (loading || !policy || !isRelevant) {
    return null
  }

  const on = policy.ecommerce_enabled
  const ratioValue = policy.ecommerce_ratio_condition_default || '9:16'
  const toneClasses = on
    ? 'border-emerald-500/40 bg-emerald-500/[0.05]'
    : 'border-slate-500/40 bg-slate-500/[0.05]'
  const toneText = on ? 'text-emerald-600' : 'text-slate-600'

  const detail = on
    ? t(
        'E-commerce or product-recommendation prompts are routed to {{target}}; requests with a reference video are never rerouted',
        { target: targetModel }
      )
    : t(
        'Turn on to route e-commerce or product-recommendation prompts to {{target}}',
        { target: targetModel }
      )

  return (
    <div className={cn('rounded-lg border p-3 transition-colors', toneClasses)}>
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-2'>
          <span className={cn('mt-0.5 shrink-0', toneText)}>
            <ShoppingBag className='h-4 w-4' />
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('Ecommerce content routing')}
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
            {t(on ? 'Ecommerce routing is on' : 'Ecommerce routing is off')}
          </span>
          <Switch
            checked={on}
            disabled={busy}
            onCheckedChange={handleToggle}
            className='h-6! w-11! data-checked:bg-emerald-600! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('Ecommerce content routing')}
          />
          {busy ? (
            <Loader2 className='text-muted-foreground h-3.5 w-3.5 animate-spin' />
          ) : null}
        </div>
      </div>

      {/* 参数入口。两个勾选框是并列的进料口，命中任意一个即改派。 */}
      <div className='border-border/50 mt-3 flex flex-col gap-3 border-t pt-3'>
        <div className='flex items-start gap-2'>
          <Checkbox
            id='ecommerce-ratio-condition'
            checked={policy.ecommerce_ratio_condition}
            disabled={busy}
            onCheckedChange={(value) =>
              void handleConditionToggle('ratio', value === true)
            }
            className='mt-0.5'
          />
          <div className='min-w-0'>
            <Label
              htmlFor='ecommerce-ratio-condition'
              className='text-foreground cursor-pointer text-xs font-semibold'
            >
              {t('Also route {{ratio}} requests', { ratio: ratioValue })}
            </Label>
            <p className='text-muted-foreground mt-0.5 text-[11px] leading-relaxed'>
              {t(
                'When checked, requests specifying {{ratio}} are rerouted even if the prompt is not e-commerce content',
                { ratio: ratioValue }
              )}
            </p>
          </div>
        </div>

        <div className='flex items-start gap-2'>
          <Checkbox
            id='ecommerce-seconds-condition'
            checked={policy.ecommerce_seconds_condition}
            disabled={busy}
            onCheckedChange={(value) =>
              void handleConditionToggle('seconds', value === true)
            }
            className='mt-0.5'
          />
          <div className='min-w-0 flex-1'>
            <Label
              htmlFor='ecommerce-seconds-condition'
              className='text-foreground cursor-pointer text-xs font-semibold'
            >
              {t('Also route requests longer than {{seconds}}s', {
                seconds: policy.ecommerce_trigger_seconds,
              })}
            </Label>
            <p className='text-muted-foreground mt-0.5 text-[11px] leading-relaxed'>
              {t(
                'When checked, requests over the threshold are rerouted even if the prompt is not e-commerce content'
              )}
            </p>

            <div className='mt-2 flex items-center gap-2'>
              <Input
                id='ecommerce-trigger-seconds'
                type='number'
                min={1}
                step={1}
                inputMode='numeric'
                value={secondsInput}
                disabled={busy}
                onChange={(event) => setSecondsInput(event.target.value)}
                className='h-8 w-28'
              />
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={busy}
                onClick={() => void handleSecondsSave()}
              >
                {busy ? <Loader2 className='h-3.5 w-3.5 animate-spin' /> : null}
                {t('Save')}
              </Button>
            </div>

            {/* 优先级说明：这条入口与规则一（超时长改派豆包）区间重叠，
                规则七先接。不写清楚，运营会以为「勾了也没生效」。 */}
            <p className='mt-2 text-[11px] text-amber-600'>
              {t(
                'This entry takes priority over the duration rule: long requests are rerouted here first and will not reach the Doubao link.'
              )}
            </p>
          </div>
        </div>
      </div>

      {/* 关键词表：内容入口的判定依据。 */}
      <div className='border-border/50 mt-3 flex flex-col gap-2 border-t pt-3'>
        <div className='flex items-center justify-between gap-2'>
          <Label
            htmlFor='ecommerce-keywords'
            className='text-foreground text-xs font-semibold'
          >
            {t('E-commerce keywords')}
          </Label>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            disabled={busy}
            onClick={() => void handleKeywordsReset()}
          >
            {t('Restore defaults')}
          </Button>
        </div>
        <Textarea
          id='ecommerce-keywords'
          value={keywordsInput}
          disabled={busy}
          onChange={(event) => setKeywordsInput(event.target.value)}
          rows={5}
          placeholder={t('One keyword per line')}
          className='font-mono text-xs'
        />
        <div className='flex items-center justify-between gap-2'>
          <p className='text-muted-foreground text-[11px] leading-relaxed'>
            {t(
              'Case-insensitive substring match. A prompt hits when it contains any of these words.'
            )}
          </p>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={busy}
            onClick={() => void handleKeywordsSave()}
          >
            {busy ? <Loader2 className='h-3.5 w-3.5 animate-spin' /> : null}
            {t('Save')}
          </Button>
        </div>
      </div>

      {/* 抽屉是表单，保存才生效；这些开关是即时 POST。 */}
      <p className='text-muted-foreground mt-2 text-[11px]'>
        {t('Takes effect immediately. No need to save.')}
      </p>
      <p className='text-muted-foreground mt-1 text-[11px]'>
        {t(
          'Pricing is unchanged: customers are still billed for the model they requested.'
        )}
      </p>
      <p className='text-muted-foreground mt-1 text-[11px]'>
        {t('Requests with a reference video are never rerouted by this rule.')}
      </p>
    </div>
  )
}
