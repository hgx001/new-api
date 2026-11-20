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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getSuccessRateDotClass } from '@/features/performance-metrics/lib/format'
import type { TaskHealthBucket, TaskHealthModel } from '@/features/performance-metrics/types'
import { cn } from '@/lib/utils'

import { splitTaskDuration } from '../lib/task-health'

// ---------------------------------------------------------------------------
// 模型健康度面板
// ---------------------------------------------------------------------------
//
// 展示每个模型「最近一段时间的请求成功率 + 平均完成时间 + 24 小时样本趋势」。
//
// 三个数字的口径（后端 model.GetTaskHealthSummary 保证一致）：
//   - 成功率 = 成功数 / (成功数 + 失败数)，**在途任务不进分母**
//   - 平均完成时间 = 成功任务的 (finish_time - submit_time) 均值
//   - 趋势条 = 每小时一个色块，绿色=全成功、黄色=部分失败、红色=全失败、灰色=无样本
//
// 关键区分：**无样本 ≠ 0%**。灰色块表示那个时段没有已完成任务，
// 画成红色会让用户以为模型全挂了。

const TREND_BAR_COUNT = 24

type TrendBar = {
  key: string
  rate: number | null
  bucket: TaskHealthBucket | null
}

/**
 * 把后端返回的稀疏桶补齐成固定长度的时间轴。
 *
 * 后端只返回**有样本**的桶（稀疏），前端按时间轴补空位：
 * 空位画灰色。这样色块条的时间位置才对得上，不会把「早上有流量、晚上没流量」
 * 挤成连续的一段，看上去像是一直有流量。
 */
function buildTrendBars(
  buckets: TaskHealthBucket[],
  bucketSeconds: number,
  count: number
): TrendBar[] {
  const sorted = [...buckets].sort((a, b) => a.bucket_ts - b.bucket_ts)
  const byTs = new Map<number, TaskHealthBucket>()
  for (const bucket of sorted) {
    byTs.set(bucket.bucket_ts, bucket)
  }

  if (sorted.length === 0) {
    return Array.from({ length: count }, (_, index) => ({
      key: `empty-${index}`,
      rate: null,
      bucket: null,
    }))
  }

  const lastTs = sorted.at(-1)?.bucket_ts ?? 0
  const bars: TrendBar[] = []
  for (let index = count - 1; index >= 0; index--) {
    const ts = lastTs - index * bucketSeconds
    const bucket = byTs.get(ts) ?? null
    bars.push({
      key: `${ts}`,
      rate: bucket ? bucket.success_rate : null,
      bucket,
    })
  }
  return bars
}

/** 单个色块的颜色。无样本走灰色，其余按成功率分级。 */
function barClass(rate: number | null): string {
  if (rate === null) return 'bg-muted-foreground/15'
  return getSuccessRateDotClass(rate)
}

function HealthMetric(props: {
  label: string
  value: React.ReactNode
  valueClassName?: string
}) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground/70 text-[11px] leading-4'>
        {props.label}
      </div>
      <div
        className={cn(
          'font-mono text-base leading-6 font-semibold tabular-nums',
          props.valueClassName
        )}
      >
        {props.value}
      </div>
    </div>
  )
}

/**
 * 按当前语言拼装耗时文案。
 *
 * 中文是「16 分 44 秒」，英文是「16 minutes 44 seconds」——单位名必须走
 * i18n，否则中文界面里会夹一串英文。
 */
function useTaskDurationFormatter() {
  const { t } = useTranslation()
  return useMemo(
    () => (ms: number): string => {
      const parts = splitTaskDuration(ms)
      if (!parts) return '—'
      const chunks: string[] = []
      if (parts.hours > 0) {
        chunks.push(
          t('{{count}} hours', {
            count: parts.hours,
          })
        )
      }
      if (parts.hours > 0 || parts.minutes > 0) {
        chunks.push(
          t('{{count}} minutes', {
            count: parts.minutes,
          })
        )
      }
      chunks.push(
        t('{{count}} seconds', {
          count: parts.seconds,
        })
      )
      return chunks.join(' ')
    },
    [t]
  )
}

