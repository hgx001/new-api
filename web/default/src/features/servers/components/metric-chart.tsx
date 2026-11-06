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

import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import type { ServerMetricPoint } from '../types'

interface MetricChartProps {
  title: string
  points: ServerMetricPoint[]
  /** 数值上限，曲线按此归一化 */
  max?: number
  /** 单位，例如 % 或 Mbps */
  unit?: string
  /** 折线颜色 */
  color?: string
}

/**
 * 监控指标曲线（自绘 SVG，不用 recharts）。
 *
 * 为什么不用图表库：
 *   1. recharts 的 ResponsiveContainer 依赖 DOM 测量，SSR 渲染不出曲线，
 *      我们的组件测试是 node:test + renderToStaticMarkup，自绘 SVG 才能被断言；
 *   2. 面板里只有一条折线 + 一个面积填充，recharts 的体积与配置成本不划算；
 *   3. 曲线数据是确定性生成的，自绘不会引入 hydration 不一致。
 */
export function MetricChart({
  title,
  points,
  max = 100,
  unit = '%',
  color = '#3a8f7a',
}: MetricChartProps) {
  const { t } = useTranslation()

  const width = 320
  const height = 96
  const padding = 6

  const values = points.map((point) => point.value)
  const peak = values.length ? Math.max(...values) : 0
  const average = values.length
    ? Math.round(values.reduce((sum, value) => sum + value, 0) / values.length)
    : 0

  // 全 0（停机实例）时不画线，否则会退化成一条贴底的横线，看着像「有负载」
  const isFlat = peak === 0
  const step =
    points.length > 1 ? (width - padding * 2) / (points.length - 1) : 0
  const scale = (value: number) => {
    const ratio = max > 0 ? Math.min(1, value / max) : 0
    return height - padding - ratio * (height - padding * 2)
  }

  const line = points
    .map((point, index) => {
      const x = padding + index * step
      const y = scale(point.value)
      return `${index === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`
    })
    .join(' ')
  const area = points.length
    ? `${line} L${(padding + (points.length - 1) * step).toFixed(1)} ${height - padding} L${padding} ${height - padding} Z`
    : ''

  return (
    <div className='border-border/70 rounded-xl border p-4'>
      <div className='flex items-baseline justify-between gap-3'>
        <span className='text-muted-foreground text-xs font-medium'>
          {title}
        </span>
        <span className='text-[11px] font-semibold text-[#1a4a3f]'>
          {isFlat
            ? t('No data')
            : t('Peak {{value}}{{unit}} · Avg {{avg}}{{unit}}', {
                value: peak,
                unit,
                avg: average,
              })}
        </span>
      </div>

      <svg
        viewBox={`0 0 ${width} ${height}`}
        className='mt-3 h-24 w-full'
        role='img'
        aria-label={title}
        preserveAspectRatio='none'
      >
        <defs>
          <linearGradient id={`fill-${title}`} x1='0' y1='0' x2='0' y2='1'>
            <stop offset='0%' stopColor={color} stopOpacity='0.28' />
            <stop offset='100%' stopColor={color} stopOpacity='0.02' />
          </linearGradient>
        </defs>
        {!isFlat && (
          <>
            <path d={area} fill={`url(#fill-${title})`} />
            <path
              d={line}
              fill='none'
              stroke={color}
              strokeWidth='2'
              strokeLinecap='round'
              strokeLinejoin='round'
              vectorEffect='non-scaling-stroke'
            />
          </>
        )}
        {isFlat && (
          <line
            x1={padding}
            y1={height - padding}
            x2={width - padding}
            y2={height - padding}
            stroke='currentColor'
            className='text-border'
            strokeWidth='1'
          />
        )}
      </svg>

      <div className='text-muted-foreground mt-1 flex justify-between text-[10px]'>
        <span>{points.at(0)?.label ?? ''}</span>
        <span>{points.at(-1)?.label ?? ''}</span>
      </div>
    </div>
  )
}

/** 指标条：一条水平进度条 + 数值 */
const TONE_CLASS = {
  default: 'bg-[#3a8f7a]',
  warning: 'bg-amber-500',
  danger: 'bg-red-500',
} as const

export function MetricBar({
  label,
  value,
  max = 100,
  unit = '%',
  tone,
}: {
  label: string
  value: number
  max?: number
  unit?: string
  tone?: 'default' | 'warning' | 'danger'
}) {
  const ratio = max > 0 ? Math.min(100, (value / max) * 100) : 0
  // 用查表而不是嵌套三元：三种色调的映射一眼能看完，
  // 嵌套三元在这个文件里已经触发过 lint 规则
  const toneClass = cn(
    'h-1.5 rounded-full transition-all',
    TONE_CLASS[tone ?? 'default']
  )
  return (
    <div>
      <div className='flex items-baseline justify-between gap-2'>
        <span className='text-muted-foreground text-xs'>{label}</span>
        <span className='text-xs font-medium text-[#1a4a3f]'>
          {value}
          {unit}
        </span>
      </div>
      <div className='bg-muted mt-1.5 h-1.5 overflow-hidden rounded-full'>
        <div className={toneClass} style={{ width: `${ratio.toFixed(1)}%` }} />
      </div>
    </div>
  )
}
