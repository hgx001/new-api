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
import { useNavigate } from '@tanstack/react-router'
import {
  BadgeCheck,
  CalendarClock,
  Check,
  Copy,
  Download,
  Globe,
  Laptop,
  Loader2,
  Lock,
  Monitor,
  QrCode,
  Rocket,
  Shield,
  Smartphone,
  Zap,
} from 'lucide-react'
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
import { useAuthStore } from '@/stores/auth-store'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { cn } from '@/lib/utils'
import { submitPaymentForm } from '@/features/wallet/lib/payment'

import {
  createVpnOrder,
  getMyVpnKey,
  getMyVpnOrders,
  getTopupInfo,
  type TopupInfo,
  type VpnKeyResult,
  type VpnOrder,
} from './api'

const V2RAYN_VERSION = '7.24.8'
const V2RAYN_WINDOWS_URL = `https://github.com/2dust/v2rayN/releases/download/${V2RAYN_VERSION}/v2rayN-windows-64.zip`
const V2RAYN_MACOS_ARM_URL = `https://github.com/2dust/v2rayN/releases/download/${V2RAYN_VERSION}/v2rayN-macos-arm64.dmg`
const V2RAYN_MACOS_INTEL_URL = `https://github.com/2dust/v2rayN/releases/download/${V2RAYN_VERSION}/v2rayN-macos-64.dmg`
const V2RAYN_RELEASES_URL = 'https://github.com/2dust/v2rayN/releases'

function formatDate(ts: number) {
  return new Date(ts * 1000).toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })
}

