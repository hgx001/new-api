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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { getVideoRoutePolicy } from '../api'

type RouteStatusBadgeProps = {
  /** 当前渠道绑定的模型（逗号分隔）。 */
  channelModels: string
}

/**
 * 渠道列表里的「改派已暂停」角标。
 *
 * 为什么必须有它：按钮最怕的不是找不到，是**忘了按**。运营不该为了知道
 * 「现在有没有在改派」而点开每一条渠道的抽屉 —— 状态要能在一屏里看见。
 *
 * 只在本渠道承载目标模型时显示：渠道 20 上还挂着另外 7 个与豆包无关的
 * 模型，给它们也挂个「改派已暂停」的角标只会制造噪音。
 */
export function RouteStatusBadge({ channelModels }: RouteStatusBadgeProps) {
  const { t } = useTranslation()
  const { data } = useQuery({
    // 所有行共用一个 key，react-query 会去重，整张表只发一次请求。
    queryKey: ['video-route-policy'],
    queryFn: getVideoRoutePolicy,
    staleTime: 30_000,
    retry: false,
  })

  if (!data?.paused || !data.target_model) {
    return null
  }

  const models = channelModels
    .split(',')
    .map((model) => model.trim())
    .filter(Boolean)
  if (!models.includes(data.target_model)) {
    return null
  }

  let reason = t('Paused manually')
  if (data.pause_source === 'auto') {
    reason = t('Paused automatically: {{reason}}', {
      reason: data.pause_reason || t('Channel daily quota exhausted'),
    })
  }

  return (
    <TooltipProvider delay={100}>
      <Tooltip>
        <TooltipTrigger render={<span />}>
          <StatusBadge
            label={t('Routing paused')}
            variant='warning'
            size='sm'
            copyable={false}
          />
        </TooltipTrigger>
        <TooltipContent side='bottom' className='max-w-xs'>
          <div className='space-y-1 text-xs'>
            <div>{reason}</div>
            {data.paused_until_text && (
              <div>{t('Until {{time}}', { time: data.paused_until_text })}</div>
            )}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
