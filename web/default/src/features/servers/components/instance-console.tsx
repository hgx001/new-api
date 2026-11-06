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

import {
  Activity,
  Copy,
  Gauge,
  Gpu,
  KeyRound,
  LifeBuoy,
  MapPin,
  Network,
  Play,
  Power,
  Receipt,
  RefreshCcw,
  Server as ServerIcon,
  Square,
  Timer,
  Trash2,
  Wrench,
} from 'lucide-react'
import { useMemo, useState, type ElementType, type ReactNode } from 'react'
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Separator } from '@/components/ui/separator'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { cn } from '@/lib/utils'

import { useServerEvents, useServerMetrics } from '../hooks/use-servers'
import {
  daysLeft,
  emptyMetrics,
  formatDate,
  formatDateTime,
  hoursLeft,
  isExpired,
  resolveStatus,
  statusKeyOf,
} from '../lib/instance-runtime'
import { formatAmount } from '../lib/pricing'
import { formatDataDiskExpand } from '../constants'
import type { ServerInstance } from '../types'
import { CredentialField } from './credential-field'
import { MetricBar, MetricChart } from './metric-chart'
import { OrderHistoryDialog } from './order-history-dialog'
import { ReleaseDialog } from './release-dialog'
import { RenewDialog } from './renew-dialog'

interface InstanceConsoleProps {
  servers: ServerInstance[]
  loading?: boolean
  onChangePassword: (server: ServerInstance) => void
  onBrowse: () => void
  /** 开关机：服务端确认后才更新列表 */
  onTogglePower: (server: ServerInstance, running: boolean) => void
  onReboot: (server: ServerInstance) => void
  /** 续费成功返回 true，续费弹窗据此关闭 */
  onRenew: (server: ServerInstance, durationId: string) => Promise<boolean>
  /** 释放成功返回 true */
  onRelease: (server: ServerInstance) => Promise<boolean>
}

type StatusFilter = 'all' | 'running' | 'stopped' | 'expiring'

const FILTERS: { id: StatusFilter; labelKey: string }[] = [
  { id: 'all', labelKey: 'All' },
  { id: 'running', labelKey: 'Running' },
  { id: 'stopped', labelKey: 'Stopped' },
  { id: 'expiring', labelKey: 'Expiring soon' },
]

function statusBadgeClass(instance: ServerInstance): string {
  const key = statusKeyOf(instance)
  if (key === 'Running') return 'bg-emerald-500/15 text-emerald-600'
  if (key === 'Stopped') return 'bg-slate-500/15 text-slate-600'
  if (key === 'Expiring soon') return 'bg-amber-500/15 text-amber-600'
  if (key === 'Provisioning') return 'bg-blue-500/15 text-blue-600'
  return 'bg-red-500/15 text-red-600'
}

/**
 * 云服务器实例控制台。
 *
 * 参照云厂商（ECS 类）的实例管理视图：**列表 + 详情抽屉**，
 * 而不是购买页那种卡片墙。数据全部来自服务端：
 * 状态、到期时间、凭据、监控、操作记录都以接口返回为准。
 */
