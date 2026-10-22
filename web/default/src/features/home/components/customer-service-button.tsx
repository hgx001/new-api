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
import { Download, Maximize2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SiWechat } from 'react-icons/si'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'

// The WeChat QR lives in `web/default/public/` and is served from the site root.
// It is a vector asset re-encoded from the original card artwork
// (`customer-service-wechat.jpg`), so it stays crisp at any zoom level instead
// of turning blurry when scaled up. The PNG twin exists because mobile browsers
// cannot long-press-to-save an SVG.
const CUSTOMER_SERVICE_QR_SVG = '/customer-service-wechat-qr.svg'
const CUSTOMER_SERVICE_QR_PNG = '/customer-service-wechat-qr.png'
const CUSTOMER_SERVICE_WECHAT_NAME = 'heibaidao'

/**
 * Floating button anchored to the bottom-right of the homepage. Tapping it
 * opens a popover with the WeChat customer-service QR code so visitors can
 * scan it to add support. The code can be tapped again to open a full-screen
 * preview for scanning from a distance or saving to the photo album.
 * Visible only on the default home layout.
 */
export function CustomerServiceButton() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [previewOpen, setPreviewOpen] = useState(false)

  function openPreview() {
    setOpen(false)
    setPreviewOpen(true)
  }

  return (
    <>
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
            className='w-80 max-w-[calc(100vw-2rem)] p-4'
          >
            <PopoverTitle className='text-popover-foreground text-sm font-semibold'>
              {t('WeChat support team')}
            </PopoverTitle>
            <PopoverDescription className='text-muted-foreground text-xs'>
              {t('Scan the QR code with WeChat to add customer service')}
            </PopoverDescription>
            <button
              type='button'
              onClick={openPreview}
              aria-label={t('Enlarge QR code')}
              className='group border-border mt-3 flex w-full cursor-zoom-in flex-col items-center gap-1.5 rounded-lg border bg-white p-2.5 transition-shadow hover:shadow-md focus-visible:ring-2 focus-visible:ring-[#3a8f7a] focus-visible:ring-offset-2 focus-visible:outline-none'
            >
              <img
                src={CUSTOMER_SERVICE_QR_SVG}
                alt={t('WeChat customer service QR code')}
                className='block w-full'
              />
              <span className='flex items-center gap-1 text-xs font-medium text-slate-600 transition-colors group-hover:text-[#1a4a3f]'>
                <Maximize2 className='size-3.5' aria-hidden='true' />
                {t('Click to enlarge')}
              </span>
            </button>
            <p className='text-muted-foreground mt-3 text-xs'>
              {t('WeChat ID')}:{' '}
              <span className='text-foreground font-medium'>
                {CUSTOMER_SERVICE_WECHAT_NAME}
              </span>
            </p>
          </PopoverContent>
        </Popover>
      </div>

      <Dialog open={previewOpen} onOpenChange={setPreviewOpen}>
        <DialogContent className='w-auto max-w-[calc(100%-2rem)] gap-4 sm:max-w-md'>
          <DialogHeader>
            <DialogTitle className='flex items-center gap-2'>
              <SiWechat className='size-5 text-[#3a8f7a]' aria-hidden='true' />
              {t('WeChat support team')}
            </DialogTitle>
            <DialogDescription>
              {t('Scan the QR code with WeChat to add customer service')}
            </DialogDescription>
          </DialogHeader>
          <div className='flex justify-center'>
            <img
              src={CUSTOMER_SERVICE_QR_SVG}
              alt={t('WeChat customer service QR code')}
              className='border-border w-[min(360px,74vw)] rounded-lg border bg-white'
            />
          </div>
          <p className='text-muted-foreground text-center text-xs'>
            {t('Save it to your album and scan later')}
          </p>
          <div className='border-border flex items-center justify-between gap-3 border-t pt-3'>
            <p className='text-muted-foreground text-xs'>
              {t('WeChat ID')}:{' '}
              <span className='text-foreground font-medium'>
                {CUSTOMER_SERVICE_WECHAT_NAME}
              </span>
            </p>
            <Button
              variant='outline'
              size='sm'
              render={
                <a
                  href={CUSTOMER_SERVICE_QR_PNG}
                  download='customer-service-wechat-qr.png'
                />
              }
            >
              <Download className='size-3.5' aria-hidden='true' />
              {t('Save QR code')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
