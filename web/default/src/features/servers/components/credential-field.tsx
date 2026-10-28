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
import { Check, Copy, Eye, EyeOff } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { cn } from '@/lib/utils'

interface CredentialFieldProps {
  label: string
  value: string
  /** 密码类字段：默认打码，可切换明文 */
  secret?: boolean
  /** 左侧图标 */
  icon?: React.ReactNode
  /** 紧凑模式（我的服务器卡片里用） */
  dense?: boolean
}

/**
 * 单条凭据（IP / 用户名 / 密码）。
 *
 * 「付款成功凭据面板」与「我的服务器卡片」都要展示同一批凭据，
 * 复制 / 明文切换的行为必须一致，所以抽成共享组件，
 * 而不是各写一份（两边的复制按钮行为不一样会让人怀疑其中一边的实现）。
 */
export function CredentialField({
  label,
  value,
  secret,
  icon,
  dense,
}: CredentialFieldProps) {
  const { t } = useTranslation()
  const [visible, setVisible] = useState(false)
  const [copied, setCopied] = useState(false)

  const display = secret && !visible ? '••••••••••••' : value

  const handleCopy = async () => {
    const ok = await copyToClipboard(value)
    if (ok) {
      setCopied(true)
      toast.success(t('Copied to clipboard'))
      setTimeout(() => setCopied(false), 1800)
    } else {
      toast.error(t('Copy failed, please select and copy manually'))
    }
  }

  return (
    <div
      className={cn(
        'rounded-xl border bg-[#f8fbfa] transition-colors',
        dense ? 'px-3.5 py-3' : 'px-4 py-3.5'
      )}
    >
      <div className='flex items-center justify-between gap-2'>
        <span className='text-muted-foreground flex items-center gap-1.5 text-[11px] font-medium tracking-wide uppercase'>
          {icon}
          {label}
        </span>
        <div className='flex items-center gap-0.5'>
          {secret && (
            <button
              type='button'
              onClick={() => setVisible((v) => !v)}
              className='text-muted-foreground hover:bg-muted hover:text-foreground rounded-md p-1.5 transition-colors'
              aria-label={t('Toggle password visibility')}
            >
              {visible ? (
                <EyeOff className='size-3.5' />
              ) : (
                <Eye className='size-3.5' />
              )}
            </button>
          )}
          <button
            type='button'
            onClick={handleCopy}
            className='text-muted-foreground hover:bg-muted hover:text-foreground rounded-md p-1.5 transition-colors'
            aria-label={t('Copy')}
          >
            {copied ? (
              <Check className='size-3.5 text-emerald-500' />
            ) : (
              <Copy className='size-3.5' />
            )}
          </button>
        </div>
      </div>
      <div
        className={cn(
          'mt-1.5 truncate font-mono text-[13px] font-medium text-[#1a4a3f]',
          dense && 'text-sm'
        )}
      >
        {display}
      </div>
    </div>
  )
}
