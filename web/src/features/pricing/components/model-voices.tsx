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
// CUSTOM: 模型详情页的预置音色列表（fork 扩展）。
// 试听直接播放厂商的音频地址，不经过本站转存；播放失败时只隐藏该段试听。
import { ExternalLink, Pause, Play, Search } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import {
  EMPTY_VOICE_FILTER,
  filterVoices,
  voiceFilterOptions,
  type VoiceFilter,
} from '../lib/voice-filters'
import type { CatalogVoice, ModelVoiceCatalog } from '../types'

const GENDER_LABEL_KEYS: Record<string, string> = {
  female: 'Female',
  male: 'Male',
}

const KIND_MESSAGE_KEYS: Record<string, string> = {
  reference_audio:
    'This model has no preset voices. Provide 1–3 reference audio clips in the request, or describe the voice in the prompt.',
  cloned_voices:
    'This model only uses voices you cloned. Create a cloned voice first, then pass its ID as the voice.',
}

function useSamplePlayer() {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [playing, setPlaying] = useState<string | null>(null)
  const [failed, setFailed] = useState<Set<string>>(() => new Set())

  useEffect(() => {
    return () => audioRef.current?.pause()
  }, [])

  const markFailed = (url: string) => {
    setPlaying(null)
    setFailed((prev) => new Set(prev).add(url))
  }

  const toggle = (url: string) => {
    if (!audioRef.current) {
      // 只创建一个播放器并注册一次监听；切换试听时只换地址。
      const created = new Audio()
      created.addEventListener('ended', () => setPlaying(null))
      created.addEventListener('error', () => markFailed(created.src))
      audioRef.current = created
    }
    const audio = audioRef.current
    if (playing === url) {
      audio.pause()
      setPlaying(null)
      return
    }
    audio.src = url
    setPlaying(url)
    audio.play().catch(() => markFailed(url))
  }

  return { playing, failed, toggle }
}

