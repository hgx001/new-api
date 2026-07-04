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
import { useTranslation } from 'react-i18next'

import { useStatus } from '@/hooks/use-status'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

export function Hero(props: HeroProps) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const docsUrl =
    (status?.docs_link as string | undefined) || 'https://docs.newapi.pro'
  const isExternalDocs = docsUrl.startsWith('http')
  const docsButtonClassName =
    'rounded-full border border-[#c8e8dc] bg-white px-6 py-2.5 text-sm font-medium text-[#3a8f7a] transition-colors hover:bg-[#f5f9f7]'

  return (
    <section className='relative z-10 overflow-hidden bg-[#f5f9f7] px-6 pt-28 pb-16 md:pt-36 md:pb-24'>
      <div className='mx-auto grid max-w-6xl grid-cols-1 items-center gap-12 lg:grid-cols-2'>
        {/* Left Column */}
        <div>
          <span className='inline-flex items-center gap-1.5 rounded-full bg-[#e8f4f0] px-3 py-1.5 text-xs font-medium text-[#3a8f7a]'>
            <span className='relative flex size-1.5'>
              <span className='absolute inline-flex h-full w-full animate-ping rounded-full bg-[#7dd3c0]' />
              <span className='relative inline-flex size-1.5 rounded-full bg-[#3a8f7a]' />
            </span>
            {t('Enterprise SLA Guarantee')}
          </span>

          <h1 className='mt-6 text-4xl font-extrabold leading-tight text-[#1a4a3f] md:text-5xl'>
            {t('Support Listed Enterprises')}
            <br />
            {t('AI Scale Deployment')}
          </h1>

          <p className='mt-5 max-w-xl text-base leading-relaxed text-[#5a7a72]'>
            {t(
              'heibaidao enterprise gateway description'
            )}
          </p>

          <div className='mt-8 flex flex-wrap gap-3'>
            {props.isAuthenticated ? (
              <Link
                to='/dashboard'
                className='rounded-full bg-[#7dd3c0] px-6 py-2.5 text-sm font-medium text-white transition-colors hover:bg-[#6bc9b4]'
              >
                {t('Go to Dashboard')} →
              </Link>
            ) : (
              <Link
                to='/sign-up'
                className='rounded-full bg-[#7dd3c0] px-6 py-2.5 text-sm font-medium text-white transition-colors hover:bg-[#6bc9b4]'
              >
                {t('Try Now')} →
              </Link>
            )}
            {isExternalDocs ? (
              <a
                href={docsUrl}
                target='_blank'
                rel='noopener noreferrer'
                className={docsButtonClassName}
              >
                {t('Technical Docs')}
              </a>
            ) : (
              <Link to={docsUrl} className={docsButtonClassName}>
                {t('Technical Docs')}
              </Link>
            )}
          </div>

          <div className='mt-10'>
            <p className='text-xs text-[#8aa89e]'>
              {t('Trusted by leading enterprises')}
            </p>
            <div className='mt-3 flex flex-wrap gap-6 text-sm font-semibold text-[#5a7a72]'>
              <span>OPENAI</span>
              <span>ANTHROPIC</span>
              <span>GOOGLE</span>
              <span>GROK</span>
            </div>
          </div>
        </div>

        {/* Right Column: Metrics card placeholder */}
        <div className='rounded-3xl bg-white p-6 shadow-[0_20px_60px_rgba(120,180,160,0.15)]'>
          <div className='flex items-center gap-2'>
            <span className='size-2 rounded-full bg-[#7dd3c0]' />
            <span className='font-mono text-xs text-[#5a7a72]'>
              system_status: active
            </span>
          </div>
          <div className='mt-4 grid grid-cols-2 gap-4'>
            <div className='rounded-2xl bg-[#f5f9f7] p-4'>
              <p className='text-xs text-[#8aa89e]'>{t('Model Coverage')}</p>
              <p className='mt-1 text-2xl font-bold text-[#1a4a3f]'>--</p>
            </div>
            <div className='rounded-2xl bg-[#f5f9f7] p-4'>
              <p className='text-xs text-[#8aa89e]'>{t('Enterprise Clients')}</p>
              <p className='mt-1 text-2xl font-bold text-[#1a4a3f]'>--</p>
            </div>
            <div className='rounded-2xl bg-[#f5f9f7] p-4'>
              <p className='text-xs text-[#8aa89e]'>{t('Uptime SLA')}</p>
              <p className='mt-1 text-2xl font-bold text-[#1a4a3f]'>99.99%</p>
            </div>
            <div className='rounded-2xl bg-[#f5f9f7] p-4'>
              <p className='text-xs text-[#8aa89e]'>{t('Daily Requests')}</p>
              <p className='mt-1 text-2xl font-bold text-[#1a4a3f]'>--</p>
            </div>
          </div>
          <div className='mt-4 h-32 rounded-2xl bg-gradient-to-br from-[#e8f4f0] to-[#f5f9f7]' />
        </div>
      </div>
    </section>
  )
}
