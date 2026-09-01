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

/**
 * 参数在请求体里的位置。文档必须写清楚，同名参数放错层级会被静默忽略。
 * 各适配器的嵌套层级不同：百炼把 metadata 反序列化进 parameters/input，
 * 火山则直接摊平在 metadata 下。
 */
export type ParamLocation =
  | 'body'
  | 'metadata'
  | 'metadata.parameters'
  | 'metadata.input'

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

// ---------------------------------------------------------------------------
// 可灵视频（百炼）
// 依据：relay/channel/task/ali/adaptor.go validateAliKlingRequest
// ---------------------------------------------------------------------------

const KLING_VIDEO: ModelApiSpec = {
  family: 'kling-video',
  // 同前缀下还有 Kling-3.0-image，那是图片模型、端点不同，必须排除。
  match: /^kling-3\.0-(video|omni)$/i,
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
      range: '≤ 2500',
      descriptionKey:
        'Text prompt describing the video. Up to 2500 characters.',
    },
    {
      name: 'duration',
      location: 'body',
      type: 'integer',
      required: true,
      range: '3 ~ 15',
      defaultValue: 5,
      descriptionKey:
        'Video length in seconds. Capped at 10 when a feature reference video is supplied.',
    },
    {
      name: 'size',
      location: 'body',
      type: 'enum',
      enumValues: ['std', 'pro', '4k', '720p', '1080p'],
      descriptionKey:
        'Quality tier. std is 720P, pro is 1080P, 4k is the 4K tier. Turbo models do not support 4k.',
    },
    {
      name: 'metadata.parameters.aspect_ratio',
      location: 'metadata.parameters',
      type: 'enum',
      enumValues: ['16:9', '9:16', '1:1'],
      descriptionKey:
        'Aspect ratio. Required for text-to-video and for reference-driven generation; ignored when a first frame decides the ratio.',
    },
    {
      name: 'metadata.parameters.audio',
      location: 'metadata.parameters',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Generates a soundtrack. Must stay false when a base or feature video is supplied.',
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
        'Also returns a watermarked copy of the video alongside the clean one.',
    },
  ],
  samples: [
    {
      titleKey: 'Text to video',
      body: {
        model: 'kling-3.0-video',
        prompt: '一只小猫在月光下奔跑，镜头缓慢跟随',
        size: 'pro',
        duration: 5,
        metadata: { parameters: { aspect_ratio: '16:9', audio: false } },
      },
    },
    {
      titleKey: 'Image to video (first frame)',
      body: {
        model: 'kling-3.0-video',
        prompt: '让图片中的人物动起来，头发被微风吹动',
        image: 'https://example.com/first-frame.png',
        size: 'std',
        duration: 5,
      },
    },
  ],
  applyMatrix: (params, table) => withMatrixResolution(params, table, 'size'),
}

// ---------------------------------------------------------------------------
// Vidu 参考生视频（百炼）
// 依据：relay/channel/task/ali/adaptor.go validateAliViduRequest
// ---------------------------------------------------------------------------

const VIDU_REFERENCE: ModelApiSpec = {
  family: 'vidu-reference',
  match: /^vidu-q\d-(ad|drama)$/i,
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
      range: '≤ 5000',
      descriptionKey:
        'Text prompt describing the video. Longer input is truncated rather than rejected.',
    },
    {
      name: 'images',
      location: 'body',
      type: 'array',
      required: true,
      range: '1 ~ 7',
      descriptionKey:
        'Reference image URLs whose subjects are composed into the generated scene. At least one is required.',
    },
    {
      name: 'duration',
      location: 'body',
      type: 'integer',
      required: true,
      descriptionKey:
        'Video length in seconds. The accepted range differs per model: 3-15 for the ad model and 2-15 for the drama model.',
    },
    {
      name: 'size',
      location: 'body',
      type: 'enum',
      descriptionKey:
        'Resolution tier. The drama model defaults to 1080P, the others to 720P.',
    },
    {
      name: 'metadata.parameters.seed',
      location: 'metadata.parameters',
      type: 'integer',
      range: '0 ~ 2147483647',
      descriptionKey:
        'Random seed. Fixing it improves reproducibility but does not guarantee identical output.',
    },
  ],
  samples: [
    {
      titleKey: 'Reference to video',
      body: {
        model: 'vidu-q3-ad',
        prompt: '男人坐在靠窗的椅子上弹吉他，暖色调，镜头缓慢推近',
        images: [
          'https://example.com/subject.png',
          'https://example.com/background.png',
        ],
        size: '720P',
        duration: 5,
      },
    },
  ],
  applyMatrix: (params, table) => withMatrixResolution(params, table, 'size'),
}