export function ModelVoices(props: { catalog: ModelVoiceCatalog }) {
  const { t } = useTranslation()
  const { catalog } = props
  const [filter, setFilter] = useState<VoiceFilter>(EMPTY_VOICE_FILTER)
  const player = useSamplePlayer()
  const options = useMemo(
    () => voiceFilterOptions(catalog.voices),
    [catalog.voices]
  )
  const voices = useMemo(
    () => filterVoices(catalog.voices, filter),
    [catalog.voices, filter]
  )

  const docsLink = (
    <a
      href={catalog.docs_url}
      target='_blank'
      rel='noreferrer'
      className='text-primary inline-flex items-center gap-1 text-xs hover:underline'
    >
      {t('Official voice documentation')}
      <ExternalLink className='size-3' />
    </a>
  )

  if (catalog.kind !== 'voices') {
    return (
      <div className='bg-muted/20 space-y-2 rounded-lg border p-4 text-sm'>
        <p>{t(KIND_MESSAGE_KEYS[catalog.kind])}</p>
        {docsLink}
      </div>
    )
  }

  if (catalog.voices.length === 0) {
    return (
      <div className='bg-muted/20 space-y-2 rounded-lg border p-4 text-sm'>
        <p>
          {t(
            'The voice list has not been synced yet. Browse the official voice library for now.'
          )}
        </p>
        {docsLink}
      </div>
    )
  }

  const setField = (key: keyof VoiceFilter) => (value: string) =>
    setFilter((prev) => ({ ...prev, [key]: value }))

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <p className='text-muted-foreground text-xs'>
          {t('{{count}} voices', { count: catalog.voices.length })}
          {catalog.updated_at &&
            ` · ${t('Updated {{date}}', { date: catalog.updated_at })}`}
        </p>
        {docsLink}
      </div>
      {catalog.note_key && (
        <p className='bg-muted/30 rounded-lg border px-3 py-2 text-xs'>
          {t(catalog.note_key)}
        </p>
      )}

      <div className='grid gap-2 @md/details:grid-cols-[1fr_auto_auto_auto]'>
        <div className='relative'>
          <Search className='text-muted-foreground absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2' />
          <Input
            value={filter.query}
            onChange={(event) => setField('query')(event.target.value)}
            placeholder={t('Search name, ID or scenario')}
            aria-label={t('Search voices')}
            className='h-8 pl-8 text-sm'
          />
        </div>
        {options.categories.length > 1 && (
          <NativeSelect
            size='sm'
            value={filter.category}
            onChange={(event) => setField('category')(event.target.value)}
            aria-label={t('Category')}
          >
            <NativeSelectOption value=''>
              {t('All categories')}
            </NativeSelectOption>
            {options.categories.map((category) => (
              <NativeSelectOption key={category} value={category}>
                {category}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        )}
        {options.genders.length > 1 && (
          <NativeSelect
            size='sm'
            value={filter.gender}
            onChange={(event) => setField('gender')(event.target.value)}
            aria-label={t('Gender')}
          >
            <NativeSelectOption value=''>{t('All genders')}</NativeSelectOption>
            {options.genders.map((gender) => (
              <NativeSelectOption key={gender} value={gender}>
                {t(GENDER_LABEL_KEYS[gender] ?? gender)}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        )}
        {options.languages.length > 1 && (
          <NativeSelect
            size='sm'
            value={filter.language}
            onChange={(event) => setField('language')(event.target.value)}
            aria-label={t('Language')}
          >
            <NativeSelectOption value=''>
              {t('All languages')}
            </NativeSelectOption>
            {options.languages.map((language) => (
              <NativeSelectOption key={language} value={language}>
                {language}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        )}
      </div>

      {voices.length === 0 ? (
        <p className='text-muted-foreground py-6 text-center text-sm'>
          {t('No voices match the filters.')}
        </p>
      ) : (
        <ul className='grid gap-2 @lg/details:grid-cols-2'>
          {voices.map((voice) => (
            <VoiceCard key={voice.voice} voice={voice} player={player} />
          ))}
        </ul>
      )}
    </div>
  )
}

function VoiceCard(props: {
  voice: CatalogVoice
  player: ReturnType<typeof useSamplePlayer>
}) {
  const { t } = useTranslation()
  const { voice, player } = props
  const tags = [
    ...(voice.accent ? [voice.accent] : []),
    ...(voice.traits ?? []),
    ...(voice.scenes ?? []),
    ...(voice.labels ?? []),
  ]
  const samples = (voice.samples ?? []).filter(
    (sample) => !player.failed.has(sample.url)
  )
  const features = voice.features
    ? [
        voice.features.instruction && t('Instruction control'),
        voice.features.ssml && 'SSML',
        voice.features.timestamp && t('Timestamps'),
      ].filter(Boolean)
    : []

  return (
    <li className='bg-card/60 space-y-2 rounded-lg border p-3'>
      <div className='flex items-start gap-2'>
        {voice.avatar ? (
          <img
            src={voice.avatar}
            alt=''
            className='size-9 shrink-0 rounded-full object-cover'
            loading='lazy'
          />
        ) : (
          <div className='bg-muted text-muted-foreground flex size-9 shrink-0 items-center justify-center rounded-full text-sm font-medium'>
            {voice.name.charAt(0)}
          </div>
        )}
        <div className='min-w-0 flex-1'>
          <div className='flex flex-wrap items-baseline gap-x-2'>
            <span className='text-sm font-medium'>{voice.name}</span>
            <span className='text-muted-foreground text-xs'>
              {[
                voice.gender
                  ? t(GENDER_LABEL_KEYS[voice.gender] ?? voice.gender)
                  : '',
                voice.age,
                voice.category,
              ]
                .filter(Boolean)
                .join(' · ')}
            </span>
          </div>
          <div className='flex items-center gap-1'>
            <code className='text-muted-foreground truncate font-mono text-xs'>
              {voice.voice}
            </code>
            <CopyButton
              value={voice.voice}
              className='size-6'
              iconClassName='size-3'
              aria-label={t('Copy voice ID')}
            />
          </div>
        </div>
      </div>

      {tags.length > 0 && (
        <div className='flex flex-wrap gap-1'>
          {tags.slice(0, 6).map((tag) => (
            <Badge
              key={tag}
              variant='secondary'
              className='text-xs font-normal'
            >
              {tag}
            </Badge>
          ))}
        </div>
      )}
      {(voice.emotions?.length ?? 0) > 0 && (
        <p className='text-muted-foreground text-xs'>
          {t('Emotions')}:{' '}
          {voice.emotions?.map((emotion) => emotion.label).join('、')}
        </p>
      )}
      {features.length > 0 && (
        <p className='text-muted-foreground text-xs'>
          {t('Supports')}: {features.join(' · ')}
        </p>
      )}
      {voice.description && (
        <p className='text-muted-foreground line-clamp-2 text-xs'>
          {voice.description}
        </p>
      )}
      {samples.length > 0 && (
        <div className='flex flex-wrap gap-1.5'>
          {samples.map((sample, index) => {
            const active = player.playing === sample.url
            return (
              <Button
                key={sample.url}
                type='button'
                size='xs'
                variant={active ? 'default' : 'outline'}
                title={sample.text}
                onClick={() => player.toggle(sample.url)}
              >
                {active ? (
                  <Pause className='size-3' />
                ) : (
                  <Play className='size-3' />
                )}
                {sample.label ||
                  (samples.length > 1
                    ? t('Sample {{index}}', { index: index + 1 })
                    : t('Play sample'))}
              </Button>
            )
          })}
        </div>
      )}
    </li>
  )
}
