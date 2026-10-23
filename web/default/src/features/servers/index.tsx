import { useNavigate } from '@tanstack/react-router'
import {
  BadgeCheck,
  Clock3,
  Headphones,
  RefreshCcw,
  ServerCog,
  ShieldCheck,
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
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useAuthStore } from '@/stores/auth-store'

import {
  changeServerPassword,
  createServerOrder,
  getServerPayMethods,
  payServerOrder,
} from './api'
import { BillingPanel } from './components/billing-panel'
import { CheckoutDialog } from './components/checkout-dialog'
import { ConfigSection } from './components/config-section'
import { MyServers } from './components/my-servers'
import { PasswordDialog } from './components/password-dialog'
import { ServerHero } from './components/server-hero'
import {
  DEFAULT_MBPS,
  DEFAULT_PLAN,
  DEFAULT_REGION,
  DEFAULT_TERM,
} from './constants'
import { computePrice, defaultConfigFor } from './lib/pricing'
import { useOwnedServers, useServerStore } from './store'
import type { OwnedServer, PayMethod, ServerConfig } from './types'

const ADVANTAGES = [
  {
    icon: Clock3,
    titleKey: 'Instant delivery',
    descKey:
      'Instances are provisioned automatically right after the payment is confirmed, usually within five minutes.',
  },
  {
    icon: ShieldCheck,
    titleKey: 'DDoS protection included',
    descKey:
      'Every instance comes with network-level traffic cleaning and a free firewall rule set.',
  },
  {
    icon: RefreshCcw,
    titleKey: 'Upgrade any time',
    descKey:
      'Change plan, specs or bandwidth and the remaining period is charged at a prorated price.',
  },
  {
    icon: Headphones,
    titleKey: 'Engineer support',
    descKey:
      'Talk to the engineers who run the platform instead of a generic ticket queue.',
  },
] as const

const FAQS = [
  {
    q: 'How long does it take to get my server?',
    a: 'Instances are created automatically after the payment succeeds. Most orders are ready within five minutes, and the IP address, username and password appear under My servers as soon as provisioning finishes.',
  },
  {
    q: 'Can I change the password after the server is created?',
    a: 'Yes. Open My servers, find the instance and use the Change password action. The new password takes effect on the next login.',
  },
  {
    q: 'What happens when the subscription expires?',
    a: 'The instance is kept for seven days after the expiry date so you can renew it. After that the data is released and the instance is reclaimed.',
  },
  {
    q: 'Can I switch between Guangzhou and Shanghai?',
    a: 'Yes. Both regions run the same images and the same plan catalogue, so you can move an instance by rebuilding it in the other region.',
  },
  {
    q: 'Which GPU should I choose for my workload?',
    a: 'Match the card memory to your model first. A 24 GB card is enough for 7B-class inference and video generation, while fine-tuning a 13B model or serving several models at once needs a larger card or two cards. Card memory decides whether the model fits at all, so choose it before the CPU and memory.',
  },
] as const