export function InstanceConsole(props: InstanceConsoleProps) {
  const { t } = useTranslation()
  const [filter, setFilter] = useState<StatusFilter>('all')
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [renewTarget, setRenewTarget] = useState<ServerInstance | null>(null)
  const [releaseTarget, setReleaseTarget] = useState<ServerInstance | null>(
    null
  )
  const [billingOpen, setBillingOpen] = useState(false)

  const filtered = useMemo(() => {
    switch (filter) {
      case 'running':
        return props.servers.filter(
          (server) => resolveStatus(server) === 'running'
        )
      case 'stopped':
        return props.servers.filter(
          (server) => resolveStatus(server) === 'stopped'
        )
      case 'expiring':
        return props.servers.filter((server) => {
          const key = statusKeyOf(server)
          return key === 'Expiring soon' || key === 'Expired'
        })
      default:
        return props.servers
    }
  }, [props.servers, filter])

  const selected = props.servers.find((server) => server.id === selectedId)

  const runningCount = props.servers.filter(
    (server) => resolveStatus(server) === 'running'
  ).length
  const stoppedCount = props.servers.filter(
    (server) => resolveStatus(server) === 'stopped'
  ).length
  const expiringCount = props.servers.filter((server) => {
    const key = statusKeyOf(server)
    return key === 'Expiring soon' || key === 'Expired'
  }).length

  const copyText = (value: string, messageKey: string) => {
    void copyToClipboard(value).then((ok) => {
      if (ok) toast.success(t(messageKey))
      else toast.error(t('Copy failed, please select and copy manually'))
    })
  }

  if (props.servers.length === 0) {
    return (
      <div className='bg-card/60 rounded-2xl border border-dashed px-6 py-16 text-center'>
        <div className='mx-auto flex size-14 items-center justify-center rounded-2xl bg-[#e8f4f0]'>
          <ServerIcon className='size-6 text-[#3a8f7a]' />
        </div>
        <h3 className='mt-4 text-lg font-semibold'>
          {props.loading ? t('Loading your servers...') : t('No servers yet')}
        </h3>
        <p className='text-muted-foreground mx-auto mt-2 max-w-md text-sm'>
          {t(
            'Configure a server and complete the payment. The instance details will show up here.'
          )}
        </p>
        <Button
          className='mt-6 rounded-full bg-[#3a8f7a] text-white hover:bg-[#2f7a66]'
          onClick={props.onBrowse}
        >
          {t('Configure a server')}
        </Button>
        {/* 没有实例也可能是退过款的老用户，账单入口不能跟着实例一起消失 */}
        <div className='mt-3'>
          <Button
            variant='ghost'
            className='rounded-full'
            onClick={() => setBillingOpen(true)}
          >
            <Receipt className='size-4' />
            {t('Billing history')}
          </Button>
        </div>
        <OrderHistoryDialog
          open={billingOpen}
          onOpenChange={setBillingOpen}
        />
      </div>
    )
  }

  return (
    <div className='space-y-5'>
      {/* 概览统计条：云厂商控制台的第一屏是「我有多少资源」 */}
      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
        <StatCard
          icon={ServerIcon}
          label={t('Total instances')}
          value={props.servers.length}
        />
        <StatCard
          icon={Power}
          label={t('Running')}
          value={runningCount}
          tone='emerald'
        />
        <StatCard
          icon={Square}
          label={t('Stopped')}
          value={stoppedCount}
          tone='slate'
        />
        <StatCard
          icon={Timer}
          label={t('Expiring soon')}
          value={expiringCount}
          tone='amber'
        />
      </div>

      {/* 筛选条 */}
      <div className='border-border/70 bg-card flex flex-wrap items-center gap-2 rounded-xl border px-3 py-2.5'>
        {FILTERS.map((item) => (
          <button
            key={item.id}
            type='button'
            onClick={() => setFilter(item.id)}
            className={cn(
              'rounded-lg px-3 py-1.5 text-xs font-medium transition-colors',
              filter === item.id
                ? 'bg-[#e8f4f0] text-[#1a4a3f]'
                : 'text-muted-foreground hover:bg-muted hover:text-foreground'
            )}
          >
            {t(item.labelKey)}
          </button>
        ))}
        <span className='text-muted-foreground ml-auto text-xs'>
          {t('{{count}} instance(s)', { count: filtered.length })}
        </span>
        <button
          type='button'
          onClick={() => setBillingOpen(true)}
          className='text-muted-foreground hover:bg-muted hover:text-foreground flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-medium transition-colors'
        >
          <Receipt className='size-3.5' />
          {t('Billing history')}
        </button>
      </div>

      {/* 实例列表 */}
      {filtered.length === 0 ? (
        <div className='bg-card/60 rounded-2xl border border-dashed px-6 py-14 text-center'>
          <div className='text-muted-foreground text-sm'>
            {t('No instances match the current filter')}
          </div>
        </div>
      ) : (
        <div className='space-y-3'>
          {filtered.map((server) => (
            <InstanceRow
              key={server.id}
              server={server}
              onOpen={() => setSelectedId(server.id)}
              onChangePassword={props.onChangePassword}
              onTogglePower={props.onTogglePower}
              onRenew={() => setRenewTarget(server)}
              onRelease={() => setReleaseTarget(server)}
              onCopy={copyText}
            />
          ))}
        </div>
      )}

      {selected && (
        <InstanceDetails
          server={selected}
          onClose={() => setSelectedId(null)}
          onChangePassword={props.onChangePassword}
          onTogglePower={props.onTogglePower}
          onReboot={props.onReboot}
          onRenew={() => setRenewTarget(selected)}
          onRelease={() => setReleaseTarget(selected)}
          onCopy={copyText}
        />
      )}

      {renewTarget && (
        <RenewDialog
          instance={renewTarget}
          onClose={() => setRenewTarget(null)}
          onConfirm={async (durationId) => {
            const ok = await props.onRenew(renewTarget, durationId)
            if (ok) setRenewTarget(null)
          }}
        />
      )}

      {releaseTarget && (
        <ReleaseDialog
          instance={releaseTarget}
          onClose={() => setReleaseTarget(null)}
          onConfirm={async () => {
            const ok = await props.onRelease(releaseTarget)
            if (ok) setReleaseTarget(null)
          }}
        />
      )}

      <OrderHistoryDialog
        open={billingOpen}
        onOpenChange={setBillingOpen}
      />
    </div>
  )
}

