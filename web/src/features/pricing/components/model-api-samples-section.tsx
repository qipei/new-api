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
// CUSTOM: 已登记模型族的按场景请求示例（fork 扩展）。
//
// 整块独立在本文件里，接入点只在 model-details-api.tsx 加一个条件分支，
// 未登记的模型仍走上游原有的通用示例，尽量减少与上游的冲突面。
import { ScrollText } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { BundledLanguage } from 'shiki/bundle/web'

import {
  CodeBlock,
  CodeBlockCopyButton,
} from '@/components/ai-elements/code-block'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useStatus } from '@/hooks/use-status'

import { buildScenarioSample, type SampleLang } from '../lib/model-api-samples'
import { findModelApiSpec, resolveModelApiParams } from '../lib/model-api-specs'
import type { PricingModel } from '../types'

const LANG_LABELS: Record<SampleLang, string> = {
  curl: 'cURL',
  python: 'Python',
  javascript: 'JavaScript',
}

const LANG_HIGHLIGHT: Record<SampleLang, BundledLanguage> = {
  curl: 'bash',
  python: 'python',
  javascript: 'javascript',
}

export function ModelApiSamplesSection(props: { model: PricingModel }) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const [scenario, setScenario] = useState(0)
  const [lang, setLang] = useState<SampleLang>('curl')

  // 与上游示例区块取同一个来源，站点地址变更时两边一致。
  const baseUrl = useMemo(() => {
    const data = status?.data as Record<string, unknown> | undefined
    const candidate = data?.server_address
    if (typeof candidate === 'string' && candidate) {
      return candidate.replace(/\/$/, '')
    }
    if (typeof window !== 'undefined') return window.location.origin
    return 'https://api.example.com'
  }, [status])

  const spec = findModelApiSpec(props.model.model_name)
  const params = useMemo(
    () => resolveModelApiParams(props.model) ?? [],
    [props.model]
  )

  if (!spec || spec.samples.length === 0) return null

  const active = spec.samples[Math.min(scenario, spec.samples.length - 1)]
  const code = buildScenarioSample(lang, {
    baseUrl,
    endpointPath: spec.endpointPath,
    // 示例里的模型名统一换成当前查看的模型，方便直接复制运行。
    body: { ...active.body, model: props.model.model_name },
    params,
    translate: t,
    scenarioTitle: t(active.titleKey),
  })

  return (
    <section>
      <h3 className='mb-3 flex items-center gap-2 text-sm font-semibold'>
        <ScrollText className='text-muted-foreground size-4' />
        {t('Code samples')}
      </h3>

      <div className='flex flex-wrap items-center gap-2'>
        {spec.samples.length > 1 && (
          <Tabs
            value={String(scenario)}
            onValueChange={(v) => setScenario(Number(v))}
          >
            <TabsList className='bg-muted/40 h-8 p-0.5'>
              {spec.samples.map((sample, index) => (
                <TabsTrigger
                  key={sample.titleKey}
                  value={String(index)}
                  className='h-7 px-2.5 text-xs'
                >
                  {t(sample.titleKey)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        )}

        <Tabs
          value={lang}
          onValueChange={(v) => setLang(v as SampleLang)}
          className='ml-auto'
        >
          <TabsList className='bg-muted/40 h-8 p-0.5'>
            {(Object.keys(LANG_LABELS) as SampleLang[]).map((l) => (
              <TabsTrigger key={l} value={l} className='h-7 px-2.5 text-xs'>
                {LANG_LABELS[l]}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      <div className='mt-3'>
        <CodeBlock code={code} language={LANG_HIGHLIGHT[lang]}>
          <CodeBlockCopyButton />
        </CodeBlock>
      </div>

      <p className='text-muted-foreground mt-2 text-xs'>
        {t(
          'Set NEW_API_KEY to a key from your token settings before running the sample.'
        )}
      </p>
    </section>
  )
}
