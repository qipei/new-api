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
import { useQuery } from '@tanstack/react-query'
import {
  ArrowRight,
  Box,
  Check,
  Clipboard,
  Code2,
  Copy,
  FileCheck2,
  Headphones,
  ShieldCheck,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import {
  TOKEN01_LOGO_RADIUS_CLASS,
  Token01Wordmark,
} from '@/components/layout/components/public-brand'
import { usePricingData } from '@/features/pricing/hooks'
import { isTokenBasedModel } from '@/features/pricing/lib/model-helpers'
import {
  formatPrice,
  formatRequestPrice,
  stripTrailingZeros,
} from '@/features/pricing/lib/price'
import type { ModelRanking } from '@/features/rankings/types'
import { useStatus } from '@/hooks/use-status'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import { getHomeRankings } from '../api'
import { selectHomeModels } from '../lib/home-models'

const DASHBOARD_URL = 'https://token01.net/dashboard'
const DOCS_URL = 'https://token01.apifox.cn/'
const PRICING_URL = 'https://token01.net/pricing'
const DOUJU_URL = 'https://files.token01.net/tools/dj/index.html'
const DOUKUAIZHUANG_URL = 'https://files.token01.net/tools/dkz/index.html'
const API_ENDPOINT = 'https://www.token01.net/v1'

const GATEWAY_MODELS = [
  { name: 'DeepSeek', icon: 'DeepSeek.Color' },
  { name: 'Kimi', icon: 'Moonshot' },
  { name: 'Qwen', icon: 'Qwen.Color' },
  { name: 'GLM', icon: 'Zhipu.Color' },
  { name: 'OpenAI', icon: 'OpenAI' },
  { name: 'Claude', icon: 'Claude.Color' },
  { name: 'Gemini', icon: 'Gemini.Color' },
] as const

const FALLBACK_MODELS: ModelRanking[] = [
  ['DeepSeek R1', 'DeepSeek', 'DeepSeek.Color'],
  ['DeepSeek V3', 'DeepSeek', 'DeepSeek.Color'],
  ['Kimi K2.5', 'Moonshot', 'Moonshot'],
  ['GLM-5.2', 'Zhipu', 'Zhipu.Color'],
  ['Claude 3.5 Sonnet', 'Anthropic', 'Claude.Color'],
  ['GPT-4o', 'OpenAI', 'OpenAI'],
].map(([modelName, vendor, vendorIcon], index) => ({
  rank: index + 1,
  model_name: modelName,
  vendor,
  vendor_icon: vendorIcon,
  category: 'all',
  total_tokens: 0,
  share: 0,
  growth_pct: 0,
}))

const FALLBACK_MODEL_PRICES: Record<string, string> = {
  'DeepSeek R1': '¥1.4 / ¥5.6',
  'DeepSeek V3': '¥0.7 / ¥2.8',
  'Kimi K2.5': '¥2.4 / ¥12.6',
  'GLM-5.2': '¥4.8 / ¥16.8',
  'Claude 3.5 Sonnet': '¥9 / ¥27',
  'GPT-4o': '¥11 / ¥33',
}

const FEATURES = [
  {
    title: 'Transparent billing',
    description:
      'Pay as you go with no minimum spend\nPrices are always visible',
    icon: Clipboard,
  },
  {
    title: 'Compliant invoicing',
    description: 'VAT invoices for business customers\nSimple reimbursement',
    icon: FileCheck2,
  },
  {
    title: 'Traceable model sources',
    description: 'Official access channels\nClearly identified model versions',
    icon: Box,
  },
  {
    title: 'Enterprise support',
    description: 'Dedicated technical support\nResponsive around the clock',
    icon: Headphones,
  },
] as const

function resolveContactQr(status: ReturnType<typeof useStatus>['status']) {
  return (status?.wechat_qrcode ||
    status?.wechat_qr_code ||
    status?.wechat_qrcode_image_url ||
    status?.wechat_qr_code_image_url ||
    status?.wechat_account_qrcode_image_url ||
    status?.WeChatAccountQRCodeImageURL ||
    status?.data?.wechat_qrcode ||
    status?.data?.WeChatAccountQRCodeImageURL ||
    '/contact-wechat-qr.png') as string
}

function GatewayDiagram() {
  const { t } = useTranslation()

  return (
    <div className='rounded-[20px] border border-[#eae8e0] bg-white p-5 shadow-[0_20px_48px_rgba(20,20,20,0.06)] sm:p-8 dark:border-white/10 dark:bg-[#191919]'>
      <h2 className='mb-6 text-[17px] font-black'>
        {t('One endpoint for every model')}
      </h2>
      <div className='grid grid-cols-[auto_1fr_auto] items-center gap-2'>
        <div className='flex flex-col items-center gap-2.5'>
          <div className='grid size-[76px] place-items-center rounded-[16px] border-2 border-dashed border-[#c9c6bb] bg-[#faf9f5] dark:bg-white/5'>
            <Code2 className='size-7 text-[#55534b] dark:text-white/70' />
          </div>
          <div className='text-center leading-5'>
            <div className='text-[13px] font-bold'>{t('Existing app')}</div>
            <div className='text-[11px] text-[#8c8a82]'>
              {t('Your service or system')}
            </div>
          </div>
        </div>
        <div className='-mt-8 flex min-w-0 flex-col items-center gap-1 px-1 sm:px-3'>
          <span className='text-center text-[10px] text-[#8c8a82] sm:text-[11px]'>
            {t('OpenAI compatible · HTTPS')}
          </span>
          <div className='relative h-0.5 w-full bg-[repeating-linear-gradient(90deg,#c9c6bb_0_6px,transparent_6px_12px)]'>
            <span className='absolute -top-[7px] -right-1 text-[#c9c6bb]'>
              ▶
            </span>
          </div>
        </div>
        <div className='flex flex-col items-center gap-2.5'>
          <img
            src='/token01-arcade.gif'
            alt='token01 API gateway'
            className={cn(
              'size-[76px] object-cover shadow-[0_8px_20px_rgba(20,20,20,0.25)]',
              TOKEN01_LOGO_RADIUS_CLASS
            )}
          />
          <div className='text-center leading-5'>
            <Token01Wordmark className='text-[13px] font-bold' />
            <div className='text-[11px] text-[#8c8a82]'>{t('API gateway')}</div>
          </div>
        </div>
      </div>
      <div className='my-5 h-px bg-[#efede5] dark:bg-white/10' />
      <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
        {GATEWAY_MODELS.map((model) => (
          <div
            key={model.name}
            className='flex items-center gap-2 rounded-[10px] border border-[#efede5] bg-[#faf9f5] px-2.5 py-2 text-[12px] font-medium dark:border-white/10 dark:bg-white/5'
          >
            {getLobeIcon(model.icon, 18)}
            <span className='truncate'>{model.name}</span>
          </div>
        ))}
        <a
          href={PRICING_URL}
          className='flex items-center gap-2 rounded-[10px] border border-[#efede5] bg-[#faf9f5] px-2.5 py-2 text-[12px] font-medium transition-colors hover:border-[#ffc800] dark:border-white/10 dark:bg-white/5'
        >
          {getLobeIcon('OpenRouter', 18)}
          <span className='truncate'>{t('More models')}</span>
        </a>
      </div>
    </div>
  )
}

function HotModels() {
  const { t } = useTranslation()
  const pricing = usePricingData()
  const rankings = useQuery({
    queryKey: ['home-rankings', 'week'],
    queryFn: () => getHomeRankings('week'),
    staleTime: 5 * 60 * 1000,
    retry: false,
  })
  const liveModels = useMemo(
    () => selectHomeModels(rankings.data?.data.models ?? []),
    [rankings.data?.data.models]
  )
  const models = liveModels.length > 0 ? liveModels : FALLBACK_MODELS
  const pricingByModel = useMemo(
    () =>
      new Map(
        pricing.models.map((model) => [model.model_name.toLowerCase(), model])
      ),
    [pricing.models]
  )

  return (
    <section className='mx-auto max-w-[1180px] px-5 pt-2 pb-14 sm:px-8 lg:px-10'>
      <div className='mb-5 flex items-end justify-between gap-4'>
        <h2 className='text-xl font-black'>
          {t('Popular models · price examples')}{' '}
          <span className='text-[13px] font-normal text-[#8c8a82]'>
            {t('(Input / output, per 1M tokens)')}
          </span>
        </h2>
        <a
          href={PRICING_URL}
          className='shrink-0 text-sm font-medium text-[#a77900] transition-colors hover:text-[#141414] dark:text-[#ffc800]'
        >
          {t('View all models')} →
        </a>
      </div>
      <div className='grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6'>
        {models.map((model) => {
          const priceModel = pricingByModel.get(model.model_name.toLowerCase())
          let price = FALLBACK_MODEL_PRICES[model.model_name]

          if (priceModel) {
            price = isTokenBasedModel(priceModel)
              ? `${stripTrailingZeros(
                  formatPrice(
                    priceModel,
                    'input',
                    'M',
                    false,
                    pricing.priceRate,
                    pricing.usdExchangeRate
                  )
                )} / ${stripTrailingZeros(
                  formatPrice(
                    priceModel,
                    'output',
                    'M',
                    false,
                    pricing.priceRate,
                    pricing.usdExchangeRate
                  )
                )}`
              : stripTrailingZeros(
                  formatRequestPrice(
                    priceModel,
                    false,
                    pricing.priceRate,
                    pricing.usdExchangeRate
                  )
                )
          }

          return (
            <article
              key={model.model_name}
              className='flex min-w-0 flex-col gap-3 rounded-[16px] border border-[#eae8e0] bg-white p-4 transition-all hover:-translate-y-0.5 hover:border-[#ffc800] hover:shadow-[0_8px_24px_rgba(20,20,20,0.06)] dark:border-white/10 dark:bg-[#191919]'
            >
              {getLobeIcon(model.vendor_icon, 26)}
              <div className='truncate text-[13px] font-bold'>
                {model.model_name}
              </div>
              <div className="[font-family:'IBM_Plex_Mono',ui-monospace,monospace] text-[12px] text-[#55534b] dark:text-white/65">
                {price || t('Price unavailable')}
              </div>
            </article>
          )
        })}
      </div>
    </section>
  )
}

function FeatureStrip() {
  const { t } = useTranslation()

  return (
    <section className='border-y border-[#eae8e0] bg-white dark:border-white/10 dark:bg-[#191919]'>
      <div className='mx-auto grid max-w-[1180px] gap-8 px-5 py-10 sm:grid-cols-2 sm:px-8 lg:grid-cols-4 lg:px-10'>
        {FEATURES.map((feature) => {
          const Icon = feature.icon
          return (
            <article key={feature.title} className='flex items-start gap-3.5'>
              <span className='grid size-11 shrink-0 place-items-center rounded-full bg-[#fff4cc] text-[#141414]'>
                <Icon className='size-[22px]' />
              </span>
              <div>
                <h3 className='text-[15px] font-bold'>{t(feature.title)}</h3>
                <p className='mt-1 text-[13px] leading-6 whitespace-pre-line text-[#8c8a82]'>
                  {t(feature.description)}
                </p>
              </div>
            </article>
          )
        })}
      </div>
    </section>
  )
}

export function ProductTools() {
  const { t } = useTranslation()

  return (
    <section className='mx-auto max-w-[1180px] px-5 py-14 sm:px-8 lg:px-10'>
      <h2 className='text-xl font-black'>{t('Product tools')}</h2>
      <p className='mt-1 text-[13px] text-[#8c8a82]'>
        {t('Tools that make creating and working with AI simpler.')}
      </p>
      <div className='mt-5 grid gap-4 md:grid-cols-2'>
        <a
          href={DOUJU_URL}
          target='_blank'
          rel='noopener noreferrer'
          className='flex gap-5 rounded-[18px] border border-[#eae8e0] bg-white p-5 text-[#141414] transition-colors hover:border-[#ffc800] hover:text-[#141414] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#ffc800] sm:p-7 dark:border-white/10 dark:bg-[#191919] dark:text-white dark:hover:text-white'
        >
          <img
            src='/douju.png'
            alt='DouJu'
            loading='lazy'
            className='size-[76px] shrink-0 rounded-[20px] object-cover sm:size-[84px]'
          />
          <div>
            <h3 className='text-[17px] font-black'>{t('DouJu')}</h3>
            <p className='mt-2 text-[13px] leading-6 text-[#55534b] dark:text-white/65'>
              {t(
                'A local AI short-drama studio covering scripts, characters, storyboards, video generation, and final assembly.'
              )}
            </p>
            <span className='mt-2 inline-block text-[13px] font-medium text-[#a77900] dark:text-[#ffc800]'>
              {t('Learn about DouJu')} →
            </span>
          </div>
        </a>
        <a
          href={DOUKUAIZHUANG_URL}
          target='_blank'
          rel='noopener noreferrer'
          className='flex gap-5 rounded-[18px] border border-[#eae8e0] bg-white p-5 text-[#141414] transition-colors hover:border-[#ffc800] hover:text-[#141414] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#ffc800] sm:p-7 dark:border-white/10 dark:bg-[#191919] dark:text-white dark:hover:text-white'
        >
          <img
            src='/doukuaizhuang.png'
            alt='DouKuaizhuang'
            loading='lazy'
            className='size-[76px] shrink-0 rounded-[20px] object-cover sm:size-[84px]'
          />
          <div>
            <h3 className='text-[17px] font-black'>{t('DouKuaizhuang')}</h3>
            <p className='mt-2 text-[13px] leading-6 text-[#55534b] dark:text-white/65'>
              {t(
                'Install and configure popular AI desktop apps in one step, including the gateway address and API key.'
              )}
            </p>
            <span className='mt-2 inline-block text-[13px] font-medium text-[#128c4b]'>
              {t('Learn about DouKuaizhuang')} →
            </span>
          </div>
        </a>
      </div>
    </section>
  )
}

export function DefaultHome() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const [copied, setCopied] = useState(false)
  const [contactOpen, setContactOpen] = useState(false)
  const contactQr = resolveContactQr(status)

  const copyEndpoint = async () => {
    if (!navigator.clipboard) return

    try {
      await navigator.clipboard.writeText(API_ENDPOINT)
    } catch {
      return
    }

    setCopied(true)
    window.setTimeout(() => setCopied(false), 1500)
  }

  return (
    <main className="min-h-svh bg-[#f7f6f2] [font-family:'Noto_Sans_SC_Variable','PingFang_SC','Microsoft_YaHei',sans-serif] text-[#141414] dark:bg-[#111] dark:text-[#f5f5f2]">
      <section className='mx-auto grid max-w-[1180px] items-center gap-12 px-5 pt-28 pb-16 sm:px-8 lg:grid-cols-2 lg:gap-14 lg:px-10 lg:pt-32'>
        <div className='flex flex-col items-start gap-5'>
          <div className='inline-flex items-center gap-2 rounded-full border border-[#e3e1d8] bg-white px-4 py-2 text-[13px] text-[#55534b] dark:border-white/10 dark:bg-[#191919] dark:text-white/70'>
            <ShieldCheck className='size-4' />
            {t('Enterprise AI model API gateway · all routes operational')}
          </div>
          <h1 className='text-[clamp(2.35rem,5vw,2.9rem)] leading-[1.22] font-black tracking-[-0.01em]'>
            {t('Connect to leading AI models worldwide')}
          </h1>
          <h2 className='text-[clamp(1.55rem,3.5vw,1.9rem)] leading-[1.3] font-black'>
            <span className='bg-[linear-gradient(transparent_62%,#ffc800_62%_94%,transparent_94%)] px-0.5'>
              {t('One API')}
            </span>
            {t(', compatible with the OpenAI protocol')}
          </h2>
          <p className='max-w-[470px] text-[15px] leading-7 text-[#55534b] dark:text-white/65'>
            {t(
              'Use DeepSeek, Kimi, Qwen, GLM, OpenAI, Claude, and more through one interface. No repeated top-ups or key changes—one endpoint is all your team needs.'
            )}
          </p>
          <div className='flex flex-wrap gap-3'>
            <a
              href={DASHBOARD_URL}
              className='inline-flex items-center gap-1 rounded-[12px] bg-[#ffc800] px-7 py-3 text-[15px] font-bold text-[#141414] shadow-[0_6px_18px_rgba(255,200,0,0.4)] transition-colors hover:bg-[#141414] hover:text-[#ffc800]'
            >
              {t('Get API key')} <ArrowRight className='size-4' />
            </a>
            <a
              href={DOCS_URL}
              target='_blank'
              rel='noopener noreferrer'
              className='rounded-[12px] border border-[#e3e1d8] bg-white px-7 py-3 text-[15px] transition-colors hover:border-[#141414] dark:border-white/10 dark:bg-[#191919]'
            >
              {t('View integration docs')}
            </a>
          </div>
          <div className='mt-1 flex w-full max-w-[430px] flex-col gap-2'>
            <span className='text-xs text-[#8c8a82]'>
              {t('API endpoint (OpenAI compatible)')}
            </span>
            <div className='flex items-center gap-2 rounded-[12px] border border-[#e3e1d8] bg-white p-3 pl-4 dark:border-white/10 dark:bg-[#191919]'>
              <code className="min-w-0 flex-1 truncate [font-family:'IBM_Plex_Mono',ui-monospace,monospace] text-[13px]">
                {API_ENDPOINT}
              </code>
              <button
                type='button'
                onClick={copyEndpoint}
                className='inline-flex items-center gap-1 rounded-[8px] bg-[#f2f0e9] px-3 py-1.5 text-xs font-medium text-[#55534b] dark:bg-white/10 dark:text-white/70'
                aria-label={t('Copy API endpoint')}
              >
                {copied ? (
                  <Check className='size-3.5' />
                ) : (
                  <Copy className='size-3.5' />
                )}
                {copied ? t('Copied') : t('Copy')}
              </button>
            </div>
          </div>
        </div>
        <GatewayDiagram />
      </section>

      <HotModels />
      <FeatureStrip />
      <ProductTools />

      <section className='mx-auto max-w-[1180px] px-5 pb-16 sm:px-8 lg:px-10'>
        <div className='flex flex-col items-start justify-between gap-7 rounded-[20px] border-2 border-[#ffc800] bg-[linear-gradient(135deg,#fffbee,#fff)] px-6 py-8 sm:px-10 lg:flex-row lg:items-center dark:bg-[linear-gradient(135deg,#2c2718,#191919)]'>
          <div>
            <h2 className='text-[28px] font-black'>
              {t('Always on, always dependable')}
            </h2>
            <p className='mt-2 max-w-[560px] text-[14px] leading-7 text-[#55534b] dark:text-white/65'>
              {t(
                'Top up automatically around the clock. Personal projects, development teams, and enterprise workloads can all reach multiple models through one stable gateway.'
              )}
            </p>
          </div>
          <div className='flex w-full shrink-0 flex-col gap-2.5 lg:w-auto'>
            <a
              href={DASHBOARD_URL}
              className='rounded-[12px] bg-[#ffc800] px-10 py-3 text-center text-[15px] font-bold text-[#141414] shadow-[0_6px_18px_rgba(255,200,0,0.4)]'
            >
              {t('Connect now')}
            </a>
            <button
              type='button'
              onClick={() => setContactOpen(true)}
              className='rounded-[12px] border border-[#e3e1d8] bg-white px-10 py-3 text-[15px] dark:border-white/10 dark:bg-[#191919]'
            >
              {t('Contact support')}
            </button>
          </div>
        </div>
      </section>

      <footer className='border-t border-[#eae8e0] bg-white dark:border-white/10 dark:bg-[#191919]'>
        <div className='mx-auto flex max-w-[1180px] flex-wrap justify-center gap-x-7 gap-y-2 px-5 py-6 text-[12px] text-[#8c8a82]'>
          <span>© 2026 {t('Doubyte. All rights reserved.')}</span>
          <a
            href='https://beian.mps.gov.cn/'
            target='_blank'
            rel='noopener noreferrer'
            className='inline-flex items-center gap-1.5 hover:text-[#141414]'
          >
            <img
              src='/gongan.png'
              alt=''
              className='h-3.5 w-3.5 object-contain'
            />
            {t('Shaanxi Public Security No. 61019002004001')}
          </a>
          <a
            href='https://beian.miit.gov.cn/'
            target='_blank'
            rel='noopener noreferrer'
            className='hover:text-[#141414]'
          >
            {t('Shaanxi ICP No. 19023877-5')}
          </a>
        </div>
      </footer>

      <Dialog
        open={contactOpen}
        onOpenChange={setContactOpen}
        title={t('Contact support')}
        description={t(
          'Scan the QR code with WeChat to contact our support team.'
        )}
        contentClassName='sm:max-w-sm'
        contentHeight='auto'
      >
        <img
          src={contactQr}
          alt={t('WeChat support QR code')}
          className='mx-auto aspect-square w-full max-w-[280px] rounded-2xl bg-white object-contain p-2'
        />
      </Dialog>
    </main>
  )
}
