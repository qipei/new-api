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

import type { TFunction } from 'i18next'
import { describe, test } from 'vitest'

import { ENDPOINT_TYPES, getEndpointTypeLabels } from '../../constants'
import type { PricingModel } from '../../types'
import { filterByEndpointType } from '../filters'

function model(name: string, endpoints: string[]): PricingModel {
  return {
    model_name: name,
    supported_endpoint_types: endpoints,
  } as unknown as PricingModel
}

describe('speech endpoint filter', () => {
  // 语音模型由后端标成 audio-speech，筛选栏要能单独选出它们，且不混进对话。
  test('offers a Speech option and filters speech models', () => {
    const labels = getEndpointTypeLabels(
      ((key: string) => key) as unknown as TFunction
    )
    assert.equal(labels[ENDPOINT_TYPES.AUDIO_SPEECH], 'Speech')

    const models = [
      model('seed-tts-2.0', ['audio-speech']),
      model('qwen-audio-3.1-tts-plus', ['audio-speech']),
      model('gpt-4o', ['openai']),
    ]
    assert.deepEqual(
      filterByEndpointType(models, ENDPOINT_TYPES.AUDIO_SPEECH).map(
        (m) => m.model_name
      ),
      ['seed-tts-2.0', 'qwen-audio-3.1-tts-plus']
    )
    assert.deepEqual(
      filterByEndpointType(models, ENDPOINT_TYPES.OPENAI).map(
        (m) => m.model_name
      ),
      ['gpt-4o']
    )
  })
})
