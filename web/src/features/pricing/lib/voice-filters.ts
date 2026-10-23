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
// CUSTOM: 预置音色列表的筛选（fork 扩展）。
import type { CatalogVoice } from '../types'

export type VoiceFilter = {
  query: string
  category: string
  gender: string
  language: string
}

export const EMPTY_VOICE_FILTER: VoiceFilter = {
  query: '',
  category: '',
  gender: '',
  language: '',
}

/** 搜索覆盖名称、音色 ID 以及特质、场景、标签、口音、描述等文字信息。 */
function searchableText(voice: CatalogVoice): string {
  return [
    voice.name,
    voice.voice,
    voice.category,
    voice.accent,
    voice.description,
    ...(voice.traits ?? []),
    ...(voice.scenes ?? []),
    ...(voice.labels ?? []),
    ...(voice.samples ?? []).map((sample) => sample.label ?? ''),
  ]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()
}

export function filterVoices(
  voices: CatalogVoice[],
  filter: VoiceFilter
): CatalogVoice[] {
  const terms = filter.query.trim().toLowerCase().split(/\s+/).filter(Boolean)
  return voices.filter((voice) => {
    if (filter.category && voice.category !== filter.category) return false
    if (filter.gender && voice.gender !== filter.gender) return false
    if (filter.language && !(voice.languages ?? []).includes(filter.language)) {
      return false
    }
    if (terms.length === 0) return true
    const text = searchableText(voice)
    return terms.every((term) => text.includes(term))
  })
}

/** 列出数据里实际出现过的分类、性别与语种，作为筛选项。 */
export function voiceFilterOptions(voices: CatalogVoice[]) {
  const categories = new Set<string>()
  const genders = new Set<string>()
  const languages = new Set<string>()
  for (const voice of voices) {
    if (voice.category) categories.add(voice.category)
    if (voice.gender) genders.add(voice.gender)
    for (const language of voice.languages ?? []) languages.add(language)
  }
  return {
    categories: [...categories],
    genders: [...genders],
    languages: [...languages].sort(),
  }
}
