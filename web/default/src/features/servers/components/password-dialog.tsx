import { Eye, EyeOff, KeyRound, Loader2 } from 'lucide-react'
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
import { useEffect, useState } from 'react'
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

interface PasswordDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  serverId: string
  serverLabel: string
  submitting: boolean
  onSubmit: (password: string) => void
}

export function PasswordDialog({
  open,
  onOpenChange,
  serverLabel,
  submitting,
  onSubmit,
}: PasswordDialogProps) {
  const { t } = useTranslation()
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    if (!open) {
      setPassword('')
      setConfirm('')
      setVisible(false)
    }
  }, [open])

  const mismatched = confirm.length > 0 && password !== confirm
  const tooShort = password.length > 0 && password.length < 8
  const canSubmit = password.length >= 8 && password === confirm && !submitting

  const handleSubmit = () => {
    if (!canSubmit) {
      if (tooShort) {
        toast.error(t('Password must be at least 8 characters'))
      } else if (mismatched) {
        toast.error(t('The two passwords do not match'))
      }
      return
    }
    onSubmit(password)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <KeyRound className='size-4.5 text-[#3a8f7a]' />
            {t('Change root password')}
          </DialogTitle>
          <DialogDescription>
            {t('Server: {{server}}', { server: serverLabel })}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4 py-2'>
          <div className='space-y-2'>
            <Label htmlFor='server-password'>{t('New password')}</Label>
            <div className='relative'>
              <Input
                id='server-password'
                type={visible ? 'text' : 'password'}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={t('At least 8 characters')}
                autoComplete='new-password'
                className='pr-10'
              />
              <button
                type='button'
                onClick={() => setVisible((v) => !v)}
                className='text-muted-foreground hover:bg-muted hover:text-foreground absolute top-1/2 right-2 -translate-y-1/2 rounded-md p-1.5 transition-colors'
                aria-label={t('Toggle password visibility')}
              >
                {visible ? (
                  <EyeOff className='size-4' />
                ) : (
                  <Eye className='size-4' />
                )}
              </button>
            </div>
            {tooShort && (
              <p className='text-destructive text-xs'>
                {t('Password must be at least 8 characters')}
              </p>
            )}
          </div>

          <div className='space-y-2'>
            <Label htmlFor='server-password-confirm'>
              {t('Confirm password')}
            </Label>
            <Input
              id='server-password-confirm'
              type={visible ? 'text' : 'password'}
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              placeholder={t('Repeat the new password')}
              autoComplete='new-password'
            />
            {mismatched && (
              <p className='text-destructive text-xs'>
                {t('The two passwords do not match')}
              </p>
            )}
          </div>
        </div>

        <DialogFooter className='gap-2'>
          <Button
            variant='outline'
            disabled={submitting}
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            disabled={!canSubmit}
            onClick={handleSubmit}
            className='bg-[#3a8f7a] text-white hover:bg-[#2f7a66]'
          >
            {submitting && <Loader2 className='mr-2 size-4 animate-spin' />}
            {t('Update password')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