export function ServersContent({
  embedded = false,
}: {
  /** 嵌入首页 Tab 时为 true：顶部已有 Tab 栏，不再为 fixed header 预留空白 */
  embedded?: boolean
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { auth } = useAuthStore()
  const isAuthenticated = !!auth.user

  const [config, setConfig] = useState<ServerConfig>(() =>
    defaultConfigFor(DEFAULT_PLAN, {
      region: DEFAULT_REGION,
      mbps: DEFAULT_MBPS,
      term: DEFAULT_TERM,
    })
  )
  const [activeView, setActiveView] = useState<'configure' | 'owned'>(
    'configure'
  )
  const [payMethods, setPayMethods] = useState<PayMethod[]>([])
  const [selectedMethod, setSelectedMethod] = useState('')
  const [checkoutOpen, setCheckoutOpen] = useState(false)
  const [paying, setPaying] = useState(false)
  const [paid, setPaid] = useState(false)
  const [passwordTarget, setPasswordTarget] = useState<OwnedServer | null>(null)
  const [passwordSubmitting, setPasswordSubmitting] = useState(false)

  const servers = useOwnedServers()
  const addOrder = useServerStore((s) => s.addOrder)
  const settleOrder = useServerStore((s) => s.settleOrder)
  const updatePassword = useServerStore((s) => s.updatePassword)

  const configRef = useRef<HTMLDivElement>(null)
  const ownedRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    // 本页（与首页一致）固定浅色。站点全局主题可能是暗色，会以两种方式渗进来：
    // 1) CSS 变量：弹窗等 Portal 内容挂在 body 上，不在 .light 作用域内，会整体变暗；
    // 2) dark: 变体：只要 html 上还带 .dark，共享组件里的 dark: 样式就会命中。
    // 因此本页挂载期间把文档切成浅色，卸载时还原。
    const root = document.documentElement
    const wasDark = root.classList.contains('dark')
    root.classList.remove('dark')
    return () => {
      if (wasDark) root.classList.add('dark')
    }
  }, [])

  useEffect(() => {
    // 支付方式列表要登录后才有意义，未登录时不必去请求（否则必然 401）
    if (!isAuthenticated) return
    getServerPayMethods()
      .then((methods) => {
        setPayMethods(methods)
        // 空列表表示站点当前没有可用的在线支付渠道，收银台会提示并禁用支付
        setSelectedMethod((current) => current || methods[0]?.type || '')
      })
      .catch(() => {
        setPayMethods([])
      })
  }, [isAuthenticated])

  const handleConfigChange = useCallback((patch: Partial<ServerConfig>) => {
    setConfig((current) => ({ ...current, ...patch }))
  }, [])

  const scrollToConfig = useCallback(() => {
    configRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [])

  const scrollToOwned = useCallback(() => {
    setActiveView('owned')
    setTimeout(
      () =>
        ownedRef.current?.scrollIntoView({
          behavior: 'smooth',
          block: 'start',
        }),
      60
    )
  }, [])

  const handleCheckout = useCallback(() => {
    if (!isAuthenticated) {
      toast.info(t('Please sign in before ordering'))
      navigate({ to: '/sign-in' })
      return
    }
    setPaid(false)
    setCheckoutOpen(true)
  }, [isAuthenticated, navigate, t])

  const handlePay = useCallback(async () => {
    setPaying(true)
    try {
      const price = computePrice(config)
      const order = await createServerOrder(config, price.total, selectedMethod)
      addOrder(order)
      const result = await payServerOrder(order)
      if (result.success && result.server) {
        settleOrder(order.id, result.server)
        setPaid(true)
        toast.success(t('Payment successful, your server is being provisioned'))
      } else {
        toast.error(t('Payment failed, please try again'))
      }
    } catch {
      toast.error(t('Network error, please try again later'))
    } finally {
      setPaying(false)
    }
  }, [addOrder, config, selectedMethod, settleOrder, t])

  const handleCheckoutDone = useCallback(() => {
    setCheckoutOpen(false)
    setPaid(false)
    setActiveView('owned')
    setTimeout(
      () =>
        ownedRef.current?.scrollIntoView({
          behavior: 'smooth',
          block: 'start',
        }),
      80
    )
  }, [])

  const handlePasswordSubmit = useCallback(
    async (password: string) => {
      if (!passwordTarget) return
      setPasswordSubmitting(true)
      const result = await changeServerPassword(passwordTarget.id, password)
      setPasswordSubmitting(false)
      if (result.success) {
        updatePassword(passwordTarget.id, password)
        setPasswordTarget(null)
        toast.success(t('Password updated'))
      } else {
        toast.error(
          result.messageKey
            ? t(result.messageKey)
            : t('Failed to update the password')
        )
      }
    },
    [passwordTarget, t, updatePassword]
  )

  return (
    <>
      {/* 独立路由时给 fixed header 留出位置：header 悬浮在页面浅色底上，
          避免浅色模式下深色的导航文字压在深色 hero 上看不清。
          嵌入首页 Tab 时由 tab 栏负责留白，不需要这里再留。 */}
      <div className={embedded ? undefined : 'pt-16'}>
        <ServerHero
          embedded={embedded}
          ownedCount={servers.length}
          onScrollToConfig={scrollToConfig}
          onScrollToOwned={scrollToOwned}
        />
      </div>

      <div className='bg-[#f5f9f7]'>
        {/* 主区 */}
        <section className='mx-auto max-w-6xl px-4 py-10 md:py-14'>
          <div ref={configRef} className='scroll-mt-24'>
            <Tabs
              value={activeView}
              onValueChange={(value) =>
                setActiveView(value as 'configure' | 'owned')
              }
            >
              <TabsList>
                <TabsTrigger value='configure'>
                  <ServerCog className='size-4' />
                  {t('Configure server')}
                </TabsTrigger>
                <TabsTrigger value='owned'>
                  <BadgeCheck className='size-4' />
                  {t('My servers ({{count}})', { count: servers.length })}
                </TabsTrigger>
              </TabsList>
            </Tabs>
          </div>

          <div className='mt-7'>
            {activeView === 'configure' ? (
              <div className='grid gap-6 lg:grid-cols-[minmax(0,1fr)_360px] lg:gap-8'>
                <div className='border-border/70 bg-card rounded-2xl border p-5 shadow-sm md:p-7'>
                  <ConfigSection
                    config={config}
                    onChange={handleConfigChange}
                  />
                </div>
                <div className='lg:sticky lg:top-24 lg:h-fit'>
                  <BillingPanel
                    config={config}
                    onChange={handleConfigChange}
                    onCheckout={handleCheckout}
                    requiresLogin={!isAuthenticated}
                  />
                </div>
              </div>
            ) : (
              <div ref={ownedRef} className='scroll-mt-24'>
                <MyServers
                  servers={servers}
                  onChangePassword={setPasswordTarget}
                  onBrowse={scrollToConfig}
                />
              </div>
            )}
          </div>
        </section>

        {/* 优势 */}
        <section className='bg-card/60 border-y py-12 md:py-16'>
          <div className='mx-auto max-w-6xl px-4'>
            <h2 className='text-center text-2xl font-bold text-[#1a4a3f] md:text-3xl'>
              {t('Why choose our cloud servers')}
            </h2>
            <p className='text-muted-foreground mx-auto mt-3 max-w-2xl text-center text-sm'>
              {t(
                'Everything you need to run production workloads, without the setup overhead.'
              )}
            </p>
            <div className='mt-9 grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
              {ADVANTAGES.map((item) => (
                <div
                  key={item.titleKey}
                  className='border-border/70 bg-card rounded-2xl border p-6 transition-shadow hover:shadow-md'
                >
                  <div className='flex size-10 items-center justify-center rounded-xl bg-[#e8f4f0]'>
                    <item.icon className='size-5 text-[#3a8f7a]' />
                  </div>
                  <h3 className='mt-4 font-semibold text-[#1a4a3f]'>
                    {t(item.titleKey)}
                  </h3>
                  <p className='text-muted-foreground mt-1.5 text-sm leading-relaxed'>
                    {t(item.descKey)}
                  </p>
                </div>
              ))}
            </div>
          </div>
        </section>

        {/* FAQ */}
        <section className='py-12 md:py-16'>
          <div className='mx-auto max-w-3xl px-4'>
            <h2 className='text-center text-2xl font-bold text-[#1a4a3f] md:text-3xl'>
              {t('Frequently asked questions')}
            </h2>
            <div className='mt-8 space-y-3'>
              {FAQS.map((item) => (
                <details
                  key={item.q}
                  className='group border-border/70 bg-card rounded-2xl border px-5 py-4 transition-colors open:bg-[#f8fbfa]'
                >
                  <summary className='flex cursor-pointer list-none items-center justify-between gap-4 text-sm font-semibold text-[#1a4a3f]'>
                    {t(item.q)}
                    <span className='text-muted-foreground transition-transform group-open:rotate-45'>
                      +
                    </span>
                  </summary>
                  <p className='text-muted-foreground mt-3 text-sm leading-relaxed'>
                    {t(item.a)}
                  </p>
                </details>
              ))}
            </div>
          </div>
        </section>
      </div>

      <CheckoutDialog
        open={checkoutOpen}
        onOpenChange={setCheckoutOpen}
        config={config}
        payMethods={payMethods}
        selectedMethod={selectedMethod}
        onSelectMethod={setSelectedMethod}
        paying={paying}
        paid={paid}
        onPay={handlePay}
        onDone={handleCheckoutDone}
      />

      <PasswordDialog
        open={!!passwordTarget}
        onOpenChange={(open) => {
          if (!open) setPasswordTarget(null)
        }}
        serverId={passwordTarget?.id ?? ''}
        serverLabel={
          passwordTarget
            ? `${passwordTarget.ip} (${passwordTarget.username})`
            : ''
        }
        submitting={passwordSubmitting}
        onSubmit={handlePasswordSubmit}
      />
    </>
  )
}

/** 独立路由用的完整页面 */
export function Servers() {
  // 与首页一致：本页固定浅色（首页用的是 PublicLayout forceLightMode）。
  // 站点的主题切换是全局的，dark 变体会在 .light 作用域里继续命中，
  // 而本页的品牌配色是硬编码的浅色系，因此这里同样强制浅色，避免出现
  // 「浅色底 + 暗色卡片」的撕裂。
  return (
    <PublicLayout showMainContainer={false} forceLightMode>
      <ServersContent />
      <Footer />
    </PublicLayout>
  )
}
