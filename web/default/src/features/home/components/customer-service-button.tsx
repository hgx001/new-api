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
import { useTranslation } from 'react-i18next'
import { SiWechat } from 'react-icons/si'

import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'

// QR code image lives in `web/default/public/` and is served from the site root.
const CUSTOMER_SERVICE_QR_SRC = '/customer-service-wechat.jpg'

/**
 * Floating button anchored to the bottom-right of the homepage. Tapping it
 * opens a popover with the WeChat customer-service QR code so visitors can
 * scan it to add support. Visible only on the default home layout.
 */
export function CustomerServiceButton() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <div className='fixed right-4 bottom-4 z-40 md:right-6 md:bottom-6'>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <button
              type='button'
              aria-label={t('Scan to contact')}
              aria-expanded={open}
              className={cn(
                'flex size-12 items-center justify-center rounded-full shadow-lg transition-all',
                'bg-[#3a8f7a] text-white hover:bg-[#337d6c] hover:shadow-xl',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#3a8f7a] focus-visible:ring-offset-2',
                'md:size-14'
              )}
            />
          }
        >
          <SiWechat className='size-6 md:size-7' aria-hidden='true' />
        </PopoverTrigger>
        <PopoverContent
          align='end'
          side='top'
          sideOffset={8}
          className='w-64 p-4'
        >
          <PopoverTitle className='text-sm font-semibold text-[#1a4a3f]'>
            {t('WeChat support team')}
          </PopoverTitle>
          <PopoverDescription className='text-xs text-[#5a7a72]'>
            {t('Scan the QR code with WeChat to add customer service')}
          </PopoverDescription>
          <img
            src={CUSTOMER_SERVICE_QR_SRC}
            alt={t('WeChat customer service QR code')}
            className='mt-2 size-56 rounded-md bg-white object-contain'
            loading='lazy'
          />
        </PopoverContent>
      </Popover>
    </div>
  )
}
