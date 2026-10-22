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
import { Cpu, Server } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

export type HomeTab = 'ai' | 'servers'

interface HomeTabBarProps {
  value: HomeTab
  onValueChange: (value: HomeTab) => void
}

const TABS: { id: HomeTab; labelKey: string; icon: typeof Cpu }[] = [
  { id: 'ai', labelKey: 'AI Gateway', icon: Cpu },
  { id: 'servers', labelKey: 'Cloud Servers', icon: Server },
]

export function HomeTabBar({ value, onValueChange }: HomeTabBarProps) {
  const { t } = useTranslation()

  return (
    <div className='border-border/60 bg-background/85 sticky top-0 z-40 border-b pt-16 backdrop-blur-md'>
      <div className='mx-auto max-w-6xl px-4 py-2.5'>
        <div className='bg-muted inline-flex items-center gap-1 rounded-full p-1'>
          {TABS.map((tab) => {
            const active = value === tab.id
            return (
              <button
                key={tab.id}
                type='button'
                onClick={() => onValueChange(tab.id)}
                className={cn(
                  'inline-flex items-center gap-1.5 rounded-full px-4 py-1.5 text-sm font-medium transition-all',
                  active
                    ? 'bg-[#7dd3c0] text-[#06231d] shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                )}
              >
                <tab.icon className='size-3.5' />
                {t(tab.labelKey)}
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}
