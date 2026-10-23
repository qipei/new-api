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
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import type { CatalogVoice } from '../../types'
import {
  EMPTY_VOICE_FILTER,
  filterVoices,
  voiceFilterOptions,
} from '../voice-filters'

const voices: CatalogVoice[] = [
  {
    voice: 'yuxiaoyun_v3.1',
    name: '于小云',
    gender: 'female',
    category: '精品中文音色',
    traits: ['元气', '亲切'],
    scenes: ['广告营销', '客服助手'],
  },
  {
    voice: 'Eric_v3.1',
    name: 'Eric',
    gender: 'male',
    category: '精品英文音色',
    accent: '英式男声',
  },
  {
    voice: 'zh_female_vv_uranus_bigtts',
    name: 'Vivi',
    gender: 'female',
    category: '通用',
    languages: ['zh-cn', 'en'],
    labels: ['温柔'],
  },
]

const names = (list: CatalogVoice[]) => list.map((voice) => voice.voice)

describe('voice filters', () => {
  test('returns every voice without filters', () => {
    assert.equal(filterVoices(voices, EMPTY_VOICE_FILTER).length, 3)
  })

  // 用户常按场景或特质找音色，不只是名称。
  test('searches name, id, scenes, traits, accent and labels', () => {
    const search = (query: string) =>
      names(filterVoices(voices, { ...EMPTY_VOICE_FILTER, query }))
    assert.deepEqual(search('客服'), ['yuxiaoyun_v3.1'])
    assert.deepEqual(search('英式'), ['Eric_v3.1'])
    assert.deepEqual(search('URANUS'), ['zh_female_vv_uranus_bigtts'])
    assert.deepEqual(search('温柔 vivi'), ['zh_female_vv_uranus_bigtts'])
    assert.deepEqual(search('不存在'), [])
  })

  test('combines category, gender and language filters', () => {
    assert.deepEqual(
      names(filterVoices(voices, { ...EMPTY_VOICE_FILTER, gender: 'female' })),
      ['yuxiaoyun_v3.1', 'zh_female_vv_uranus_bigtts']
    )
    assert.deepEqual(
      names(filterVoices(voices, { ...EMPTY_VOICE_FILTER, language: 'en' })),
      ['zh_female_vv_uranus_bigtts']
    )
    assert.deepEqual(
      names(
        filterVoices(voices, {
          ...EMPTY_VOICE_FILTER,
          category: '精品英文音色',
          gender: 'female',
        })
      ),
      []
    )
  })

  test('offers only options present in the data', () => {
    assert.deepEqual(voiceFilterOptions(voices), {
      categories: ['精品中文音色', '精品英文音色', '通用'],
      genders: ['female', 'male'],
      languages: ['en', 'zh-cn'],
    })
  })
})
