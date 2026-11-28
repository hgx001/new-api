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
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { fetchTokenKey, getApiKeys } from '@/features/keys/api'

/**
 * 无限画布（infinite-canvas）整页嵌入。
 *
 * 画布本身是纯前端应用，没有账号体系：它只认「服务地址 + 密钥」。
 * 这里把当前登录用户在本站的令牌取出来，通过同源 iframe 注入给画布：
 *
 * - 走 iframe 的同源接口而不是把密钥放进 URL：密钥不会进浏览器历史和访问日志；
 * - 用用户自己的令牌：画布里的每次生成都记在本人名下，额度、分组、限流全部走本站规则；
 * - 画布不另做登录：身份完全由本站会话决定。
 */

type CanvasCapability = 'text' | 'image' | 'video' | 'audio'

type CanvasModel = {
  name: string
  capability: CanvasCapability
  scriptId: string
}

type InjectedChannelPayload = {
  baseUrl: string
  apiKey: string
  models: CanvasModel[]
  channelId: string
  channelName: string
  groupName: string
}

type CanvasFrameWindow = Window & {
  __infiniteCanvasApplyChannel?: (
    payload: InjectedChannelPayload
  ) => Promise<unknown>
}

// 画布的调用脚本按用途区分：视频与图片走 new-api 的任务协议，文本走 chat completions。
const VIDEO_MODEL_PATTERN =
  /seedance|wan\d|wan3|video|magi|omni|kling|vidu|hailuo|h3/i
const IMAGE_MODEL_PATTERN =
  /seedream|banana|image|nano|flux|dall|midjourney|mj/i
const AUDIO_MODEL_PATTERN = /tts|speech|voice|audio|music|suno/i

function detectCapability(name: string): CanvasCapability {
  if (VIDEO_MODEL_PATTERN.test(name)) return 'video'
  if (IMAGE_MODEL_PATTERN.test(name)) return 'image'
  if (AUDIO_MODEL_PATTERN.test(name)) return 'audio'
  return 'text'
}

function detectScriptId(capability: CanvasCapability): string {
  if (capability === 'video') return 'newapi-video'
  if (capability === 'image') return 'newapi-image'
  if (capability === 'audio') return 'openai-chat-audio'
  return 'openai-chat-text'
}

/**
 * 画布用哪一个令牌：优先「无限制 + 无限额度」，其次任意可用。
 *
 * 服务端列表按 id 倒序，直接取第一个可用会把画布绑到用户最后创建的那个令牌上；
 * 那个令牌常常带 model_limits 或限了分组，画布上就会「少模型」或按非预期倍率扣费。
 */
function pickCanvasToken<
  T extends {
    status: number
    expired_time: number
    remain_quota: number
    unlimited_quota: boolean
    model_limits_enabled: boolean
  },
>(tokens: T[]): T | undefined {
  const usable = tokens.filter((token) => {
    if (token.status !== 1) return false
    if (token.expired_time !== -1 && token.expired_time * 1000 < Date.now()) {
      return false
    }
    if (!token.unlimited_quota && token.remain_quota <= 0) return false
    return true
  })
  return usable.find((token) => !token.model_limits_enabled) ?? usable[0]
}

export function Canvas() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const iframeRef = useRef<HTMLIFrameElement | null>(null)
  const [frameReady, setFrameReady] = useState(false)
  const [injected, setInjected] = useState(false)
  const [injectedError, setInjectedError] = useState('')

  const { data: tokenList } = useQuery({
    queryKey: ['canvas', 'api-keys'],
    queryFn: () => getApiKeys({ p: 1, size: 50 }),
    enabled: !!user,
    staleTime: 60_000,
  })

  const usableToken = useMemo(
    () => pickCanvasToken(tokenList?.data?.items ?? []),
    [tokenList]
  )

  const { data: plainKey } = useQuery({
    queryKey: ['canvas', 'token-key', usableToken?.id],
    queryFn: async () => {
      if (!usableToken) return ''
      const res = await fetchTokenKey(usableToken.id)
      const key = res.data?.key ?? ''
      return key ? `sk-${key.replace(/^sk-/, '')}` : ''
    },
    enabled: !!usableToken,
    staleTime: 60_000,
  })

  const { data: modelNames } = useQuery({
    queryKey: ['canvas', 'models', plainKey],
    queryFn: async () => {
      const res = await api.get('/v1/models', {
        headers: { Authorization: `Bearer ${plainKey}` },
      })
      const list = res.data?.data
      if (!Array.isArray(list)) return []
      return list
        .map((item: { id?: string; name?: string }) => item.id ?? item.name ?? '')
        .filter((name: string) => Boolean(name))
    },
    enabled: !!plainKey,
    staleTime: 300_000,
  })

  const models = useMemo<CanvasModel[]>(() => {
    return (modelNames ?? []).map((name: string) => {
      const capability = detectCapability(name)
      return { name, capability, scriptId: detectScriptId(capability) }
    })
  }, [modelNames])

  const baseUrl = useMemo(() => `${window.location.origin}/v1`, [])

  const inject = useCallback(async () => {
    const frameWindow = iframeRef.current?.contentWindow as
      | CanvasFrameWindow
      | null
    if (!frameWindow) return
    if (!plainKey) return
    if (!frameWindow.__infiniteCanvasApplyChannel) return
    try {
      await frameWindow.__infiniteCanvasApplyChannel({
        baseUrl,
        apiKey: plainKey,
        models,
        channelId: 'newapi',
        channelName: 'new-api',
        groupName: t('Default Group'),
      })
      setInjected(true)
      setInjectedError('')
    } catch (error) {
      setInjectedError(error instanceof Error ? error.message : String(error))
    }
  }, [baseUrl, models, plainKey, t])

  useEffect(() => {
    if (!frameReady || injected || !plainKey) return
    // 画布是单页应用，注入入口在首屏脚本执行后才挂上，这里轮询等待。
    let cancelled = false
    let timer = 0
    const attempt = async () => {
      if (cancelled) return
      await inject()
      if (cancelled || injected) return
      timer = window.setTimeout(attempt, 500)
    }
    void attempt()
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [frameReady, inject, injected, plainKey])

  const buildNotice = () => {
    if (!user) {
      return t(
        'Please sign in before using the infinite canvas. The canvas calls platform models with your own account token.'
      )
    }
    if (tokenList && !usableToken) {
      return t(
        'You have no usable token yet. Create and enable one on the Keys page; the canvas will pick it up automatically.'
      )
    }
    return ''
  }
  const notice = buildNotice()

  return (
    <PublicLayout showMainContainer={false}>
      {/* 顶部固定导航占 h-16，画布铺满剩余视口；页脚与首页内容都不出现在画布页。 */}
      <div className='h-svh w-full pt-16'>
        {notice ? (
          <div className='flex h-full items-center justify-center px-4 text-center'>
            <p className='text-muted-foreground max-w-md text-sm'>{notice}</p>
          </div>
        ) : (
          <>
            {injectedError ? (
              <p className='text-destructive px-4 py-2 text-sm'>
                {injectedError}
              </p>
            ) : null}
            <iframe
              ref={iframeRef}
              src='/canvas-app/'
              title={t('Infinite Canvas')}
              className='h-full w-full border-none'
              // 画布与本站同源，宿主页面要读 contentWindow 注入渠道配置，缺 allow-same-origin 会跨域。
              // eslint-disable-next-line react/iframe-missing-sandbox
              sandbox='allow-scripts allow-same-origin allow-forms allow-popups'
              onLoad={() => setFrameReady(true)}
            />
          </>
        )}
      </div>
    </PublicLayout>
  )
}
