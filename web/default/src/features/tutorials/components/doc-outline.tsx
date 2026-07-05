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
import { useEffect, useState } from 'react'

import type { TutorialSection } from '../content'

export function DocOutline({ sections }: { sections: TutorialSection[] }) {
  const [activeId, setActiveId] = useState<string | null>(null)

  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            setActiveId(entry.target.id)
          }
        })
      },
      { rootMargin: '-20% 0px -60% 0px', threshold: 0 }
    )

    sections.forEach((section) => {
      const el = document.querySelector(`#${section.id}`)
      if (el) observer.observe(el)
    })

    return () => observer.disconnect()
  }, [sections])

  return (
    <aside className='hidden w-56 shrink-0 xl:block'>
      <nav className='sticky top-24 max-h-[calc(100vh-8rem)] overflow-y-auto border-l border-[#e3eeea] pl-4'>
        <div className='mb-3 text-xs font-semibold uppercase tracking-wider text-[#8aa89e]'>
          本页目录
        </div>
        <ul className='space-y-1.5'>
          {sections.map((section) => (
            <li key={section.id}>
              <a
                href={`#${section.id}`}
                className={`block text-sm transition-colors ${
                  activeId === section.id
                    ? 'font-medium text-[#3a8f7a]'
                    : 'text-[#5a7a72] hover:text-[#1a4a3f]'
                }`}
              >
                {section.title}
              </a>
            </li>
          ))}
        </ul>
      </nav>
    </aside>
  )
}