// ---------------------------------------------------------------------------
// Vidu 图生视频（百炼）
// 依据：relay/channel/task/ali/adaptor.go validateAliViduImg2VideoRequest
// ---------------------------------------------------------------------------

const VIDU_IMG2VIDEO: ModelApiSpec = {
  family: 'vidu-img2video',
  match: /^vidu-q\d-(pro|pro-fast|turbo)$/i,
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
      range: '≤ 5000',
      descriptionKey:
        'Text prompt describing the motion. Optional for image-to-video.',
    },
    {
      name: 'image',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'The single input image. Exactly one image is accepted; the output aspect ratio follows it.',
    },
    {
      name: 'duration',
      location: 'body',
      type: 'integer',
      defaultValue: 5,
      descriptionKey:
        'Video length in seconds. Up to 16 for the q3 family and up to 10 for the q2 family.',
    },
    {
      name: 'size',
      location: 'body',
      type: 'enum',
      descriptionKey:
        'Resolution tier. The pro-fast model has no 540P tier; the others accept 540P, 720P and 1080P.',
    },
    {
      name: 'metadata.parameters.audio',
      location: 'metadata.parameters',
      type: 'boolean',
      defaultValue: false,
      descriptionKey: 'Generates a soundtrack. Only the q3 family supports it.',
      partialRoutes: true,
    },
  ],
  samples: [
    {
      titleKey: 'Image to video (first frame)',
      body: {
        model: 'vidu-q3-pro',
        prompt: '镜头从海龟下方缓缓上移，海龟悠然游动',
        image: 'https://example.com/turtle.webp',
        size: '720P',
        duration: 5,
      },
    },
  ],
  applyMatrix: (params, table) => withMatrixResolution(params, table, 'size'),
}

// ---------------------------------------------------------------------------
// 对话通用参数
// 依据：relaykit/dto/openai_request.go GeneralOpenAIRequest 里真实会透传的字段。
// 这里只列各家上游普遍支持的部分；上游特有的字段用 extra_body 原样透传。
// ---------------------------------------------------------------------------

export const CHAT_PARAMS: ModelApiParam[] = [
  {
    name: 'model',
    location: 'body',
    type: 'string',
    required: true,
    descriptionKey: 'Model name, e.g. MiniMax-H3.',
  },
  {
    name: 'messages',
    location: 'body',
    type: 'array',
    required: true,
    descriptionKey:
      'Conversation history. Each item carries a role (system, user, assistant or tool) and its content.',
  },
  {
    name: 'stream',
    location: 'body',
    type: 'boolean',
    defaultValue: false,
    descriptionKey:
      'Streams the reply as server-sent events instead of returning it in one response.',
  },
  {
    name: 'max_tokens',
    location: 'body',
    type: 'integer',
    descriptionKey:
      'Upper bound on generated tokens. Leaving it empty lets the upstream decide; it never extends the model context window.',
  },
  {
    name: 'temperature',
    location: 'body',
    type: 'number',
    range: '0 ~ 2',
    descriptionKey:
      'Sampling randomness. Lower values are more deterministic; use either this or top_p rather than both.',
  },
  {
    name: 'top_p',
    location: 'body',
    type: 'number',
    range: '0 ~ 1',
    descriptionKey:
      'Nucleus sampling threshold. Only the most probable tokens summing to this value are considered.',
  },
  {
    name: 'n',
    location: 'body',
    type: 'integer',
    defaultValue: 1,
    descriptionKey:
      'Number of completions to generate. Every completion is billed, so the cost scales with this value.',
  },
  {
    name: 'stop',
    location: 'body',
    type: 'array',
    descriptionKey:
      'Up to four strings that stop generation as soon as one of them appears.',
  },
  {
    name: 'frequency_penalty',
    location: 'body',
    type: 'number',
    range: '-2 ~ 2',
    descriptionKey:
      'Positive values discourage repeating tokens that already appeared often.',
  },
  {
    name: 'presence_penalty',
    location: 'body',
    type: 'number',
    range: '-2 ~ 2',
    descriptionKey:
      'Positive values encourage the model to introduce new topics.',
  },
  {
    name: 'response_format',
    location: 'body',
    type: 'object',
    descriptionKey:
      'Forces a reply shape, for example JSON. Support varies by upstream and is ignored where unavailable.',
    partialRoutes: true,
  },
  {
    name: 'tools',
    location: 'body',
    type: 'array',
    descriptionKey:
      'Function definitions the model may call. Pair it with tool_choice to force or forbid a call.',
    partialRoutes: true,
  },
  {
    name: 'seed',
    location: 'body',
    type: 'integer',
    descriptionKey:
      'Random seed. Fixing it improves reproducibility but does not guarantee identical output.',
    partialRoutes: true,
  },
  {
    name: 'extra_body',
    location: 'body',
    type: 'object',
    descriptionKey:
      'Passed through to the upstream untouched. Use it for provider-specific fields that have no standard equivalent.',
  },
]

