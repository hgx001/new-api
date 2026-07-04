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
  Zap,
  ShieldCheck,
  Shield,
  Scaling,
  Gauge,
  Code2,
  DollarSign,
  MessageCircleQuestion,
} from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'
import { useStatus } from '@/hooks/use-status'

interface FeaturesProps {
  className?: string
}

export function Features(_props: FeaturesProps) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const wechatQr = status?.wechat_qrcode as string | undefined

  const pillars = [
    {
      id: 'speed',
      title: t('Speed'),
      description: t('Speed description'),
      icon: <Zap className='size-6 text-[#3a8f7a]' strokeWidth={1.5} />,
    },
    {
      id: 'stability',
      title: t('Stability'),
      description: t('Stability description'),
      icon: <ShieldCheck className='size-6 text-[#3a8f7a]' strokeWidth={1.5} />,
    },
    {
      id: 'security',
      title: t('Security'),
      description: t('Security description'),
      icon: <Shield className='size-6 text-[#3a8f7a]' strokeWidth={1.5} />,
    },
    {
      id: 'scalability',
      title: t('Scalability'),
      description: t('Scalability description'),
      icon: <Scaling className='size-6 text-[#3a8f7a]' strokeWidth={1.5} />,
    },
  ]

  const advantages = [
    {
      id: 'stable',
      title: t('Stable and Reliable'),
      description: t('Stable and Reliable description'),
      icon: <ShieldCheck className='size-6 text-[#3a8f7a]' strokeWidth={1.5} />,
    },
    {
      id: 'performance',
      title: t('Extreme Performance'),
      description: t('Extreme Performance description'),
      icon: <Gauge className='size-6 text-[#3a8f7a]' strokeWidth={1.5} />,
    },
    {
      id: 'compatible',
      title: t('Open and Compatible'),
      description: t('Open and Compatible description'),
      icon: <Code2 className='size-6 text-[#3a8f7a]' strokeWidth={1.5} />,
    },
    {
      id: 'billing',
      title: t('Transparent Billing'),
      description: t('Transparent Billing description'),
      icon: <DollarSign className='size-6 text-[#3a8f7a]' strokeWidth={1.5} />,
    },
  ]

  return (
    <section className='relative z-10 bg-[#f5f9f7] px-6 py-24 md:py-32'>
      <div className='mx-auto max-w-6xl'>
        {/* Core Pillars */}
        <AnimateInView className='mb-16 text-center'>
          <p className='mb-3 text-xs font-medium tracking-widest uppercase text-[#8aa89e]'>
            {t('Enterprise Core Pillars')}
          </p>
          <h2 className='text-2xl leading-tight font-bold tracking-tight text-[#1a4a3f] md:text-3xl'>
            {t('Enterprise Core Pillars heading')}
          </h2>
        </AnimateInView>

        <div className='grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-4'>
          {pillars.map((pillar, index) => (
            <AnimateInView
              key={pillar.id}
              delay={index * 100}
              animation='fade-up'
            >
              <div className='h-full rounded-3xl bg-white p-7 shadow-[0_20px_60px_rgba(120,180,160,0.15)] transition-transform duration-300 hover:-translate-y-1 md:p-8'>
                <div className='mb-5 flex size-12 items-center justify-center rounded-2xl bg-[#e8f4f0]'>
                  {pillar.icon}
                </div>
                <h3 className='mb-2 text-lg font-semibold text-[#1a4a3f]'>
                  {pillar.title}
                </h3>
                <p className='text-sm leading-relaxed text-[#5a7a72]'>
                  {pillar.description}
                </p>
              </div>
            </AnimateInView>
          ))}
        </div>

        {/* Core Advantages */}
        <AnimateInView className='mb-16 mt-24 text-center'>
          <p className='mb-3 text-xs font-medium tracking-widest uppercase text-[#8aa89e]'>
            {t('Core Advantages')}
          </p>
          <h2 className='text-2xl leading-tight font-bold tracking-tight text-[#1a4a3f] md:text-3xl'>
            {t('Core Advantages heading')}
          </h2>
        </AnimateInView>

        <div className='grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3'>
          {advantages.map((advantage, index) => (
            <AnimateInView
              key={advantage.id}
              delay={index * 100}
              animation='fade-up'
            >
              <div className='h-full rounded-3xl bg-white p-7 shadow-[0_20px_60px_rgba(120,180,160,0.15)] transition-transform duration-300 hover:-translate-y-1 md:p-8'>
                <div className='mb-5 flex size-12 items-center justify-center rounded-2xl bg-[#e8f4f0]'>
                  {advantage.icon}
                </div>
                <h3 className='mb-2 text-lg font-semibold text-[#1a4a3f]'>
                  {advantage.title}
                </h3>
                <p className='text-sm leading-relaxed text-[#5a7a72]'>
                  {advantage.description}
                </p>
              </div>
            </AnimateInView>
          ))}

          {/* 24/7 Service + WeChat QR */}
          <AnimateInView delay={400} animation='fade-up' className='md:col-span-2 lg:col-span-1'>
            <div className='h-full rounded-3xl bg-white p-7 shadow-[0_20px_60px_rgba(120,180,160,0.15)] transition-transform duration-300 hover:-translate-y-1 md:p-8'>
              <div className='mb-5 flex size-12 items-center justify-center rounded-2xl bg-[#e8f4f0]'>
                <MessageCircleQuestion
                  className='size-6 text-[#3a8f7a]'
                  strokeWidth={1.5}
                />
              </div>
              <h3 className='mb-2 text-lg font-semibold text-[#1a4a3f]'>
                {t('24/7 Service')}
              </h3>
              <p className='text-sm leading-relaxed text-[#5a7a72]'>
                {t('24/7 Service description')}
              </p>

              <div className='mt-6 flex items-center gap-5 rounded-2xl bg-[#f5f9f7] p-4'>
                {wechatQr ? (
                  <QRCodeSVG
                    value={wechatQr}
                    size={96}
                    level='M'
                    bgColor='#f5f9f7'
                    fgColor='#1a4a3f'
                  />
                ) : (
                  <div className='flex size-24 flex-col items-center justify-center rounded-xl bg-[#e8f4f0]'>
                    <MessageCircleQuestion
                      className='mb-1 size-7 text-[#7dd3c0]'
                      strokeWidth={1.5}
                    />
                    <span className='px-2 text-center text-[10px] text-[#5a7a72]'>
                      {t('WeChat QR code placeholder')}
                    </span>
                  </div>
                )}
                <div className='text-sm text-[#5a7a72]'>
                  <p className='font-medium text-[#1a4a3f]'>{t('Scan to contact')}</p>
                  <p className='mt-1 text-xs'>{t('WeChat support team')}</p>
                </div>
              </div>
            </div>
          </AnimateInView>
        </div>
      </div>
    </section>
  )
}
