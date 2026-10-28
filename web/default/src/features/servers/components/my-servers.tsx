import {
  Clock,
  Copy,
  Gpu,
  KeyRound,
  MapPin,
  Server as ServerIcon,
} from 'lucide-react'
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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { cn } from '@/lib/utils'

import {
  getBandwidth,
  getDuration,
  getPlan,
  getRegion,
  getSpec,
  formatGpu,
  formatSpec,
} from '../constants'
import { resolveDurationId } from '../lib/pricing'
import type { OwnedServer } from '../types'
import { CredentialField } from './credential-field'

interface MyServersProps {
  servers: OwnedServer[]
  onChangePassword: (server: OwnedServer) => void
  onBrowse: () => void
}

function formatDate(ts: number) {
  return new Date(ts * 1000).toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })
}

function daysLeft(ts: number) {
  return Math.max(0, Math.ceil((ts * 1000 - Date.now()) / 86400000))
}

/** 按小时计费的实例剩余小时数（向上取整，不足 1 小时算 1 小时） */
function hoursLeft(ts: number) {
  return Math.max(0, Math.ceil((ts * 1000 - Date.now()) / 3600000))
}

export function MyServers({
  servers,
  onChangePassword,
  onBrowse,
}: MyServersProps) {
  const { t } = useTranslation()

  if (servers.length === 0) {
    return (
      <div className='bg-card/60 rounded-2xl border border-dashed px-6 py-16 text-center'>
        <div className='mx-auto flex size-14 items-center justify-center rounded-2xl bg-[#e8f4f0]'>
          <ServerIcon className='size-6 text-[#3a8f7a]' />
        </div>
        <h3 className='mt-4 text-lg font-semibold'>{t('No servers yet')}</h3>
        <p className='text-muted-foreground mx-auto mt-2 max-w-md text-sm'>
          {t(
            'Configure a server and complete the payment. The instance details will show up here.'
          )}
        </p>
        <Button
          className='mt-6 rounded-full bg-[#3a8f7a] text-white hover:bg-[#2f7a66]'
          onClick={onBrowse}
        >
          {t('Configure a server')}
        </Button>
      </div>
    )
  }

  return (
    <div className='grid gap-4 lg:grid-cols-2'>
      {servers.map((server) => {
        const plan = getPlan(server.config.plan)
        const spec = getSpec(plan, server.config.specId)
        const region = getRegion(server.config.region)
        const bandwidth = getBandwidth(server.config.mbps)
        // 到期时间到了同样算过期：演示态的 status 只在开通时写成 running，
        // 不会随时间翻转，只看 status 会一直显示「运行中」。
        const expired =
          server.status === 'expired' || server.expireAt * 1000 <= Date.now()
        // 按小时的实例到期很快，「还剩 N 天」对它没有意义，要按小时说
        const duration = getDuration(resolveDurationId(server.config))
        const isHourly = duration.pricing === 'hourly'

        return (
          <Card
            key={server.id}
            className={cn(
              'overflow-hidden border-border/70',
              expired && 'opacity-80'
            )}
          >
            <CardHeader className='gap-3 bg-[#f5f9f7] pb-4'>
              <div className='flex items-start justify-between gap-3'>
                <div>
                  <CardTitle className='flex items-center gap-2 text-base'>
                    <ServerIcon className='size-4 text-[#3a8f7a]' />
                    {t(plan.nameKey)}
                  </CardTitle>
                  <CardDescription className='mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1'>
                    <span className='inline-flex items-center gap-1'>
                      <MapPin className='size-3' />
                      {t(region.nameKey)}
                    </span>
                    <span>{formatSpec(spec)}</span>
                    {spec.gpu && (
                      <span className='inline-flex items-center gap-1 font-medium text-[#3a8f7a]'>
                        <Gpu className='size-3' />
                        {formatGpu(spec.gpu)}
                      </span>
                    )}
                    <span>{bandwidth.mbps} Mbps</span>
                    <span className='inline-flex items-center gap-1 font-medium text-[#3a8f7a]'>
                      <Clock className='size-3' />
                      {t(duration.nameKey)}
                    </span>
                  </CardDescription>
                </div>
                <Badge
                  className={cn(
                    'shrink-0',
                    expired
                      ? 'bg-amber-500/15 text-amber-600'
                      : 'bg-emerald-500/15 text-emerald-600'
                  )}
                >
                  {expired ? t('Expired') : t('Running')}
                </Badge>
              </div>
            </CardHeader>

            <CardContent className='space-y-4 pt-5'>
              <div className='grid gap-3 sm:grid-cols-2'>
                <CredentialField
                  label={t('Public IP')}
                  value={server.ip}
                  dense
                />
                <CredentialField
                  label={t('Username')}
                  value={server.username}
                  dense
                />
                <CredentialField
                  label={t('Password')}
                  value={server.password}
                  secret
                  dense
                />
                <div className='rounded-xl border bg-[#f8fbfa] px-3.5 py-3'>
                  <div className='text-muted-foreground text-[11px] font-medium tracking-wide uppercase'>
                    {t('Expires on')}
                  </div>
                  <div className='mt-1.5 text-sm font-medium text-[#1a4a3f]'>
                    {formatDate(server.expireAt)}
                  </div>
                  <div className='text-muted-foreground mt-0.5 text-[11px]'>
                    {isHourly
                      ? t('{{hours}} hours left', {
                          hours: hoursLeft(server.expireAt),
                        })
                      : t('{{days}} days left', {
                          days: daysLeft(server.expireAt),
                        })}
                  </div>
                </div>
              </div>

              <div className='flex flex-wrap items-center gap-2 border-t pt-4'>
                <Button
                  size='sm'
                  variant='outline'
                  className='rounded-lg'
                  onClick={() => {
                    const command = `ssh ${server.username}@${server.ip}`
                    void copyToClipboard(command).then((ok) => {
                      if (ok) {
                        toast.success(t('SSH command copied'))
                      } else {
                        toast.error(
                          t('Copy failed, please select and copy manually')
                        )
                      }
                    })
                  }}
                >
                  <Copy className='mr-1.5 size-3.5' />
                  {t('Copy SSH command')}
                </Button>
                <Button
                  size='sm'
                  variant='outline'
                  className='rounded-lg'
                  onClick={() => onChangePassword(server)}
                >
                  <KeyRound className='mr-1.5 size-3.5' />
                  {t('Change password')}
                </Button>
                <span className='text-muted-foreground ml-auto font-mono text-[11px]'>
                  {server.username}@{server.ip}
                </span>
              </div>
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}
