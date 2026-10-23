import {
  ArrowRight,
  Gauge,
  Globe2,
  Server,
  ShieldCheck,
  Zap,
} from 'lucide-react'
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

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface ServerHeroProps {
  ownedCount: number
  /** 嵌入首页 Tab 时为 true：顶部已有 Tab 栏，不再为 fixed header 预留空白 */
  embedded?: boolean
  onScrollToConfig: () => void
  onScrollToOwned: () => void
}

const HIGHLIGHTS = [
  { icon: Zap, valueKey: '5 minutes', labelKey: 'Provisioning' },
  { icon: ShieldCheck, valueKey: '99.9%', labelKey: 'Uptime SLA' },
  { icon: Globe2, valueKey: 'BGP', labelKey: 'Multi-line network' },
  { icon: Gauge, valueKey: '100 Mbps', labelKey: 'Peak bandwidth' },
] as const

export function ServerHero({
  ownedCount,
  embedded = false,
  onScrollToConfig,
  onScrollToOwned,
}: ServerHeroProps) {
  const { t } = useTranslation()

  return (
    <section
      className={cn(
        'relative overflow-hidden bg-gradient-to-br from-[#08201c] via-[#0c3129] to-[#0a2436] px-6 pb-14 text-white md:pb-16',
        embedded ? 'pt-8 md:pt-10' : 'pt-10 md:pt-14'
      )}
    >
      {/* 光晕与网格纹理 */}
      <div className='pointer-events-none absolute -top-32 -left-20 size-96 rounded-full bg-[#7dd3c0]/15 blur-3xl' />
      <div className='pointer-events-none absolute -right-24 -bottom-32 size-[28rem] rounded-full bg-sky-500/10 blur-3xl' />
      <div
        className='pointer-events-none absolute inset-0 opacity-[0.07]'
        style={{
          backgroundImage:
            'linear-gradient(to right, #ffffff 1px, transparent 1px), linear-gradient(to bottom, #ffffff 1px, transparent 1px)',
          backgroundSize: '56px 56px',
        }}
      />

      <div className='relative mx-auto max-w-6xl'>
        <div className='max-w-3xl'>
          <Badge
            variant='secondary'
            className='mb-6 border border-[#7dd3c0]/30 bg-[#7dd3c0]/10 text-[#9fe3d3] backdrop-blur'
          >
            <Server className='mr-1.5 size-3.5' />
            {t('New: cloud servers are now available')}
          </Badge>

          <h1 className='text-4xl leading-[1.15] font-extrabold tracking-tight md:text-5xl'>
            {t('Reliable cloud servers for your next project')}
          </h1>

          <p className='mt-5 max-w-2xl text-base leading-relaxed text-white/70 md:text-lg'>
            {t(
              'Pick a region, a plan, the specs and the bandwidth you need. Pay for 6 months, 1 year or 3 years, and get your server credentials right after the payment succeeds.'
            )}{' '}
            {t(
              'Need the machine for a few hours only? Switch to hourly billing and pick how many hours you need.'
            )}
          </p>

          <div className='mt-8 flex flex-wrap items-center gap-3'>
            <Button
              size='lg'
              className='rounded-full bg-[#7dd3c0] px-6 text-[#06231d] hover:bg-[#6bc9b4]'
              onClick={onScrollToConfig}
            >
              {t('Configure your server')}
              <ArrowRight className='ml-2 size-4' />
            </Button>
            {ownedCount > 0 && (
              <Button
                size='lg'
                variant='outline'
                className='rounded-full border-white/25 bg-white/5 text-white hover:bg-white/10'
                onClick={onScrollToOwned}
              >
                {t('My servers ({{count}})', { count: ownedCount })}
              </Button>
            )}
          </div>
        </div>

        <div className='mt-12 grid grid-cols-2 gap-4 md:grid-cols-4'>
          {HIGHLIGHTS.map((item) => (
            <div
              key={item.labelKey}
              className='rounded-2xl border border-white/10 bg-white/[0.06] p-5 backdrop-blur-sm'
            >
              <item.icon className='size-5 text-[#7dd3c0]' />
              <div className='mt-3 text-xl font-bold'>{t(item.valueKey)}</div>
              <div className='mt-0.5 text-xs text-white/55'>
                {t(item.labelKey)}
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
