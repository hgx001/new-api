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
  AlertTriangle,
  ChevronDown,
  Copy,
  Cpu,
  Gauge,
  Gpu,
  HardDrive,
  KeyRound,
  LifeBuoy,
  Loader2,
  MemoryStick,
  Network,
  Play,
  Power,
  Receipt,
  RefreshCcw,
  Server as ServerIcon,
  Square,
  Timer,
  Trash2,
} from 'lucide-react'
import { useMemo, useState, type ElementType, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { cn } from '@/lib/utils'

import { formatDiskBreakdown } from '../constants'
import { useServerEvents, useServerMetrics } from '../hooks/use-servers'
import {
  daysLeft,
  emptyMetrics,
  formatDate,
  formatDateTime,
  hoursLeft,
  resolveStatus,
  statusKeyOf,
  type InstanceStatusKey,
} from '../lib/instance-runtime'
import { formatAmount } from '../lib/pricing'
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

type StatusFilter =
  | 'all'
  | 'provisioning'
  | 'running'
  | 'stopped'
  | 'expiring'

const FILTERS: { id: StatusFilter; labelKey: string }[] = [
  { id: 'all', labelKey: 'All' },
  { id: 'provisioning', labelKey: 'Waiting for assignment' },
  { id: 'running', labelKey: 'Running' },
  { id: 'stopped', labelKey: 'Stopped' },
  { id: 'expiring', labelKey: 'Expiring soon' },
]

/** 详情抽屉的初始页签，展开面板里的「监控 / 操作记录」直接跳到对应页 */
type DetailTab = 'basic' | 'monitor' | 'network' | 'storage' | 'events'

/**
 * 表格列宽（xl 及以上）。
 *
 * 用 CSS Grid 而不是 <table>：同一份 DOM 在窄屏会自然堆成卡片，
 * 不必为移动端再写一套标记。列的顺序与云厂商控制台一致 ——
 * 实例 → 状态 → 规格 → 磁盘 → 健康 → 计费 → 释放时间 → 登录 → 操作。
 */
const ROW_GRID =
  'grid gap-x-3 gap-y-3 px-4 py-4 sm:grid-cols-2 ' +
  'xl:grid-cols-[minmax(0,1.35fr)_minmax(0,0.72fr)_minmax(0,1.35fr)_minmax(0,1fr)_minmax(0,0.72fr)_minmax(0,0.95fr)_minmax(0,1.05fr)_auto] ' +
  'xl:items-center'

function statusTone(key: InstanceStatusKey): string {
  if (key === 'Running') {
    return 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
  }
  if (key === 'Stopped') {
    return 'bg-slate-500/15 text-slate-700 dark:text-slate-300'
  }
  if (key === 'Expiring soon') {
    return 'bg-amber-500/15 text-amber-700 dark:text-amber-400'
  }
  if (key === 'Provisioning') {
    return 'bg-blue-500/15 text-blue-700 dark:text-blue-400'
  }
  return 'bg-red-500/15 text-red-700 dark:text-red-400'
}

/**
 * 健康状态：只回答「这台机器现在能不能用」。
 *
 * 与生命周期状态（即将到期 / 已过期）不是一回事 —— 一台「即将到期」的
 * 机器本身是健康的，把它显示成红色会让人以为机器坏了。
 * 「Healthy」只在这里出现，所以单独给它一个类型，不塞进 InstanceStatusKey。
 */
type HealthKey =
  | 'Healthy'
  | 'Stopped'
  | 'Provisioning'
  | 'Expired'
  | 'Released'

function healthKeyOf(server: ServerInstance): HealthKey {
  const status = resolveStatus(server)
  if (status === 'released') return 'Released'
  if (status === 'expired') return 'Expired'
  if (status === 'provisioning') return 'Provisioning'
  if (status === 'running') return 'Healthy'
  return 'Stopped'
}

function healthTone(key: HealthKey): string {
  if (key === 'Healthy') return 'text-emerald-600 dark:text-emerald-400'
  if (key === 'Stopped') return 'text-muted-foreground'
  if (key === 'Provisioning') return 'text-blue-600 dark:text-blue-400'
  return 'text-red-600 dark:text-red-400'
}

/** 剩余时间的文案：按量报小时、包月报天；已释放就不再提剩余时间。 */
function remainingHintOf(
  server: ServerInstance,
  t: (key: string, options?: Record<string, unknown>) => string
): string | undefined {
  const status = resolveStatus(server)
  if (status === 'released') return undefined
  if (server.billing === 'hourly') {
    return t('{{hours}} hours left', { hours: hoursLeft(server) })
  }
  return t('{{days}} days left', { days: daysLeft(server) })
}

/**
 * 云服务器实例控制台。
 *
 * 参照云厂商（ECS 类）的实例管理视图：**表格式列表 + 行内展开详情**。
 * 之前是卡片墙 —— 一屏只能放两三台，到期时间、计费口径、SSH 入口
 * 这些高频信息要滚半天才看得到。表格化之后一屏能扫完所有实例，
 * 展开行才看完整规格与凭据。
 *
 * 数据全部来自服务端：状态、到期时间、凭据、监控、操作记录都以接口为准。
 */
export function InstanceConsole(props: InstanceConsoleProps) {
  const { t } = useTranslation()
  const [filter, setFilter] = useState<StatusFilter>('all')
  const [expandedId, setExpandedId] = useState<number | null>(null)
  const [detailTarget, setDetailTarget] = useState<{
    id: number
    tab: DetailTab
  } | null>(null)
  const [renewTarget, setRenewTarget] = useState<ServerInstance | null>(null)
  const [releaseTarget, setReleaseTarget] = useState<ServerInstance | null>(
    null
  )
  const [billingOpen, setBillingOpen] = useState(false)

  const filtered = useMemo(() => {
    switch (filter) {
      case 'provisioning':
        return props.servers.filter(
          (server) => resolveStatus(server) === 'provisioning'
        )
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

  const selected = props.servers.find(
    (server) => server.id === detailTarget?.id
  )

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
  const pendingCount = props.servers.filter(
    (server) => resolveStatus(server) === 'provisioning'
  ).length

  const copyText = (value: string, messageKey: string) => {
    void copyToClipboard(value).then((ok) => {
      if (ok) toast.success(t(messageKey))
      else toast.error(t('Copy failed, please select and copy manually'))
    })
  }

  if (props.servers.length === 0) {
    return (
      <div className='bg-card/60 rounded-2xl border border-dashed px-6 py-16 text-center'>
        <div className='mx-auto flex size-14 items-center justify-center rounded-2xl bg-emerald-500/10'>
          <ServerIcon className='size-6 text-emerald-600 dark:text-emerald-400' />
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
          className='mt-6 rounded-full bg-emerald-600 text-white hover:bg-emerald-700'
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
        <OrderHistoryDialog open={billingOpen} onOpenChange={setBillingOpen} />
      </div>
    )
  }

  return (
    <div className='space-y-4'>
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

      {/* 待分配提示：付款后机器不是立刻就好，得让用户知道自己在等什么、
          以及「等待不算钱」这件事，否则会产生「我是不是被坑了」的疑虑 */}
      {pendingCount > 0 && (
        <div className='flex items-start gap-2 rounded-xl border border-blue-500/30 bg-blue-500/10 px-4 py-2.5 text-xs leading-relaxed text-blue-800 dark:text-blue-300'>
          <Loader2 className='mt-0.5 size-3.5 shrink-0 animate-spin' />
          <span>
            {t(
              '{{count}} instance(s) paid and waiting for assignment. We usually deliver within 30 minutes, and the rental clock only starts once the credentials are handed over.',
              { count: pendingCount }
            )}
          </span>
        </div>
      )}

      {/* 到期释放说明：这是用户最容易被坑的一条规则，常驻在第一屏 */}
      <div className='flex items-start gap-2 rounded-xl border border-amber-500/30 bg-amber-500/10 px-4 py-2.5 text-xs leading-relaxed text-amber-800 dark:text-amber-300'>
        <AlertTriangle className='mt-0.5 size-3.5 shrink-0' />
        <span>
          {t(
            'Instances stop as soon as the rental period ends and stay recoverable for 7 days — renew within that window and they come back. After 7 days they are released and all data is deleted.'
          )}
        </span>
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
                ? 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-400'
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

      {/* 实例表 */}
      {filtered.length === 0 ? (
        <div className='bg-card/60 rounded-2xl border border-dashed px-6 py-14 text-center'>
          <div className='text-muted-foreground text-sm'>
            {t('No instances match the current filter')}
          </div>
        </div>
      ) : (
        <div className='border-border/70 bg-card overflow-hidden rounded-xl border'>
          {/* 表头只在宽屏出现：窄屏每格里自带标签，不需要再来一行 */}
          <div
            className={cn(
              ROW_GRID,
              'text-muted-foreground hidden border-b bg-muted/40 py-2.5 text-[11px] font-medium xl:grid'
            )}
          >
            <span>{t('Instance')}</span>
            <span>{t('Status')}</span>
            <span>{t('Specifications')}</span>
            <span>{t('Local disks')}</span>
            <span>{t('Health status')}</span>
            <span>{t('Billing type')}</span>
            <span>{t('Auto release time')}</span>
            <span className='text-right'>{t('Actions')}</span>
          </div>

          {filtered.map((server) => (
            <InstanceRow
              key={server.id}
              server={server}
              expanded={expandedId === server.id}
              onToggleExpand={() =>
                setExpandedId(expandedId === server.id ? null : server.id)
              }
              onOpenDetail={(tab) =>
                setDetailTarget({ id: server.id, tab })
              }
              onChangePassword={props.onChangePassword}
              onTogglePower={props.onTogglePower}
              onReboot={props.onReboot}
              onRenew={() => setRenewTarget(server)}
              onRelease={() => setReleaseTarget(server)}
              onCopy={copyText}
            />
          ))}
        </div>
      )}

      {selected && detailTarget && (
        <InstanceDetails
          server={selected}
          initialTab={detailTarget.tab}
          onClose={() => setDetailTarget(null)}
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

      <OrderHistoryDialog open={billingOpen} onOpenChange={setBillingOpen} />
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
    default: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
    emerald: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
    slate: 'bg-slate-500/10 text-slate-600 dark:text-slate-300',
    amber: 'bg-amber-500/10 text-amber-600 dark:text-amber-400',
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
        <div className='text-foreground text-lg leading-tight font-bold'>
          {value}
        </div>
      </div>
    </div>
  )
}

/**
 * 表格里的一个单元格。
 *
 * 宽屏靠表头说明列名，窄屏（xl 以下）每格自带一行小标签 ——
 * 同一份 DOM 兼顾两种布局，不用为移动端再写一遍。
 */
function Cell({
  label,
  className,
  children,
}: {
  label: string
  className?: string
  children: ReactNode
}) {
  return (
    <div className={cn('min-w-0', className)}>
      <div className='text-muted-foreground mb-1 text-[11px] font-medium xl:hidden'>
        {label}
      </div>
      {children}
    </div>
  )
}

function InstanceRow({
  server,
  expanded,
  onToggleExpand,
  onOpenDetail,
  onChangePassword,
  onTogglePower,
  onReboot,
  onRenew,
  onRelease,
  onCopy,
}: {
  server: ServerInstance
  expanded: boolean
  onToggleExpand: () => void
  onOpenDetail: (tab: DetailTab) => void
  onChangePassword: (server: ServerInstance) => void
  onTogglePower: (server: ServerInstance, running: boolean) => void
  onReboot: (server: ServerInstance) => void
  onRenew: () => void
  onRelease: () => void
  onCopy: (value: string, messageKey: string) => void
}) {
  const { t } = useTranslation()
  const status = resolveStatus(server)
  const running = status === 'running'
  const isHourly = server.billing === 'hourly'
  const released = status === 'released'
  const expired = status === 'expired'
  // 等待分配的机器还不能操作：既没有 IP 也没有凭据，开机/改密都无从谈起。
  const provisioning = status === 'provisioning'
  const locked = released || expired || provisioning

  // 释放时间：未分配时还没开始计时，不能显示 1970 年。
  let expireLabel = formatDateTime(server.expire_at)
  if (released) {
    expireLabel = t('Released')
  } else if (provisioning) {
    expireLabel = t('Not counting yet')
  }

  // 剩余时间按计费口径报：按量报小时、包月报天；已释放就不再提剩余时间。
  const remainingHint = remainingHintOf(server, t)

  // 健康状态回答「它现在能不能用」，与生命周期状态是两回事
  const healthKey = healthKeyOf(server)

  return (
    <div className='border-border/70 border-t first:border-t-0'>
      <div className={ROW_GRID}>
        {/* 实例：名称 + ID。名称点开行内详情，是最自然的「看这台机器」入口 */}
        <Cell label={t('Instance')}>
          <button
            type='button'
            onClick={onToggleExpand}
            className='text-foreground flex min-w-0 items-center gap-1.5 text-left font-semibold transition-colors hover:text-emerald-600 dark:hover:text-emerald-400'
          >
            <ServerIcon className='size-3.5 shrink-0 text-emerald-600 dark:text-emerald-400' />
            <span className='truncate'>{server.name}</span>
          </button>
          <div className='text-muted-foreground mt-0.5 font-mono text-[11px]'>
            #{server.id} · {t(server.config.region)}
          </div>
        </Cell>

        {/* 状态 */}
        <Cell label={t('Status')}>
          <Badge className={cn('shrink-0 gap-1', statusTone(statusKeyOf(server)))}>
            <span className='size-1.5 rounded-full bg-current' />
            {t(statusKeyOf(server))}
          </Badge>
        </Cell>

        {/* 规格：先给显卡（选型第一依据），再给 CPU / 内存 / 磁盘 */}
        <Cell label={t('Specifications')}>
          <div className='text-foreground truncate text-[13px] font-medium'>
            {server.spec.gpu
              ? `${server.spec.gpu.count}× ${server.spec.gpu.model} ${server.spec.gpu.vram}GB`
              : t('No graphics card')}
          </div>
          <div className='text-muted-foreground mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px]'>
            <span className='inline-flex items-center gap-1'>
              <Cpu className='size-3' />
              {server.spec.cpu} vCPU
            </span>
            <span className='inline-flex items-center gap-1'>
              <MemoryStick className='size-3' />
              {server.spec.memory} GB
            </span>
            <button
              type='button'
              onClick={onToggleExpand}
              className='text-emerald-700 hover:underline dark:text-emerald-400'
            >
              {expanded ? t('Hide details') : t('Details')}
            </button>
          </div>
        </Cell>

        {/* 本地磁盘：系统盘 / 数据盘分开报 */}
        <Cell label={t('Local disks')}>
          <div className='text-foreground flex items-start gap-1.5 text-[12px]'>
            <HardDrive className='mt-0.5 size-3 shrink-0 text-muted-foreground' />
            <span className='min-w-0'>
              {formatDiskBreakdown(server.spec, t)}
            </span>
          </div>
        </Cell>

        {/* 健康 */}
        <Cell label={t('Health status')}>
          <span
            className={cn(
              'inline-flex items-center gap-1.5 text-[12px] font-medium',
              healthTone(healthKey)
            )}
          >
            <span className='size-1.5 rounded-full bg-current' />
            {t(healthKey)}
          </span>
        </Cell>

        {/* 计费 */}
        <Cell label={t('Billing type')}>
          <div className='text-foreground text-[12px] font-medium'>
            {isHourly ? t('Pay-as-you-go') : t('Prepaid')}
          </div>
          <div className='text-muted-foreground mt-0.5 text-[11px]'>
            {t('Paid {{amount}}', { amount: formatAmount(server.paid_amount) })}
          </div>
        </Cell>

        {/* 释放时间 */}
        <Cell label={t('Auto release time')}>
          <div className='text-foreground text-[12px] font-medium'>
            {expireLabel}
          </div>
          {remainingHint && (
            <div
              className={cn(
                'mt-0.5 text-[11px]',
                statusKeyOf(server) === 'Expiring soon'
                  ? 'text-amber-600 dark:text-amber-400'
                  : 'text-muted-foreground'
              )}
            >
              {remainingHint}
            </div>
          )}
        </Cell>

        {/* SSH 登录：一行命令 + 一键复制。改密/重启这类非常用操作收进「更多」，
            避免操作列把表格挤得只剩几个字 */}
        <Cell label={t('SSH login')} className='sm:col-span-2 xl:col-span-1'>
          {provisioning ? (
            <div className='flex items-center justify-end'>
              <span className='inline-flex items-center gap-1.5 rounded-md bg-blue-500/10 px-2 py-1 text-[11px] font-medium text-blue-700 dark:text-blue-400'>
                <Loader2 className='size-3 animate-spin' />
                {t('Waiting for assignment')}
              </span>
            </div>
          ) : (
          <div className='flex flex-wrap items-center justify-end gap-1.5'>
            <button
              type='button'
              onClick={() =>
                onCopy(
                  `ssh ${server.username}@${server.ip}`,
                  'SSH command copied'
                )
              }
              title={`ssh ${server.username}@${server.ip}`}
              className='text-muted-foreground hover:text-foreground inline-flex max-w-[9.5rem] items-center gap-1 truncate font-mono text-[11px] transition-colors'
            >
              <span className='truncate'>
                {server.username}@{server.ip}
              </span>
              <Copy className='size-3 shrink-0' />
            </button>
            {!locked && (
              <Button
                size='sm'
                variant='outline'
                className='h-7 rounded-md px-2 text-[11px]'
                onClick={() => onTogglePower(server, !running)}
              >
                {running ? (
                  <>
                    <Square className='mr-1 size-3' />
                    {t('Stop')}
                  </>
                ) : (
                  <>
                    <Play className='mr-1 size-3' />
                    {t('Start')}
                  </>
                )}
              </Button>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    size='sm'
                    variant='ghost'
                    className='h-7 rounded-md px-2 text-[11px]'
                    aria-label={t('More actions')}
                  />
                }
              >
                {t('More')}
                <ChevronDown className='ml-1 size-3' />
              </DropdownMenuTrigger>
              <DropdownMenuContent align='end'>
                <DropdownMenuItem onClick={onToggleExpand}>
                  <Gauge className='size-4' />
                  {t('Details')}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => onOpenDetail('monitor')}>
                  <Activity className='size-4' />
                  {t('Monitoring')}
                </DropdownMenuItem>
                {!locked && (
                  <DropdownMenuItem onClick={() => onChangePassword(server)}>
                    <KeyRound className='size-4' />
                    {t('Change password')}
                  </DropdownMenuItem>
                )}
                {!locked && running && (
                  <DropdownMenuItem onClick={() => onReboot(server)}>
                    <RefreshCcw className='size-4' />
                    {t('Reboot')}
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem onClick={() => onOpenDetail('events')}>
                  <Receipt className='size-4' />
                  {t('Operation log')}
                </DropdownMenuItem>
                {!released && (
                  <>
                    <DropdownMenuSeparator />
                    {!locked && (
                      <DropdownMenuItem onClick={onRenew}>
                        <RefreshCcw className='size-4' />
                        {t('Renew')}
                      </DropdownMenuItem>
                    )}
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
                <DropdownMenuItem onClick={() => onOpenDetail('basic')}>
                  <LifeBuoy className='size-4' />
                  {t('Support')}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
          )}
        </Cell>
      </div>

      {expanded && (
        <div className='bg-muted/30 border-border/70 grid gap-5 border-t px-4 py-4 lg:grid-cols-2'>
          {/* 规格明细 */}
          <div>
            <div className='text-muted-foreground mb-2 text-[11px] font-semibold tracking-wide uppercase'>
              {t('Instance information')}
            </div>
            <dl className='grid grid-cols-2 gap-x-4 gap-y-2 text-[12px]'>
              <DetailItem
                icon={Gpu}
                label={t('Graphics card')}
                value={
                  server.spec.gpu
                    ? `${server.spec.gpu.count}× ${server.spec.gpu.model} ${server.spec.gpu.vram}GB`
                    : t('None')
                }
              />
              <DetailItem
                icon={Cpu}
                label={t('CPU and memory')}
                value={`${server.spec.cpu} vCPU · ${server.spec.memory} GB`}
                hint={server.spec.cpu_model}
              />
              <DetailItem
                icon={HardDrive}
                label={t('System disk')}
                value={`${server.spec.disk} GB NVMe`}
              />
              <DetailItem
                icon={HardDrive}
                label={t('Data disk')}
                value={
                  server.spec.data_disk ? `${server.spec.data_disk} GB` : t('None')
                }
              />
              <DetailItem
                icon={Network}
                label={t('Public bandwidth')}
                value={`${server.mbps} Mbps`}
              />
              <DetailItem
                icon={Timer}
                label={t('Created at')}
                value={formatDate(server.created_at)}
              />
            </dl>
          </div>

          {/* 登录凭据 + 快捷入口 */}
          <div>
            <div className='text-muted-foreground mb-2 text-[11px] font-semibold tracking-wide uppercase'>
              {t('Login credentials')}
            </div>
            {provisioning ? (
              <div className='border-border/70 bg-muted/40 flex items-start gap-2.5 rounded-xl border border-dashed px-3.5 py-3'>
                <Loader2 className='mt-0.5 size-4 shrink-0 animate-spin text-blue-600 dark:text-blue-400' />
                <div className='text-[12px] leading-relaxed'>
                  <div className='text-foreground font-medium'>
                    {t('Waiting for assignment')}
                  </div>
                  <div className='text-muted-foreground mt-0.5'>
                    {t(
                      'Payment received. Login credentials appear here as soon as the machine is assigned, and the rental clock starts at that moment.'
                    )}
                  </div>
                </div>
              </div>
            ) : (
            <div className='grid gap-2 sm:grid-cols-3'>
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
            )}
            {!provisioning && (
            <div className='mt-2.5 flex flex-wrap gap-2'>
              <Button
                size='sm'
                variant='outline'
                className='h-7 rounded-md px-2.5 text-[11px]'
                onClick={() =>
                  onCopy(
                    `ssh ${server.username}@${server.ip}`,
                    'SSH command copied'
                  )
                }
              >
                <Copy className='mr-1 size-3' />
                {t('Copy SSH command')}
              </Button>
              <Button
                size='sm'
                variant='outline'
                className='h-7 rounded-md px-2.5 text-[11px]'
                onClick={() => onOpenDetail('monitor')}
              >
                <Activity className='mr-1 size-3' />
                {t('Monitoring')}
              </Button>
              <Button
                size='sm'
                variant='outline'
                className='h-7 rounded-md px-2.5 text-[11px]'
                onClick={() => onOpenDetail('events')}
              >
                <Receipt className='mr-1 size-3' />
                {t('Operation log')}
              </Button>
              {!locked && (
                <Button
                  size='sm'
                  variant='outline'
                  className='h-7 rounded-md px-2.5 text-[11px]'
                  onClick={onRenew}
                >
                  <RefreshCcw className='mr-1 size-3' />
                  {t('Renew')}
                </Button>
              )}
            </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function DetailItem({
  icon: Icon,
  label,
  value,
  hint,
}: {
  icon: ElementType
  label: string
  value: string
  hint?: string
}) {
  return (
    <div className='min-w-0'>
      <dt className='text-muted-foreground flex items-center gap-1 text-[11px]'>
        <Icon className='size-3' />
        {label}
      </dt>
      <dd className='text-foreground mt-0.5 truncate font-medium'>{value}</dd>
      {hint && (
        <dd className='text-muted-foreground truncate text-[10px]'>{hint}</dd>
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
    return <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
  }

  if (events.length === 0) {
    return <p className='text-muted-foreground text-sm'>{t('No operations yet')}</p>
  }

  return (
    <ul className='divide-border/70 divide-y'>
      {events.map((event) => (
        <li
          key={event.id}
          className='flex items-start justify-between gap-3 py-2.5 text-sm'
        >
          <div className='min-w-0'>
            <div className='text-foreground font-medium'>
              {t(`server.event.${event.action}`, {
                defaultValue: event.action,
              })}
            </div>
            {event.detail ? (
              <div className='text-muted-foreground mt-0.5 truncate text-xs'>
                {event.detail}
              </div>
            ) : null}
          </div>
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
 *
 * `initialTab` 让列表里的「监控 / 操作记录」直接落到对应页，
 * 不用先打开再让用户自己切一次。
 */
function InstanceDetails({
  server,
  initialTab = 'basic',
  onClose,
  onChangePassword,
  onTogglePower,
  onReboot,
  onRenew,
  onRelease,
  onCopy,
}: {
  server: ServerInstance
  initialTab?: DetailTab
  onClose: () => void
  onChangePassword: (server: ServerInstance) => void
  onTogglePower: (server: ServerInstance, running: boolean) => void
  onReboot: (server: ServerInstance) => void
  onRenew: () => void
  onRelease: () => void
  onCopy: (value: string, messageKey: string) => void
}) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<DetailTab>(initialTab)

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
        className='absolute inset-0 bg-black/40 backdrop-blur-[2px]'
      />
      <div className='bg-background relative flex h-full w-full max-w-3xl flex-col shadow-2xl'>
        {/* 头部 */}
        <div className='border-border/70 bg-card border-b px-6 py-4'>
          <div className='flex flex-wrap items-start justify-between gap-3'>
            <div className='min-w-0'>
              <div className='flex flex-wrap items-center gap-2'>
                <h2 className='text-foreground truncate text-lg font-bold'>
                  {server.name}
                </h2>
                <Badge className={cn('shrink-0', statusTone(statusKeyOf(server)))}>
                  {t(statusKeyOf(server))}
                </Badge>
                {server.provision_mode === 'simulated' && (
                  <Badge className='shrink-0 bg-amber-500/15 text-amber-700 dark:text-amber-400'>
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
                    ? 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-400'
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
                      released ? t('Released') : formatDateTime(server.expire_at)
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
                      value={`${server.spec.data_disk} GB`}
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
                    <span className='text-foreground font-medium'>
                      {t('Allowed from your account IP only')}
                    </span>
                  </div>
                  <div className='flex items-center justify-between gap-3'>
                    <span className='text-muted-foreground'>
                      {t('Outbound traffic')}
                    </span>
                    <span className='text-foreground font-medium'>
                      {t('Unrestricted')}
                    </span>
                  </div>
                  <div className='flex items-center justify-between gap-3'>
                    <span className='text-muted-foreground'>
                      {t('DDoS protection')}
                    </span>
                    <span className='text-foreground font-medium'>
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
                {server.spec.data_disk && (
                  <InfoRow
                    label={t('Data disk')}
                    value={`${server.spec.data_disk} GB NVMe`}
                  />
                )}
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
      <h3 className='text-foreground mb-4 text-sm font-semibold'>{title}</h3>
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
          'text-foreground mt-0.5 truncate text-sm font-medium',
          mono && 'font-mono text-[13px]'
        )}
      >
        {value}
      </div>
    </div>
  )
}
