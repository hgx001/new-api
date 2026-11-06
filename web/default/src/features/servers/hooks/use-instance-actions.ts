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

import { useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { newRequestId } from '../api'
import { localizedServerError } from '../lib/errors'
import { formatAmount } from '../lib/pricing'
import type { ServerInstance } from '../types'
import { useServerActions } from './use-servers'

/**
 * 实例操作的统一入口：调用接口 + 成功/失败提示 + 布尔返回值。
 *
 * 购买页与控制台共用同一份实现，避免两处对「重启失败该提示什么」
 * 给出不同答案。返回 true/false 是给弹窗用的 —— 失败时不关闭弹窗，
 * 用户可以直接改参数重试。
 */
export function useInstanceActions() {
  const { t } = useTranslation()
  const actions = useServerActions()

  const togglePower = useCallback(
    async (server: ServerInstance, running: boolean) => {
      try {
        await actions.power.mutateAsync({
          instanceId: server.id,
          start: running,
        })
        toast.success(running ? t('Instance started') : t('Instance stopped'))
      } catch (error) {
        toast.error(localizedServerError(error, t))
      }
    },
    [actions.power, t]
  )

  const reboot = useCallback(
    async (server: ServerInstance) => {
      try {
        await actions.reboot.mutateAsync(server.id)
        toast.success(t('Instance rebooting'))
      } catch (error) {
        toast.error(localizedServerError(error, t))
      }
    },
    [actions.reboot, t]
  )

  const renew = useCallback(
    async (server: ServerInstance, durationId: string) => {
      try {
        await actions.renew.mutateAsync({
          instanceId: server.id,
          durationId,
          requestId: newRequestId(),
        })
        toast.success(
          t('Renewal successful, the expiry time has been extended')
        )
        return true
      } catch (error) {
        toast.error(localizedServerError(error, t))
        return false
      }
    },
    [actions.renew, t]
  )

  const release = useCallback(
    async (server: ServerInstance) => {
      try {
        const result = await actions.release.mutateAsync(server.id)
        if (result.refunded > 0) {
          toast.success(
            t('Instance released, {{amount}} refunded to your balance', {
              amount: formatAmount(result.refunded),
            })
          )
        } else {
          toast.success(t('Instance released'))
        }
        return true
      } catch (error) {
        toast.error(localizedServerError(error, t))
        return false
      }
    },
    [actions.release, t]
  )

  const resetPassword = useCallback(
    async (server: ServerInstance, password: string) => {
      try {
        await actions.resetPassword.mutateAsync({
          instanceId: server.id,
          password,
        })
        toast.success(t('Password updated'))
        return true
      } catch (error) {
        toast.error(localizedServerError(error, t))
        return false
      }
    },
    [actions.resetPassword, t]
  )

  return { togglePower, reboot, renew, release, resetPassword }
}
