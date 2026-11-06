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

import { Check, Copy, KeyRound, Loader2, RefreshCw, Wand2 } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { copyToClipboard } from '@/lib/copy-to-clipboard'

import { useAssignServerOrder } from '../hooks/use-servers'
import { formatAmount } from '../lib/pricing'
import { formatDateTime } from '../lib/instance-runtime'
import { formatDiskBreakdown, formatGpu } from '../constants'
import type { ServerInstance, ServerProvisionOrder } from '../types'

interface AssignDialogProps {
  order: ServerProvisionOrder
  onClose: () => void
  onDone: () => void
}

/** 一眼能读的随机密码：去掉了 0/O/1/l/I 这些复制后容易看错的字符。 */
function randomPassword(len = 16): string {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'
  const bytes = new Uint32Array(len)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (n) => chars[n % chars.length]).join('')
}

/**
 * 交付弹窗：把「已付款」变成「可用机器」的最后一步。
 *
 * 左边永远钉着订单摘要 —— 管理员一天要开十几台机器，最容易出的事故是
 * 「IP 填到别人那台去了」。把用户、规格、金额放在同一屏，填之前扫一眼就能
 * 确认交付对象没搞错。
 *
 * 分配成功后弹窗不关闭，转成「交付摘要」：管理员最常见的下一步是把
 * 这几行粘到微信里发给用户，所以直接给一个可复制的整块文本。
 */
