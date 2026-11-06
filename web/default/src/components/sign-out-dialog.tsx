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

import { ConfirmDialog } from '@/components/confirm-dialog'
import { logout } from '@/features/auth/api'
import { useAuthStore } from '@/stores/auth-store'

interface SignOutDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function SignOutDialog({ open, onOpenChange }: SignOutDialogProps) {
  const { t } = useTranslation()
  const { auth } = useAuthStore()

  const handleSignOut = async () => {
    try {
      await logout()
    } catch {
      /* empty */
    }
    auth.reset()
    try {
      if (typeof window !== 'undefined') {
        // 服务器凭据现在只存在服务端、按登录账号鉴权，登出后本就取不到。
        // 这里清的是**演示态遗留**的本地副本：老版本把 IP 与 root 密码
        // 明文存在 localStorage 里，不清掉的话同一台电脑上的下一个登录
        // 用户还能把它翻出来。
        window.localStorage.removeItem('heibaidao-servers')
        window.localStorage.removeItem('uid')
      }
    } catch {
      /* empty */
    }
    toast.success(t('Signed out'))
    // Refresh the page to clear all state and update UI
    if (typeof window !== 'undefined') {
      window.location.reload()
    }
  }

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Sign out')}
      desc={t(
        'Are you sure you want to sign out? You will need to sign in again to access your account.'
      )}
      confirmText={t('Sign out')}
      handleConfirm={handleSignOut}
      className='sm:max-w-sm'
    />
  )
}
