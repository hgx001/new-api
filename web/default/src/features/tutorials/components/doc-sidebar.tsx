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

function SidebarItem({
  section,
  activeId,
  level,
}: {
  section: TutorialSection
  activeId: string | null
  level: number
}) {
  const isActive = activeId === section.id
  return (
    <li className='my-0.5'>
      <a
        href={`#${section.id}`}
        className={`block rounded-md px-2 py-1 text-sm transition-colors ${
          level === 0
            ? 'font-medium text-[#1a4a3f]'
            : 'pl-6 text-[#5a7a72] hover:text-[#1a4a3f]'
        } ${isActive ? 'bg-[#e8f4f0] text-[#3a8f7a]' : 'hover:bg-[#f5f9f7]'}`}
      >
        {section.title}
      </a>
      {section.children && section.children.length > 0 && (
        <ul className='mt-0.5'>
          {section.children.map((child) => (
            <SidebarItem
              key={child.id}
              section={child}
              activeId={activeId}
              level={level + 1}
            />
          ))}
        </ul>
      )}
    </li>
  )
}

export function DocSidebar({ sections }: { sections: TutorialSection[] }) {
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
    <aside className='hidden w-64 shrink-0 lg:block'>
      <nav className='sticky top-24 max-h-[calc(100vh-8rem)] overflow-y-auto pr-2'>
        <div className='mb-3 px-2 text-xs font-semibold uppercase tracking-wider text-[#8aa89e]'>
          教程目录
        </div>
        <ul>
          {sections.map((section) => (
            <SidebarItem
              key={section.id}
              section={section}
              activeId={activeId}
              level={0}
            />
          ))}
        </ul>
      </nav>
    </aside>
  )
}
