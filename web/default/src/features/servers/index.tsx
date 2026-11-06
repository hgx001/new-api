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
import { submitPaymentForm } from '@/features/wallet/lib/payment'
import { useAuthStore } from '@/stores/auth-store'

import { newRequestId, quoteServer } from './api'
import { BillingPanel } from './components/billing-panel'
import { CheckoutDialog } from './components/checkout-dialog'
import { ConfigSection } from './components/config-section'
import { InstanceConsole } from './components/instance-console'
import { PasswordDialog } from './components/password-dialog'
import { ServerHero } from './components/server-hero'
import {
  DEFAULT_DURATION_ID,
  DEFAULT_MBPS,
  DEFAULT_PLAN,
  DEFAULT_REGION,
} from './constants'
import { useInstanceActions } from './hooks/use-instance-actions'
import {
  useServerActions,
  useServerInstances,
  useServerOrderStatus,
  useServerOrders,
  useServerPayMethods,
} from './hooks/use-servers'
import { localizedServerError } from './lib/errors'
import { defaultConfigFor } from './lib/pricing'
import type {
  ServerConfig,
  ServerInstance,
  ServerQuoteResult,
} from './types'

const ADVANTAGES = [  {
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
    titleKey: 'Flexible rental terms',
    descKey:
      'Rent for a single hour or up to three years. Longer terms get a discount, and the credentials stay in My servers for the whole period.',
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
    a: 'The instance is released once the purchased period runs out. You can order a new one at any time with the configuration you need.',
  },
  {
    q: 'Can I switch between Guangzhou and Shanghai?',
    a: 'Both regions run the same images and the same plan catalogue. Pick the region that matches where your users are when you place the order.',
  },
  {
    q: 'Which GPU should I choose for my workload?',
    a: 'Match the card memory to your model first. A 24 GB card is enough for 7B-class inference and video generation, while fine-tuning a 13B model or serving several models at once needs a larger card or two cards. Card memory decides whether the model fits at all, so choose it before the CPU and memory.',
  },
  {
    q: 'Can I pay by the hour?',
    a: 'Yes. The rental duration list runs from a single hour all the way to three years, so you can take one hour for a quick job or a prepaid term for a long-running service. Durations of a day or more get a small discount, and a pay-as-you-go instance is released automatically when the purchased time runs out.',
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
      durationId: DEFAULT_DURATION_ID,
    })
  )
  const [activeView, setActiveView] = useState<'configure' | 'owned'>(
    'configure'
  )
  const [checkoutOpen, setCheckoutOpen] = useState(false)
  const [serverQuote, setServerQuote] = useState<ServerQuoteResult | null>(null)
  const [checkoutRequestId, setCheckoutRequestId] = useState('')
  const [paying, setPaying] = useState(false)
  // 支付成功后拿到实例凭据：收银台要直接展示 IP / 用户名 / 密码
  const [paidServer, setPaidServer] = useState<ServerInstance | null>(null)
  // 第三方支付：下单后拿到的订单号，收银台跳回来后靠它轮询支付结果
  const [pendingTradeNo, setPendingTradeNo] = useState<string | null>(null)
  // 收银台里选中的支付方式
  const [selectedMethod, setSelectedMethod] = useState('balance')
  const [passwordTarget, setPasswordTarget] = useState<ServerInstance | null>(
    null
  )
  const [passwordSubmitting, setPasswordSubmitting] = useState(false)

  const instancesQuery = useServerInstances(isAuthenticated)
  const servers = instancesQuery.data?.items ?? []
  const balance = instancesQuery.data?.balance_amount ?? 0
  const payMethodsQuery = useServerPayMethods(isAuthenticated)
  const payMethods = payMethodsQuery.data?.methods ?? []
  const ordersQuery = useServerOrders(isAuthenticated)
  const actions = useServerActions()
  // 实例运维操作（开关机/重启/续费/释放/改密）与控制台共用一份实现
  const instanceActions = useInstanceActions()

  // 第三方支付的订单状态轮询：钱到了、机器开了就停
  const orderStatusQuery = useServerOrderStatus(pendingTradeNo)
  const paidFromOrder = orderStatusQuery.data?.instance ?? null

  /**
   * 从收银台跳回来时：把之前挂起的订单接上，继续轮询支付结果。
   *
   * 第三方支付是「下单 → 跳网关 → 回调开通」的异步链路。用户在网关付完钱
   * 会跳回本站（return_url 带 ?paid=1），此时网关回调可能还没到 ——
   * 所以要接上订单号继续轮询，而不是直接判定失败。
   */
  useEffect(() => {
    if (!isAuthenticated || pendingTradeNo) return
    const search = new URLSearchParams(window.location.search)
    if (search.get('paid') !== '1') return

    const orders = ordersQuery.data?.items ?? []
    // 找最近一笔还没付掉的购买订单。
    const pending = orders.find(
      (order) =>
        order.type === 'purchase' &&
        order.status === 'pending' &&
        !!order.trade_no
    )
    if (pending) {
      setPendingTradeNo(pending.trade_no)
      setCheckoutOpen(true)
    }
  }, [isAuthenticated, ordersQuery.data?.items, pendingTradeNo])

  const configRef = useRef<HTMLDivElement>(null)
  const ownedRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    // 本页（与首页一致）固定浅色。站点全局主题可能是暗色，会以两种方式渗进来：
    // 1) CSS 变量：弹窗等 Portal 内容挂在 body 上，不在 .light 作用域内，会整体变暗；
    // 2) dark: 变体：只要 html 上还带 .dark，共享组件里的 dark: 样式就会命中。
    // 因此本页挂载期间把文档切回浅色，卸载时还原。
    const root = document.documentElement
    const wasDark = root.classList.contains('dark')
    root.classList.remove('dark')
    return () => {
      if (wasDark) root.classList.add('dark')
    }
  }, [])

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
    setPaidServer(null)
    setServerQuote(null)
    setPendingTradeNo(null)
    // 默认选余额支付：不需要跳网关，是最快的路径。
    setSelectedMethod('balance')
    // 每次打开收银台生成一个幂等号：双击或网络重试都只会成交一台机器
    setCheckoutRequestId(newRequestId())
    setCheckoutOpen(true)
    // 金额以服务端报价为准；报价失败时退回本地计算值展示，支付时服务端仍会校验
    quoteServer(config)
      .then(setServerQuote)
      .catch(() => setServerQuote(null))
  }, [config, isAuthenticated, navigate, t])

  /**
   * 支付。
   *
   * 两条路径：
   *  1. 余额支付 —— 同步扣款、机器立刻开，直接展示凭据；
   *  2. 第三方支付 —— 先落一张 pending 订单，再拉起网关收银台。
   *     机器要等网关异步回调才开通，所以这里跳出去后由订单轮询接管，
   *     支付完成后在同一个弹窗里展示凭据。
   */
  const handlePay = useCallback(async () => {
    setPaying(true)
    try {
      const result = await actions.purchase.mutateAsync({
        config,
        requestId: checkoutRequestId,
        paymentMethod: selectedMethod,
      })

      // 余额支付：服务端已同步开通，直接展示凭据
      if (result.instance) {
        setPaidServer(result.instance)
        toast.success(t('Payment successful, your server is ready'))
        return
      }

      // 第三方支付：机器还没开，等回调。先记下订单号，再拉起收银台。
      setPendingTradeNo(result.order.trade_no)
      const launch = await actions.launchEpay.mutateAsync({
        tradeNo: result.order.trade_no,
        paymentMethod: selectedMethod,
      })
      if (!launch?.url) {
        toast.error(t('Failed to open the payment page'))
        return
      }
      if (launch.pay_mode === 'redirect') {
        window.location.assign(launch.url)
        return
      }
      // 易支付网关：浏览器要 POST 一张表单过去
      submitPaymentForm(launch.url, launch.params ?? {})
      toast.success(
        t('Payment page opened. Your server will be ready once the payment is confirmed.')
      )
    } catch (error) {
      toast.error(localizedServerError(error, t))
    } finally {
      setPaying(false)
    }
  }, [actions.launchEpay, actions.purchase, checkoutRequestId, config, selectedMethod, t])

  // 第三方支付到账后：展示凭据，并停掉轮询
  useEffect(() => {
    if (!paidFromOrder) return
    setPaidServer(paidFromOrder)
    setPendingTradeNo(null)
    toast.success(t('Payment successful, your server is ready'))
  }, [paidFromOrder, t])

  const handleCheckoutDone = useCallback(() => {
    setCheckoutOpen(false)
    setPaidServer(null)
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
      const ok = await instanceActions.resetPassword(passwordTarget, password)
      setPasswordSubmitting(false)
      // 失败时不关闭弹窗，用户可以直接改一个密码重试
      if (ok) setPasswordTarget(null)
    },
    [instanceActions, passwordTarget]
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
                <InstanceConsole
                  servers={servers}
                  loading={instancesQuery.isLoading}
                  onChangePassword={setPasswordTarget}
                  onBrowse={scrollToConfig}
                  onTogglePower={instanceActions.togglePower}
                  onReboot={instanceActions.reboot}
                  onRenew={instanceActions.renew}
                  onRelease={instanceActions.release}
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
        paying={paying}
        paidServer={paidServer}
        serverQuote={serverQuote}
        balance={balance}
        payMethods={payMethods}
        selectedMethod={selectedMethod}
        onSelectMethod={setSelectedMethod}
        onPay={handlePay}
        onDone={handleCheckoutDone}
      />

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
