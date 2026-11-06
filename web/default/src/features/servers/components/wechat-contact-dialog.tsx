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
import {
  Check,
  Copy,
  Download,
  Maximize2,
  MessageSquareText,
  Minimize2,
  ScanLine,
  ShieldCheck,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SiWechat } from 'react-icons/si'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  CUSTOMER_SERVICE_QR_PNG,
  CUSTOMER_SERVICE_QR_SVG,
  CUSTOMER_SERVICE_WECHAT_ID,
} from '@/lib/customer-service'
import { cn } from '@/lib/utils'

interface WeChatContactDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /**
   * 当前选中的配置摘要（例如「广州 · 24 vCPU · 96 GB · RTX PRO 6000 · 1 小时」）。
   * 会自动拼进咨询话术，管理员收到消息就知道要哪台机器，不用来回追问。
   */
  configSummary: string
  /** 用户确认「已联系过管理员」时回调，用于放行收银台的支付按钮 */
  onConfirmed?: () => void
}

type CopiedKind = 'id' | 'message' | null

/**
 * 管理员微信联系弹窗。
 *
 * 复用首页客服按钮的同一份二维码资源。相比只甩一张码，这里多做三件事：
 *  1. 三步引导，让用户知道「加完之后干什么」；
 *  2. 自动生成带配置的咨询话术，一键复制，管理员不用反复问配置；
 *  3. 「我已联系」按钮直接把收银台的确认勾上，少一次来回。
 */
export function WeChatContactDialog({
  open,
  onOpenChange,
  configSummary,
  onConfirmed,
}: WeChatContactDialogProps) {
  const { t } = useTranslation()
  const [zoomed, setZoomed] = useState(false)
  const [copied, setCopied] = useState<CopiedKind>(null)

  const message = t(
    'Hi, I would like to rent {{config}}. Could you confirm availability and reserve it for me?',
    { config: configSummary }
  )

  async function copy(text: string, kind: Exclude<CopiedKind, null>) {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(kind)
      toast.success(t('Copied to clipboard'))
      window.setTimeout(() => setCopied(null), 2000)
    } catch {
      toast.error(t('Copy failed, please select the text manually'))
    }
  }

  function handleDone() {
    onConfirmed?.()
    onOpenChange(false)
    setZoomed(false)
  }

  const steps = [
    { icon: ScanLine, label: t('Scan to add admin') },
    { icon: MessageSquareText, label: t('Send your configuration') },
    { icon: ShieldCheck, label: t('Return and pay') },
  ]

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='gap-0 overflow-hidden p-0 sm:max-w-md'>
        <DialogHeader className='space-y-1.5 border-b bg-[#fbfdfc] px-6 py-5 text-left'>
          <DialogTitle className='flex items-center gap-2 text-base font-semibold text-[#1a4a3f]'>
            <span className='flex size-8 items-center justify-center rounded-lg bg-[#e8f4f0]'>
              <SiWechat className='size-4 text-[#3a8f7a]' aria-hidden='true' />
            </span>
            {t('Contact admin to confirm availability')}
          </DialogTitle>
          <DialogDescription className='text-xs leading-relaxed'>
            {t(
              'Scan the QR code to add the admin on WeChat. We will reserve the machine for you before you are charged.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4 px-6 py-5'>
          {/* 三步引导：把「扫码之后呢」讲清楚 */}
          <div className='grid grid-cols-3 gap-1.5'>
            {steps.map((step, index) => (
              <div
                key={step.label}
                className='flex flex-col items-center gap-1.5 rounded-xl border border-[#e4efe9] bg-[#f7fbf9] px-2 py-2.5 text-center'
              >
                <span className='flex size-6 items-center justify-center rounded-full bg-[#3a8f7a] text-[10px] font-bold text-white'>
                  {index + 1}
                </span>
                <step.icon className='size-3.5 text-[#3a8f7a]' aria-hidden='true' />
                <span className='text-[10px] leading-tight text-[#4b6b61]'>
                  {step.label}
                </span>
              </div>
            ))}
          </div>

          {/* 二维码：点击放大，方便隔屏扫 / 存相册 */}
          <button
            type='button'
            onClick={() => setZoomed((value) => !value)}
            aria-label={zoomed ? t('Shrink QR code') : t('Enlarge QR code')}
            className='group flex w-full cursor-zoom-in flex-col items-center gap-2 rounded-2xl border border-[#e4efe9] bg-white p-3 transition-shadow hover:shadow-md focus-visible:ring-2 focus-visible:ring-[#3a8f7a] focus-visible:outline-none'
          >
            <img
              src={CUSTOMER_SERVICE_QR_SVG}
              alt={t('WeChat customer service QR code')}
              className={cn(
                'rounded-lg bg-white transition-all duration-200',
                zoomed ? 'w-[min(320px,70vw)]' : 'w-40'
              )}
            />
            <span className='flex items-center gap-1 text-[11px] font-medium text-[#6b8a80] transition-colors group-hover:text-[#1a4a3f]'>
              {zoomed ? (
                <Minimize2 className='size-3' aria-hidden='true' />
              ) : (
                <Maximize2 className='size-3' aria-hidden='true' />
              )}
              {zoomed ? t('Click to shrink') : t('Click to enlarge')}
            </span>
          </button>

          {/* 微信号：不能扫码时手动搜索 */}
          <div className='flex items-center justify-between gap-3 rounded-xl border border-[#e4efe9] px-3.5 py-2.5'>
            <div className='text-xs'>
              <span className='text-muted-foreground'>{t('WeChat ID')}: </span>
              <span className='font-semibold text-[#1a4a3f]'>
                {CUSTOMER_SERVICE_WECHAT_ID}
              </span>
            </div>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              className='h-7 text-xs'
              onClick={() => void copy(CUSTOMER_SERVICE_WECHAT_ID, 'id')}
            >
              {copied === 'id' ? (
                <Check className='size-3.5' aria-hidden='true' />
              ) : (
                <Copy className='size-3.5' aria-hidden='true' />
              )}
              {copied === 'id' ? t('Copied') : t('Copy')}
            </Button>
          </div>

          {/* 咨询话术：自动带上用户刚选的配置，省一轮来回 */}
          <div className='rounded-xl border border-[#e4efe9] bg-[#f7fbf9] p-3.5'>
            <div className='flex items-center gap-1.5 text-xs font-semibold text-[#1a4a3f]'>
              <MessageSquareText className='size-3.5' aria-hidden='true' />
              {t('Suggested message')}
            </div>
            <p className='mt-2 rounded-lg bg-white px-3 py-2 text-[11px] leading-relaxed break-words text-[#4b6b61]'>
              {message}
            </p>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='mt-2 h-7 w-full text-xs'
              onClick={() => void copy(message, 'message')}
            >
              {copied === 'message' ? (
                <Check className='size-3.5' aria-hidden='true' />
              ) : (
                <Copy className='size-3.5' aria-hidden='true' />
              )}
              {copied === 'message' ? t('Copied') : t('Copy message')}
            </Button>
          </div>
        </div>

        <div className='flex items-center justify-between gap-3 border-t bg-[#fbfdfc] px-6 py-4'>
          <Button
            variant='ghost'
            size='sm'
            className='text-xs'
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
          <Button
            type='button'
            size='sm'
            onClick={handleDone}
            className='rounded-xl bg-[#3a8f7a] text-white hover:bg-[#2f7a66]'
          >
            {t('Done, return to payment')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
