import { Loader2, PauseCircle, PlayCircle, Route } from 'lucide-react'
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
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import {
  getVideoRoutePolicy,
  pauseVideoRoute,
  resumeVideoRoute,
  type VideoRoutePolicy,
} from '../api'

type RouteSwitchCardProps = {
  /**
   * 当前渠道绑定的模型（逗号分隔）。
   *
   * 改派只跟承载**目标模型**的渠道有关：渠道 20 上还挂着另外 7 个与豆包
   * 无关的模型，在它们的抽屉里显示这个开关会让人以为按了会停掉整条渠道。
   */
  channelModels: string
}

type BusyKind = 'pause' | 'resume' | 'custom' | null

/**
 * 「跨模型改派」开关卡片。
 *
 * 为什么做成三个明确动作而不是一个 toggle：真实状态是「暂停到某个时刻」，
 * 不是开/关。toggle 关掉之后没人记得打开，就会长期跑在另一条链路上，
 * 而且没有任何报错 —— 这正是后端用 SecondsRulePausedUntil 而不是布尔值的原因。
 */
export function RouteSwitchCard({ channelModels }: RouteSwitchCardProps) {
  const { t } = useTranslation()
  const [policy, setPolicy] = useState<VideoRoutePolicy | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<BusyKind>(null)
  const [customUntil, setCustomUntil] = useState('')

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

  const targetModel = policy?.target_model ?? ''
  const isRelevant = useMemo(() => {
    if (!targetModel) return false
    return channelModels
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
      .includes(targetModel)
  }, [channelModels, targetModel])

  const applyPolicy = useCallback(
    async (
      kind: BusyKind,
      action: () => Promise<VideoRoutePolicy>,
      successText: string
    ) => {
      setBusy(kind)
      try {
        setPolicy(await action())
        toast.success(successText)
      } catch (error) {
        toast.error(
          (error as { response?: { data?: { message?: string } } })?.response
            ?.data?.message ?? t('Failed to update routing policy')
        )
      } finally {
        setBusy(null)
      }
    },
    [t]
  )

  const handlePauseToday = useCallback(() => {
    void applyPolicy(
      'pause',
      () => pauseVideoRoute(),
      t('Routing paused until end of today')
    )
  }, [applyPolicy, t])

  const handleResume = useCallback(() => {
    void applyPolicy('resume', () => resumeVideoRoute(), t('Routing resumed'))
  }, [applyPolicy, t])

  const handleCustomPause = useCallback(() => {
    const seconds = Math.floor(new Date(customUntil).getTime() / 1000)
    if (!customUntil || Number.isNaN(seconds)) {
      toast.error(t('Please pick a valid deadline'))
      return
    }
    if (seconds <= Math.floor(Date.now() / 1000)) {
      toast.error(t('Deadline must be in the future'))
      return
    }
    void applyPolicy(
      'custom',
      () => pauseVideoRoute(seconds),
      t('Routing paused until the selected time')
    )
  }, [applyPolicy, customUntil, t])

  if (loading || !policy || !isRelevant) {
    return null
  }

  let pauseSourceText = t('Paused')
  if (policy.pause_source === 'auto') {
    pauseSourceText = t('Paused automatically: {{reason}}', {
      reason: policy.pause_reason || t('Channel daily quota exhausted'),
    })
  } else if (policy.pause_source === 'manual') {
    pauseSourceText = t('Paused manually')
  }

  return (
    <div className='border-border/60 rounded-lg border p-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <span className='text-muted-foreground'>
            <Route className='h-3.5 w-3.5' />
          </span>
          <h4 className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
            {t('Cross-model routing')}
          </h4>
        </div>
        {policy.paused ? (
          <Badge
            variant='outline'
            className='border-amber-500/60 text-amber-600'
          >
            {t('Paused')}
          </Badge>
        ) : (
          <Badge
            variant='outline'
            className='border-emerald-500/60 text-emerald-600'
          >
            {t('Active')}
          </Badge>
        )}
      </div>

      <p className='text-muted-foreground mt-2 text-xs leading-relaxed'>
        {t('Requests of {{seconds}}s are routed to {{target}}', {
          seconds: policy.trigger_seconds,
          target: policy.target_model,
        })}
        {policy.paused ? ` · ${pauseSourceText}` : ''}
        {policy.paused && policy.paused_until_text
          ? ` · ${t('Until {{time}}', { time: policy.paused_until_text })}`
          : ''}
      </p>

      <div className='mt-3 flex flex-wrap items-center gap-2'>
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={busy !== null}
          onClick={handlePauseToday}
        >
          {busy === 'pause' ? (
            <Loader2 className='mr-1.5 h-3.5 w-3.5 animate-spin' />
          ) : (
            <PauseCircle className='mr-1.5 h-3.5 w-3.5' />
          )}
          {t('Pause until end of today')}
        </Button>
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={busy !== null || !policy.paused}
          onClick={handleResume}
        >
          {busy === 'resume' ? (
            <Loader2 className='mr-1.5 h-3.5 w-3.5 animate-spin' />
          ) : (
            <PlayCircle className='mr-1.5 h-3.5 w-3.5' />
          )}
          {t('Resume now')}
        </Button>
      </div>

      <div className='mt-2 flex flex-wrap items-center gap-2'>
        <Input
          type='datetime-local'
          value={customUntil}
          onChange={(event) => setCustomUntil(event.target.value)}
          className='h-8 w-auto min-w-[13rem] text-xs'
          aria-label={t('Pause until a specific time')}
        />
        <Button
          type='button'
          size='sm'
          variant='ghost'
          disabled={busy !== null || !customUntil}
          onClick={handleCustomPause}
        >
          {busy === 'custom' ? (
            <Loader2 className='mr-1.5 h-3.5 w-3.5 animate-spin' />
          ) : null}
          {t('Pause until a specific time')}
        </Button>
      </div>

      {/* 抽屉是表单，保存才生效；这个开关是即时 POST。不写清楚，
          运营按完按钮会再去找保存按钮，或者以为改了表单没保存。 */}
      <p className='text-muted-foreground mt-2 text-[11px]'>
        {t('Takes effect immediately. No need to save.')}
      </p>
    </div>
  )
}