function StatCard({
  icon: Icon,
  label,
  value,
  tone = 'default',
}: {
  icon: ElementType
  label: string
  value: number
  tone?: 'default' | 'emerald' | 'slate' | 'amber'
}) {
  const toneClass = {
    default: 'bg-[#e8f4f0] text-[#3a8f7a]',
    emerald: 'bg-emerald-500/10 text-emerald-600',
    slate: 'bg-slate-500/10 text-slate-600',
    amber: 'bg-amber-500/10 text-amber-600',
  }[tone]

  return (
    <div className='border-border/70 bg-card flex items-center gap-3 rounded-xl border px-4 py-3.5'>
      <span
        className={cn(
          'flex size-9 items-center justify-center rounded-lg',
          toneClass
        )}
      >
        <Icon className='size-4' />
      </span>
      <div>
        <div className='text-muted-foreground text-[11px]'>{label}</div>
        <div className='text-lg leading-tight font-bold text-[#1a4a3f]'>
          {value}
        </div>
      </div>
    </div>
  )
}

function InstanceRow({
  server,
  onOpen,
  onChangePassword,
  onTogglePower,
  onRenew,
  onRelease,
  onCopy,
}: {
  server: ServerInstance
  onOpen: () => void
  onChangePassword: (server: ServerInstance) => void
  onTogglePower: (server: ServerInstance, running: boolean) => void
  onRenew: () => void
  onRelease: () => void
  onCopy: (value: string, messageKey: string) => void
}) {
  const { t } = useTranslation()
  const status = resolveStatus(server)
  const running = status === 'running'
  const isHourly = server.billing === 'hourly'
  const released = status === 'released'

  // 剩余时间按计费口径报：按量报小时、包月报天；已释放就不再提剩余时间。
  let remainingHint: string | undefined
  if (!released) {
    remainingHint = isHourly
      ? t('{{hours}} hours left', { hours: hoursLeft(server) })
      : t('{{days}} days left', { days: daysLeft(server) })
  }

  return (
    <Card
      className={cn(
        'border-border/70 transition-shadow hover:shadow-md',
        (isExpired(server) || released) && 'opacity-75'
      )}
    >
      <CardHeader className='gap-3 pb-3'>
        <div className='flex flex-wrap items-start justify-between gap-3'>
          <div className='min-w-0'>
            <div className='flex flex-wrap items-center gap-2'>
              <CardTitle className='text-base'>
                <button
                  type='button'
                  onClick={onOpen}
                  className='flex items-center gap-1.5 text-left transition-colors hover:text-[#3a8f7a]'
                >
                  <ServerIcon className='size-4 text-[#3a8f7a]' />
                  {server.name}
                </button>
              </CardTitle>
              <Badge className={cn('shrink-0', statusBadgeClass(server))}>
                {t(statusKeyOf(server))}
              </Badge>
              {server.provision_mode === 'simulated' && (
                <Badge className='shrink-0 bg-amber-500/15 text-amber-600'>
                  {t('Simulated instance')}
                </Badge>
              )}
            </div>
            <CardDescription className='mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1'>
              <span className='inline-flex items-center gap-1'>
                <MapPin className='size-3' />
                {server.config.region}
              </span>
              <span>
                {server.spec.cpu} vCPU · {server.spec.memory} GB ·{' '}
                {server.spec.disk} GB
              </span>
              {server.spec.gpu && (
                <span className='inline-flex items-center gap-1 font-medium text-[#3a8f7a]'>
                  <Gpu className='size-3' />
                  {server.spec.gpu.count}× {server.spec.gpu.model}{' '}
                  {server.spec.gpu.vram}GB
                </span>
              )}
              <span className='inline-flex items-center gap-1'>
                <Network className='size-3' />
                {server.mbps} Mbps
              </span>
            </CardDescription>
          </div>

          {/* 行操作：云厂商把高频操作摆按钮，次要操作收进「更多」 */}
          <div className='flex shrink-0 items-center gap-2'>
            {!released && (
              <Button
                size='sm'
                variant='outline'
                className='rounded-lg'
                disabled={status === 'expired' && !running}
                onClick={() => onTogglePower(server, !running)}
              >
                {running ? (
                  <>
                    <Square className='mr-1.5 size-3.5' />
                    {t('Stop')}
                  </>
                ) : (
                  <>
                    <Play className='mr-1.5 size-3.5' />
                    {t('Start')}
                  </>
                )}
              </Button>
            )}
            <Button
              size='sm'
              variant='outline'
              className='rounded-lg'
              onClick={onOpen}
            >
              <Gauge className='mr-1.5 size-3.5' />
              {t('Details')}
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    size='sm'
                    variant='outline'
                    className='rounded-lg'
                    aria-label={t('More actions')}
                  />
                }
              >
                <Wrench className='size-3.5' />
              </DropdownMenuTrigger>
              <DropdownMenuContent align='end'>
                <DropdownMenuItem onClick={onOpen}>
                  <Activity className='size-4' />
                  {t('Monitoring')}
                </DropdownMenuItem>
                {!released && (
                  <DropdownMenuItem onClick={() => onChangePassword(server)}>
                    <KeyRound className='size-4' />
                    {t('Change password')}
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem
                  onClick={() =>
                    onCopy(
                      `ssh ${server.username}@${server.ip}`,
                      'SSH command copied'
                    )
                  }
                >
                  <Copy className='size-4' />
                  {t('Copy SSH command')}
                </DropdownMenuItem>
                {!released && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem onClick={onRenew}>
                      <RefreshCcw className='size-4' />
                      {t('Renew')}
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={onRelease}
                      className='text-destructive'
                    >
                      <Trash2 className='size-4' />
                      {t('Release instance')}
                    </DropdownMenuItem>
                  </>
                )}
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={onOpen}>
                  <LifeBuoy className='size-4' />
                  {t('Support')}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </CardHeader>

      <CardContent className='pt-0'>
        <Separator className='mb-4' />
        <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
          <Field
            icon={Network}
            label={t('Public IP')}
            value={server.ip}
            onCopy={() => onCopy(server.ip, 'Copied to clipboard')}
          />
          <Field
            icon={KeyRound}
            label={t('Username')}
            value={server.username}
          />
          <Field
            icon={Timer}
            label={t('Expires on')}
            value={released ? t('Released') : formatDate(server.expire_at)}
            hint={remainingHint}
          />
          <Field
            icon={RefreshCcw}
            label={t('Amount paid')}
            value={formatAmount(server.paid_amount)}
            hint={
              server.refunded_amount > 0
                ? t('Refunded {{amount}}', {
                    amount: formatAmount(server.refunded_amount),
                  })
                : undefined
            }
          />
        </div>
      </CardContent>
    </Card>
  )
}

