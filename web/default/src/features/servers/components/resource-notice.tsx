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
import { ArrowUpRight, Radio } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SiWechat } from 'react-icons/si'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface ResourceNoticeProps {
  /** banner：购买页顶部通栏；inline：收银台内的紧凑版本 */
  variant?: 'banner' | 'inline'
  onContact: () => void
  className?: string
}

/**
 * 「资源紧张，支付前先联系管理员」的引导条。
 *
 * 刻意不用红色警告块：这不是故障，而是人工排期的正常流程。
 * 用暖琥珀 + 呼吸圆点表达「当前需要人工确认」，把「拦住支付」
 * 变成「先帮你锁一台机器」，语气是帮忙而不是报错。
 */
export function ResourceNotice({
  variant = 'banner',
  onContact,
  className,
}: ResourceNoticeProps) {
  const { t } = useTranslation()
  const isBanner = variant === 'banner'

  return (
    <div
      className={cn(
        'relative overflow-hidden rounded-2xl border border-[#f0d9a8]',
        'bg-gradient-to-r from-[#fffaf0] via-[#fffdf7] to-[#f4faf7]',
        isBanner ? 'px-5 py-4 md:px-6 md:py-5' : 'px-3.5 py-3',
        className
      )}
    >
      {/* 右上角一圈很淡的光晕，让提示条看起来像「状态卡」而不是警告条 */}
      <div
        aria-hidden='true'
        className='pointer-events-none absolute -top-16 -right-12 size-40 rounded-full bg-[#f6d99a]/20 blur-2xl'
      />

      <div className='relative flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
        <div className='flex min-w-0 items-start gap-3'>
          <span
            className={cn(
              'relative mt-0.5 flex shrink-0 items-center justify-center rounded-xl bg-[#fbe9c4] text-[#a86a12]',
              isBanner ? 'size-10' : 'size-8'
            )}
          >
            <Radio className={isBanner ? 'size-5' : 'size-4'} aria-hidden='true' />
            {/* 呼吸点：表示「正在人工调度」 */}
            <span className='absolute -top-0.5 -right-0.5 flex size-2.5'>
              <span className='absolute inline-flex size-full animate-ping rounded-full bg-[#e59a2b] opacity-60' />
              <span className='relative inline-flex size-2.5 rounded-full bg-[#e59a2b]' />
            </span>
          </span>

          <div className='min-w-0'>
            <div className='flex flex-wrap items-center gap-2'>
              <p
                className={cn(
                  'font-semibold text-[#7a4f10]',
                  isBanner ? 'text-sm md:text-base' : 'text-xs'
                )}
              >
                {t('Server capacity is limited right now')}
              </p>
              <span className='rounded-full border border-[#e8c98c] bg-white/70 px-2 py-0.5 text-[10px] font-medium text-[#a86a12]'>
                {t('Manual allocation')}
              </span>
            </div>
            <p
              className={cn(
                'mt-1 leading-relaxed text-[#8a6a3a]',
                isBanner ? 'text-xs md:text-sm' : 'text-[11px]'
              )}
            >
              {t(
                'Please contact the admin before paying to confirm availability. We will reserve the machine for you before you are charged.'
              )}
            </p>
          </div>
        </div>

        <Button
          type='button'
          size={isBanner ? 'default' : 'sm'}
          onClick={onContact}
          className={cn(
            'shrink-0 rounded-xl bg-[#a86a12] text-white hover:bg-[#8d580b]',
            !isBanner && 'h-8 text-xs'
          )}
        >
          <SiWechat className='size-4' aria-hidden='true' />
          {t('Contact admin')}
          <ArrowUpRight className='size-3.5' aria-hidden='true' />
        </Button>
      </div>
    </div>
  )
}
