import { Cpu, Gpu, HardDrive, MapPin, MemoryStick, Network } from 'lucide-react'
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
import { motion } from 'motion/react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import {
  BANDWIDTHS,
  PLANS,
  REGIONS,
  formatGpu,
  formatSpec,
  formatDataDiskExpand,
  getDuration,
  getSpec,
  planGpuSummary,
} from '../constants'
import {
  formatAmount,
  HOURS_PER_MONTH,
  planFromPrice,
  resolveDurationId,
  specMachinePrice,
} from '../lib/pricing'
import type { ServerConfig, ServerSpec } from '../types'

interface ConfigSectionProps {
  config: ServerConfig
  onChange: (patch: Partial<ServerConfig>) => void
}

function StepHeading({
  index,
  title,
  hint,
}: {
  index: number
  title: string
  hint?: string
}) {
  return (
    <div className='mb-3 flex items-center gap-2.5'>
      <span className='flex size-6 items-center justify-center rounded-full bg-[#e8f4f0] text-xs font-semibold text-[#3a8f7a]'>
        {index}
      </span>
      <h3 className='text-sm font-semibold text-[#1a4a3f]'>{title}</h3>
      {hint && <span className='text-muted-foreground text-xs'>{hint}</span>}
    </div>
  )
}

export function ConfigSection({ config, onChange }: ConfigSectionProps) {
  const { t } = useTranslation()

  const plan = PLANS.find((p) => p.id === config.plan) ?? PLANS[0]
  const spec = getSpec(plan, config.specId)
  // 左侧所有价格一律用「当前选中时长档」的口径显示：
  // 规格按钮标的价 = 右侧摘要的「套餐」行 + 「配置」行，两边同口径才对得上。
  const billing = getDuration(resolveDurationId(config)).pricing

  const specPriceText = (item: ServerSpec) => {
    const value = formatAmount(specMachinePrice(plan, item, billing))
    return billing === 'hourly'
      ? t('{{price}} / hour', { price: value })
      : t('{{price}} / month', { price: value })
  }

  return (
    <div className='space-y-8'>
      {/* 地区 */}
      <section>
        <StepHeading
          index={1}
          title={t('Region')}
          hint={t('Where the server is located')}
        />
        <div className='grid gap-3 sm:grid-cols-2'>
          {REGIONS.map((region) => {
            const active = config.region === region.id
            return (
              <button
                key={region.id}
                type='button'
                onClick={() => onChange({ region: region.id })}
                className={cn(
                  'group relative rounded-2xl border p-4 text-left transition-all',
                  active
                    ? 'border-[#7dd3c0] bg-[#7dd3c0]/[0.07] shadow-[0_0_0_3px_rgba(125,211,192,0.18)]'
                    : 'border-border bg-background hover:border-[#7dd3c0]/60 hover:bg-[#7dd3c0]/[0.03]'
                )}
              >
                <div className='flex items-start justify-between gap-3'>
                  <div className='flex items-center gap-2.5'>
                    <span
                      className={cn(
                        'flex size-9 items-center justify-center rounded-xl transition-colors',
                        active
                          ? 'bg-[#7dd3c0] text-[#06231d]'
                          : 'bg-muted text-muted-foreground'
                      )}
                    >
                      <MapPin className='size-4.5' />
                    </span>
                    <div>
                      <div className='font-semibold'>{t(region.nameKey)}</div>
                      <div className='text-muted-foreground mt-0.5 text-xs'>
                        {t(region.descriptionKey)}
                      </div>
                    </div>
                  </div>
                  {active && (
                    <span className='mt-1 size-2 shrink-0 rounded-full bg-[#3a8f7a]' />
                  )}
                </div>
                <div className='mt-3 flex flex-wrap items-center gap-2 text-xs'>
                  <span className='bg-muted rounded-md px-2 py-1 font-medium'>
                    {region.line}
                  </span>
                  <span className='bg-muted rounded-md px-2 py-1 font-medium'>
                    {region.latency}
                  </span>
                </div>
              </button>
            )
          })}
        </div>
      </section>

      {/* 型号 */}
      <section>
        <StepHeading
          index={2}
          title={t('Plan')}
          hint={t('Choose a family that matches your workload')}
        />
        <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
          {PLANS.map((item) => {
            const active = config.plan === item.id
            return (
              <button
                key={item.id}
                type='button'
                onClick={() =>
                  onChange({
                    plan: item.id,
                    specId: item.specs[0].id,
                    // 换机型时若首选档带默认带宽，一并套上（与规格按钮同规则）
                    ...(item.specs[0].defaultMbps
                      ? { mbps: item.specs[0].defaultMbps }
                      : {}),
                  })
                }
                className={cn(
                  'relative rounded-2xl border p-4 text-left transition-all',
                  active
                    ? 'border-[#7dd3c0] bg-[#7dd3c0]/[0.07] shadow-[0_0_0_3px_rgba(125,211,192,0.18)]'
                    : 'border-border bg-background hover:border-[#7dd3c0]/60 hover:bg-[#7dd3c0]/[0.03]'
                )}
              >
                {item.badgeKey && (
                  <Badge className='absolute -top-2 right-3 bg-[#3a8f7a] text-[10px] text-white'>
                    {t(item.badgeKey)}
                  </Badge>
                )}
                <div className='font-semibold'>{t(item.nameKey)}</div>
                <div className='text-muted-foreground mt-1 text-xs'>
                  {t(item.taglineKey)}
                </div>
                {planGpuSummary(item) && (
                  <div className='mt-2 inline-flex items-center gap-1.5 rounded-md bg-[#e8f4f0] px-2 py-1 text-[11px] font-semibold text-[#1a4a3f]'>
                    <Gpu className='size-3.5' />
                    {planGpuSummary(item)}
                  </div>
                )}
                {/* 双口径起步价：右侧的时长选择器同时含按量与包月档，
                    卡片只标一个口径会让用户没法横向比机型，所以两个都给出。
                    这两个数是「该机型最便宜那一档的整机价」—— 不能拿裸机型
                    基准价充数，GPU 的基准价 ¥2.50 比它最便宜的整机 ¥5.60
                    低一半，写上去等于误导。 */}
                <div className='mt-3 text-sm font-medium text-[#1a4a3f]'>
                  {t('From {{price}} / month', {
                    price: formatAmount(planFromPrice(item, 'prepaid')),
                  })}
                </div>
                <div className='text-muted-foreground mt-0.5 text-[11px]'>
                  {t('From {{price}} / hour', {
                    price: formatAmount(planFromPrice(item, 'hourly')),
                  })}
                </div>
              </button>
            )
          })}
        </div>
        {/* 口径说明：卡片上的价是整机起步价（= 最低那一档的整机价，
            对多数机型就是基准档），右侧总价还含带宽与地区系数。 */}
        <p className='text-muted-foreground mt-2 text-[11px]'>
          {t(
            'Plan prices are for the base spec and exclude bandwidth and regional pricing.'
          )}
        </p>
      </section>

      {/* 规格 */}
      <section>
        <StepHeading
          index={3}
          title={t('Specifications')}
          hint={
            spec.gpu
              ? t('vCPU, memory, system disk and graphics card')
              : t('vCPU, memory and system disk')
          }
        />
        <div className='flex flex-wrap gap-2.5'>
          {plan.specs.map((item) => {
            const active = config.specId === item.id
            return (
              <button
                key={item.id}
                type='button'
                // 选到带默认带宽的规格（PRO 6000 → 100 Mbps）时一并套上：
                // 显卡机器默认配 5M 没意义，拉几十 GB 权重会卡一整天。
                // 反过来从旗舰档切到小档时不动用户已选的带宽 —— 静默把
                // 100M 降到 20M 会让人以为自己选的档没生效。
                onClick={() =>
                  onChange(
                    item.defaultMbps
                      ? { specId: item.id, mbps: item.defaultMbps }
                      : { specId: item.id }
                  )
                }
                className={cn(
                  'rounded-xl border px-4 py-2.5 text-sm font-medium transition-all',
                  active
                    ? 'border-[#7dd3c0] bg-[#7dd3c0]/[0.12] text-[#1a4a3f] shadow-[0_0_0_3px_rgba(125,211,192,0.18)]'
                    : 'border-border bg-background hover:border-[#7dd3c0]/60'
                )}
              >
                <span className='block'>{formatSpec(item, t)}</span>
                {/* 规格价：选之前就能横向比价，选中后右侧摘要的
                    「套餐」行 + 「配置」行也正好等于这个数 */}
                <span className='text-muted-foreground mt-0.5 block text-[11px]'>
                  {specPriceText(item)}
                </span>
                {item.gpu && (
                  <span className='mt-1 block text-[11px] font-semibold text-[#3a8f7a]'>
                    {formatGpu(item.gpu)}
                  </span>
                )}
                {item.cpuModel && (
                  <span className='text-muted-foreground mt-0.5 block text-[11px]'>
                    {item.cpuModel}
                  </span>
                )}
              </button>
            )
          })}
        </div>
        <div className='text-muted-foreground mt-3 flex flex-wrap gap-4 text-xs'>
          <span className='inline-flex items-center gap-1.5'>
            <Cpu className='size-3.5' /> {spec.cpu} vCPU
          </span>
          <span className='inline-flex items-center gap-1.5'>
            <MemoryStick className='size-3.5' /> {spec.memory} GB RAM
          </span>
          <span className='inline-flex items-center gap-1.5'>
            <HardDrive className='size-3.5' /> {spec.disk} GB NVMe
          </span>
          {spec.dataDisk && (
            <span className='inline-flex items-center gap-1.5'>
              <HardDrive className='size-3.5' /> {spec.dataDisk} GB{' '}
              {t('Data disk')}
              {formatDataDiskExpand(spec, t) && (
                <span className='text-muted-foreground'>
                  （{formatDataDiskExpand(spec, t)}）
                </span>
              )}
            </span>
          )}
          {spec.cpuModel && (
            <span className='text-muted-foreground'>{spec.cpuModel}</span>
          )}
          {spec.gpu && (
            <span className='inline-flex items-center gap-1.5 font-semibold text-[#3a8f7a]'>
              <Gpu className='size-3.5' /> {formatGpu(spec.gpu)}
            </span>
          )}
        </div>
      </section>

      {/* 带宽 */}
      <section>
        <StepHeading
          index={4}
          title={t('Bandwidth')}
          hint={t('Dedicated inbound and outbound bandwidth, charged monthly')}
        />
        <div className='grid grid-cols-2 gap-2.5 sm:grid-cols-3 lg:grid-cols-5'>
          {BANDWIDTHS.map((item) => {
            const active = config.mbps === item.mbps
            return (
              <motion.button
                key={item.mbps}
                type='button'
                whileTap={{ scale: 0.97 }}
                onClick={() => onChange({ mbps: item.mbps })}
                className={cn(
                  'rounded-xl border px-3 py-3 text-center transition-all',
                  active
                    ? 'border-[#7dd3c0] bg-[#7dd3c0]/[0.12] shadow-[0_0_0_3px_rgba(125,211,192,0.18)]'
                    : 'border-border bg-background hover:border-[#7dd3c0]/60'
                )}
              >
                <Network
                  className={cn(
                    'mx-auto size-4',
                    active ? 'text-[#3a8f7a]' : 'text-muted-foreground'
                  )}
                />
                <div className='mt-1.5 text-sm font-semibold'>
                  {item.mbps} Mbps
                </div>
                <div className='text-muted-foreground mt-0.5 text-[11px]'>
                  {item.pricePerMonth === 0
                    ? t('Included')
                    : t('+{{price}} / month', {
                        price: `¥${item.pricePerMonth}`,
                      })}
                </div>
                {/* 带宽按月定价；按量租用时它会折算成小时价，
                    两个口径都摆出来，右侧摘要里的小时加价才追得到 */}
                {item.pricePerMonth > 0 && (
                  <div className='text-muted-foreground mt-0.5 text-[11px]'>
                    {t('≈ +{{price}} / hour', {
                      price: formatAmount(item.pricePerMonth / HOURS_PER_MONTH),
                    })}
                  </div>
                )}
              </motion.button>
            )
          })}
        </div>
      </section>
    </div>
  )
}
