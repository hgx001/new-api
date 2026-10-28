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
  KeyRound,
  Server,
  ShieldCheck,
  Terminal,
  User,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { copyToClipboard } from '@/lib/copy-to-clipboard'

import type { OwnedServer } from '../types'
import { CredentialField } from './credential-field'

interface CredentialsPanelProps {
  server: OwnedServer
  /** 是否显示底部的 SSH 命令行 */
  showSshCommand?: boolean
}

/**
 * 付款成功后的凭据面板。
 *
 * 付款那一刻用户最关心的是「怎么连上去」——公网 IP、用户名、密码三样。
 * 之前这里只有一句「可在我的服务器中查看」，用户得再点一次、翻一个 Tab
 * 才能看到凭据。现在直接把凭据摆在支付成功弹窗里，附带可一键复制的
 * SSH 命令，闭环少一次跳转。
 */
export function CredentialsPanel({
  server,
  showSshCommand = true,
}: CredentialsPanelProps) {
  const { t } = useTranslation()
  const [sshCopied, setSshCopied] = useState(false)

  const sshCommand = `ssh ${server.username}@${server.ip}`

  const handleCopySsh = async () => {
    const ok = await copyToClipboard(sshCommand)
    if (ok) {
      setSshCopied(true)
      toast.success(t('SSH command copied'))
      setTimeout(() => setSshCopied(false), 1800)
    } else {
      toast.error(t('Copy failed, please select and copy manually'))
    }
  }

  return (
    <div className='space-y-3'>
      <div className='flex items-center gap-2 text-xs font-semibold text-[#1a4a3f]'>
        <Server className='size-3.5 text-[#3a8f7a]' />
        {t('Server credentials')}
        {/* 演示态必须显式标出来：否则用户会以为这台机器真的开出来了，
            拿着 IP 去连却连不上 —— 这是「闭环断裂」最容易被误判成 bug 的地方 */}
        {server.demo && (
          <span className='rounded-md bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-amber-600'>
            {t('Demo')}
          </span>
        )}
      </div>

      <div className='grid gap-2.5 sm:grid-cols-2'>
        <CredentialField
          label={t('Public IP')}
          value={server.ip}
          icon={<Server className='size-3' />}
        />
        <CredentialField
          label={t('Username')}
          value={server.username}
          icon={<User className='size-3' />}
        />
        {/* 密码最长，单独占满一行 —— 放在两列网格里会在右侧留一块空白 */}
        <div className='sm:col-span-2'>
          <CredentialField
            label={t('Password')}
            value={server.password}
            secret
            icon={<KeyRound className='size-3' />}
          />
        </div>
      </div>

      {showSshCommand && (
        <button
          type='button'
          onClick={handleCopySsh}
          className='group flex w-full items-center gap-2.5 rounded-xl border border-[#cfe8e1] bg-[#06231d] px-3.5 py-3 text-left transition-colors hover:border-[#7dd3c0]'
        >
          <Terminal className='size-4 shrink-0 text-[#7dd3c0]' />
          <span className='min-w-0 flex-1 truncate font-mono text-[12px] text-[#d7f0e8]'>
            {sshCommand}
          </span>
          <span className='shrink-0 text-[#7dd3c0]'>
            {sshCopied ? (
              <Check className='size-3.5' />
            ) : (
              <Copy className='size-3.5' />
            )}
          </span>
        </button>
      )}

      <p className='text-muted-foreground flex items-start gap-2 text-[11px] leading-relaxed'>
        <ShieldCheck className='mt-0.5 size-3.5 shrink-0 text-[#3a8f7a]' />
        <span>
          {t(
            'Please change the password after your first login. The credentials above are also listed under My servers.'
          )}
        </span>
      </p>
    </div>
  )
}