// ---------------------------------------------------------------------------
// 火山方舟 Seedance 视频
// 依据：relay/channel/task/doubao/constants.go validateModelParameters
// 各代能力差异很大，取值范围写的是本站已上线的 2.0 / 2.5 两代。
// ---------------------------------------------------------------------------

const SEEDANCE_VIDEO: ModelApiSpec = {
  family: 'seedance-video',
  match: /^doubao-seedance-/i,
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
      descriptionKey:
        'Text prompt describing the video. Billed per token, so a longer prompt costs slightly more.',
    },
    {
      name: 'duration',
      location: 'body',
      type: 'integer',
      defaultValue: 5,
      descriptionKey:
        'Video length in seconds. 4-15 for the 2.0 family and 4-30 for 2.5; pass -1 to let the model decide.',
    },
    {
      name: 'size',
      location: 'body',
      type: 'enum',
      descriptionKey:
        'Resolution tier. The fast and mini variants only accept 480p and 720p.',
    },
    {
      name: 'metadata.ratio',
      location: 'metadata',
      type: 'string',
      descriptionKey: 'Aspect ratio of the generated video, for example 16:9.',
    },
    {
      name: 'metadata.generate_audio',
      location: 'metadata',
      type: 'boolean',
      descriptionKey:
        'Generates a soundtrack together with the video. Not available on the 1.0 family.',
      partialRoutes: true,
    },
    {
      name: 'metadata.camera_fixed',
      location: 'metadata',
      type: 'boolean',
      descriptionKey:
        'Locks the camera so the scene is rendered without camera movement.',
    },
    {
      name: 'image',
      location: 'body',
      type: 'string',
      descriptionKey:
        'First-frame image URL. Supplying it switches the request to image-to-video; a second image becomes the last frame.',
    },
    {
      name: 'metadata.watermark',
      location: 'metadata',
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
        model: 'Doubao-Seedance-2.0',
        prompt: '航拍视角掠过雪山之巅，云海翻涌，晨光洒在山脊上',
        size: '720p',
        duration: 5,
        metadata: { ratio: '16:9', generate_audio: false },
      },
    },
    {
      titleKey: 'Image to video (first frame)',
      body: {
        model: 'Doubao-Seedance-2.0',
        prompt: '画面中的花朵缓缓绽放，光线由暗转亮',
        image: 'https://example.com/flower.png',
        size: '720p',
        duration: 5,
      },
    },
  ],
  applyMatrix: (params, table) => withMatrixResolution(params, table, 'size'),
}

// ---------------------------------------------------------------------------
// 图片生成（按张计费矩阵）
// 走同步图片端点，与视频任务的异步协议不同。
// ---------------------------------------------------------------------------

const IMAGE_GENERATION: ModelApiSpec = {
  family: 'image-generation',
  // vidu-q2 名字里没有 image，但它是按张计费的图片模型，必须显式列出。
  match:
    /^(qwen-image|z-image|wan[\d.]*-image|seedream|doubao-seedream|kling-3\.0-image|vidu-image|vidu-q2$)/i,
  endpointPath: '/v1/images/generations',
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
      descriptionKey: 'Text description of the image to generate.',
    },
    {
      name: 'size',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Output size as WxH, for example 1024x1024. It selects the billing tier, so the price follows the size you ask for.',
    },
    {
      name: 'n',
      location: 'body',
      type: 'integer',
      defaultValue: 1,
      descriptionKey:
        'Number of images to generate. Every image is billed, so the cost scales with this value.',
    },
    {
      name: 'image',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Reference image URL. Supplying it switches the request to image editing, which is priced separately from text-to-image.',
    },
  ],
  samples: [
    {
      titleKey: 'Text to image',
      body: {
        model: 'qwen-image-3.0',
        prompt: '一只戴着宇航头盔的柴犬，工作室灯光，超写实',
        size: '1024x1024',
        n: 1,
      },
    },
    {
      titleKey: 'Image editing',
      body: {
        model: 'qwen-image-3.0',
        prompt: '把背景换成星空',
        image: 'https://example.com/dog.png',
        size: '1024x1024',
      },
    },
  ],
}