function daysLeft(expireTs: number) {
  return Math.max(0, Math.ceil((expireTs * 1000 - Date.now()) / 86400000))
}

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

  const keyData = keyResult?.success ? keyResult.data : null
  const isExpired = !!keyResult && !keyResult.success && !!keyResult.data?.expired
  const hasPaid = orders.some((o) => o.status === 'success') || !!keyData
  const hasPending = orders.some((o) => o.status === 'pending')

  const refresh = useCallback(async () => {
    const [orderList, key] = await Promise.all([getMyVpnOrders(), getMyVpnKey()])
    setOrders(orderList)
    setKeyResult(key)
    return orderList
  }, [])

  useEffect(() => {
    if (!isAuthed) return
    getTopupInfo().then((info: TopupInfo | null) => {
      if (info?.enable_online_topup && info.pay_methods?.length) {
        setOnlineTopupEnabled(true)
        setPayMethods(info.pay_methods)
        setSelectedMethod(info.pay_methods[0].type)
      }
    })
    refresh()
    return () => {
      if (pollTimer.current) clearInterval(pollTimer.current)
    }
  }, [isAuthed, refresh])

  // 存在待支付订单时轮询支付状态（支付在新标签页完成，回到本页自动刷新）
  useEffect(() => {
    if (!isAuthed || !hasPending) return
    pollTimer.current = setInterval(async () => {
      const list = await refresh()
      if (list.some((o) => o.status === 'success')) {
        if (pollTimer.current) clearInterval(pollTimer.current)
        toast.success('支付成功！密钥已生成')
      }
    }, 3000)
    return () => {
      if (pollTimer.current) clearInterval(pollTimer.current)
    }
  }, [isAuthed, hasPending, refresh])

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
    submitPaymentForm(res.url, (res.params as Record<string, unknown>) ?? {})
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

  return (
    <PublicLayout>
      <div className='mx-auto max-w-6xl space-y-16 pb-20'>
        {/* Hero */}
        <section className='relative overflow-hidden rounded-3xl border bg-gradient-to-br from-indigo-950 via-slate-950 to-cyan-950 px-6 py-16 text-center text-white md:py-20'>
          <div className='pointer-events-none absolute -top-24 left-1/2 h-72 w-72 -translate-x-1/2 rounded-full bg-indigo-500/20 blur-3xl' />
          <div className='pointer-events-none absolute -bottom-24 right-10 h-64 w-64 rounded-full bg-cyan-500/20 blur-3xl' />
          <div className='relative space-y-6'>
            <Badge
              variant='secondary'
              className='mx-auto bg-white/10 text-white backdrop-blur'
            >
              <BadgeCheck className='mr-1 h-3.5 w-3.5' /> 全新上线 · 年付专享
            </Badge>
            <h1 className='mx-auto max-w-3xl text-4xl font-extrabold leading-tight tracking-tight md:text-5xl'>
              极速 · 安全 · 稳定的
              <span className='bg-gradient-to-r from-indigo-300 to-cyan-300 bg-clip-text text-transparent'>
                {' '}
                全球网络加速
              </span>
            </h1>
            <p className='mx-auto max-w-2xl text-base text-white/70 md:text-lg'>
              基于 VLESS + Reality 协议的下一代加速网络。支付后即刻获得专属密钥，
              一键导入 v2rayN 客户端，即买即用。
            </p>
            <div className='flex flex-wrap items-center justify-center gap-3 pt-2'>
              <Button
                size='lg'
                className='bg-white text-slate-900 hover:bg-white/90'
                onClick={() =>
                  document
                    .getElementById('vpn-pricing')
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
                    .getElementById('vpn-download')
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
          <h2 className='mb-8 text-center text-3xl font-bold'>
            为什么选择我们
          </h2>
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
            {FEATURES.map((f) => (
              <Card key={f.title} className='border-border/60'>
                <CardContent className='space-y-3 p-6'>
                  <div className='flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10'>
                    <f.icon className='h-5 w-5 text-primary' />
                  </div>
                  <div className='font-semibold'>{f.title}</div>
                  <p className='text-sm text-muted-foreground'>{f.desc}</p>
                </CardContent>
              </Card>
            ))}
          </div>
        </section>

        {/* Pricing */}
        <section id='vpn-pricing' className='scroll-mt-24'>
          <h2 className='mb-2 text-center text-3xl font-bold'>套餐与定价</h2>
          <p className='mb-8 text-center text-muted-foreground'>
            一次付费，全年畅用。支付成功即刻下发密钥。
          </p>
          <div className='grid gap-6 md:grid-cols-2'>
            {/* 年费套餐卡 */}
            <Card className='relative border-primary/40 shadow-lg shadow-primary/5'>
              <div className='absolute -top-3 left-1/2 -translate-x-1/2'>
                <Badge className='bg-primary text-primary-foreground'>
                  推荐套餐
                </Badge>
              </div>
              <CardHeader className='pt-8 text-center'>
                <CardTitle className='text-xl'>VPN 加速 · 年费套餐</CardTitle>
                <CardDescription>
                  日本东京节点 · VLESS Reality · 不限设备数（合理使用）
                </CardDescription>
              </CardHeader>
              <CardContent className='space-y-6'>
                <div className='text-center'>
                  <span className='text-5xl font-extrabold'>¥199</span>
                  <span className='text-muted-foreground'> / 年</span>
                  <div className='mt-1 text-sm text-muted-foreground'>
                    <span className='line-through'>¥399</span> ·
                    相当于每月仅 ¥16.6
                  </div>
                </div>
                <Separator />
                <ul className='space-y-2 text-sm'>
                  {[
                    '日本东京 IEPL 优化线路',
                    'Reality 协议，稳定抗干扰',
                    '支付成功即时下发导入密钥',
                    'Windows / macOS 全平台可用',
                    '到期前可随时续费',
                  ].map((item) => (
                    <li key={item} className='flex items-center gap-2'>
                      <Check className='h-4 w-4 shrink-0 text-primary' />
                      {item}
                    </li>
                  ))}
                </ul>

                {isAuthed && onlineTopupEnabled ? (
                  <div className='space-y-4 rounded-xl border border-emerald-500/20 bg-emerald-500/[0.04] p-4'>
                    <div className='flex items-center justify-between'>
                      <span className='text-sm font-semibold'>支付方式</span>
                      <span className='inline-flex items-center gap-1 text-xs text-emerald-600'>
                        <Shield className='h-3.5 w-3.5' />
                        加密 · 即时到账
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
                                ? 'border-emerald-500 bg-emerald-500/10 text-foreground shadow-sm'
                                : 'border-border bg-background text-muted-foreground hover:border-emerald-400/50'
                            )}
                          >
                            {isAli ? (
                              <span className='flex h-5 w-5 items-center justify-center rounded bg-[#1677ff] text-[11px] font-bold text-white'>
                                支
                              </span>
                            ) : (
                              <span className='h-2.5 w-2.5 rounded-full bg-primary' />
                            )}
                            {isAli ? '支付宝' : m.name || m.type}
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
                          {hasPaid ? '续费（¥199/年）' : '立即支付 ¥199'}
                        </>
                      )}
                    </Button>
                    <p className='text-center text-xs text-muted-foreground'>
                      点击后将打开支付宝支付页面，支付完成后回到本页即可查看密钥
                    </p>
                  </div>
                ) : (
                  <Button
                    size='lg'
                    className='w-full text-base'
                    onClick={() =>
                      isAuthed
                        ? toast.error('在线支付暂未开通，请联系管理员')
                        : navigate({ to: '/sign-in' })
                    }
                  >
                    {isAuthed ? '立即支付 ¥199' : '登录后购买'}
                  </Button>
                )}
              </CardContent>
            </Card>

            {/* 密钥卡 */}
            <Card className='border-emerald-500/30 bg-emerald-500/[0.03]'>
              <CardHeader>
                <CardTitle className='flex items-center gap-2 text-xl'>
                  <BadgeCheck className='h-5 w-5 text-emerald-500' />
                  我的专属密钥
                </CardTitle>
                <CardDescription>
                  {keyData
                    ? '支付已确认，复制下方密钥到 v2rayN 即可使用'
                    : isExpired
                      ? '套餐已到期，续费后密钥将重新开放'
                      : '支付成功后，专属密钥将显示在这里'}
                </CardDescription>
              </CardHeader>
              <CardContent className='space-y-4'>
                {keyData ? (
                  <>
                    {keyData.expire_time && (
                      <div className='flex items-center justify-between rounded-lg border bg-background/60 px-4 py-2.5 text-sm'>
                        <span className='text-muted-foreground'>有效期至</span>
                        <span className='font-medium'>
                          {formatDate(keyData.expire_time)}
                          <span className='ml-2 text-xs text-muted-foreground'>
                            （剩余 {daysLeft(keyData.expire_time)} 天）
                          </span>
                        </span>
                      </div>
                    )}
                    <div className='break-all rounded-lg border bg-muted/50 p-4 font-mono text-xs leading-relaxed'>
                      {keyData.key}
                    </div>
                    <Button
                      className='w-full'
                      variant='outline'
                      onClick={handleCopyKey}
                    >
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
                    <div className='rounded-lg border border-dashed p-4 text-sm text-muted-foreground'>
                      <div className='mb-1 font-medium text-foreground'>
                        导入方法（v2rayN）：
                      </div>
                      打开 v2rayN → 「服务器」→「从剪贴板导入批量URL」→
                      选中「日本东京」节点 → 回车设为活动服务器 →
                      托盘图标右键「系统代理」→「自动配置系统代理」。
                    </div>
                  </>
                ) : isExpired ? (
                  <div className='flex flex-col items-center gap-3 py-10 text-center'>
                    <CalendarClock className='h-8 w-8 text-amber-500' />
                    <div className='font-medium'>套餐已到期</div>
                    <p className='text-sm text-muted-foreground'>
                      有效期至 {formatDate(keyResult?.data?.expire_time ?? 0)}
                      <br />
                      续费 ¥199 后密钥将立即重新开放，剩余时长顺延一年。
                    </p>
                  </div>
                ) : hasPending ? (
                  <div className='flex flex-col items-center gap-3 py-10 text-center'>
                    <Loader2 className='h-8 w-8 animate-spin text-primary' />
                    <div className='font-medium'>支付确认中...</div>
                    <p className='text-sm text-muted-foreground'>
                      完成支付后本页面会自动刷新并显示密钥；
                      <br />
                      如长时间未显示，请刷新页面或重新进入。
                    </p>
                  </div>
                ) : (
                  <div className='flex flex-col items-center gap-3 py-10 text-center text-muted-foreground'>
                    <Lock className='h-10 w-10 opacity-30' />
                    <p className='text-sm'>
                      尚未开通。
                      <br />
                      购买年费套餐后，专属密钥将在此处展示。
                    </p>
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </section>

        {/* Download */}
        <section id='vpn-download' className='scroll-mt-24'>
          <h2 className='mb-2 text-center text-3xl font-bold'>下载客户端</h2>
          <p className='mb-8 text-center text-muted-foreground'>
            v2rayN v{V2RAYN_VERSION}（官方最新版） · 支持支付宝购买的密钥直接导入
          </p>
          <div className='grid gap-4 sm:grid-cols-2'>
            <Card className='group transition-colors hover:border-primary/50'>
              <CardContent className='flex items-center gap-4 p-6'>
                <div className='flex h-14 w-14 items-center justify-center rounded-xl bg-primary/10'>
                  <Monitor className='h-7 w-7 text-primary' />
                </div>
                <div className='flex-1'>
                  <div className='font-semibold'>Windows 版</div>
                  <div className='text-sm text-muted-foreground'>
                    v2rayN-windows-64 · 64位 · 自带运行库
                  </div>
                  <div className='mt-0.5 text-xs text-muted-foreground/70'>
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
            <Card className='group transition-colors hover:border-primary/50'>
              <CardContent className='flex items-center gap-4 p-6'>
                <div className='flex h-14 w-14 items-center justify-center rounded-xl bg-primary/10'>
                  <Laptop className='h-7 w-7 text-primary' />
                </div>
                <div className='flex-1'>
                  <div className='font-semibold'>macOS 版</div>
                  <div className='text-sm text-muted-foreground'>
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
                  Intel 芯片
                </Button>
              </div>
            </Card>
          </div>
          <p className='mt-4 text-center text-xs text-muted-foreground'>
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
          <h2 className='mb-8 text-center text-3xl font-bold'>使用教程</h2>
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
            {STEPS.map((s, i) => (
              <Card key={s.title}>
                <CardContent className='space-y-3 p-6'>
                  <div className='flex items-center gap-3'>
                    <div className='flex h-9 w-9 items-center justify-center rounded-full bg-primary text-sm font-bold text-primary-foreground'>
                      {i + 1}
                    </div>
                    <s.icon className='h-5 w-5 text-muted-foreground' />
                  </div>
                  <div className='font-semibold'>{s.title}</div>
                  <p className='text-sm text-muted-foreground'>{s.desc}</p>
                </CardContent>
              </Card>
            ))}
          </div>
          <div className='mx-auto mt-6 flex max-w-2xl items-start gap-3 rounded-xl border border-dashed p-4 text-sm text-muted-foreground'>
            <Smartphone className='mt-0.5 h-4 w-4 shrink-0' />
            <span>
              小提示：v2rayN 首次运行后，可在「设置 → 参数设置 →
              核心：基础设置」中选择「自动配置系统代理」，浏览器无需手动设置代理即可生效。
            </span>
          </div>
        </section>

        {/* FAQ */}
        <section>
          <h2 className='mb-8 text-center text-3xl font-bold'>常见问题</h2>
          <div className='mx-auto max-w-3xl space-y-3'>
            {[
              {
                q: '支付成功后没看到密钥？',
                a: '支付在新标签页完成，回到本页后系统每 3 秒自动确认一次；如仍未显示，刷新页面或重新登录后查看「我的专属密钥」卡片。',
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
            ].map((f) => (
              <Card key={f.q}>
                <CardContent className='p-5'>
                  <div className='font-medium'>{f.q}</div>
                  <p className='mt-1 text-sm text-muted-foreground'>{f.a}</p>
                </CardContent>
              </Card>
            ))}
          </div>
          <p className='mt-10 text-center text-xs text-muted-foreground'>
            本服务为网络加速技术服务，请遵守当地法律法规合理使用。
          </p>
        </section>
      </div>
    </PublicLayout>
  )
}