export function AssignDialog({ order, onClose, onDone }: AssignDialogProps) {
  const { t } = useTranslation()
  const assign = useAssignServerOrder()

  const [ip, setIp] = useState('')
  const [ipMissing, setIpMissing] = useState(false)
  const ipRef = useRef<HTMLInputElement>(null)
  const [port, setPort] = useState('22')
  const [username, setUsername] = useState('root')
  const [password, setPassword] = useState('')
  const [privateKey, setPrivateKey] = useState('')
  const [note, setNote] = useState('')
  const [showKey, setShowKey] = useState(false)
  const [delivered, setDelivered] = useState<ServerInstance | null>(null)
  const [copied, setCopied] = useState(false)

  const spec = order.spec
  const expireHint = t('Rental clock starts at assignment')

  const deliveryText = delivered
    ? [
        `${delivered.name} · ${t('Cloud servers')}`,
        `IP: ${delivered.ip}`,
        `SSH: ssh ${delivered.username}@${delivered.ip} -p ${delivered.ssh_port ?? 22}`,
        delivered.password_readable ? `${t('Password')}: ${delivered.password}` : '',
        delivered.private_key_ready ? t('Login with the private key you were given') : '',
        `${t('Expires at')}: ${formatDateTime(delivered.expire_at)}`,
        delivered.admin_note,
      ]
        .filter(Boolean)
        .join('\n')
    : ''

  const handleAssign = async () => {
    if (!ip.trim()) {
      // 光标直接落到空字段上：只弹一句提示，管理员还得自己找是哪一栏。
      setIpMissing(true)
      ipRef.current?.focus()
      toast.error(t('Please fill in the public IP'))
      return
    }
    if (!password.trim() && !privateKey.trim()) {
      toast.error(t('Fill in a password or a private key'))
      return
    }
    const portNumber = Number.parseInt(port, 10)
    if (!Number.isFinite(portNumber) || portNumber < 1 || portNumber > 65535) {
      toast.error(t('SSH port must be between 1 and 65535'))
      return
    }
    try {
      const result = await assign.mutateAsync({
        trade_no: order.trade_no,
        ip: ip.trim(),
        ssh_port: portNumber,
        username: username.trim(),
        password: password.trim(),
        private_key: privateKey.trim(),
        note: note.trim(),
      })
      setDelivered(result.instance)
      onDone()
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t('Assignment failed')
      toast.error(message)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <KeyRound className='size-4' />
            {delivered ? t('Machine delivered') : t('Assign this machine')}
          </DialogTitle>
          <DialogDescription>
            {delivered
              ? t(
                  'The user can see these credentials now. Copy them and send them over if you like.'
                )
              : t(
                  'Fill in the credentials you prepared. The user sees nothing until you assign.'
                )}
          </DialogDescription>
        </DialogHeader>

        {/* 订单摘要：填之前先确认没开错机器 */}
        <div className='border-border/70 bg-muted/40 rounded-xl border px-3.5 py-3'>
          <div className='grid gap-2 text-[12px] sm:grid-cols-2'>
            <SummaryRow label={t('User')} value={order.username || `#${order.user_id}`} />
            <SummaryRow label={t('Order number')} value={order.trade_no} mono />
            <SummaryRow
              label={t('Configuration')}
              value={
                spec.id
                  ? `${spec.cpu} vCPU · ${spec.memory} GB · ${formatDiskBreakdown(spec, t)}`
                  : order.spec_id
              }
            />
            <SummaryRow
              label={t('Graphics card')}
              value={spec.gpu ? formatGpu(spec.gpu) : t('No graphics card')}
            />
            <SummaryRow
              label={t('Amount paid')}
              value={formatAmount(order.amount)}
            />
            <SummaryRow
              label={t('Paid at')}
              value={formatDateTime(order.paid_at)}
            />
          </div>
          {!delivered && (
            <div className='text-muted-foreground border-border/60 mt-2.5 border-t pt-2 text-[11px]'>
              {expireHint}
            </div>
          )}
        </div>

        {delivered ? (
          <div className='space-y-2'>
            <div className='text-muted-foreground text-xs font-medium'>
              {t('Delivery summary')}
            </div>
            <pre className='border-border/70 bg-background max-h-56 overflow-auto rounded-xl border p-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap'>
              {deliveryText}
            </pre>
            <Button
              variant='outline'
              size='sm'
              className='h-8'
              onClick={() => {
                void copyToClipboard(deliveryText).then((ok) => {
                  if (ok) {
                    setCopied(true)
                    toast.success(t('Copied'))
                    window.setTimeout(() => setCopied(false), 2000)
                  }
                })
              }}
            >
              {copied ? (
                <Check className='mr-1.5 size-3.5' />
              ) : (
                <Copy className='mr-1.5 size-3.5' />
              )}
              {t('Copy delivery summary')}
            </Button>
          </div>
        ) : (
          <div className='space-y-3'>
            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-1.5'>
                <Label htmlFor='assign-ip'>{t('Public IP')}</Label>
                <Input
                  id='assign-ip'
                  ref={ipRef}
                  value={ip}
                  onChange={(event) => {
                    setIp(event.target.value)
                    if (event.target.value.trim()) setIpMissing(false)
                  }}
                  placeholder={t('e.g. 203.0.113.10')}
                  aria-invalid={ipMissing}
                  className='font-mono aria-invalid:border-red-500'
                  autoComplete='off'
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='assign-port'>{t('SSH port')}</Label>
                <Input
                  id='assign-port'
                  value={port}
                  onChange={(event) => setPort(event.target.value)}
                  inputMode='numeric'
                  className='font-mono'
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='assign-user'>{t('Username')}</Label>
                <Input
                  id='assign-user'
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  className='font-mono'
                  autoComplete='off'
                />
              </div>
              <div className='space-y-1.5'>
                <div className='flex items-center justify-between'>
                  <Label htmlFor='assign-pass'>{t('Password')}</Label>
                  <button
                    type='button'
                    onClick={() => setPassword(randomPassword())}
                    className='text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-[11px] transition-colors'
                  >
                    <Wand2 className='size-3' />
                    {t('Generate')}
                  </button>
                </div>
                <Input
                  id='assign-pass'
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder={t('Leave empty if you only deliver a key')}
                  className='font-mono'
                  autoComplete='off'
                />
              </div>
            </div>

            <div className='space-y-1.5'>
              <button
                type='button'
                onClick={() => setShowKey((value) => !value)}
                className='text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-[12px] transition-colors'
              >
                <RefreshCw className='size-3' />
                {showKey ? t('Hide private key') : t('Add an SSH private key (optional)')}
              </button>
              {showKey && (
                <Textarea
                  value={privateKey}
                  onChange={(event) => setPrivateKey(event.target.value)}
                  placeholder='-----BEGIN OPENSSH PRIVATE KEY-----'
                  className='max-h-40 min-h-24 font-mono text-[11px]'
                />
              )}
            </div>

            <div className='space-y-1.5'>
              <Label htmlFor='assign-note'>
                {t('Note for the user (optional)')}
              </Label>
              <Input
                id='assign-note'
                value={note}
                onChange={(event) => setNote(event.target.value)}
                placeholder={t('e.g. Ubuntu 22.04, reinstall via ticket')}
                maxLength={200}
              />
            </div>
          </div>
        )}

        <DialogFooter>
          {delivered ? (
            <Button onClick={onClose}>{t('Done')}</Button>
          ) : (
            <>
              <Button variant='outline' onClick={onClose} disabled={assign.isPending}>
                {t('Cancel')}
              </Button>
              <Button onClick={handleAssign} disabled={assign.isPending}>
                {assign.isPending && (
                  <Loader2 className='mr-2 size-4 animate-spin' />
                )}
                {t('Assign and start billing')}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function SummaryRow({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className='flex min-w-0 items-baseline gap-2'>
      <span className='text-muted-foreground w-20 shrink-0 text-[11px]'>
        {label}
      </span>
      <span
        className={`text-foreground min-w-0 truncate font-medium ${mono ? 'font-mono text-[11px]' : ''}`}
      >
        {value}
      </span>
    </div>
  )
}
