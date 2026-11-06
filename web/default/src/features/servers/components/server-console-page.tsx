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

import { useNavigate } from '@tanstack/react-router'
import { RefreshCcw, ServerCog } from 'lucide-react'
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { useInstanceActions } from '../hooks/use-instance-actions'
import { useServerInstances } from '../hooks/use-servers'
import type { ServerInstance } from '../types'
import { InstanceConsole } from './instance-console'
import { PasswordDialog } from './password-dialog'

/**
 * 云服务器控制台（已登录用户）。
 *
 * 与公开的购买页（`/servers`）分开：购买页是营销 + 配置，
 * 这里是登录后的**资源管理台**，只有已购实例的用户有内容。
 * 参照云厂商 ECS 控制台的「实例」视图。
 *
 * 数据全部来自服务端：实例列表、到期时间、监控曲线、操作记录都由
 * `/api/server/*` 提供，本地不保存副本。刷新按钮只是手动触发一次拉取，
 * 平时靠 60 秒轮询保持与后端一致。
 */
export function ServerConsolePage() {
  const { t } = useTranslation()
  const navigate = useNavigate()

  const instancesQuery = useServerInstances(true)
  const servers = instancesQuery.data?.items ?? []
  const instanceActions = useInstanceActions()

  const [passwordTarget, setPasswordTarget] = useState<ServerInstance | null>(
    null
  )
  const [passwordSubmitting, setPasswordSubmitting] = useState(false)

  const handlePasswordSubmit = useCallback(
    async (password: string) => {
      if (!passwordTarget) return
      setPasswordSubmitting(true)
      const ok = await instanceActions.resetPassword(passwordTarget, password)
      setPasswordSubmitting(false)
      // 失败时不关闭弹窗：用户可以直接改一个密码重试
      if (ok) setPasswordTarget(null)
    },
    [instanceActions, passwordTarget]
  )

  return (
    <div className='flex flex-col'>
      <div className='border-border/70 bg-card border-b px-6 py-5'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div>
            <h1 className='text-foreground flex items-center gap-2 text-xl font-bold'>
              <ServerCog className='size-5' />
              {t('Cloud servers')}
            </h1>
            <p className='text-muted-foreground mt-1 text-sm'>
              {t(
                'Manage your instances: start, stop, monitor and renew them at any time.'
              )}
            </p>
          </div>
          <div className='flex items-center gap-2'>
            <Button
              variant='ghost'
              className='rounded-lg'
              disabled={instancesQuery.isFetching}
              onClick={() => void instancesQuery.refetch()}
            >
              <RefreshCcw
                className={
                  instancesQuery.isFetching ? 'size-4 animate-spin' : 'size-4'
                }
              />
              {t('Refresh')}
            </Button>
            <Button
              variant='outline'
              className='rounded-lg'
              onClick={() => navigate({ to: '/servers' })}
            >
              {t('Buy a new instance')}
            </Button>
          </div>
        </div>
      </div>

      <div className='px-6 py-6'>
        <InstanceConsole
          servers={servers}
          loading={instancesQuery.isLoading}
          onChangePassword={setPasswordTarget}
          onBrowse={() => {
            navigate({ to: '/servers' })
          }}
          onTogglePower={instanceActions.togglePower}
          onReboot={instanceActions.reboot}
          onRenew={instanceActions.renew}
          onRelease={instanceActions.release}
        />
      </div>

      <PasswordDialog
        open={!!passwordTarget}
        onOpenChange={(open) => {
          if (!open) setPasswordTarget(null)
        }}
        serverId={String(passwordTarget?.id ?? '')}
        serverLabel={
          passwordTarget
            ? `${passwordTarget.ip} (${passwordTarget.username})`
            : ''
        }
        submitting={passwordSubmitting}
        onSubmit={handlePasswordSubmit}
      />
    </div>
  )
}
