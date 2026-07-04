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

import type { HomeMetrics } from '../../types'

interface StatsProps {
  metrics?: HomeMetrics
}

export function Stats({ metrics }: StatsProps) {
  const { t } = useTranslation()

  const items = [
    { label: t('API Availability'), value: metrics?.availability ?? '99.99%' },
    { label: t('Throughput'), value: `${metrics?.throughput ?? '1.2M+'} ${metrics?.throughput_unit ?? 'RPM'}` },
    { label: t('Average Latency'), value: metrics?.latency ?? '24ms' },
    { label: t('End-to-End Encryption'), value: metrics?.encryption ?? 'AES-256' },
  ]

  return (
    <div className='rounded-3xl bg-white p-6 shadow-[0_20px_60px_rgba(120,180,160,0.15)]'>
      <div className='flex items-center gap-2'>
        <span className='size-2 rounded-full bg-[#7dd3c0]' />
        <span className='font-mono text-xs text-[#5a7a72]'>system_status: {metrics?.system_status ?? 'active'}</span>
      </div>
      <div className='mt-4 grid grid-cols-2 gap-4'>
        {items.map((item) => (
          <div key={item.label} className='rounded-2xl bg-[#f5f9f7] p-4'>
            <div className='text-xs text-[#8aa89e]'>{item.label}</div>
            <div className='mt-1 text-xl font-bold text-[#3a8f7a]'>{item.value}</div>
          </div>
        ))}
      </div>
      <div className='mt-4 flex items-center gap-2 rounded-xl bg-[#f0f7f5] p-3'>
        <span className='size-4 rounded bg-[#a8d8c8]' />
        <span className='text-xs text-[#5a7a72]'>{metrics?.certification ?? 'ISO 27001'}</span>
      </div>
    </div>
  )
}
