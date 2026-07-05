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
import { Link } from '@tanstack/react-router'

import type { TutorialSection } from '../content'
import { RenderContent } from './render-content'

export function DocContent({
  title,
  sections,
}: {
  title: string
  sections: TutorialSection[]
}) {
  return (
    <article className='min-w-0 max-w-3xl flex-1 px-4 pb-20 pt-6 md:px-8 lg:pt-8'>
      <div className='mb-6 flex items-center gap-2 text-sm text-[#8aa89e]'>
        <Link to='/' className='hover:text-[#3a8f7a]'>
          首页
        </Link>
        <span>/</span>
        <Link to='/tutorials' className='hover:text-[#3a8f7a]'>
          接入教程
        </Link>
        <span>/</span>
        <span className='text-[#5a7a72]'>{title}</span>
      </div>

      <h1 className='mb-8 text-3xl font-bold text-[#1a4a3f] md:text-4xl'>
        {title}
      </h1>

      <div className='prose-doc'>
        {sections.map((section) => (
          <section key={section.id} id={section.id} className='scroll-mt-28'>
            <h2 className='group relative mt-12 mb-4 flex items-center text-2xl font-bold text-[#1a4a3f]'>
              <a
                href={`#${section.id}`}
                className='mr-2 text-[#c8e8dc] opacity-0 transition-opacity group-hover:opacity-100'
                aria-label='链接到本节'
              >
                #
              </a>
              {section.title}
            </h2>
            {section.content && <RenderContent nodes={section.content} />}
          </section>
        ))}
      </div>
    </article>
  )
}