function Field({
  icon: Icon,
  label,
  value,
  hint,
  onCopy,
}: {
  icon: ElementType
  label: string
  value: string
  hint?: string
  onCopy?: () => void
}) {
  return (
    <div className='rounded-xl border bg-[#f8fbfa] px-3.5 py-3'>
      <div className='text-muted-foreground flex items-center gap-1.5 text-[11px] font-medium tracking-wide uppercase'>
        <Icon className='size-3' />
        {label}
        {onCopy && (
          <button
            type='button'
            onClick={onCopy}
            className='hover:bg-muted ml-auto rounded p-1 transition-colors'
            aria-label='Copy'
          >
            <Copy className='size-3' />
          </button>
        )}
      </div>
      <div className='mt-1.5 truncate font-mono text-[13px] font-medium text-[#1a4a3f]'>
        {value}
      </div>
      {hint && (
        <div className='text-muted-foreground mt-0.5 text-[11px]'>{hint}</div>
      )}
    </div>
  )
}

/**
 * 操作记录列表。
 *
 * 单列一个组件而不是在详情抽屉里写三元嵌套：三种状态（加载中 / 无记录 /
 * 有记录）各自是一段完整 UI，平铺开比嵌套三元好读，也方便以后加分页。
 */
function OperationLog({ instanceId }: { instanceId: number }) {
  const { t } = useTranslation()
  const eventsQuery = useServerEvents(instanceId, true)
  const events = eventsQuery.data?.items ?? []

  if (eventsQuery.isLoading) {
    return (
      <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
    )
  }

  if (events.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>{t('No operations yet')}</p>
    )
  }

  return (
    <ul className='divide-border/70 divide-y'>
      {events.map((event) => (
        <li
          key={event.id}
          className='flex items-start justify-between gap-3 py-2.5 text-sm'
        >
          <div className='min-w-0'>
            <div className='font-medium text-[#1a4a3f]'>
              {t(`server.event.${event.action}`, {
                defaultValue: event.action,
              })}
            </div>
            {event.detail ? (
              <div className='text-muted-foreground mt-0.5 truncate text-xs'>
                {event.detail}
              </div>
            ) : null}          </div>
          <span className='text-muted-foreground shrink-0 text-xs'>
            {formatDateTime(event.created_at)}
          </span>
        </li>
      ))}
    </ul>
  )
}