export interface ModelHealthPanelProps {
  /** 该模型的健康度数据；undefined = 没有这个模型的任务记录。 */
  health: TaskHealthModel | undefined
  /** 趋势色块的时间轴长度（小时）。 */
  barCount?: number
  /** 趋势桶宽度（秒），用于补齐稀疏桶。 */
  bucketSeconds?: number
  /** 紧凑模式：模型卡片里用，省掉副标题。 */
  compact?: boolean
  className?: string
}

export function ModelHealthPanel(props: ModelHealthPanelProps) {
  const { t } = useTranslation()
  const formatDuration = useTaskDurationFormatter()
  const barCount = props.barCount ?? TREND_BAR_COUNT
  const bucketSeconds = props.bucketSeconds ?? 3600

  const bars = useMemo(
    () => buildTrendBars(props.health?.buckets ?? [], bucketSeconds, barCount),
    [props.health?.buckets, bucketSeconds, barCount]
  )

  if (!props.health) {
    return null
  }

  const health = props.health
  const hasSamples = health.request_count > 0
  const successRate = health.success_rate

  return (
    <div className={cn('flex flex-col gap-2', props.className)}>
      <div className='grid grid-cols-2 gap-3'>
        <HealthMetric
          // ⚠️ 文案必须跟着**实际查询窗口**走：历史上这里写死「近 12 小时」，
          // 而 model-card-grid / model-details-performance 查的是 hours=24，
          // 同一块里还写着「24 小时样本趋势」，口径自相矛盾（2026-11-20 修）。
          // barCount 就是趋势条覆盖的小时数，直接拿它插值即不会漂。
          label={t('Success rate (last {{hours}} hours)', {
            hours: barCount,
          })}
          value={
            successRate === null
              ? '—'
              : `${successRate.toFixed(1)}%`
          }
          valueClassName={
            successRate === null
              ? 'text-muted-foreground'
              : 'text-emerald-600 dark:text-emerald-400'
          }
        />
        <HealthMetric
          label={t('Average completion time')}
          value={formatDuration(health.avg_duration_ms)}
          valueClassName='text-foreground'
        />
      </div>

      <div className='flex flex-col gap-1.5'>
        <div className='text-muted-foreground/70 flex items-center justify-between text-[11px]'>
          <span>{t('24-hour sample trend')}</span>
          <span>
            {hasSamples
              ? t('{{count}} completed requests', {
                  count: health.request_count,
                })
              : t('No completed requests yet')}
          </span>
        </div>

        <div className='flex items-end gap-[3px]' role='img' aria-label={t('24-hour sample trend')}>
          {bars.map((bar) => (
            <Tooltip key={bar.key}>
              <TooltipTrigger
                render={
                  <div
                    className={cn(
                      'h-4 flex-1 rounded-sm transition-opacity hover:opacity-80',
                      barClass(bar.rate)
                    )}
                  />
                }
              >
                <div className='sr-only'>
                  {bar.rate === null
                    ? t('No samples')
                    : `${bar.rate.toFixed(1)}%`}
                </div>
              </TooltipTrigger>
              <TooltipContent side='top' className='font-mono text-xs'>
                <div className='font-medium'>
                  {new Date(bar.bucket ? bar.bucket.bucket_ts * 1000 : 0)
                    .toLocaleString()
                    .slice(0, 16)}
                </div>
                {bar.rate === null ? (
                  <div>{t('No samples')}</div>
                ) : (
                  <>
                    <div>{`${bar.rate.toFixed(1)}%`}</div>
                    <div className='text-muted-foreground'>
                      {t('{{ok}} ok / {{failed}} failed', {
                        ok: bar.bucket?.success_count ?? 0,
                        failed: bar.bucket?.failure_count ?? 0,
                      })}
                    </div>
                    {bar.bucket && bar.bucket.avg_duration_ms > 0 && (
                      <div className='text-muted-foreground'>
                        {formatDuration(bar.bucket.avg_duration_ms)}
                      </div>
                    )}
                  </>
                )}
              </TooltipContent>
            </Tooltip>
          ))}
        </div>

        <div className='text-muted-foreground/60 flex items-center justify-between text-[10px]'>
          <span>{t('24 hours ago')}</span>
          <span>{t('Now')}</span>
        </div>
      </div>
    </div>
  )
}