// ---------------------------------------------------------------------------
// HappyHorse 视频（百炼）
// 依据：relay/channel/task/ali/adaptor.go validateAliHappyHorseRequest
// 四个模型分别对应文生、图生、参考生与视频编辑，输入媒体要求不同。
// ---------------------------------------------------------------------------

const HAPPYHORSE_VIDEO: ModelApiSpec = {
  family: 'happyhorse-video',
  match: /^happyhorse-/i,
  endpointPath: '/v1/video/generations',
  params: [
    {
      name: 'model',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'Model name. The suffix selects the task: t2v for text, i2v for a single image, r2v for reference images and video-edit for editing an existing clip.',
    },
    {
      name: 'prompt',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Text prompt describing the video. Required for every variant except image-to-video.',
    },
    {
      name: 'size',
      location: 'body',
      type: 'enum',
      descriptionKey:
        'Resolution tier. Defaults to 1080P; the video-edit model does not accept 480P.',
    },
    {
      name: 'metadata.parameters.ratio',
      location: 'metadata.parameters',
      type: 'string',
      descriptionKey:
        'Aspect ratio of the generated video. Not accepted by the video-edit model, which follows the input clip.',
      partialRoutes: true,
    },
    {
      name: 'metadata.parameters.audio_setting',
      location: 'metadata.parameters',
      type: 'enum',
      enumValues: ['auto', 'origin'],
      descriptionKey:
        'Only for the video-edit model: auto regenerates the soundtrack, origin keeps the sound of the input clip.',
      partialRoutes: true,
    },
    {
      name: 'metadata.parameters.seed',
      location: 'metadata.parameters',
      type: 'integer',
      range: '0 ~ 2147483647',
      descriptionKey:
        'Random seed. Fixing it improves reproducibility but does not guarantee identical output.',
    },
    {
      name: 'image',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Input image URL. The i2v model accepts exactly one; the r2v model takes reference images.',
    },
    {
      name: 'video',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Input video URL. Only the video-edit model uses it, and its duration decides the billed length.',
      partialRoutes: true,
    },
  ],
  samples: [
    {
      titleKey: 'Text to video',
      body: {
        model: 'happyhorse-1.1-t2v',
        prompt: '雨后的城市街道，霓虹倒映在积水里，镜头缓慢平移',
        size: '1080P',
        duration: 5,
        metadata: { parameters: { ratio: '16:9' } },
      },
    },
    {
      titleKey: 'Image to video (first frame)',
      body: {
        model: 'happyhorse-1.1-i2v',
        image: 'https://example.com/scene.png',
        size: '1080P',
        duration: 5,
      },
    },
  ],
  applyMatrix: (params, table) => withMatrixResolution(params, table, 'size'),
}

const SPECS: ModelApiSpec[] = [
  MINIMAX_VIDEO,
  KLING_VIDEO,
  VIDU_REFERENCE,
  VIDU_IMG2VIDEO,
  SEEDANCE_VIDEO,
  HAPPYHORSE_VIDEO,
  IMAGE_GENERATION,
]

/** 返回命中的族定义；没有专属定义时返回 undefined，由调用方回落到通用逻辑。 */
export function findModelApiSpec(modelName: string): ModelApiSpec | undefined {
  const name = modelName.trim()
  if (!name) return undefined
  return SPECS.find((spec) => spec.match.test(name))
}

/**
 * 解析出该模型的参数表。命中专属族时用族定义；否则对话类模型回落到真实的
 * 通用表。返回 undefined 表示这类模型（图片、向量等）暂无专属定义，
 * 由调用方回落到上游原有逻辑。
 */
export function resolveModelApiParams(
  model: PricingModel
): ModelApiParam[] | undefined {
  const spec = findModelApiSpec(model.model_name)
  if (!spec) {
    return isChatModel(model) ? CHAT_PARAMS : undefined
  }
  const table = model.matrix_price_table
  if (!table || !spec.applyMatrix) return spec.params
  return spec.applyMatrix(spec.params, table)
}

/** 只有走对话补全端点的模型才适用通用对话参数表。 */
function isChatModel(model: PricingModel): boolean {
  const types = model.supported_endpoint_types ?? []
  return types.includes('openai') || types.includes('openai-response')
}

/** 暴露给测试与调试：当前登记了哪些族。 */
export const MODEL_API_SPECS = SPECS