/**
 * 实例详情抽屉。
 *
 * 参照云厂商 ECS 的实例详情页：左侧是「基本信息 / 监控 / 网络与安全 /
 * 存储与备份 / 操作记录」。监控与操作记录都按需拉取 —— 打开抽屉就先拉
 * 24 小时曲线不划算，用户可能只想看一眼 IP。
 */
function InstanceDetails({
  server,
  onClose,
  onChangePassword,
  onTogglePower,
  onReboot,
  onRenew,
  onRelease,
  onCopy,
}: {
  server: ServerInstance
  onClose: () => void
  onChangePassword: (server: ServerInstance) => void
  onTogglePower: (server: ServerInstance, running: boolean) => void
  onReboot: (server: ServerInstance) => void
  onRenew: () => void
  onRelease: () => void
  onCopy: (value: string, messageKey: string) => void
}) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<
    'basic' | 'monitor' | 'network' | 'storage' | 'events'
  >('basic')

  const status = resolveStatus(server)
  const running = status === 'running'
  const isHourly = server.billing === 'hourly'
  const released = status === 'released'
  const metricsQuery = useServerMetrics(server.id, tab === 'monitor')
  const metrics = metricsQuery.data ?? emptyMetrics(server)

  const tabs = [
    { id: 'basic', labelKey: 'Basic info' },
    { id: 'monitor', labelKey: 'Monitoring' },
    { id: 'network', labelKey: 'Network and security' },
    { id: 'storage', labelKey: 'Storage and backup' },
    { id: 'events', labelKey: 'Operation log' },
  ] as const

  return (
    <div className='fixed inset-0 z-50 flex justify-end'>
      <button
        type='button'
        aria-label={t('Close')}
        onClick={onClose}
        className='absolute inset-0 bg-black/30 backdrop-blur-[2px]'
      />
      <div className='relative flex h-full w-full max-w-3xl flex-col bg-[#f5f9f7] shadow-2xl'>
        {/* 头部 */}
        <div className='border-border/70 bg-card border-b px-6 py-4'>
          <div className='flex flex-wrap items-start justify-between gap-3'>
            <div className='min-w-0'>
              <div className='flex flex-wrap items-center gap-2'>
                <h2 className='truncate text-lg font-bold text-[#1a4a3f]'>
                  {server.name}
                </h2>
                <Badge className={cn('shrink-0', statusBadgeClass(server))}>
                  {t(statusKeyOf(server))}
                </Badge>
                {server.provision_mode === 'simulated' && (
                  <Badge className='shrink-0 bg-amber-500/15 text-amber-600'>
                    {t('Simulated instance')}
                  </Badge>
                )}
              </div>
              <div className='text-muted-foreground mt-1 text-xs'>
                {server.ip} · {server.plan} · {server.config.region}
              </div>
            </div>
            <div className='flex flex-wrap items-center gap-2'>
              {!released && (
                <>
                  <Button
                    size='sm'
                    variant='outline'
                    className='rounded-lg'
                    onClick={() => onTogglePower(server, !running)}
                  >
                    {running ? (
                      <>
                        <Square className='mr-1.5 size-3.5' />
                        {t('Stop')}
                      </>
                    ) : (
                      <>
                        <Play className='mr-1.5 size-3.5' />
                        {t('Start')}
                      </>
                    )}
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
                  <Button
                    size='sm'
                    variant='outline'
                    className='rounded-lg'
                    onClick={onRenew}
                  >
                    <RefreshCcw className='mr-1.5 size-3.5' />
                    {t('Renew')}
                  </Button>
                </>
              )}
              <Button
                size='sm'
                variant='ghost'
                className='rounded-lg'
                onClick={onClose}
              >
                {t('Close')}
              </Button>
            </div>
          </div>

          <div className='mt-4 flex flex-wrap gap-1.5'>
            {tabs.map((item) => (
              <button
                key={item.id}
                type='button'
                onClick={() => setTab(item.id)}
                className={cn(
                  'rounded-lg px-3 py-1.5 text-xs font-medium transition-colors',
                  tab === item.id
                    ? 'bg-[#e8f4f0] text-[#1a4a3f]'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                )}
              >
                {t(item.labelKey)}
              </button>
            ))}
          </div>
        </div>

        {/* 内容 */}
        <div className='flex-1 space-y-4 overflow-y-auto px-6 py-5'>
          {tab === 'basic' && (
            <>
              <Panel title={t('Instance information')}>
                <div className='grid gap-x-6 gap-y-3 sm:grid-cols-2'>
                  <InfoRow
                    label={t('Health status')}
                    value={t(statusKeyOf(server))}
                  />
                  <InfoRow
                    label={t('Billing type')}
                    value={isHourly ? t('Pay-as-you-go') : t('Prepaid')}
                  />
                  <InfoRow
                    label={t('Auto release time')}
                    value={
                      released
                        ? t('Released')
                        : formatDateTime(server.expire_at)
                    }
                  />
                  <InfoRow
                    label={t('Region')}
                    value={`${server.config.region} · ${server.mbps} Mbps`}
                  />
                  <InfoRow label={t('Public IP')} value={server.ip} mono />
                  <InfoRow
                    label={t('CPU and memory')}
                    value={`${server.spec.cpu} vCPU · ${server.spec.memory} GB`}
                  />
                  {server.spec.cpu_model && (
                    <InfoRow
                      label={t('CPU model')}
                      value={server.spec.cpu_model}
                    />
                  )}
                  <InfoRow
                    label={t('System disk')}
                    value={`${server.spec.disk} GB NVMe`}
                  />
                  {server.spec.data_disk && (
                    <InfoRow
                      label={t('Data disk')}
                      value={`${server.spec.data_disk} GB${
                        formatDataDiskExpand(server.spec, t)
                          ? ` (${formatDataDiskExpand(server.spec, t)})`
                          : ''
                      }`}
                    />
                  )}
                  <InfoRow
                    label={t('Graphics card')}
                    value={
                      server.spec.gpu
                        ? `${server.spec.gpu.count}× ${server.spec.gpu.model} ${server.spec.gpu.vram}GB`
                        : t('None')
                    }
                  />
                  <InfoRow
                    label={t('Operating system')}
                    value={t('Ubuntu 24.04 LTS')}
                  />
                  <InfoRow
                    label={t('Amount paid')}
                    value={formatAmount(server.paid_amount)}
                  />
                  {server.refunded_amount > 0 && (
                    <InfoRow
                      label={t('Refunded')}
                      value={formatAmount(server.refunded_amount)}
                    />
                  )}
                  <InfoRow
                    label={t('Created at')}
                    value={formatDateTime(server.created_at)}
                  />
                </div>
              </Panel>

              <Panel title={t('Login credentials')}>
                <div className='grid gap-3 sm:grid-cols-3'>
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
                    value={
                      server.password_readable
                        ? server.password
                        : t('Unavailable, please reset the password')
                    }
                    secret={server.password_readable}
                    dense
                  />
                </div>
                <Button
                  size='sm'
                  variant='outline'
                  className='mt-3 rounded-lg'
                  onClick={() =>
                    onCopy(
                      `ssh ${server.username}@${server.ip}`,
                      'SSH command copied'
                    )
                  }
                >
                  <Copy className='mr-1.5 size-3.5' />
                  {t('Copy SSH command')}
                </Button>
              </Panel>
            </>
          )}

          {tab === 'monitor' && (
            <>
              <Panel title={t('Resource usage')}>
                <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
                  <MetricBar
                    label={t('CPU')}
                    value={metrics.cpu.at(-1)?.value ?? 0}
                  />
                  <MetricBar
                    label={t('Memory')}
                    value={metrics.memory.at(-1)?.value ?? 0}
                  />
                  <MetricBar
                    label={t('Disk')}
                    value={metrics.disk}
                    tone={metrics.disk > 85 ? 'danger' : 'default'}
                  />
                  {/* 没买显卡就没有 GPU 指标：显示一根 0% 的柱子会让人以为
                      这张卡是空闲的，而不是「这台机器根本没有卡」 */}
                  {server.spec.gpu && (
                    <MetricBar
                      label={t('GPU')}
                      value={metrics.gpu}
                      tone={metrics.gpu > 90 ? 'warning' : 'default'}
                    />
                  )}
                  <MetricBar
                    label={t('Temperature')}
                    value={metrics.temperature}
                    max={100}
                    unit='°C'
                  />
                  <MetricBar
                    label={t('Network')}
                    value={metrics.network.at(-1)?.value ?? 0}
                    max={Math.max(1, metrics.bandwidth_mbps)}
                    unit=' Mbps'
                  />
                </div>
                {metricsQuery.isFetching && (
                  <p className='text-muted-foreground mt-3 text-[11px]'>
                    {t('Refreshing metrics...')}
                  </p>
                )}
              </Panel>

              <Panel title={t('Last 24 hours')}>
                <div className='grid gap-3 lg:grid-cols-2'>
                  <MetricChart title={t('CPU')} points={metrics.cpu} />
                  <MetricChart title={t('Memory')} points={metrics.memory} />
                  <MetricChart
                    title={t('Network')}
                    points={metrics.network}
                    unit=' Mbps'
                    color='#1677ff'
                  />
                </div>
              </Panel>
            </>
          )}

          {tab === 'network' && (
            <>
              <Panel title={t('Network')}>
                <div className='grid gap-x-6 gap-y-3 sm:grid-cols-2'>
                  <InfoRow
                    label={t('Public bandwidth')}
                    value={`${server.mbps} Mbps`}
                  />
                  <InfoRow label={t('Public IP')} value={server.ip} mono />
                  <InfoRow
                    label={t('Private IP')}
                    value={`10.0.${server.id % 250}.${(server.id % 250) + 2}`}
                    mono
                  />
                  <InfoRow label={t('Instance ID')} value={String(server.id)} />
                </div>
              </Panel>

              <Panel title={t('Security groups')}>
                <div className='space-y-2 text-sm'>
                  <div className='flex items-center justify-between gap-3'>
                    <span className='text-muted-foreground'>
                      {t('SSH port 22')}
                    </span>
                    <span className='font-medium text-[#1a4a3f]'>
                      {t('Allowed from your account IP only')}
                    </span>
                  </div>
                  <div className='flex items-center justify-between gap-3'>
                    <span className='text-muted-foreground'>
                      {t('Outbound traffic')}
                    </span>
                    <span className='font-medium text-[#1a4a3f]'>
                      {t('Unrestricted')}
                    </span>
                  </div>
                  <div className='flex items-center justify-between gap-3'>
                    <span className='text-muted-foreground'>
                      {t('DDoS protection')}
                    </span>
                    <span className='font-medium text-[#1a4a3f]'>
                      {t('Enabled')}
                    </span>
                  </div>
                </div>
              </Panel>
            </>
          )}

          {tab === 'storage' && (
            <Panel title={t('Storage and backup')}>
              <div className='grid gap-x-6 gap-y-3 sm:grid-cols-2'>
                <InfoRow
                  label={t('System disk')}
                  value={`${server.spec.disk} GB NVMe`}
                />
                <InfoRow label={t('Disk usage')} value={`${metrics.disk}%`} />
                <InfoRow
                  label={t('Automatic snapshots')}
                  value={t('Enabled, daily')}
                />
                <InfoRow label={t('Latest snapshot')} value={t('None yet')} />
              </div>
            </Panel>
          )}

          {tab === 'events' && (
            <Panel title={t('Operation log')}>
              <OperationLog instanceId={server.id} />
            </Panel>
          )}
        </div>

        {!released && (
          <div className='border-border/70 bg-card flex items-center justify-between gap-3 border-t px-6 py-3'>
            <Button
              size='sm'
              variant='outline'
              className='rounded-lg'
              onClick={onRelease}
            >
              <Trash2 className='mr-1.5 size-3.5' />
              {t('Release instance')}
            </Button>
            {running && (
              <Button
                size='sm'
                variant='outline'
                className='rounded-lg'
                onClick={() => onReboot(server)}
              >
                <RefreshCcw className='mr-1.5 size-3.5' />
                {t('Reboot')}
              </Button>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className='border-border/70 bg-card rounded-xl border p-5'>
      <h3 className='mb-4 text-sm font-semibold text-[#1a4a3f]'>{title}</h3>
      {children}
    </section>
  )
}

function InfoRow({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-[11px]'>{label}</div>
      <div
        className={cn(
          'mt-0.5 truncate text-sm font-medium text-[#1a4a3f]',
          mono && 'font-mono text-[13px]'
        )}
      >
        {value}
      </div>
    </div>
  )
}
