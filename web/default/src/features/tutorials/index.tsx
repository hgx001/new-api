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

import { CopyButton } from '@/components/copy-button'
import { PublicLayout } from '@/components/layout'

function CodeBlock({ value }: { value: string }) {
  return (
    <div className='group relative my-3 flex items-center justify-between gap-2 rounded-lg bg-[#0d2820] px-4 py-3'>
      <code className='overflow-x-auto font-mono text-sm text-[#e8f4f0]'>
        {value}
      </code>
      <CopyButton
        value={value}
        variant='ghost'
        size='sm'
        className='text-[#7dd3c0] hover:bg-[#1a4a3f]/40 hover:text-white'
        iconClassName='size-4'
        aria-label='Copy code'
      />
    </div>
  )
}

function StepNumber({ children }: { children: number }) {
  return (
    <span className='inline-flex size-7 shrink-0 items-center justify-center rounded-full bg-[#3a8f7a] text-sm font-bold text-white'>
      {children}
    </span>
  )
}

function StepSection({
  step,
  title,
  children,
}: {
  step: number
  title: string
  children: React.ReactNode
}) {
  return (
    <section className='flex flex-col gap-3'>
      <div className='flex items-center gap-3'>
        <StepNumber>{step}</StepNumber>
        <h2 className='text-lg font-bold text-[#1a4a3f]'>{title}</h2>
      </div>
      <div className='ml-10 text-sm leading-relaxed text-[#5a7a72]'>
        {children}
      </div>
    </section>
  )
}

export function Tutorials() {
  const { t } = useTranslation()
  // Use the current deployment origin so the tutorial always matches the
  // platform the user is actually visiting (e.g. https://api.heibaidao.cn).
  const platformUrl =
    typeof window !== 'undefined' ? window.location.origin : 'https://your-deployment.example.com'

  return (
    <PublicLayout showMainContainer={false} forceLightMode>
      <main className='mx-auto max-w-3xl px-4 pb-20 pt-28 md:pt-32'>
        <div className='mb-10'>
          <span className='inline-flex items-center gap-1.5 rounded-full bg-[#e8f4f0] px-3 py-1.5 text-xs font-medium text-[#3a8f7a]'>
            {t('Integration Tutorial')}
          </span>
          <h1 className='mt-4 text-3xl font-extrabold text-[#1a4a3f] md:text-4xl'>
            {t('Claude Code Installation')}
          </h1>
          <p className='mt-3 text-base text-[#5a7a72]'>
            {t(
              'This guide walks you through installing Claude Code and connecting it to this platform.'
            )}
          </p>
        </div>

        <div className='space-y-10'>
          <StepSection step={1} title={t('Prerequisites')}>
            <ul className='ml-4 list-disc space-y-1.5'>
              <li>
                {t('A valid API Key from this platform. ')}
                <Link
                  to='/keys'
                  className='font-medium text-[#3a8f7a] hover:underline'
                >
                  {t('Create a key here')} →
                </Link>
              </li>
              <li>{t('Node.js installed (download from nodejs.org)')}</li>
              <li>{t('Git installed (required on Windows)')}</li>
            </ul>
          </StepSection>

          <StepSection step={2} title={t('Install Claude Code')}>
            <p className='mb-2'>{t('Run the following command in your terminal:')}</p>
            <CodeBlock value='npm install -g @anthropic-ai/claude-code' />
            <p className='text-xs text-[#8aa89e]'>
              {t('Supported on macOS, Linux, WSL, Windows PowerShell and CMD.')}
            </p>
          </StepSection>

          <StepSection step={3} title={t('Configure Environment Variables')}>
            <p className='mb-2'>
              {t(
                'Set the base URL to this platform and the auth token to your API Key. Restart any open terminals afterwards.'
              )}
            </p>
            <p className='mt-2 font-semibold text-[#3a8f7a]'>
              {t('Option A: Command Line (Windows CMD / PowerShell)')}
            </p>
            <p className='mb-1 text-xs text-[#8aa89e]'>
              {t(
                'Use setx for permanent variables. Open a new terminal after running these.'
              )}
            </p>
            <CodeBlock value='setx ANTHROPIC_AUTH_TOKEN "sk-..."' />
            <CodeBlock value={`setx ANTHROPIC_BASE_URL "${platformUrl}"`} />
            <p className='mt-2 text-xs text-[#8aa89e]'>
              {t(
                'Replace sk-... with your actual API Key and the URL with this platform address.'
              )}
            </p>

            <p className='mt-4 font-semibold text-[#3a8f7a]'>
              {t('Option B: GUI (recommended for Windows)')}
            </p>
            <ol className='ml-4 list-decimal space-y-1.5'>
              <li>
                {t('Press Win + R, type sysdm.cpl, press Enter to open System Properties.')}
              </li>
              <li>{t('Switch to the Advanced tab, click Environment Variables...')}</li>
              <li>
                {t('Click New... under user variables. Name: ANTHROPIC_AUTH_TOKEN, Value: your API Key.')}
              </li>
              <li>
                {t(
                  'Click New... again. Name: ANTHROPIC_BASE_URL, Value: this platform address ({{url}}).',
                  { url: platformUrl }
                )}
              </li>
              <li>{t('Confirm all dialogs, then reopen any terminal windows.')}</li>
            </ol>

            <p className='mt-4 font-semibold text-[#3a8f7a]'>
              {t('Option C: macOS / Linux')}
            </p>
            <p className='mb-1 text-xs text-[#8aa89e]'>
              {t('Add these lines to your ~/.zshrc or ~/.bashrc, then run source ~/.zshrc')}
            </p>
            <CodeBlock value='export ANTHROPIC_AUTH_TOKEN="sk-..."' />
            <CodeBlock value={`export ANTHROPIC_BASE_URL="${platformUrl}"`} />
          </StepSection>

          <StepSection step={4} title={t('Start Using Claude Code')}>
            <p className='mb-2'>{t('Open a terminal in your project folder and launch:')}</p>
            <CodeBlock value='cd your-project' />
            <CodeBlock value='claude' />
            <p className='mt-3 text-xs text-[#8aa89e]'>
              {t(
                'Claude Code will read the environment variables and route requests through this platform.'
              )}
            </p>
          </StepSection>

          <StepSection step={5} title={t('What Claude Code Can Do')}>
            <ul className='ml-4 list-disc space-y-1.5'>
              <li>{t('Build features from natural language descriptions.')}</li>
              <li>{t('Debug and fix issues by analyzing your codebase.')}</li>
              <li>{t('Navigate any codebase and answer questions about it.')}</li>
              <li>{t('Automate tedious tasks like lint fixes and release notes.')}</li>
            </ul>
          </StepSection>
        </div>

        <div className='mt-12 rounded-xl border border-[#c8e8dc] bg-[#f5f9f7] p-5'>
          <h3 className='text-sm font-semibold text-[#1a4a3f]'>
            {t('Need help?')}
          </h3>
          <p className='mt-1 text-xs text-[#5a7a72]'>
            {t(
              'If you run into any issues, scan the WeChat QR code on the homepage to contact our support team.'
            )}
          </p>
        </div>
      </main>
    </PublicLayout>
  )
}
