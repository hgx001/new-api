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
import { useState } from 'react'

import { Sheet, SheetContent, SheetTrigger } from '@/components/ui/sheet'

import type { TutorialSection } from '../content'
import { DocOutline } from './doc-outline'
import { DocPagination, type PaginationItem } from './doc-pagination'
import { DocSidebar } from './doc-sidebar'
import { MenuIcon } from './menu-icon'

function MobileSidebar({ sections }: { sections: TutorialSection[] }) {
  const [open, setOpen] = useState(false)

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger className='mb-4 inline-flex h-8 items-center gap-2 rounded-md border border-[#e3eeea] bg-white px-3 text-sm font-medium text-[#5a7a72] transition-colors hover:bg-[#f5f9f7] hover:text-[#1a4a3f] lg:hidden'>
        <MenuIcon className='size-4' />
        教程目录
      </SheetTrigger>
      <SheetContent side='left' className='w-72 bg-white p-0'>
        <nav className='h-full overflow-y-auto p-5'>
          <div className='mb-3 text-xs font-semibold uppercase tracking-wider text-[#8aa89e]'>
            教程目录
          </div>
          <ul className='space-y-1'>
            {sections.map((section) => (
              <li key={section.id}>
                <a
                  href={`#${section.id}`}
                  onClick={() => setOpen(false)}
                  className='block rounded-md px-2 py-1.5 text-sm font-medium text-[#1a4a3f] hover:bg-[#f5f9f7]'
                >
                  {section.title}
                </a>
              </li>
            ))}
          </ul>
        </nav>
      </SheetContent>
    </Sheet>
  )
}

function DocIndex({
  prev,
  next,
  index,
}: {
  prev: PaginationItem
  next: PaginationItem
  index: { title: string; href: string }[]
}) {
  return (
    <div className='mt-12 border-t border-[#e3eeea] pt-8'>
      <p className='mb-4 text-xs text-[#8aa89e]'>修改于刚刚</p>
      <div className='mb-6 flex flex-col gap-2 text-sm'>
        <div className='flex items-center gap-2 text-[#5a7a72]'>
          <span className='text-[#8aa89e]'>上一页</span>
          <span className='font-medium'>{prev.title}</span>
        </div>
        <div className='flex items-center gap-2 text-[#5a7a72]'>
          <span className='text-[#8aa89e]'>下一页</span>
          <span className='font-medium'>{next.title}</span>
        </div>
      </div>
      <div className='rounded-lg border border-[#e3eeea] bg-[#f8fbfa] p-4'>
        <div className='mb-2 text-xs font-semibold uppercase tracking-wider text-[#8aa89e]'>
          页面索引
        </div>
        <ul className='grid grid-cols-1 gap-1 sm:grid-cols-2'>
          {index.map((item) => (
            <li key={item.href}>
              <a
                href={item.href}
                className='block rounded px-2 py-1 text-sm text-[#3a8f7a] hover:bg-[#e8f4f0] hover:underline'
              >
                {item.title}
              </a>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

export function DocLayout({
  sections,
  prev,
  next,
  index,
  children,
}: {
  sections: TutorialSection[]
  prev: PaginationItem
  next: PaginationItem
  index: { title: string; href: string }[]
  children: React.ReactNode
}) {
  return (
    <div className='relative mx-auto flex max-w-[90rem] justify-center px-4 lg:px-6'>
      <DocSidebar sections={sections} />

      <div className='flex min-w-0 flex-1 flex-col lg:flex-row lg:justify-center'>
        <main className='min-w-0 flex-1 lg:max-w-3xl xl:max-w-4xl'>
          <div className='py-4 lg:hidden'>
            <MobileSidebar sections={sections} />
          </div>
          {children}
          <div className='px-4 md:px-8'>
            <DocPagination prev={prev} next={next} />
            <DocIndex prev={prev} next={next} index={index} />
          </div>
        </main>
      </div>

      <DocOutline sections={sections} />
    </div>
  )
}
