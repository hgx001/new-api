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
import { Loader2, TriangleAlert } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  getVideoRoutePolicy,
  setTotalFallbackEnabled,
  type VideoRoutePolicy,
} from '../api'

type TotalFallbackCardProps = {
  /**
   * 当前渠道绑定的模型（逗号分隔）。
   *
   * 口径与电商卡一致：**源渠道和目标渠道都显示**。
   * 源渠道（承载 seedance2.5-特惠 / 活动）是运营按这个按钮的地方 ——
   * 「源链路挂了怎么办」本来就是源链路的经营判断；目标渠道
   * （承载 cp-seedance-2.5flash）也要显示，因为运营在那里看到流量
   * 全部涌过来时，需要能立刻关掉入口。
   */
  channelModels: string
}

/**
 * 「全量兜底」开关 —— 规则八。
 *
 * 与前七条规则**不是一类**：前七条解决「怎么调度更省/更适配」，
 * 这条解决「源链路（有赞）彻底不能用时，这两个模型还有没有出口」。
 *
 * 打开后 seedance2.5-特惠 / seedance2.5-活动 收到的所有请求无条件改派
 * CapCut —— 不看比例、不看提示词内容、不看首尾帧；目标按时长分档：
 * ≤{{shortMaxSeconds}} 秒走 cp-seedance-2.0，更长的走 cp-seedance-2.5flash。
 * 所有阈值都从后端状态读，前端不写死数字（两处各写一份必然漂）。
 *
 * 界面上必须写清的三件事（少一条运营就会误判）：
 *
 *  1. **只在救火时开**：开着就等于把这两个模型的全部流量挪到另一条上游，
 *     成本结构与交付形态都变了。所以打开要二次确认，关闭则即时生效
 *     （关掉是安全方向，不该也拦一道）。
 *  2. **物理能力边界仍然生效**：1080P / 2–3 秒 / 带首尾帧 /
 *     自适应比例带参考图 仍留在原链路 —— 改派过去必 400，
 *     那会把「链路挂了」伪装成「你的参数不合法」。
 *     （参考音视频 2026-11-15 起**不再**属于边界，CapCut 链路可透传。）
 *  3. **计费不变**：客户仍按他请求的模型（特惠/活动）付费。
 */
