import { useNavigate } from '@tanstack/react-router'
import {
  BadgeCheck,
  CalendarClock,
  Check,
  CircleAlert,
  Copy,
  CreditCard,
  Download,
  Globe,
  Laptop,
  Loader2,
  Lock,
  Monitor,
  QrCode,
  Rocket,
  Shield,
  ShieldCheck,
  Smartphone,
  Sparkles,
  Zap,
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
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { SiAlipay } from 'react-icons/si'
import { toast } from 'sonner'

import { PublicLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { submitPaymentForm } from '@/features/wallet/lib/payment'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  createVpnOrder,
  getMyVpnKey,
  getMyVpnOrders,
  getTopupInfo,
  type TopupInfo,
  type VpnKeyResult,
  type VpnOrder,
} from './api'
import {
  daysLeft,
  formatDate,
  formatDateTime,
  isTrustedAlipayUrl,
  PENDING_FAST_WINDOW_SEC,
  resolvePollIntervalMs,
  resolveVpnState,
  type VpnState,
} from './page-logic'

const V2RAYN_VERSION = '7.24.8'
const V2RAYN_WINDOWS_URL = `https://github.com/2dust/v2rayN/releases/download/${V2RAYN_VERSION}/v2rayN-windows-64.zip`
const V2RAYN_MACOS_ARM_URL = `https://github.com/2dust/v2rayN/releases/download/${V2RAYN_VERSION}/v2rayN-macos-arm64.dmg`
const V2RAYN_MACOS_INTEL_URL = `https://github.com/2dust/v2rayN/releases/download/${V2RAYN_VERSION}/v2rayN-macos-64.dmg`
const V2RAYN_RELEASES_URL = 'https://github.com/2dust/v2rayN/releases'

const PLAN_PRICE = 199
const PLAN_ORIGINAL_PRICE = 399
const PLAN_DAYS = 365

const FEATURES = [
  {
    icon: Shield,
    title: 'VLESS + Reality',
    desc: 'Reality 协议直连，无需域名与证书，抗干扰能力强，稳定不易掉线。',
  },
  {
    icon: Zap,
    title: '全速带宽',
    desc: '日本东京优质线路，低延迟高吞吐，看视频、开会、开发调试都流畅。',
  },
  {
    icon: Lock,
    title: '加密传输',
    desc: '流量全程加密，ISP 只能看到你在访问加密流量，隐私无忧。',
  },
  {
    icon: Globe,
    title: '多平台支持',
    desc: 'Windows / macOS 客户端一码通用，一个密钥多设备轻松接入。',
  },
]

const PLAN_FEATURES = [
  '日本东京 IEPL 优化线路',
  'Reality 协议，稳定抗干扰',
  '支付成功即时下发导入密钥',
  'Windows / macOS 全平台可用',
  '不限设备数（合理使用）',
  '到期前可随时续费',
]

const STEPS = [
  {
    icon: Download,
    title: '下载安装客户端',
    desc: '根据你的系统下载并安装 v2rayN 客户端（见下方下载区）。',
  },
  {
    icon: QrCode,
    title: '购买套餐获取密钥',
    desc: '支付成功后，页面会立即显示你的专属导入密钥。',
  },
  {
    icon: Copy,
    title: '复制并导入密钥',
    desc: '复制密钥 → 打开 v2rayN → 服务器 → 从剪贴板导入批量URL。',
  },
  {
    icon: Rocket,
    title: '选节点开启加速',
    desc: '选择「日本东京」节点 → 系统代理 → 自动配置系统代理，完成！',
  },
]

const FAQS = [
  {
    q: '支付成功后没看到密钥？',
    a: '支付在支付宝页面完成，回到本页后系统会自动确认（3 秒一次，订单较久未确认时自动降频但不会停止）。如仍未显示，查看下方「最近订单」里该笔订单的状态：显示「支付成功」即已到账；显示「待支付」说明尚未收到支付确认，请重新下单或联系客服。',
  },
  {
    q: '支付渠道是哪个？',
    a: '本套餐通过支付宝官方直连收款，订单与支付宝交易号一一对应，不经过第三方聚合网关。',
  },
  {
    q: '密钥可以在多台设备上使用吗？',
    a: '可以。同一密钥可在 Windows / macOS 客户端上同时导入使用，请合理使用避免影响速度。',
  },
  {
    q: '套餐到期后如何续费？',
    a: '到期后在本页重新购买即可；开通状态下按钮会显示「续费」，支付后服务期限顺延一年。',
  },
  {
    q: '支持退款吗？',
    a: '虚拟服务商品，密钥一经下发不支持退款。如遇节点故障请优先联系客服处理。',
  },
]

const STATE_CHIP: Record<VpnState, { label: string; className: string }> = {
  active: {
    label: '已开通',
    className: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-600',
  },
  pending: {
    label: '待支付',
    className: 'border-amber-500/30 bg-amber-500/10 text-amber-600',
  },
  expired: {
    label: '已到期',
    className: 'border-amber-500/30 bg-amber-500/10 text-amber-600',
  },
  none: {
    label: '未开通',
    className: 'border-border bg-muted text-muted-foreground',
  },
}

const ORDER_STATUS: Record<
  VpnOrder['status'],
  { label: string; className: string }
> = {
  success: {
    label: '支付成功',
    className: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-600',
  },
  pending: {
    label: '待支付',
    className: 'border-amber-500/30 bg-amber-500/10 text-amber-600',
  },
  failed: {
    label: '支付失败',
    className: 'border-destructive/30 bg-destructive/10 text-destructive',
  },
  expired: {
    label: '已超时',
    className: 'border-border bg-muted text-muted-foreground',
  },
}

function SectionHeading({
  eyebrow,
  title,
  desc,
}: {
  eyebrow: string
  title: string
  desc?: string
}) {
  return (
    <div className='mb-8 text-center'>
      <div className='bg-muted/50 text-muted-foreground mb-3 inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium'>
        <Sparkles className='h-3 w-3' />
        {eyebrow}
      </div>
      <h2 className='text-3xl font-bold tracking-tight'>{title}</h2>
      {desc ? <p className='text-muted-foreground mt-2'>{desc}</p> : null}
    </div>
  )
}

export function Vpn() {
  const navigate = useNavigate()
  const { auth } = useAuthStore()
  const isAuthed = !!auth?.user

  const [payMethods, setPayMethods] = useState<
    { name: string; type: string }[]
  >([])
  const [onlineTopupEnabled, setOnlineTopupEnabled] = useState(false)
  const [selectedMethod, setSelectedMethod] = useState('')
  const [paying, setPaying] = useState(false)
  const [orders, setOrders] = useState<VpnOrder[]>([])
  const [keyResult, setKeyResult] = useState<VpnKeyResult | null>(null)
  const [copied, setCopied] = useState(false)
  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const firstLoadRef = useRef(true)
  const knownSuccessRef = useRef<Set<string>>(new Set())
  const paidFlagRef = useRef(false)

  const keyData = keyResult?.success ? keyResult.data : null
  const isExpired =
    !!keyResult && !keyResult.success && !!keyResult.data?.expired
  const hasPaid = orders.some((o) => o.status === 'success') || !!keyData
  const pendingOrders = orders.filter((o) => o.status === 'pending')
  const hasPending = pendingOrders.length > 0
  const latestPending = pendingOrders[0]
  const pendingAgeSec = latestPending
    ? Math.floor(Date.now() / 1000) - latestPending.create_time
    : 0
  // 订单刚创建时用 3 秒快轮询；长时间没确认就降到 30 秒，但**永不停止**：
  // 支付宝的异步通知可能迟到，停了轮询就等于放弃这一单。
  const pollMs = resolvePollIntervalMs(pendingAgeSec)

  const vpnState: VpnState = resolveVpnState({
    hasKey: !!keyData,
    isExpired,
    hasPending,
  })

  const refresh = useCallback(async () => {
    const [orderList, key] = await Promise.all([
      getMyVpnOrders(),
      getMyVpnKey(),
    ])
    setOrders(orderList)
    setKeyResult(key)

    const successNos = orderList
      .filter((o) => o.status === 'success')
      .map((o) => o.trade_no)
    if (firstLoadRef.current) {
      firstLoadRef.current = false
      // 首次加载只记录已有成功订单，否则「打开页面就弹支付成功」。
      if (
        paidFlagRef.current &&
        orderList.some((o) => o.status === 'pending')
      ) {
        toast.info('支付已提交，正在确认到账，请稍候…')
      }
    } else {
      // 只有「本次会话中新变成 success」的订单才提示，
      // 否则历史成功订单会让每次刷新都弹一次「支付成功」。
      const newlyPaid = successNos.filter(
        (no) => !knownSuccessRef.current.has(no)
      )
      if (newlyPaid.length > 0) {
        toast.success('支付成功！密钥已生成，可直接复制导入')
      }
    }
    knownSuccessRef.current = new Set(successNos)
    return orderList
  }, [])

  // 从支付宝同步跳回时会带 ?paid=1
  useEffect(() => {
    if (typeof window === 'undefined') return
    paidFlagRef.current =
      new URLSearchParams(window.location.search).get('paid') === '1'
  }, [])

  useEffect(() => {
    if (!isAuthed) return
    getTopupInfo()
      .then((info: TopupInfo | null) => {
        if (info?.enable_online_topup && info.pay_methods?.length) {
          setOnlineTopupEnabled(true)
          setPayMethods(info.pay_methods)
          setSelectedMethod(info.pay_methods[0].type)
        }
      })
      .catch(() => {
        // 支付方式拉取失败时保持「未开通」状态，由界面提示用户
      })
    refresh()
    return () => {
      if (pollTimer.current) clearInterval(pollTimer.current)
    }
  }, [isAuthed, refresh])

  // 存在待支付订单时轮询支付状态（支付在支付宝页面完成，回到本页自动刷新）
  useEffect(() => {
    if (!isAuthed || !hasPending) return
    pollTimer.current = setInterval(async () => {
      await refresh()
    }, pollMs)
    return () => {
      if (pollTimer.current) clearInterval(pollTimer.current)
    }
  }, [isAuthed, hasPending, pollMs, refresh])

  const handlePay = async () => {
    if (!isAuthed) {
      toast.info('请先登录后再购买')
      navigate({ to: '/sign-in' })
      return
    }
    if (!selectedMethod) return
    setPaying(true)
    const res = await createVpnOrder(selectedMethod)
    setPaying(false)
    if (!res.success || !res.url) {
      toast.error(res.message || '拉起支付失败')
      return
    }
    if (res.payMode === 'redirect') {
      // 支付宝官方直连：整页跳转到支付宝收银台
      if (!isTrustedAlipayUrl(res.url)) {
        toast.error('支付跳转地址异常，已取消本次支付，请重试')
        return
      }
      window.location.assign(res.url)
      return
    }
    // 易支付网关：需要浏览器 POST 一张表单
    submitPaymentForm(res.url, res.params ?? {})
    toast.success('已打开支付页面，支付完成后回到本页即可看到密钥')
    await refresh()
  }

  const handleCopyKey = async () => {
    if (!keyData?.key) return
    const ok = await copyToClipboard(keyData.key)
    if (ok) {
      setCopied(true)
      toast.success('密钥已复制到剪贴板')
      setTimeout(() => setCopied(false), 2000)
    } else {
      toast.error('复制失败，请手动选择复制')
    }
  }

  const remainingRatio = keyData?.expire_time
    ? Math.min(
        100,
        Math.max(
          0,
          ((keyData.expire_time * 1000 - Date.now()) / (PLAN_DAYS * 86400000)) *
            100
        )
      )
    : 0

  const keyPanelDescription = (() => {
    if (keyData) return '支付已确认，复制下方密钥到 v2rayN 即可使用'
    if (isExpired) return '套餐已到期，续费后密钥将重新开放'
    if (hasPending) return '已收到支付请求，正在等待支付确认'
    return '支付成功后，专属密钥将显示在这里'
  })()

  const keyPanelContent: ReactNode = (() => {
    if (keyData) {
      return (
        <>
          {keyData.expire_time ? (
            <div className='space-y-2'>
              <div className='text-muted-foreground flex items-center justify-between text-xs'>
                <span>有效期至 {formatDate(keyData.expire_time)}</span>
                <span>剩余 {daysLeft(keyData.expire_time)} 天</span>
              </div>
              <div className='bg-muted h-2 w-full overflow-hidden rounded-full'>
                <div
                  className='h-full rounded-full bg-gradient-to-r from-emerald-400 to-cyan-400'
                  style={{ width: `${remainingRatio}%` }}
                />
              </div>
            </div>
          ) : null}
          <div className='bg-muted/50 rounded-lg border p-4 font-mono text-xs leading-relaxed break-all'>
            {keyData.key}
          </div>
          <Button className='w-full' variant='outline' onClick={handleCopyKey}>
            {copied ? (
              <>
                <Check className='mr-2 h-4 w-4 text-emerald-500' />
                已复制
              </>
            ) : (
              <>
                <Copy className='mr-2 h-4 w-4' />
                一键复制密钥
              </>
            )}
          </Button>
          <div className='text-muted-foreground rounded-lg border border-dashed p-4 text-sm'>
            <div className='text-foreground mb-1 font-medium'>
              导入方法（v2rayN）：
            </div>
            打开 v2rayN → 「服务器」→「从剪贴板导入批量URL」→
            选中「日本东京」节点 → 回车设为活动服务器 →
            托盘图标右键「系统代理」→「自动配置系统代理」。
          </div>
        </>
      )
    }
    if (isExpired) {
      return (
        <div className='flex flex-col items-center gap-3 py-8 text-center'>
          <CalendarClock className='h-8 w-8 text-amber-500' />
          <div className='font-medium'>套餐已到期</div>
          <p className='text-muted-foreground text-sm'>
            有效期至 {formatDate(keyResult?.data?.expire_time ?? 0)}
            <br />
            续费 ¥{PLAN_PRICE} 后密钥将立即重新开放，剩余时长顺延一年。
          </p>
        </div>
      )
    }
    if (hasPending) {
      return (
        <div className='flex flex-col items-center gap-3 py-8 text-center'>
          <Loader2 className='text-primary h-8 w-8 animate-spin' />
          <div className='font-medium'>支付确认中...</div>
          <p className='text-muted-foreground text-sm'>
            完成支付后本页面会自动刷新并显示密钥；
            <br />
            如长时间未显示，请查看下方「最近订单」或联系客服。
          </p>
          {pendingAgeSec > PENDING_FAST_WINDOW_SEC ? (
            <div className='flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/5 p-3 text-left text-xs text-amber-600'>
              <CircleAlert className='mt-0.5 h-3.5 w-3.5 shrink-0' />
              <span>
                这一笔订单已超过 30 分钟仍未收到支付确认。若你确实已付款，
                请勿重复下单，联系客服核对即可（系统仍在持续确认）。
              </span>
            </div>
          ) : null}
        </div>
      )
    }
    return (
      <div className='text-muted-foreground flex flex-col items-center gap-3 py-8 text-center'>
        <Lock className='h-10 w-10 opacity-30' />
        <p className='text-sm'>
          尚未开通。
          <br />
          购买年费套餐后，专属密钥将在此处展示。
        </p>
      </div>
    )
  })()

  return (
    <PublicLayout>
      <div className='mx-auto max-w-6xl space-y-20 pb-20'>
        {/* Hero */}
        <section className='relative overflow-hidden rounded-3xl border bg-gradient-to-br from-indigo-950 via-slate-950 to-cyan-950 px-6 py-16 text-center text-white md:py-20'>
          <div className='pointer-events-none absolute -top-24 left-1/2 h-72 w-72 -translate-x-1/2 rounded-full bg-indigo-500/20 blur-3xl' />
          <div className='pointer-events-none absolute right-10 -bottom-24 h-64 w-64 rounded-full bg-cyan-500/20 blur-3xl' />
          <div className='relative space-y-6'>
            <Badge
              variant='secondary'
              className='mx-auto h-6 bg-white/10 px-3 text-white backdrop-blur'
            >
              <BadgeCheck className='mr-1 h-3.5 w-3.5' /> 全新上线 · 年付专享
            </Badge>
            <h1 className='mx-auto max-w-3xl text-4xl leading-tight font-extrabold tracking-tight md:text-5xl'>
              极速 · 安全 · 稳定的
              <span className='bg-gradient-to-r from-indigo-300 to-cyan-300 bg-clip-text text-transparent'>
                {' '}
                全球网络加速
              </span>
            </h1>
            <p className='mx-auto max-w-2xl text-base text-white/70 md:text-lg'>
              基于 VLESS + Reality 协议的下一代加速网络。支付宝官方直连支付，
              支付成功即刻获得专属密钥，一键导入 v2rayN 客户端，即买即用。
            </p>
            <div className='flex flex-wrap items-center justify-center gap-3 pt-2'>
              <Button
                size='lg'
                className='bg-white text-slate-900 hover:bg-white/90'
                onClick={() =>
                  document
                    .querySelector('#vpn-pricing')
                    ?.scrollIntoView({ behavior: 'smooth' })
                }
              >
                <Rocket className='mr-2 h-4 w-4' /> 立即订阅
              </Button>
              <Button
                size='lg'
                variant='outline'
                className='border-white/30 bg-transparent text-white hover:bg-white/10'
                onClick={() =>
                  document
                    .querySelector('#vpn-download')
                    ?.scrollIntoView({ behavior: 'smooth' })
                }
              >
                <Download className='mr-2 h-4 w-4' /> 下载客户端
              </Button>
            </div>
            <div className='mx-auto grid max-w-lg grid-cols-3 gap-4 pt-6 text-center'>
              <div>
                <div className='text-2xl font-bold'>Reality</div>
                <div className='text-xs text-white/60'>新一代协议</div>
              </div>
              <div>
                <div className='text-2xl font-bold'>东京</div>
                <div className='text-xs text-white/60'>低延迟直连节点</div>
              </div>
              <div>
                <div className='text-2xl font-bold'>¥199/年</div>
                <div className='text-xs text-white/60'>约合 ¥16.6/月</div>
              </div>
            </div>
          </div>
        </section>

        {/* Features */}
        <section>
          <SectionHeading
            eyebrow='为什么选择我们'
            title='稳定是硬道理'
            desc='协议、线路、加密、兼容性，四件事都做到位，用起来才叫省心。'
          />
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
            {FEATURES.map((f) => (
              <Card
                key={f.title}
                className='border-border/60 hover:border-primary/40 transition-colors'
              >
                <CardContent className='space-y-3 p-6'>
                  <div className='bg-primary/10 flex h-10 w-10 items-center justify-center rounded-lg'>
                    <f.icon className='text-primary h-5 w-5' />
                  </div>
                  <div className='font-semibold'>{f.title}</div>
                  <p className='text-muted-foreground text-sm'>{f.desc}</p>
                </CardContent>
              </Card>
            ))}
          </div>
        </section>

        {/* Pricing */}
        <section id='vpn-pricing' className='scroll-mt-24'>
          <SectionHeading
            eyebrow='套餐与定价'
            title='一次付费，全年畅用'
            desc='支付成功即刻下发密钥，无需等待人工发货。'
          />

          <div className='grid gap-6 lg:grid-cols-5'>
            {/* 年费套餐卡：角标放在 Card 之外，
                Card 自带 overflow-hidden，角标放里面会被裁掉。 */}
            <div className='relative pt-5 lg:col-span-3'>
              <div className='absolute top-0 left-1/2 z-20 -translate-x-1/2'>
                <Badge className='h-6 rounded-full bg-gradient-to-r from-indigo-500 to-cyan-500 px-3 text-[11px] font-semibold text-white shadow-lg shadow-indigo-500/30'>
                  <Sparkles className='h-3 w-3' />
                  推荐套餐
                </Badge>
              </div>

              <Card className='border-primary/40 shadow-primary/10 h-full shadow-lg'>
                <CardHeader className='space-y-1.5 text-center'>
                  <CardTitle className='text-xl'>VPN 加速 · 年费套餐</CardTitle>
                  <CardDescription>
                    日本东京节点 · VLESS Reality · 不限设备数（合理使用）
                  </CardDescription>
                </CardHeader>
                <CardContent className='space-y-6'>
                  <div className='border-primary/20 from-primary/[0.08] via-background to-background rounded-2xl border bg-gradient-to-br p-6 text-center'>
                    <div className='flex items-baseline justify-center gap-1'>
                      <span className='text-5xl font-extrabold tracking-tight'>
                        ¥{PLAN_PRICE}
                      </span>
                      <span className='text-muted-foreground'>/ 年</span>
                    </div>
                    <div className='mt-2 flex flex-wrap items-center justify-center gap-2 text-sm'>
                      <span className='text-muted-foreground line-through'>
                        ¥{PLAN_ORIGINAL_PRICE}
                      </span>
                      <Badge
                        variant='secondary'
                        className='h-5 border-emerald-500/30 bg-emerald-500/10 text-emerald-600'
                      >
                        省 ¥{PLAN_ORIGINAL_PRICE - PLAN_PRICE} · 约 ¥
                        {(PLAN_PRICE / 12).toFixed(1)}/月
                      </Badge>
                    </div>
                  </div>

                  <Separator />

                  <ul className='grid gap-2.5 text-sm sm:grid-cols-2'>
                    {PLAN_FEATURES.map((item) => (
                      <li key={item} className='flex items-start gap-2'>
                        <span className='bg-primary/10 mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full'>
                          <Check className='text-primary h-3 w-3' />
                        </span>
                        <span className='text-muted-foreground'>{item}</span>
                      </li>
                    ))}
                  </ul>

                  {isAuthed && onlineTopupEnabled ? (
                    <div className='bg-muted/30 space-y-4 rounded-2xl border p-4'>
                      <div className='flex items-center justify-between'>
                        <span className='flex items-center gap-1.5 text-sm font-semibold'>
                          <CreditCard className='text-muted-foreground h-4 w-4' />
                          支付方式
                        </span>
                        <span className='inline-flex items-center gap-1 text-xs text-emerald-600'>
                          <ShieldCheck className='h-3.5 w-3.5' />
                          官方通道 · 即时到账
                        </span>
                      </div>
                      <div className='flex flex-wrap gap-2'>
                        {payMethods.map((m) => {
                          const active = selectedMethod === m.type
                          const isAli = m.type === 'alipay'
                          return (
                            <button
                              key={m.type}
                              type='button'
                              onClick={() => setSelectedMethod(m.type)}
                              className={cn(
                                'flex items-center gap-2 rounded-lg border px-4 py-2.5 text-sm font-medium transition-all',
                                active
                                  ? 'border-primary bg-primary/10 text-foreground shadow-sm'
                                  : 'border-border bg-background text-muted-foreground hover:border-primary/50'
                              )}
                            >
                              {isAli ? (
                                <SiAlipay className='h-5 w-5' />
                              ) : (
                                <span className='bg-primary h-2.5 w-2.5 rounded-full' />
                              )}
                              {isAli ? '支付宝' : m.name || m.type}
                              {isAli ? (
                                <span className='bg-primary/10 text-primary rounded px-1.5 py-0.5 text-[10px] font-semibold'>
                                  官方直连
                                </span>
                              ) : null}
                            </button>
                          )
                        })}
                      </div>
                      <Button
                        size='lg'
                        className='w-full text-base'
                        disabled={paying || !selectedMethod}
                        onClick={handlePay}
                      >
                        {paying ? (
                          <>
                            <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                            正在拉起支付...
                          </>
                        ) : (
                          <>
                            <Lock className='mr-2 h-4 w-4' />
                            {hasPaid
                              ? `续费（¥${PLAN_PRICE}/年）`
                              : `立即支付 ¥${PLAN_PRICE}`}
                          </>
                        )}
                      </Button>
                      <div className='text-muted-foreground grid grid-cols-3 gap-2 text-center text-[11px]'>
                        <span>支付宝官方直连</span>
                        <span>支付即到账</span>
                        <span>订单可追溯</span>
                      </div>
                    </div>
                  ) : (
                    <div className='bg-muted/30 space-y-3 rounded-2xl border p-4'>
                      <Button
                        size='lg'
                        className='w-full text-base'
                        onClick={() =>
                          isAuthed
                            ? toast.error('在线支付暂未开通，请联系管理员')
                            : navigate({ to: '/sign-in' })
                        }
                      >
                        {isAuthed ? `立即支付 ¥${PLAN_PRICE}` : '登录后购买'}
                      </Button>
                      <p className='text-muted-foreground text-center text-xs'>
                        {isAuthed
                          ? '当前站点尚未开通在线支付，请联系管理员。'
                          : '登录后即可使用支付宝官方直连购买。'}
                      </p>
                    </div>
                  )}
                </CardContent>
              </Card>
            </div>

            {/* 密钥卡 */}
            <div className='lg:col-span-2'>
              <Card className='border-border/60 h-full'>
                <CardHeader>
                  <div className='flex items-start justify-between gap-3'>
                    <CardTitle className='flex items-center gap-2 text-xl'>
                      <BadgeCheck className='h-5 w-5 text-emerald-500' />
                      我的专属密钥
                    </CardTitle>
                    <Badge
                      variant='outline'
                      className={cn('h-5', STATE_CHIP[vpnState].className)}
                    >
                      {STATE_CHIP[vpnState].label}
                    </Badge>
                  </div>
                  <CardDescription>{keyPanelDescription}</CardDescription>
                </CardHeader>
                <CardContent className='space-y-4'>
                  {keyPanelContent}
                </CardContent>
              </Card>
            </div>
          </div>

          {/* 最近订单：让「我付了没 / 到底付没付」一眼可见 */}
          {orders.length > 0 ? (
            <div className='bg-card mt-6 rounded-2xl border p-6'>
              <div className='mb-4 flex items-center justify-between'>
                <h3 className='font-semibold'>最近订单</h3>
                <span className='text-muted-foreground text-xs'>
                  共 {orders.length} 笔
                </span>
              </div>
              <div className='divide-y'>
                {orders.slice(0, 5).map((o) => (
                  <div
                    key={o.id}
                    className='flex flex-wrap items-center justify-between gap-2 py-3 text-sm'
                  >
                    <div className='flex min-w-0 items-center gap-3'>
                      <span className='text-muted-foreground truncate font-mono text-xs'>
                        {o.trade_no}
                      </span>
                      <span className='text-muted-foreground text-xs'>
                        {formatDateTime(o.create_time)}
                      </span>
                    </div>
                    <div className='flex items-center gap-3'>
                      <span className='font-medium'>
                        ¥{Number(o.money).toFixed(2)}
                      </span>
                      <Badge
                        variant='outline'
                        className={cn('h-5', ORDER_STATUS[o.status].className)}
                      >
                        {ORDER_STATUS[o.status].label}
                      </Badge>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ) : null}
        </section>

        {/* Download */}
        <section id='vpn-download' className='scroll-mt-24'>
          <SectionHeading
            eyebrow='下载客户端'
            title='v2rayN 官方最新版'
            desc='支持支付宝购买的密钥直接导入，Windows / macOS 一码通用。'
          />
          <div className='grid gap-4 sm:grid-cols-2'>
            <Card className='group hover:border-primary/50 transition-colors'>
              <CardContent className='flex items-center gap-4 p-6'>
                <div className='bg-primary/10 flex h-14 w-14 items-center justify-center rounded-xl'>
                  <Monitor className='text-primary h-7 w-7' />
                </div>
                <div className='flex-1'>
                  <div className='font-semibold'>Windows 版</div>
                  <div className='text-muted-foreground text-sm'>
                    v2rayN-windows-64 · 64位 · 自带运行库
                  </div>
                  <div className='text-muted-foreground/70 mt-0.5 text-xs'>
                    解压即用，无需安装 .NET / WebView2
                  </div>
                </div>
                <Button
                  render={
                    <a
                      href={V2RAYN_WINDOWS_URL}
                      target='_blank'
                      rel='noreferrer'
                    />
                  }
                >
                  <Download className='mr-2 h-4 w-4' />
                  下载
                </Button>
              </CardContent>
            </Card>
            <Card className='group hover:border-primary/50 transition-colors'>
              <CardContent className='flex items-center gap-4 p-6'>
                <div className='bg-primary/10 flex h-14 w-14 items-center justify-center rounded-xl'>
                  <Laptop className='text-primary h-7 w-7' />
                </div>
                <div className='flex-1'>
                  <div className='font-semibold'>macOS 版</div>
                  <div className='text-muted-foreground text-sm'>
                    按芯片选择 · 打开 dmg 拖入 Applications
                  </div>
                </div>
              </CardContent>
              <div className='flex gap-2 px-6 pb-6'>
                <Button
                  className='flex-1'
                  render={
                    <a
                      href={V2RAYN_MACOS_ARM_URL}
                      target='_blank'
                      rel='noreferrer'
                    />
                  }
                >
                  <Download className='mr-2 h-4 w-4' />
                  Apple 芯片
                </Button>
                <Button
                  variant='outline'
                  className='flex-1'
                  render={
                    <a
                      href={V2RAYN_MACOS_INTEL_URL}
                      target='_blank'
                      rel='noreferrer'
                    />
                  }
                >
                  <Download className='mr-2 h-4 w-4' />
                  Intel 芯片
                </Button>
              </div>
            </Card>
          </div>
          <p className='text-muted-foreground mt-4 text-center text-xs'>
            下载速度慢？
            <a
              className='text-primary hover:underline'
              href={V2RAYN_RELEASES_URL}
              target='_blank'
              rel='noreferrer'
            >
              前往 GitHub Releases
            </a>
            选择适合你系统的版本。
          </p>
        </section>

        {/* Steps */}
        <section>
          <SectionHeading
            eyebrow='使用教程'
            title='四步开启加速'
            desc='从下载到连通，四步之内完成。'
          />
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
            {STEPS.map((s, i) => (
              <Card key={s.title}>
                <CardContent className='space-y-3 p-6'>
                  <div className='flex items-center gap-3'>
                    <div className='bg-primary text-primary-foreground flex h-9 w-9 items-center justify-center rounded-full text-sm font-bold'>
                      {i + 1}
                    </div>
                    <s.icon className='text-muted-foreground h-5 w-5' />
                  </div>
                  <div className='font-semibold'>{s.title}</div>
                  <p className='text-muted-foreground text-sm'>{s.desc}</p>
                </CardContent>
              </Card>
            ))}
          </div>
          <div className='text-muted-foreground mx-auto mt-6 flex max-w-2xl items-start gap-3 rounded-xl border border-dashed p-4 text-sm'>
            <Smartphone className='mt-0.5 h-4 w-4 shrink-0' />
            <span>
              小提示：v2rayN 首次运行后，可在「设置 → 参数设置 →
              核心：基础设置」中选择「自动配置系统代理」，浏览器无需手动设置代理即可生效。
            </span>
          </div>
        </section>

        {/* FAQ */}
        <section>
          <SectionHeading
            eyebrow='常见问题'
            title='还有疑问？'
            desc='这里汇总了购买与使用中最常被问到的几个问题。'
          />
          <div className='mx-auto max-w-3xl space-y-3'>
            {FAQS.map((f) => (
              <Card key={f.q}>
                <CardContent className='p-5'>
                  <div className='font-medium'>{f.q}</div>
                  <p className='text-muted-foreground mt-1 text-sm'>{f.a}</p>
                </CardContent>
              </Card>
            ))}
          </div>
          <p className='text-muted-foreground mt-10 text-center text-xs'>
            本服务为网络加速技术服务，请遵守当地法律法规合理使用。
          </p>
        </section>
      </div>
    </PublicLayout>
  )
}
