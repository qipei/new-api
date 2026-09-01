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
import type { PricingModel, VideoPriceTable } from '../types'
// CUSTOM: 按模型族维护的 API 参数说明（fork 扩展）。
//
// 为什么放在代码里而不是后台配置：这些取值范围与组合规则的真值来源是中转适配器
// 里的校验函数（relay/channel/task/**），改了适配器就必须同批改这里，放在同一个
// 提交里 review 才能发现不一致。放后台会漂移且无人察觉。
//
// 唯一的例外是分辨率与生成模式：它们由每个部署自己的价格矩阵决定（矩阵里没有的
// 档位会被 model_price_error 拒绝），因此运行时从 matrix_price_table 推导，
// 管理员在后台加一档，文档立刻跟着变，不需要发版。
//
// 展示口径：一个模型可能挂在多个渠道上，而调用方无法指定渠道，因此这里描述的是
// 所有线路的交集；仅部分线路支持的参数用 partialRoutes 标注，不点名渠道。
import type { SupportedParameter } from './mock-stats'

/** 参数在请求体里的位置。文档必须写清楚，同名参数放错层级会被静默忽略。 */
export type ParamLocation = 'body' | 'metadata.parameters' | 'metadata.input'

export type ModelApiParam = Omit<SupportedParameter, 'descriptionKey'> & {
  location: ParamLocation
  /** 参数含义与取值说明，作为完整句子直接用作 i18n key。 */
  descriptionKey: string
  /** 仅部分线路支持；未生效时会被上游忽略而不是报错。 */
  partialRoutes?: boolean
}

export type ModelApiSample = {
  /** 场景名，例如「文生视频」「首帧图生视频」。 */
  titleKey: string
  body: Record<string, unknown>
}

export type ModelApiSpec = {
  /** 族标识，用于测试与调试定位。 */
  family: string
  /** 命中该族的模型名（平台侧名称，大小写不敏感）。 */
  match: RegExp
  endpointPath: string
  /** 静态参数；分辨率等由价格矩阵推导后合并进来。 */
  params: ModelApiParam[]
  samples: ModelApiSample[]
  /**
   * 把价格矩阵推导出的档位合并进参数表。留给各族自己决定放在哪个参数上，
   * 因为有的族叫 resolution、有的族用 size。
   */
  applyMatrix?: (
    params: ModelApiParam[],
    table: VideoPriceTable
  ) => ModelApiParam[]
}

/** 从价格矩阵里取出该模型实际可用的分辨率档位。 */
export function resolutionsFromMatrix(table?: VideoPriceTable): string[] {
  const seen = new Set<string>()
  for (const tier of table?.tiers ?? []) {
    const value = tier.resolution?.trim()
    if (value) seen.add(value)
  }
  return [...seen]
}

/** 从价格矩阵里取出该模型实际可用的生成模式。 */
export function modesFromMatrix(table?: VideoPriceTable): string[] {
  const seen = new Set<string>()
  for (const tier of table?.tiers ?? []) {
    const value = tier.mode?.trim()
    if (value) seen.add(value)
  }
  return [...seen]
}

function withMatrixResolution(
  params: ModelApiParam[],
  table: VideoPriceTable,
  paramName: string
): ModelApiParam[] {
  const values = resolutionsFromMatrix(table)
  if (values.length === 0) return params
  return params.map((param) =>
    param.name === paramName ? { ...param, enumValues: values } : param
  )
}

// ---------------------------------------------------------------------------
// MiniMax 视频（百炼）
// 依据：relay/channel/task/ali/adaptor.go validateAliMiniMaxRequest
// ---------------------------------------------------------------------------

const MINIMAX_VIDEO: ModelApiSpec = {
  family: 'minimax-video',
  match: /^minimax-h\d/i,
  endpointPath: '/v1/video/generations',
  params: [
    {
      name: 'model',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey: 'Model name, e.g. MiniMax-H3.',
    },
    {
      name: 'prompt',
      location: 'body',
      type: 'string',
      required: true,
      range: '≤ 7000',
      descriptionKey:
        'Text prompt describing the video. Up to 7000 characters; longer input is rejected.',
    },
    {
      name: 'duration',
      location: 'body',
      type: 'integer',
      required: true,
      range: '4 ~ 15',
      defaultValue: 5,
      descriptionKey:
        'Video length in seconds. Billed per second, so a longer value costs proportionally more.',
    },
    {
      name: 'size',
      location: 'body',
      type: 'enum',
      descriptionKey:
        'Resolution tier. Accepts a tier name such as 768P or 2K, or a WxH size that is mapped to the nearest tier by its long edge.',
    },
    {
      name: 'metadata.parameters.ratio',
      location: 'metadata.parameters',
      type: 'enum',
      enumValues: ['16:9', '9:16', '1:1', '4:3', '3:4', '21:9', 'adaptive'],
      defaultValue: 'adaptive',
      descriptionKey:
        'Aspect ratio. Required for text-to-video and must not be adaptive there. For image-to-video the ratio follows the input image and this value is ignored.',
    },
    {
      name: 'image',
      location: 'body',
      type: 'string',
      descriptionKey:
        'First-frame image URL. Supplying it switches the request to image-to-video; a second image becomes the last frame.',
    },
    {
      name: 'metadata.parameters.watermark',
      location: 'metadata.parameters',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Adds an "AI generated" watermark to the bottom-right corner of the output.',
    },
  ],
  samples: [
    {
      titleKey: 'Text to video',
      body: {
        model: 'MiniMax-H3',
        prompt: '一只橘猫在洒满阳光的窗台上伸懒腰，镜头缓缓推近',
        size: '768P',
        duration: 5,
        metadata: { parameters: { ratio: '16:9' } },
      },
    },
    {
      titleKey: 'Image to video (first frame)',
      body: {
        model: 'MiniMax-H3',
        prompt: '让画面中的人物自然地转头微笑',
        image: 'https://example.com/first-frame.png',
        size: '768P',
        duration: 5,
      },
    },
  ],
  applyMatrix: (params, table) => withMatrixResolution(params, table, 'size'),
}

const SPECS: ModelApiSpec[] = [MINIMAX_VIDEO]

/** 返回命中的族定义；没有专属定义时返回 undefined，由调用方回落到通用逻辑。 */
export function findModelApiSpec(modelName: string): ModelApiSpec | undefined {
  const name = modelName.trim()
  if (!name) return undefined
  return SPECS.find((spec) => spec.match.test(name))
}

/** 解析出该模型的参数表；未命中专属定义时返回 undefined。 */
export function resolveModelApiParams(
  model: PricingModel
): ModelApiParam[] | undefined {
  const spec = findModelApiSpec(model.model_name)
  if (!spec) return undefined
  const table = model.matrix_price_table
  if (!table || !spec.applyMatrix) return spec.params
  return spec.applyMatrix(spec.params, table)
}

/** 暴露给测试与调试：当前登记了哪些族。 */
export const MODEL_API_SPECS = SPECS
