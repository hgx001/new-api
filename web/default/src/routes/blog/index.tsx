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
import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'

function BlogPage() {
  const { t } = useTranslation()

  return (
    <PublicLayout>
      <div className='mx-auto max-w-2xl px-6 py-32 text-center'>
        <h1 className='text-2xl font-bold text-[#1a4a3f] md:text-3xl'>
          {t('Blog')}
        </h1>
        <p className='text-muted-foreground mt-4'>
          {t('Blog content coming soon')}
        </p>
      </div>
    </PublicLayout>
  )
}

export const Route = createFileRoute('/blog/')({
  component: BlogPage,
})