export function TotalFallbackCard({ channelModels }: TotalFallbackCardProps) {
  const { t } = useTranslation()
  const [policy, setPolicy] = useState<VideoRoutePolicy | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)

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

  const targetModel = policy?.total_fallback_target_model ?? ''
  const shortTargetModel = policy?.total_fallback_short_target_model ?? ''
  const shortMaxSeconds = policy?.total_fallback_short_max_seconds ?? 15
  const shortMaxRefImages =
    policy?.total_fallback_short_max_reference_images ?? 9
  const sourceModels = useMemo(
    () => (policy?.source_switches ?? []).map((item) => item.source_model),
    [policy?.source_switches]
  )

  const isRelevant = useMemo(() => {
    const models = channelModels
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
    if (models.length === 0) return false
    if (targetModel && models.includes(targetModel)) return true
    // 短档目标所在渠道也要显示卡片：运营在那边看到短请求涌进来时，
    // 需要能立刻关掉入口（与长档目标同理由）。
    if (shortTargetModel && models.includes(shortTargetModel)) return true
    return sourceModels.some((source) => models.includes(source))
  }, [channelModels, shortTargetModel, sourceModels, targetModel])

  const apply = useCallback(
    async (nextOn: boolean) => {
      setBusy(true)
      try {
        setPolicy(await setTotalFallbackEnabled(nextOn))
        toast.success(
          t(nextOn ? 'Total fallback is on' : 'Total fallback is off')
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

  const handleToggle = useCallback(
    (nextOn: boolean) => {
      // 打开要二次确认：一点就把这两个模型的全部流量挪走，
      // 误触的代价是真的钱（另一条上游的成本结构完全不同）。
      // 关闭不拦 —— 那是安全方向，多一步只会让人在救火时更慢。
      if (nextOn) {
        setConfirmOpen(true)
        return
      }
      void apply(false)
    },
    [apply]
  )

  if (loading || !policy || !isRelevant) {
    return null
  }

  const on = policy.total_fallback_enabled
  const toneClasses = on
    ? 'border-red-500/50 bg-red-500/[0.06]'
    : 'border-slate-500/40 bg-slate-500/[0.05]'
  const toneText = on ? 'text-red-600 dark:text-red-400' : 'text-slate-600'

  const sources = sourceModels.length
    ? sourceModels.join(' / ')
    : t('the configured source models')

  const detailVars = {
    sources,
    seconds: shortMaxSeconds,
    shortTarget: shortTargetModel,
    target: targetModel,
  }
  const detail = on
    ? t(
        'Every request to {{sources}} now goes to CapCut: up to {{seconds}}s → {{shortTarget}}, longer → {{target}}. Ratio, prompt and frames are ignored. Turn it off as soon as the source chain recovers.',
        detailVars
      )
    : t(
        'Turn on only when the source chain is completely unusable: every request to {{sources}} goes to CapCut — up to {{seconds}}s → {{shortTarget}}, longer → {{target}}, regardless of ratio, prompt or frames.',
        detailVars
      )

  return (
    <div className={cn('rounded-lg border p-3 transition-colors', toneClasses)}>
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-2'>
          <span className={cn('mt-0.5 shrink-0', toneText)}>
            <TriangleAlert className='h-4 w-4' />
          </span>
          <div className='min-w-0'>
            <h4 className='text-foreground text-sm font-semibold'>
              {t('Total fallback (all requests to CapCut)')}
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
            {t(on ? 'Total fallback is on' : 'Total fallback is off')}
          </span>
          <Switch
            checked={on}
            disabled={busy}
            onCheckedChange={handleToggle}
            className='h-6! w-11! data-checked:bg-red-600! data-[size=default]:h-6! data-[size=default]:w-11!'
            aria-label={t('Total fallback (all requests to CapCut)')}
          />
          {busy ? (
            <Loader2 className='text-muted-foreground h-3.5 w-3.5 animate-spin' />
          ) : null}
        </div>
      </div>

      {/* 不改派的几类必须在界面上说清，否则运营会以为「开了就全都过去了」，
          然后在客户拿到 400/503 时以为是开关没生效。
          ⚠️ 措辞是「仍留在原链路」而不是「CapCut 接不住」：这几类都是
          CapCut 链路的**物理边界**，不是运营意志能绕过的。
          ⚠️ 2026-11-16 修正：参考音视频已不在列表里 —— 2026-11-15 起
          CapCut 链路放开音视频透传（见 middleware/seedance_route_ecommerce.go
          的 prepareCapcutRoute ①），带参考音视频的请求照常改派；
          反过来**首尾帧**才是真的接不住（CapCut 只有参考图语义）。 */}
      <p className='text-muted-foreground mt-2 text-[11px]'>
        {t(
          'These requests still stay on the source chain: 1080P, 2–3s, requests with first/last frames, and adaptive ratio combined with reference images.'
        )}
      </p>
      {/* 短档的素材闸门：超过阈值张数时仍走长档，而不是丢回已挂掉的源链路。
          这条必须写出来的理由与上面一样 —— 运营看到「短请求怎么没走 2.0」
          时，得能在这里找到答案，而不是去查数据库。 */}
      <p className='text-muted-foreground mt-1 text-[11px]'>
        {t(
          'Short requests with more than {{count}} reference images go to {{target}} instead of {{shortTarget}}.',
          {
            count: shortMaxRefImages,
            target: targetModel,
            shortTarget: shortTargetModel,
          }
        )}
      </p>
      <p className='text-muted-foreground mt-1 text-[11px]'>
        {t(
          'Pricing is unchanged: customers are still billed for the model they requested.'
        )}
      </p>
      <p className='text-muted-foreground mt-1 text-[11px]'>
        {t(
          'Works even when the routing master switch is off — this is an emergency switch.'
        )}
      </p>
      <p className='text-muted-foreground mt-1 text-[11px]'>
        {t('Takes effect immediately. No need to save.')}
      </p>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Route all traffic of these models to CapCut?')}
            </AlertDialogTitle>
            <AlertDialogDescription
              render={<div className='space-y-3 text-sm' />}
            >
              <div>
                {t(
                  'Every request to {{sources}} will be routed to CapCut: up to {{seconds}}s → {{shortTarget}}, longer → {{target}}.',
                  detailVars
                )}
              </div>
              <div>
                {t(
                  'Only do this when the source chain is unusable — this moves all traffic of these models to another upstream.'
                )}
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                void apply(true)
              }}
            >
              {t('Turn on total fallback')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
