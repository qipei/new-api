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

/**
 * 示例的响应处理方式，决定示例代码在请求之后做什么：
 * json 打印响应；audio-binary 把响应体存成音频文件；audio-base64 解码响应里的
 * audio 字段再存文件；audio-url 下载响应里 output.audio.url 指向的音频；
 * audio-hex 把响应里 output.data.audio 的 hex 编码解码后存文件；
 * task 提交后轮询查询接口直到任务结束。
 */
export type SampleResponseKind =
  | 'json'
  | 'audio-binary'
  | 'audio-base64'
  | 'audio-url'
  | 'audio-hex'
  | 'task'

export type ModelApiSample = {
  /** 场景名，例如「文生视频」「首帧图生视频」。 */
  titleKey: string
  body: Record<string, unknown>
  /** 该场景走的端点与族默认端点不同时填写（例如 OpenAI 兼容入口）。 */
  endpointPath?: string
  response?: SampleResponseKind
  /** 音频示例保存的文件名，扩展名应与请求的格式一致。 */
  outputFile?: string
  /**
   * 该场景独有字段的说明（不在族参数表里，例如兼容入口的 input/voice），
   * 只用于示例里的行内注释。
   */
  sampleParams?: ModelApiParam[]
  /** 示例下方的补充说明。 */
  noteKey?: string
}

export type ModelApiSpec = {
  /** 族标识，用于测试与调试定位。 */
  family: string
  /** 命中该族的模型名（平台侧名称，大小写不敏感）。 */
  match: RegExp
  /**
   * 只用于全部由 ComfyUI 工作流渠道（autodl）或沿用其请求写法的 WeNex 渠道提供的模型
   * （pricing 的 workflow_api）。
   * 工作流的参数与同名官方模型不同，按渠道区分后对外模型名可以随意起，
   * 例如 autodl 的 H3 也可以叫 MiniMax-H3-Turbo。
   */
  workflowOnly?: boolean
  endpointPath: string
  /** 族默认的响应处理方式，场景可以覆盖；缺省为 json。 */
  response?: SampleResponseKind
  /** 异步任务的查询端点前缀，task_id 拼在其后。 */
  fetchPath?: string
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
// 按用途区分的素材字段（首帧/尾帧/参考图/参考视频/参考音频），各视频模型族共用。
// 依据：relay/common/relay_info.go TaskSubmitReq.RoleMedia 与各渠道适配器的组装。
// 与 image/images/video/audio 冗余并存：图片两种写法二选一，参考视频/音频两种写法合并；
// 字段写在顶层或 metadata 里都认。支持首帧的族同时列出 OpenAI 视频接口的 input_reference。
// ---------------------------------------------------------------------------

type MediaRoleSupport = {
  firstFrame?: string
  lastFrame?: string
  referenceImages?: string
  referenceVideos?: string
  referenceAudios?: string
  /** 各用途的数量上限，例如 '≤ 9'。 */
  ranges?: Partial<
    Record<'referenceImages' | 'referenceVideos' | 'referenceAudios', string>
  >
}

/** 该族支持的用途才生成参数；值是参数说明（完整句子，同时作为 i18n key）。 */
function mediaRoleParams(support: MediaRoleSupport): ModelApiParam[] {
  const params: ModelApiParam[] = []
  if (support.firstFrame) {
    params.push({
      name: 'first_frame_url',
      location: 'body',
      type: 'string',
      descriptionKey: support.firstFrame,
    })
    params.push({
      name: 'input_reference',
      location: 'body',
      type: 'string',
      descriptionKey: MEDIA_INPUT_REFERENCE,
    })
  }
  if (support.lastFrame) {
    params.push({
      name: 'last_frame_url',
      location: 'body',
      type: 'string',
      descriptionKey: support.lastFrame,
    })
  }
  if (support.referenceImages) {
    params.push({
      name: 'reference_images',
      location: 'body',
      type: 'array',
      range: support.ranges?.referenceImages,
      descriptionKey: support.referenceImages,
    })
  }
  if (support.referenceVideos) {
    params.push({
      name: 'video_urls',
      location: 'body',
      type: 'array',
      range: support.ranges?.referenceVideos,
      descriptionKey: support.referenceVideos,
    })
  }
  if (support.referenceAudios) {
    params.push({
      name: 'audio_urls',
      location: 'body',
      type: 'array',
      range: support.ranges?.referenceAudios,
      descriptionKey: support.referenceAudios,
    })
  }
  return params
}

const MEDIA_FIRST_FRAME =
  'First-frame image URL. Can also be written as metadata.first_frame_url. Prefer it over images when the image must be the first frame.'
const MEDIA_LAST_FRAME =
  'Last-frame image URL, usually paired with first_frame_url. Can also be written as metadata.last_frame_url.'
const MEDIA_REFERENCE_IMAGES =
  'Reference image URLs for reference-to-video. Can also be written as metadata.reference_images.'
const MEDIA_REFERENCE_VIDEOS =
  'Reference video URLs. Pass a single video as video_url instead if you like; both can also be written in metadata.'
const MEDIA_REFERENCE_AUDIOS =
  'Reference audio URLs. Pass a single clip as audio_url instead if you like; both can also be written in metadata.'
const MEDIA_INPUT_REFERENCE =
  'OpenAI-compatible first-frame image: a URL string or an object such as {"image_url": "https://..."}. Works like image; do not combine it with first_frame_url or reference_images.'
const MEDIA_STYLE_NOTE =
  'For images, use either image, images and input_reference, or first_frame_url, last_frame_url and reference_images, not both. Reference videos and audio can be passed as video and audio or as video_urls and audio_urls; both are sent. Each field can be written at the top level or in metadata.'
const NATIVE_CONTENT =
  'Official content array with an explicit role on each item (text, image_url, video_url, audio_url), sent upstream as is. A text item can replace prompt. Do not combine it with the media fields above.'
const VOLC_OFFICIAL_NOTE =
  'Request bodies written for the Volcengine Ark video API work here as is: content and the official top-level parameters such as generate_audio, watermark, return_last_frame, draft, tools and callback_url take effect. The same parameter under metadata takes precedence.'
const TOP_LEVEL_RATIO =
  'Aspect ratio of the generated video, for example 16:9. Same as metadata.ratio.'
const MEDIA_FRAMES_OR_REFERENCES_NOTE =
  'First and last frames cannot be combined with reference images, videos or audio in the same request.'

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
      descriptionKey:
        'Model name, e.g. MiniMax-H3. MiniMax-H3-Highspeed, MiniMax-H3-Lite and MiniMax-H3-Max are available on the MiniMax official route.',
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
        'Resolution tier: 768P or 2K for MiniMax-H3, 768P for Highspeed, 480P or 768P for Lite and Max. A WxH size is mapped to the nearest tier by its long edge.',
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
      name: 'metadata.prompt_expansion_mode',
      location: 'metadata',
      type: 'enum',
      enumValues: ['disabled', 'balanced', 'quality'],
      defaultValue: 'balanced',
      partialRoutes: true,
      descriptionKey:
        'Prompt expansion mode, MiniMax-H3-Max only: disabled, balanced or quality.',
    },
    {
      name: 'metadata.parameters.watermark',
      location: 'metadata.parameters',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Adds an "AI generated" watermark to the bottom-right corner of the output.',
    },
    {
      name: 'resolution',
      location: 'body',
      type: 'string',
      partialRoutes: true,
      descriptionKey: 'Resolution tier, same as size.',
    },
    {
      name: 'ratio',
      location: 'body',
      type: 'string',
      partialRoutes: true,
      descriptionKey: TOP_LEVEL_RATIO,
    },
    {
      name: 'content',
      location: 'body',
      type: 'array',
      partialRoutes: true,
      descriptionKey: NATIVE_CONTENT,
    },
    ...mediaRoleParams({
      firstFrame: MEDIA_FIRST_FRAME,
      lastFrame: MEDIA_LAST_FRAME,
      referenceImages: MEDIA_REFERENCE_IMAGES,
      referenceVideos: MEDIA_REFERENCE_VIDEOS,
      referenceAudios: MEDIA_REFERENCE_AUDIOS,
      ranges: {
        referenceImages: '≤ 9',
        referenceVideos: '≤ 3',
        referenceAudios: '≤ 3',
      },
    }),
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
    {
      titleKey: 'Image to video (first and last frame)',
      body: {
        model: 'MiniMax-H3',
        prompt: '镜头从清晨的街道平滑过渡到夜晚的灯火',
        first_frame_url: 'https://example.com/morning.png',
        last_frame_url: 'https://example.com/night.png',
        size: '768P',
        duration: 5,
      },
      noteKey: MEDIA_STYLE_NOTE,
    },
    {
      titleKey: 'Reference to video',
      body: {
        model: 'MiniMax-H3',
        prompt: '角色说话：Follow the wind, live free，音色参考音频1',
        reference_images: ['https://example.com/role.png'],
        video_urls: ['https://example.com/motion.mp4'],
        audio_urls: ['https://example.com/voice.mp3'],
        size: '768P',
        duration: 5,
        metadata: { parameters: { ratio: '16:9' } },
      },
      noteKey: MEDIA_FRAMES_OR_REFERENCES_NOTE,
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
    ...mediaRoleParams({
      firstFrame: MEDIA_FIRST_FRAME,
      lastFrame: MEDIA_LAST_FRAME,
      referenceImages:
        'Reference image URLs. Only kling-3.0-Omni supports them. Can also be written as metadata.reference_images.',
      referenceVideos:
        'Reference video URL. Only kling-3.0-Omni supports it, and not together with last_frame_url. Pass one URL in video_urls or video_url.',
      ranges: { referenceImages: '≤ 7', referenceVideos: '≤ 1' },
    }),
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
    {
      titleKey: 'Image to video (first and last frame)',
      body: {
        model: 'kling-3.0-video',
        prompt: '花朵从含苞到绽放',
        first_frame_url: 'https://example.com/bud.png',
        last_frame_url: 'https://example.com/bloom.png',
        duration: 5,
      },
      noteKey: MEDIA_STYLE_NOTE,
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
    ...mediaRoleParams({
      referenceImages:
        'Reference image URLs whose subjects are composed into the generated scene. Equivalent to images; can also be written as metadata.reference_images.',
      ranges: { referenceImages: '1 ~ 7' },
    }),
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
    {
      titleKey: 'Reference to video',
      body: {
        model: 'vidu-q3-ad',
        prompt: '模特手持产品在阳光下展示',
        reference_images: [
          'https://example.com/model.png',
          'https://example.com/product.png',
        ],
        duration: 5,
      },
      noteKey: MEDIA_STYLE_NOTE,
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
    ...mediaRoleParams({
      firstFrame:
        'The single input image, used as the first frame. Equivalent to image; can also be written as metadata.first_frame_url.',
    }),
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
    {
      titleKey: 'Image to video (first frame)',
      body: {
        model: 'vidu-q3-pro',
        prompt: '小海龟慢慢爬向大海',
        first_frame_url: 'https://example.com/turtle.webp',
        duration: 5,
      },
      noteKey: MEDIA_STYLE_NOTE,
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
      name: 'resolution',
      location: 'body',
      type: 'string',
      descriptionKey: 'Resolution tier, same as size.',
    },
    {
      name: 'ratio',
      location: 'body',
      type: 'string',
      descriptionKey: TOP_LEVEL_RATIO,
    },
    {
      name: 'content',
      location: 'body',
      type: 'array',
      partialRoutes: true,
      descriptionKey: NATIVE_CONTENT,
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
    ...mediaRoleParams({
      firstFrame: MEDIA_FIRST_FRAME,
      lastFrame: MEDIA_LAST_FRAME,
      referenceImages: MEDIA_REFERENCE_IMAGES,
      referenceVideos: MEDIA_REFERENCE_VIDEOS,
      referenceAudios: MEDIA_REFERENCE_AUDIOS,
      ranges: {
        referenceImages: '2.0 ≤ 9 / 2.5 ≤ 30',
        referenceVideos: '2.0 ≤ 3 / 2.5 ≤ 10',
        referenceAudios: '2.0 ≤ 3 / 2.5 ≤ 10',
      },
    }),
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
    {
      titleKey: 'Reference to video',
      body: {
        model: 'Doubao-Seedance-2.0',
        prompt: '参考图中的角色在参考视频的场景里跳舞，配上参考音频的音乐',
        reference_images: ['https://example.com/role.png'],
        video_urls: ['https://example.com/scene.mp4'],
        audio_urls: ['https://example.com/music.mp3'],
        duration: 5,
        metadata: { ratio: '16:9' },
      },
      noteKey: MEDIA_FRAMES_OR_REFERENCES_NOTE,
    },
    {
      titleKey: 'Volcengine Ark official format',
      body: {
        model: 'Doubao-Seedance-2.5',
        content: [
          { type: 'text', text: '镜头从首帧缓缓推进，人物转身微笑' },
          {
            type: 'image_url',
            image_url: { url: 'https://example.com/first-frame.png' },
            role: 'first_frame',
          },
        ],
        resolution: '720p',
        ratio: 'adaptive',
        duration: 5,
        generate_audio: true,
        watermark: false,
        return_last_frame: true,
      },
      noteKey: VOLC_OFFICIAL_NOTE,
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
    ...mediaRoleParams({
      firstFrame:
        'First-frame image URL. Only the i2v model uses it. Equivalent to image; can also be written as metadata.first_frame_url.',
      referenceImages:
        'Reference image URLs, used by the r2v and video-edit models. Can also be written as metadata.reference_images.',
      referenceVideos:
        'The video to edit. Only the video-edit model uses it; pass one URL in video_urls or video_url.',
      ranges: { referenceVideos: '1' },
    }),
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
    {
      titleKey: 'Reference to video',
      body: {
        model: 'happyhorse-1.1-r2v',
        prompt: '参考图中的小狗在草地上追蝴蝶',
        reference_images: ['https://example.com/dog.png'],
        duration: 5,
      },
      noteKey: MEDIA_STYLE_NOTE,
    },
  ],
  applyMatrix: (params, table) => withMatrixResolution(params, table, 'size'),
}

// ---------------------------------------------------------------------------
// 万相3.0 视频（百炼）
// 依据：relay/channel/task/ali/adaptor.go normalizeWan3Input / validateWan3Request
//
// 一个模型覆盖全部用法，用哪种取决于传了什么素材：带参考视频/音频、或图超过两张
// 时整体按参考模式，否则按首帧/首尾帧。首尾帧与参考类互斥，这是上游的硬规则。
// ---------------------------------------------------------------------------

const WAN3_VIDEO: ModelApiSpec = {
  family: 'wan3-video',
  match: /^wan3(\.0)?-video/i,
  endpointPath: '/v1/video/generations',
  params: [
    {
      name: 'model',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'Model name. The prime variant is the fast version and is billed at 1.5x the standard rate.',
    },
    {
      name: 'prompt',
      location: 'body',
      type: 'string',
      range: '≤ 20000',
      descriptionKey:
        'Text prompt describing the video. Either prompt or an input medium is required. In reference mode you can point at inputs by index, where images, videos and audio are numbered separately in the order you send them.',
    },
    {
      name: 'size',
      location: 'body',
      type: 'enum',
      descriptionKey:
        'Resolution tier. Accepts a tier name or WIDTH*HEIGHT, which is mapped to the nearest tier by the longer edge. Defaults to 1080P.',
    },
    {
      name: 'duration',
      location: 'body',
      type: 'integer',
      range: '2 ~ 30, or -1',
      descriptionKey:
        'Video length in seconds, 5 by default. Pass -1 for smart duration, which is the recommended setting when editing or extending a clip. With a video input, input plus output must stay within 30 seconds.',
    },
    {
      name: 'image',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Input image URL. Sent as the first frame when it is the only image and no video or audio is present.',
    },
    {
      name: 'images',
      location: 'body',
      type: 'array',
      range: '≤ 10',
      descriptionKey:
        'Input image URLs. Two images become the first and last frame; three or more, or any image sent together with a video or audio, become reference images instead.',
    },
    {
      name: 'video',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Reference video URL, up to 15 seconds. Sending one switches the request to reference mode, which also covers editing and extending a clip through the prompt.',
    },
    {
      name: 'audios',
      location: 'body',
      type: 'array',
      range: '≤ 5',
      descriptionKey:
        'Reference audio URLs, up to 15 seconds in total. Sending any switches the request to reference mode.',
    },
    {
      name: 'metadata.parameters.ratio',
      location: 'metadata.parameters',
      type: 'enum',
      enumValues: ['adaptive', '16:9', '4:3', '1:1', '3:4', '9:16'],
      descriptionKey:
        'Aspect ratio of the generated video, adaptive by default. Keep it adaptive when editing or extending a clip so the original framing is preserved.',
    },
    {
      name: 'metadata.parameters.audio',
      location: 'metadata.parameters',
      type: 'boolean',
      descriptionKey:
        'Whether the output carries a soundtrack, on by default. Both settings cost the same. This is not the same field as the top-level audio input.',
    },
    {
      name: 'metadata.parameters.prompt_extend',
      location: 'metadata.parameters',
      type: 'boolean',
      descriptionKey:
        'Whether to rewrite the prompt with a language model, on by default. It noticeably helps short prompts at the cost of extra latency.',
    },
    {
      name: 'metadata.parameters.watermark',
      location: 'metadata.parameters',
      type: 'boolean',
      descriptionKey:
        'Whether to stamp a watermark on the output, off by default.',
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
      name: 'metadata.input.media',
      location: 'metadata.input',
      type: 'array',
      range: '≤ 20',
      descriptionKey:
        'Raw media list, each entry carrying a type and a url. Use it when the plain fields cannot express what you need, such as documents and web pages. Types are first_frame and last_frame, or reference_image, reference_video, reference_audio, file and link. The two sets cannot be mixed, and file and link are mutually exclusive.',
    },
    ...mediaRoleParams({
      firstFrame: MEDIA_FIRST_FRAME,
      lastFrame: MEDIA_LAST_FRAME,
      referenceImages: MEDIA_REFERENCE_IMAGES,
      referenceVideos: MEDIA_REFERENCE_VIDEOS,
      referenceAudios: MEDIA_REFERENCE_AUDIOS,
      ranges: {
        referenceImages: '≤ 10',
        referenceVideos: '≤ 5',
        referenceAudios: '≤ 5',
      },
    }),
  ],
  samples: [
    {
      titleKey: 'Text to video',
      body: {
        model: 'wan3.0-video',
        prompt:
          '一只小猫在月光下的屋顶上奔跑，城市的霓虹灯在远处闪烁，电影级画质',
        size: '1080P',
        duration: 5,
      },
    },
    {
      titleKey: 'Image to video (first and last frame)',
      body: {
        model: 'wan3.0-video',
        prompt: '南瓜裂开，金光中出现一只小兔子',
        images: [
          'https://example.com/first.png',
          'https://example.com/last.png',
        ],
        size: '720P',
        duration: 5,
      },
    },
    {
      titleKey: 'Reference to video',
      body: {
        model: 'wan3.0-video',
        prompt: '视频1里的人抱着图1，在图2的椅子上弹奏一支舒缓的民谣',
        images: [
          'https://example.com/subject.png',
          'https://example.com/chair.png',
        ],
        video: 'https://example.com/role.mp4',
        size: '720P',
        duration: 5,
      },
    },
    {
      titleKey: 'Edit or extend a clip',
      body: {
        model: 'wan3.0-video-prime',
        prompt: '将视频1向后延长，面包师端上刷好的面包，镜头跟随他走向烤炉',
        video: 'https://example.com/source.mp4',
        size: '720P',
        duration: -1,
        metadata: { parameters: { ratio: 'adaptive' } },
      },
    },
    {
      titleKey: 'Image to video (first and last frame)',
      body: {
        model: 'wan3.0-video',
        prompt: '镜头从清晨的街道平滑过渡到夜晚的灯火',
        first_frame_url: 'https://example.com/morning.png',
        last_frame_url: 'https://example.com/night.png',
        duration: 5,
      },
      noteKey: MEDIA_STYLE_NOTE,
    },
  ],
  applyMatrix: (params, table) => withMatrixResolution(params, table, 'size'),
}

// ---------------------------------------------------------------------------
// 豆包音频生成 seed-audio（同步，按模型原始输出时长计费）
// 依据：relay/channel/volcengine/seed_audio.go、relay/helper/valid_request.go
// 原生端点原样透传全部字段；网关只校验 text_prompt 长度与参考条数，
// 参考资源的组合规则由上游校验。
// ---------------------------------------------------------------------------

const SEED_AUDIO_COMPAT_PARAMS: ModelApiParam[] = [
  {
    name: 'input',
    location: 'body',
    type: 'string',
    required: true,
    range: '≤ 3000',
    descriptionKey:
      'Text to speak, sent upstream as text_prompt. Up to 3000 characters.',
  },
  {
    name: 'voice',
    location: 'body',
    type: 'string',
    descriptionKey: 'Voice ID, sent upstream as references[0].speaker.',
  },
  {
    name: 'response_format',
    location: 'body',
    type: 'enum',
    defaultValue: 'mp3',
    enumValues: ['mp3', 'wav', 'pcm', 'opus'],
    descriptionKey:
      'Audio format of the response body. opus is returned as Ogg Opus; aac and flac are rejected.',
  },
  {
    name: 'speed',
    location: 'body',
    type: 'number',
    defaultValue: 1,
    range: '0.5 – 2',
    descriptionKey:
      'Playback speed, converted to speech_rate. Values outside 0.5-2 are clamped. Billing uses the original duration, so speeding up does not lower the cost.',
  },
  {
    name: 'metadata',
    location: 'metadata',
    type: 'object',
    descriptionKey:
      'Native seed-audio fields such as references, audio_config and watermark. They override the converted values; audio_config is merged field by field.',
  },
]

const SEED_AUDIO: ModelApiSpec = {
  family: 'seed-audio',
  match: /^seed-audio/i,
  endpointPath: '/v1/audio/generations',
  response: 'audio-base64',
  params: [
    {
      name: 'model',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey: 'Model name, e.g. seed-audio-1.0.',
    },
    {
      name: 'text_prompt',
      location: 'body',
      type: 'string',
      required: true,
      range: '≤ 3000',
      descriptionKey:
        'Text to speak or a natural-language description of the audio. You can describe the voice, emotion, pace, total length and when the speaker talks. Up to 3000 characters.',
    },
    {
      name: 'references',
      location: 'body',
      type: 'array',
      range: '≤ 3',
      descriptionKey:
        'Reference resources, up to 3 entries: voice IDs or audio clips, or a single image. Image and audio references cannot be mixed, and each entry sets exactly one field.',
    },
    {
      name: 'references[].speaker',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Voice ID from the Doubao voice library. Use it to speak with a preset voice.',
    },
    {
      name: 'references[].audio_url',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Reference audio URL used to imitate a voice. Up to 30 seconds and 10 MB, in wav, mp3, pcm or ogg_opus.',
    },
    {
      name: 'references[].audio_data',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Reference audio as Base64, with the same limits as audio_url.',
    },
    {
      name: 'references[].image_url',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Reference image URL; the audio is generated to match the picture. Up to 10 MB, in jpeg, png or webp.',
    },
    {
      name: 'references[].image_data',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Reference image as Base64, with the same limits as image_url.',
    },
    {
      name: 'audio_config.format',
      location: 'body',
      type: 'enum',
      defaultValue: 'wav',
      enumValues: ['wav', 'mp3', 'pcm', 'ogg_opus'],
      descriptionKey: 'Output audio format.',
    },
    {
      name: 'audio_config.sample_rate',
      location: 'body',
      type: 'integer',
      enumValues: [
        '8000',
        '16000',
        '24000',
        '32000',
        '40000',
        '44100',
        '48000',
      ],
      descriptionKey:
        'Sample rate in Hz. wav and pcm default to 40000, mp3 defaults to 44100, and ogg_opus only supports 48000.',
    },
    {
      name: 'audio_config.speech_rate',
      location: 'body',
      type: 'integer',
      defaultValue: 0,
      range: '-50 – 100',
      descriptionKey:
        'Speaking speed: -50 is 0.5x and 100 is 2x. Billing uses the original duration, so speeding up does not lower the cost.',
    },
    {
      name: 'audio_config.loudness_rate',
      location: 'body',
      type: 'integer',
      defaultValue: 0,
      range: '-50 – 100',
      descriptionKey: 'Volume: -50 is half volume and 100 is double.',
    },
    {
      name: 'audio_config.pitch_rate',
      location: 'body',
      type: 'integer',
      defaultValue: 0,
      range: '-12 – 12',
      descriptionKey: 'Pitch shift in semitones.',
    },
    {
      name: 'audio_config.enable_subtitle',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Returns subtitle with sentence and word timestamps in milliseconds.',
    },
    {
      name: 'watermark.aigc_watermark',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Explicit watermark: appends an audible AIGC marker at the end of the audio.',
    },
    {
      name: 'watermark.aigc_metadata.enable',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Implicit watermark: writes AIGC metadata into the audio file header.',
    },
    {
      name: 'content_producer',
      location: 'body',
      type: 'string',
      descriptionKey:
        'AIGC labelling: name or code of the content producer. Used with produce_id, content_propagator and propagate_id.',
    },
    {
      name: 'produce_id',
      location: 'body',
      type: 'string',
      descriptionKey: 'AIGC labelling: content production ID.',
    },
    {
      name: 'content_propagator',
      location: 'body',
      type: 'string',
      descriptionKey:
        'AIGC labelling: name or code of the content distributor.',
    },
    {
      name: 'propagate_id',
      location: 'body',
      type: 'string',
      descriptionKey: 'AIGC labelling: content distribution ID.',
    },
  ],
  samples: [
    {
      titleKey: 'Speak with a preset voice',
      body: {
        model: 'seed-audio-1.0',
        text_prompt: '欢迎来到我们的平台，祝你使用愉快。',
        references: [{ speaker: 'zh_female_vv_uranus_bigtts' }],
        audio_config: { format: 'mp3', enable_subtitle: true },
      },
      outputFile: 'speech.mp3',
      noteKey:
        'The response carries the audio as Base64 in audio, a download url valid for 2 hours, duration and original_duration in seconds, and subtitle when enabled. Billing uses original_duration, up to 120 seconds per request.',
    },
    {
      titleKey: 'Describe the delivery in the prompt',
      body: {
        model: 'seed-audio-1.0',
        text_prompt:
          '用温柔、略带笑意的年轻女声，语速稍慢地说：今天辛苦了，早点休息吧。',
        audio_config: { format: 'wav', sample_rate: 24000 },
      },
      outputFile: 'speech.wav',
    },
    {
      titleKey: 'Imitate a reference voice',
      body: {
        model: 'seed-audio-1.0',
        text_prompt: '这是一段用参考音色朗读的示例。',
        references: [{ audio_url: 'https://example.com/reference.wav' }],
        audio_config: { format: 'mp3', speech_rate: 10 },
      },
      outputFile: 'speech.mp3',
    },
    {
      titleKey: 'Generate audio for an image',
      body: {
        model: 'seed-audio-1.0',
        text_prompt: '为这张图配一段旁白，描述画面中的场景。',
        references: [{ image_url: 'https://example.com/scene.png' }],
        audio_config: { format: 'mp3' },
        watermark: { aigc_watermark: true },
      },
      outputFile: 'speech.mp3',
    },
    {
      titleKey: 'OpenAI-compatible speech',
      endpointPath: '/v1/audio/speech',
      response: 'audio-binary',
      body: {
        model: 'seed-audio-1.0',
        input: '你好，这是兼容 OpenAI 格式的语音合成。',
        voice: 'zh_female_vv_uranus_bigtts',
        response_format: 'mp3',
        speed: 1.0,
      },
      outputFile: 'speech.mp3',
      sampleParams: SEED_AUDIO_COMPAT_PARAMS,
      noteKey:
        'The OpenAI-compatible endpoint returns the audio bytes directly, without subtitles or duration. Use /v1/audio/generations when you need them.',
    },
  ],
}

// ---------------------------------------------------------------------------
// 豆包异步长文本语音合成 seed-tts-2.0 / seed-icl-2.0（按字符数计费）
// 依据：relay/channel/task/doubaotts/adaptor.go
// req_params 原样透传；additions 与 post_process 可以传对象，由网关转成上游
// 要求的 JSON 字符串；audio_params.format 缺省补 mp3。
// ---------------------------------------------------------------------------

const SEED_TTS_TASK: ModelApiSpec = {
  family: 'seed-tts-task',
  match: /^seed-(tts|icl)-/i,
  endpointPath: '/v1/audio/speech/tasks',
  fetchPath: '/v1/audio/speech/tasks',
  response: 'task',
  params: [
    {
      name: 'model',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'seed-tts-2.0 synthesizes with the Doubao 2.0 voices; seed-icl-2.0 uses voices you cloned.',
    },
    {
      name: 'user.uid',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Your end-user identifier, passed upstream. Defaults to new-api when omitted.',
    },
    {
      name: 'req_params.text',
      location: 'body',
      type: 'string',
      required: true,
      range: '≤ 100000',
      descriptionKey:
        'Text to synthesize, up to 100,000 characters. Billed per character: Chinese characters, letters, punctuation, spaces and line breaks each count as one.',
    },
    {
      name: 'req_params.speaker',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'Voice ID from the voice library, or the ID of a voice you cloned.',
    },
    {
      name: 'req_params.model',
      location: 'body',
      type: 'string',
      defaultValue: 'seed-tts-2.0-standard',
      descriptionKey:
        'Model version. Only needed when speaker is a cloned voice.',
    },
    {
      name: 'req_params.ssml',
      location: 'body',
      type: 'string',
      descriptionKey:
        'SSML markup to synthesize. Only Chinese and English voices support it, and additions.disable_markdown_filter must stay false. Tags are billed as characters.',
    },
    {
      name: 'req_params.audio_params.format',
      location: 'body',
      type: 'enum',
      defaultValue: 'mp3',
      enumValues: ['mp3', 'pcm', 'ogg_opus'],
      descriptionKey: 'Output audio format.',
    },
    {
      name: 'req_params.audio_params.sample_rate',
      location: 'body',
      type: 'integer',
      defaultValue: 24000,
      enumValues: [
        '8000',
        '16000',
        '22050',
        '24000',
        '32000',
        '44100',
        '48000',
      ],
      descriptionKey: 'Sample rate in Hz. ogg_opus only supports 48000.',
    },
    {
      name: 'req_params.audio_params.bit_rate',
      location: 'body',
      type: 'integer',
      enumValues: ['64000', '160000'],
      descriptionKey:
        'Bit rate in bps for mp3 and ogg_opus; mp3 defaults to 64000. Not supported for pcm.',
    },
    {
      name: 'req_params.audio_params.speech_rate',
      location: 'body',
      type: 'integer',
      defaultValue: 0,
      range: '-50 – 100',
      descriptionKey: 'Speaking speed: -50 is 0.5x and 100 is 2x.',
    },
    {
      name: 'req_params.audio_params.loudness_rate',
      location: 'body',
      type: 'integer',
      defaultValue: 0,
      range: '-50 – 100',
      descriptionKey: 'Volume: -50 is half volume and 100 is double.',
    },
    {
      name: 'req_params.audio_params.enable_timestamp',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Returns sentence and word timestamps, in seconds, in the query result.',
    },
    {
      name: 'req_params.additions',
      location: 'body',
      type: 'object',
      descriptionKey:
        'Extra synthesis options. Send an object; the gateway converts it to the JSON string the upstream expects.',
    },
    {
      name: 'req_params.additions.silence_duration',
      location: 'body',
      type: 'integer',
      defaultValue: 0,
      range: '0 – 30000',
      descriptionKey: 'Silence appended after the text, in milliseconds.',
    },
    {
      name: 'req_params.additions.disable_markdown_filter',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'When true, Markdown syntax is stripped instead of read aloud.',
    },
    {
      name: 'req_params.additions.disable_emoji_filter',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey: 'When true, emoji are removed from the text.',
    },
    {
      name: 'req_params.additions.latex_parser',
      location: 'body',
      type: 'enum',
      enumValues: ['v2'],
      descriptionKey:
        'Reads LaTeX formulas aloud, useful for education. Requires disable_markdown_filter set to true and adds latency.',
    },
    {
      name: 'req_params.additions.explicit_language',
      location: 'body',
      type: 'enum',
      enumValues: ['zh-cn', 'en', 'es-mx', 'id', 'pt-br'],
      descriptionKey:
        'Language to read. Text in other languages is skipped or fails, so the text must contain this language.',
    },
    {
      name: 'req_params.additions.max_length_to_filter_parenthesis',
      location: 'body',
      type: 'integer',
      defaultValue: 0,
      range: '0 – 100',
      descriptionKey:
        'Text inside parentheses is skipped unless it is longer than this many characters. 0 keeps all parentheses.',
    },
    {
      name: 'req_params.additions.aigc_watermark',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey: 'Appends an audible AIGC marker at the end of the audio.',
    },
    {
      name: 'req_params.additions.pronunciation_dict.tone',
      location: 'body',
      type: 'array',
      range: '≤ 5000',
      descriptionKey:
        'Pronunciation dictionary entries in the form word/replacement, for example 北京/(bei3)(jing1) or omg/oh my god. Each word is at most 9 characters; the longest match wins. Only Chinese and English voices of the 2.0 models support it, and it cannot be combined with SSML on the same text.',
    },
    {
      name: 'req_params.post_process.pitch',
      location: 'body',
      type: 'integer',
      defaultValue: 0,
      range: '-12 – 12',
      descriptionKey:
        'Pitch shift; higher sounds brighter, lower sounds deeper. post_process accepts an object and is converted like additions.',
    },
  ],
  samples: [
    {
      titleKey: 'Long text synthesis',
      body: {
        model: 'seed-tts-2.0',
        req_params: {
          text: '第一章。清晨的阳光洒进窗台，新的一天开始了。',
          speaker: 'zh_female_vv_uranus_bigtts',
          audio_params: {
            format: 'mp3',
            sample_rate: 24000,
            enable_timestamp: true,
          },
        },
      },
      noteKey:
        'Submitting returns a task_id. Poll the query endpoint until status is succeeded or failed. audio_url is valid for 1 hour and is refreshed on each query; the audio is kept for 7 days. A failed task is refunded in full.',
    },
    {
      titleKey: 'Pronunciation dictionary and pitch',
      body: {
        model: 'seed-tts-2.0',
        req_params: {
          text: '我在北京用 mobile 说 omg。',
          speaker: 'zh_female_vv_uranus_bigtts',
          audio_params: { format: 'mp3', speech_rate: 10 },
          additions: {
            silence_duration: 500,
            pronunciation_dict: {
              tone: ['北京/(bei3)(jing1)', 'omg/oh my god'],
            },
          },
          post_process: { pitch: 2 },
        },
      },
    },
  ],
}

// ---------------------------------------------------------------------------
// 百炼 Qwen-Audio-TTS 语音合成 qwen-audio-*-tts-plus / -flash
// 依据：relay/channel/ali/speech.go、relay/helper/valid_request.go
// plus 按字符计费（汉字计 2 个），flash 按输入/输出 token 计费。
// ---------------------------------------------------------------------------

const QWEN_AUDIO_LANGUAGES = [
  'zh',
  'en',
  'fr',
  'de',
  'ja',
  'ko',
  'ru',
  'pt',
  'th',
  'id',
  'vi',
  'es',
  'it',
  'ms',
  'fil',
  'ar',
]

const QWEN_AUDIO_TTS_COMPAT_PARAMS: ModelApiParam[] = [
  {
    name: 'input',
    location: 'body',
    type: 'string',
    required: true,
    range: '≤ 20000',
    descriptionKey: 'Text to synthesize, sent upstream as input.text.',
  },
  {
    name: 'voice',
    location: 'body',
    type: 'string',
    required: true,
    descriptionKey: 'Voice ID, sent upstream as input.voice.',
  },
  {
    name: 'instructions',
    location: 'body',
    type: 'string',
    descriptionKey: 'Delivery instruction, sent upstream as input.instruction.',
  },
  {
    name: 'response_format',
    location: 'body',
    type: 'enum',
    defaultValue: 'mp3',
    enumValues: ['mp3', 'wav', 'pcm', 'opus'],
    descriptionKey:
      'Audio format of the response body. aac and flac are rejected.',
  },
  {
    name: 'speed',
    location: 'body',
    type: 'number',
    defaultValue: 1,
    range: '0.5 – 2',
    descriptionKey:
      'Speaking speed, sent upstream as input.rate. Values outside 0.5-2 are clamped.',
  },
  {
    name: 'metadata',
    location: 'metadata',
    type: 'object',
    descriptionKey:
      'Native fields merged into input, such as sample_rate, language_hints and hot_fix. They override the converted values.',
  },
]

const QWEN_AUDIO_TTS: ModelApiSpec = {
  family: 'qwen-audio-tts',
  match: /^qwen-audio-[\d.]+-tts-(plus|flash)/i,
  endpointPath: '/v1/audio/generations',
  response: 'audio-url',
  params: [
    {
      name: 'model',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'qwen-audio-3.1-tts-plus is billed per 10,000 characters, with each Chinese character counting as two; qwen-audio-3.1-tts-flash is billed by input and output tokens.',
    },
    {
      name: 'input.text',
      location: 'body',
      type: 'string',
      required: true,
      range: '≤ 20000',
      descriptionKey:
        'Text to synthesize, up to 20,000 characters per request.',
    },
    {
      name: 'input.voice',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'System voice ID such as longanhuan_v3.1, or a cloned or designed voice created for this model.',
    },
    {
      name: 'input.instruction',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Natural-language instruction for dialect, emotion or role, for example: speak happily.',
    },
    {
      name: 'input.format',
      location: 'body',
      type: 'enum',
      defaultValue: 'mp3',
      enumValues: ['mp3', 'wav', 'pcm', 'opus'],
      descriptionKey: 'Output audio format.',
    },
    {
      name: 'input.sample_rate',
      location: 'body',
      type: 'integer',
      defaultValue: 22050,
      enumValues: ['8000', '16000', '22050', '24000', '44100', '48000'],
      descriptionKey: 'Sample rate in Hz.',
    },
    {
      name: 'input.volume',
      location: 'body',
      type: 'integer',
      defaultValue: 50,
      range: '0 – 100',
      descriptionKey: 'Output volume.',
    },
    {
      name: 'input.rate',
      location: 'body',
      type: 'number',
      defaultValue: 1,
      range: '0.5 – 2',
      descriptionKey: 'Speaking speed multiplier.',
    },
    {
      name: 'input.pitch',
      location: 'body',
      type: 'number',
      defaultValue: 1,
      range: '0.5 – 2',
      descriptionKey: 'Pitch multiplier.',
    },
    {
      name: 'input.bit_rate',
      location: 'body',
      type: 'integer',
      defaultValue: 32,
      range: '6 – 510',
      descriptionKey: 'Bit rate in kbps. Only applies to mp3 and opus.',
    },
    {
      name: 'input.seed',
      location: 'body',
      type: 'integer',
      defaultValue: 0,
      range: '0 – 65535',
      descriptionKey:
        'Random seed. The same parameters and seed reproduce the same audio.',
    },
    {
      name: 'input.language_hints',
      location: 'body',
      type: 'array',
      enumValues: QWEN_AUDIO_LANGUAGES,
      descriptionKey:
        'Target language. Only the first element is used, so pass a single value.',
    },
    {
      name: 'input.enable_ssml',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Treat input.text as SSML. Tags are not billed, only the text to speak.',
    },
    {
      name: 'input.enable_aigc_tag',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Embeds an invisible AIGC label in the audio. Only wav, mp3 and opus support it.',
    },
    {
      name: 'input.aigc_propagator',
      location: 'body',
      type: 'string',
      descriptionKey:
        'ContentPropagator written into the AIGC label. Defaults to the Alibaba Cloud account UID; requires enable_aigc_tag.',
    },
    {
      name: 'input.aigc_propagate_id',
      location: 'body',
      type: 'string',
      descriptionKey:
        'PropagateID written into the AIGC label. Defaults to the request ID; requires enable_aigc_tag.',
    },
    {
      name: 'input.hot_fix.pronunciation',
      location: 'body',
      type: 'array',
      descriptionKey:
        'Custom pinyin for specific words, as a list of word-to-pinyin objects.',
    },
    {
      name: 'input.hot_fix.replace',
      location: 'body',
      type: 'array',
      descriptionKey:
        'Text replacements applied before synthesis, as a list of original-to-replacement objects.',
    },
  ],
  samples: [
    {
      titleKey: 'Speak with a preset voice',
      body: {
        model: 'qwen-audio-3.1-tts-plus',
        input: {
          text: '你好，欢迎使用阿里云百炼。',
          voice: 'longanhuan_v3.1',
          format: 'mp3',
          sample_rate: 24000,
        },
      },
      outputFile: 'speech.mp3',
      noteKey:
        'The response carries output.audio.url, a download link valid for 24 hours, and usage with input_tokens, output_tokens and characters.',
    },
    {
      titleKey: 'Instruction and pronunciation fixes',
      body: {
        model: 'qwen-audio-3.1-tts-plus',
        input: {
          text: '重庆的火锅真好吃。',
          voice: 'longanhuan_v3.1',
          instruction: '用开心的语气说',
          language_hints: ['zh'],
          hot_fix: {
            pronunciation: [{ 重庆: 'chong2 qing4' }],
            replace: [{ 火锅: '火锅儿' }],
          },
          format: 'mp3',
        },
      },
      outputFile: 'speech.mp3',
    },
    {
      titleKey: 'OpenAI-compatible speech',
      endpointPath: '/v1/audio/speech',
      response: 'audio-binary',
      body: {
        model: 'qwen-audio-3.1-tts-plus',
        input: '你好，这是兼容 OpenAI 格式的语音合成。',
        voice: 'longanhuan_v3.1',
        instructions: '语气温柔',
        response_format: 'mp3',
        speed: 1.0,
        metadata: { sample_rate: 24000 },
      },
      outputFile: 'speech.mp3',
      sampleParams: QWEN_AUDIO_TTS_COMPAT_PARAMS,
      noteKey:
        'The OpenAI-compatible endpoint fetches the audio for you and returns the audio bytes directly.',
    },
  ],
}

// ---------------------------------------------------------------------------
// 百炼音频生成 qwen-audio-*-tts-next（按输入/输出 token 计费）
// 依据：relay/channel/ali/speech.go。音色只能来自参考音频，传 voice 会被忽略。
// ---------------------------------------------------------------------------

const QWEN_AUDIO_NEXT_COMPAT_PARAMS: ModelApiParam[] = [
  {
    name: 'input',
    location: 'body',
    type: 'string',
    required: true,
    range: '≤ 3000',
    descriptionKey:
      'Prompt or text to speak, sent upstream as input.text_prompt.',
  },
  {
    name: 'voice',
    location: 'body',
    type: 'string',
    descriptionKey:
      'Ignored: this model takes voices only from reference audio. Required by the OpenAI SDK, so any value works.',
  },
  {
    name: 'response_format',
    location: 'body',
    type: 'enum',
    defaultValue: 'mp3',
    enumValues: ['mp3', 'wav', 'pcm'],
    descriptionKey: 'Audio format of the response body. opus is not supported.',
  },
  {
    name: 'speed',
    location: 'body',
    type: 'number',
    defaultValue: 1,
    range: '0.5 – 2',
    descriptionKey:
      'Speaking speed, sent upstream as input.rate. Values outside 0.5-2 are clamped.',
  },
  {
    name: 'metadata',
    location: 'metadata',
    type: 'object',
    descriptionKey:
      'Native fields merged into input, such as references, sample_rate and channels.',
  },
]

const QWEN_AUDIO_NEXT: ModelApiSpec = {
  family: 'qwen-audio-next',
  match: /^qwen-audio-[\d.]+-tts-next/i,
  endpointPath: '/v1/audio/generations',
  response: 'audio-url',
  params: [
    {
      name: 'model',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'Billed by input and output tokens. Reference audio adds many input tokens.',
    },
    {
      name: 'input.text_prompt',
      location: 'body',
      type: 'string',
      required: true,
      range: '≤ 3000',
      descriptionKey:
        'Text to speak or a description of the audio, up to 3000 characters. Refer to reference voices as @voice1 to @voice3 in reference order.',
    },
    {
      name: 'input.references',
      location: 'body',
      type: 'array',
      range: '≤ 3',
      descriptionKey:
        'Up to 3 reference voices, each at most 30 seconds and 10 MB, in wav, mp3 or Ogg Opus. Preset voice IDs are not accepted; without references the voice follows the prompt.',
    },
    {
      name: 'input.references[].audio_url',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Public URL of the reference audio. Mutually exclusive with audio_data.',
    },
    {
      name: 'input.references[].audio_data',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Reference audio as a data URI: data:{mime_type};base64,{data}.',
    },
    {
      name: 'input.format',
      location: 'body',
      type: 'enum',
      defaultValue: 'wav',
      enumValues: ['wav', 'mp3', 'pcm'],
      descriptionKey: 'Output audio format. Opus output is not supported.',
    },
    {
      name: 'input.sample_rate',
      location: 'body',
      type: 'integer',
      defaultValue: 48000,
      enumValues: ['8000', '16000', '24000', '44100', '48000'],
      descriptionKey: 'Sample rate in Hz.',
    },
    {
      name: 'input.channels',
      location: 'body',
      type: 'integer',
      defaultValue: 2,
      enumValues: ['1', '2'],
      descriptionKey: 'Number of channels: 1 for mono, 2 for stereo.',
    },
    {
      name: 'input.volume',
      location: 'body',
      type: 'integer',
      defaultValue: 50,
      range: '0 – 100',
      descriptionKey: 'Output volume.',
    },
    {
      name: 'input.rate',
      location: 'body',
      type: 'number',
      defaultValue: 1,
      range: '0.5 – 2',
      descriptionKey: 'Speaking speed multiplier.',
    },
    {
      name: 'input.enable_cbr',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'MP3 only: true for constant bit rate, false for variable bit rate.',
    },
    {
      name: 'input.bit_rate',
      location: 'body',
      type: 'integer',
      defaultValue: 128,
      descriptionKey:
        'MP3 constant bit rate in kbps: 8-64 at 8 kHz, 8-160 at 16 or 24 kHz, 32-320 at 44.1 or 48 kHz. Requires enable_cbr.',
    },
    {
      name: 'input.quality',
      location: 'body',
      type: 'integer',
      defaultValue: 5,
      range: '0 – 9',
      descriptionKey: 'MP3 variable bit rate quality; 0 is the best.',
    },
    {
      name: 'input.seed',
      location: 'body',
      type: 'integer',
      defaultValue: 42,
      descriptionKey:
        'Random seed. The same parameters and seed reproduce the same audio.',
    },
    {
      name: 'input.enable_aigc_tag',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey: 'Adds an AIGC watermark to the audio.',
    },
  ],
  samples: [
    {
      titleKey: 'Describe the delivery in the prompt',
      body: {
        model: 'qwen-audio-3.1-tts-next',
        input: {
          text_prompt: '用温柔、略带笑意的女声说：晚安，祝你好梦。',
          format: 'mp3',
          channels: 1,
        },
      },
      outputFile: 'speech.mp3',
      noteKey:
        'The response carries output.audio.url, a download link valid for 24 hours, the duration in seconds and token usage. A request can take up to a few minutes.',
    },
    {
      titleKey: 'Two-speaker dialogue from reference voices',
      body: {
        model: 'qwen-audio-3.1-tts-next',
        input: {
          text_prompt:
            '@voice1 说：今天天气不错。@voice2 回答：是啊，适合出去走走。',
          references: [
            { audio_url: 'https://example.com/speaker1.wav' },
            { audio_url: 'https://example.com/speaker2.wav' },
          ],
          format: 'mp3',
        },
      },
      outputFile: 'dialogue.mp3',
    },
    {
      titleKey: 'OpenAI-compatible speech',
      endpointPath: '/v1/audio/speech',
      response: 'audio-binary',
      body: {
        model: 'qwen-audio-3.1-tts-next',
        input: '用低沉的男声说：欢迎收听今天的节目。',
        voice: 'default',
        response_format: 'mp3',
      },
      outputFile: 'speech.mp3',
      sampleParams: QWEN_AUDIO_NEXT_COMPAT_PARAMS,
    },
  ],
}

// ---------------------------------------------------------------------------
// 百炼 MiniMax 同步语音合成 MiniMax/speech-2.8-hd、speech-02-hd、speech-2.8-turbo、
// speech-02-turbo（按万字符计费，汉字计 2 个）
// 依据：relay/channel/ali/minimax_speech.go。原生入口返回 hex 编码的音频。
// ---------------------------------------------------------------------------

const MINIMAX_SPEECH_LANGUAGES = [
  'auto',
  'Chinese',
  'Chinese,Yue',
  'English',
  'Arabic',
  'Russian',
  'Spanish',
  'French',
  'Portuguese',
  'German',
  'Turkish',
  'Dutch',
  'Ukrainian',
  'Vietnamese',
  'Indonesian',
  'Japanese',
  'Italian',
  'Korean',
  'Thai',
  'Polish',
  'Romanian',
  'Greek',
  'Czech',
  'Finnish',
  'Hindi',
  'Bulgarian',
  'Danish',
  'Hebrew',
  'Malay',
  'Persian',
  'Slovak',
  'Swedish',
  'Croatian',
  'Filipino',
  'Hungarian',
  'Norwegian',
  'Slovenian',
  'Catalan',
  'Nynorsk',
  'Tamil',
  'Afrikaans',
]

const MINIMAX_SPEECH_COMPAT_PARAMS: ModelApiParam[] = [
  {
    name: 'input',
    location: 'body',
    type: 'string',
    required: true,
    range: '< 10000',
    descriptionKey: 'Text to synthesize, sent upstream as input.text.',
  },
  {
    name: 'voice',
    location: 'body',
    type: 'string',
    required: true,
    descriptionKey: 'Voice ID, sent upstream as input.voice_setting.voice_id.',
  },
  {
    name: 'response_format',
    location: 'body',
    type: 'enum',
    defaultValue: 'mp3',
    enumValues: ['mp3', 'wav', 'pcm', 'flac'],
    descriptionKey:
      'Audio format of the response body. opus and aac are rejected.',
  },
  {
    name: 'speed',
    location: 'body',
    type: 'number',
    defaultValue: 1,
    range: '0.5 – 2',
    descriptionKey:
      'Speaking speed, sent upstream as input.voice_setting.speed. Values outside 0.5-2 are clamped.',
  },
  {
    name: 'metadata',
    location: 'metadata',
    type: 'object',
    descriptionKey:
      'Native fields merged into input. voice_setting and audio_setting are merged field by field, so you can add emotion or sample_rate without losing the converted values.',
  },
]

const MINIMAX_SPEECH: ModelApiSpec = {
  family: 'minimax-speech',
  match: /^(minimax\/)?speech-[\d.]+-(hd|turbo)/i,
  endpointPath: '/v1/audio/generations',
  response: 'audio-hex',
  params: [
    {
      name: 'model',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'Billed per 10,000 characters, with each Chinese character counting as two. The hd models cost more than the turbo models.',
    },
    {
      name: 'input.text',
      location: 'body',
      type: 'string',
      required: true,
      range: '< 10000',
      descriptionKey: 'Text to synthesize, fewer than 10,000 characters.',
    },
    {
      name: 'input.voice_setting.voice_id',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'System voice ID such as male-qn-qingse. Leave it empty when mixing voices with timbre_weights.',
    },
    {
      name: 'input.voice_setting.speed',
      location: 'body',
      type: 'number',
      defaultValue: 1,
      range: '0.5 – 2',
      descriptionKey: 'Speaking speed multiplier.',
    },
    {
      name: 'input.voice_setting.vol',
      location: 'body',
      type: 'number',
      defaultValue: 1,
      range: '(0, 10]',
      descriptionKey: 'Volume.',
    },
    {
      name: 'input.voice_setting.pitch',
      location: 'body',
      type: 'integer',
      defaultValue: 0,
      range: '-12 – 12',
      descriptionKey: 'Pitch in semitones.',
    },
    {
      name: 'input.voice_setting.emotion',
      location: 'body',
      type: 'enum',
      enumValues: [
        'happy',
        'sad',
        'angry',
        'fearful',
        'disgusted',
        'surprised',
        'calm',
        'whisper',
      ],
      descriptionKey:
        'Emotion. The model picks one from the text when omitted. speech-2.8 models do not support whisper.',
    },
    {
      name: 'input.voice_setting.text_normalization',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Normalize Chinese and English text so numbers read better, at the cost of slightly higher latency.',
    },
    {
      name: 'input.voice_setting.latex_read',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Read LaTeX formulas wrapped in $$. Chinese only; it forces language_boost to Chinese.',
    },
    {
      name: 'input.audio_setting.sample_rate',
      location: 'body',
      type: 'integer',
      defaultValue: 32000,
      enumValues: ['8000', '16000', '22050', '24000', '32000', '44100'],
      descriptionKey: 'Sample rate in Hz.',
    },
    {
      name: 'input.audio_setting.bitrate',
      location: 'body',
      type: 'integer',
      defaultValue: 128000,
      enumValues: ['32000', '64000', '128000', '256000'],
      descriptionKey: 'Bit rate. Only applies to mp3.',
    },
    {
      name: 'input.audio_setting.format',
      location: 'body',
      type: 'enum',
      defaultValue: 'mp3',
      enumValues: ['mp3', 'pcm', 'flac', 'wav'],
      descriptionKey: 'Output audio format.',
    },
    {
      name: 'input.audio_setting.channel',
      location: 'body',
      type: 'integer',
      defaultValue: 1,
      enumValues: ['1', '2'],
      descriptionKey: 'Number of channels: 1 for mono, 2 for stereo.',
    },
    {
      name: 'input.pronunciation_dict.tone',
      location: 'body',
      type: 'array',
      descriptionKey:
        'Pronunciation rules in the form text/pronunciation, for example 燕少飞/(yan4)(shao3)(fei1) or omg/oh my god. Tones are 1-4, and 5 for the neutral tone.',
    },
    {
      name: 'input.timbre_weights',
      location: 'body',
      type: 'array',
      descriptionKey:
        'Mix up to 4 voices. Each item has voice_id and weight from 1 to 100.',
    },
    {
      name: 'input.language_boost',
      location: 'body',
      type: 'enum',
      enumValues: MINIMAX_SPEECH_LANGUAGES,
      descriptionKey:
        'Improve recognition of a minor language or dialect; auto lets the model decide.',
    },
    {
      name: 'input.voice_modify.pitch',
      location: 'body',
      type: 'integer',
      range: '-100 – 100',
      descriptionKey: 'Voice effect: lower is deeper, higher is brighter.',
    },
    {
      name: 'input.voice_modify.intensity',
      location: 'body',
      type: 'integer',
      range: '-100 – 100',
      descriptionKey: 'Voice effect: lower is more forceful, higher is softer.',
    },
    {
      name: 'input.voice_modify.timbre',
      location: 'body',
      type: 'integer',
      range: '-100 – 100',
      descriptionKey: 'Voice effect: lower is fuller, higher is crisper.',
    },
    {
      name: 'input.voice_modify.sound_effects',
      location: 'body',
      type: 'enum',
      enumValues: [
        'spacious_echo',
        'auditorium_echo',
        'lofi_telephone',
        'robotic',
      ],
      descriptionKey:
        'Sound effect: spacious echo, auditorium, lo-fi telephone or robotic. voice_modify supports mp3, wav and flac.',
    },
    {
      name: 'input.subtitle_enable',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey: 'Return subtitles with the audio.',
    },
    {
      name: 'input.output_format',
      location: 'body',
      type: 'enum',
      defaultValue: 'hex',
      enumValues: ['hex', 'url'],
      descriptionKey:
        'hex returns the audio in output.data.audio; url returns a download link valid for 24 hours.',
    },
    {
      name: 'input.aigc_watermark',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Append an invisible AIGC watermark to the end of the audio.',
    },
  ],
  samples: [
    {
      titleKey: 'Speak with a preset voice',
      body: {
        model: 'MiniMax/speech-2.8-hd',
        input: {
          text: '今天是不是很开心呀(laughs)，当然了！',
          voice_setting: {
            voice_id: 'male-qn-qingse',
            speed: 1,
            vol: 1,
            pitch: 0,
            emotion: 'happy',
          },
          audio_setting: {
            sample_rate: 32000,
            bitrate: 128000,
            format: 'mp3',
            channel: 1,
          },
        },
      },
      outputFile: 'speech.mp3',
      noteKey:
        'The response carries the hex-encoded audio in output.data.audio and the billable characters in usage.characters.',
    },
    {
      titleKey: 'Mix voices and fix pronunciation',
      body: {
        model: 'MiniMax/speech-02-turbo',
        input: {
          text: '燕少飞今天在处理一件危险的事。',
          voice_setting: { voice_id: '', speed: 1.1 },
          timbre_weights: [
            { voice_id: 'male-qn-qingse', weight: 60 },
            { voice_id: 'Chinese (Mandarin)_Gentleman', weight: 40 },
          ],
          pronunciation_dict: {
            tone: ['燕少飞/(yan4)(shao3)(fei1)', '处理/(chu3)(li3)'],
          },
          language_boost: 'Chinese',
          audio_setting: { format: 'mp3' },
        },
      },
      outputFile: 'speech.mp3',
    },
    {
      titleKey: 'OpenAI-compatible speech',
      endpointPath: '/v1/audio/speech',
      response: 'audio-binary',
      body: {
        model: 'MiniMax/speech-2.8-turbo',
        input: '你好，这是兼容 OpenAI 格式的语音合成。',
        voice: 'female-shaonv',
        response_format: 'mp3',
        speed: 1.0,
        metadata: {
          voice_setting: { emotion: 'happy' },
          audio_setting: { sample_rate: 44100 },
        },
      },
      outputFile: 'speech.mp3',
      sampleParams: MINIMAX_SPEECH_COMPAT_PARAMS,
      noteKey:
        'The OpenAI-compatible endpoint decodes the audio for you and returns the audio bytes directly.',
    },
  ],
}

// ---------------------------------------------------------------------------
// IndexTTS2 语音克隆（autodl ComfyUI 工作流 indextts2-v1，按生成音频的实际时长计费）
// 只用于全部由 autodl 渠道提供的模型；腾讯 TokenHub 的 IndexTTS-2 见 TOKENHUB_INDEXTTS。
// 依据：relay/channel/task/autodl/adaptor.go（请求体去掉 model 后原样透传给工作流）。
// 取值范围来自对上游参数校验的实测：emo_control_method 只接受三个中文选项，情感权重
// 上限 1.4，多传未定义的字段会被拒绝。
// ---------------------------------------------------------------------------

const INDEXTTS2_EMOTION_WEIGHT_KEY =
  'Weight of this emotion, used when emo_control_method is 使用情感向量控制. Omitted weights count as 0.'

const INDEXTTS2_TASK: ModelApiSpec = {
  family: 'autodl-indextts2',
  match: /index-?tts/i,
  workflowOnly: true,
  endpointPath: '/v1/audio/speech/tasks',
  fetchPath: '/v1/audio/speech/tasks',
  response: 'task',
  params: [
    {
      name: 'model',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey: 'Model name, e.g. indextts2.',
    },
    {
      name: 'prompt_text',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'Text to synthesize. Billed by the duration of the generated audio, not by characters.',
    },
    {
      name: 'prompt_simple',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        "Voice reference audio as a URL or a data URI (data:audio/mpeg;base64,...). The speech keeps this speaker's timbre. Use a few seconds of clean speech; music or noisy audio makes the task fail.",
    },
    {
      name: 'emo_control_method',
      location: 'body',
      type: 'enum',
      required: true,
      enumValues: [
        '与音色参考音频相同',
        '使用情感参考音频',
        '使用情感向量控制',
      ],
      descriptionKey:
        'How emotion is controlled: 与音色参考音频相同 follows the voice reference, 使用情感参考音频 follows emo_ref_audio, and 使用情感向量控制 uses the eight emo_* weights. Only these three values are accepted.',
    },
    {
      name: 'emo_ref_audio',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Emotion reference audio as a URL or a data URI, used when emo_control_method is 使用情感参考音频.',
    },
    {
      name: 'emo_happy',
      location: 'body',
      type: 'number',
      range: '0 – 1.4',
      descriptionKey: INDEXTTS2_EMOTION_WEIGHT_KEY,
    },
    {
      name: 'emo_angry',
      location: 'body',
      type: 'number',
      range: '0 – 1.4',
      descriptionKey: INDEXTTS2_EMOTION_WEIGHT_KEY,
    },
    {
      name: 'emo_sad',
      location: 'body',
      type: 'number',
      range: '0 – 1.4',
      descriptionKey: INDEXTTS2_EMOTION_WEIGHT_KEY,
    },
    {
      name: 'emo_afraid',
      location: 'body',
      type: 'number',
      range: '0 – 1.4',
      descriptionKey: INDEXTTS2_EMOTION_WEIGHT_KEY,
    },
    {
      name: 'emo_disgusted',
      location: 'body',
      type: 'number',
      range: '0 – 1.4',
      descriptionKey: INDEXTTS2_EMOTION_WEIGHT_KEY,
    },
    {
      name: 'emo_melancholic',
      location: 'body',
      type: 'number',
      range: '0 – 1.4',
      descriptionKey: INDEXTTS2_EMOTION_WEIGHT_KEY,
    },
    {
      name: 'emo_surprised',
      location: 'body',
      type: 'number',
      range: '0 – 1.4',
      descriptionKey: INDEXTTS2_EMOTION_WEIGHT_KEY,
    },
    {
      name: 'emo_calm',
      location: 'body',
      type: 'number',
      range: '0 – 1.4',
      descriptionKey: INDEXTTS2_EMOTION_WEIGHT_KEY,
    },
    {
      name: 'emo_random',
      location: 'body',
      type: 'boolean',
      defaultValue: false,
      descriptionKey:
        'Samples the emotion randomly, so the same input can sound different on each run.',
    },
  ],
  samples: [
    {
      titleKey: 'Clone a voice',
      body: {
        model: 'indextts2',
        prompt_text: '你好，欢迎收听今天的节目。',
        prompt_simple: 'https://example.com/voice.wav',
        emo_control_method: '与音色参考音频相同',
      },
      noteKey:
        'Submitting returns a task_id. Poll the query endpoint until status is succeeded or failed; the upstream queue can take several minutes. audio_url is a WAV file valid for 24 hours and refreshed when it expires. Billed by the duration of the generated audio, with a per-request minimum; a failed task is refunded in full. Fields not listed here are rejected.',
    },
    {
      titleKey: 'Emotion weights',
      body: {
        model: 'indextts2',
        prompt_text: '太好了，我们终于成功了！',
        prompt_simple: 'https://example.com/voice.wav',
        emo_control_method: '使用情感向量控制',
        emo_happy: 0.8,
        emo_surprised: 0.3,
        emo_calm: 0.2,
      },
    },
    {
      titleKey: 'Emotion reference audio',
      body: {
        model: 'indextts2',
        prompt_text: '我真的不知道该怎么办才好。',
        prompt_simple: 'https://example.com/voice.wav',
        emo_control_method: '使用情感参考音频',
        emo_ref_audio: 'https://example.com/sad.wav',
      },
    },
  ],
}

// ---------------------------------------------------------------------------
// MiniMax-H3 加速版（autodl ComfyUI 工作流，按请求时长计费）
// 依据：relay/channel/task/autodl/adaptor.go buildWorkflowParams。只用于全部由 autodl
// 渠道提供的模型，对外名可以叫 MiniMax-H3-Turbo 等；官方 H3 见 MINIMAX_VIDEO。
// WeNex 渠道（relay/channel/task/wenex）可以挂在同一个模型下，它接受这里的写法，
// 能力差异（只有 768p/1088p、4~15 秒、动态优化参数）写在渠道对外说明里。
// 每个模型映射到一个工作流，工作流只认自己的参数（文生 / 首尾帧 / 参考图音频）。
// ---------------------------------------------------------------------------

const AUTODL_H3_WORKFLOW_NOTE =
  'Each model runs one workflow that only accepts its own inputs: a text-to-video model rejects images, a first-and-last-frame model needs both frames, and a reference model needs at least one reference image. Unsupported inputs are rejected with 400. The video URL is valid for 24 hours and refreshed when you query the task.'

const AUTODL_H3_VIDEO: ModelApiSpec = {
  family: 'autodl-h3-video',
  match: /h3/i,
  workflowOnly: true,
  endpointPath: '/v1/video/generations',
  params: [
    {
      name: 'model',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'Model name, e.g. MiniMax-H3-Turbo. Each model runs one workflow, so the inputs it accepts depend on the model.',
    },
    {
      name: 'prompt',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey: 'Text prompt describing the video.',
    },
    {
      name: 'duration',
      location: 'body',
      type: 'integer',
      range: '1 ~ 15',
      defaultValue: 5,
      descriptionKey:
        'Video length in seconds. Billed per second of the requested length.',
    },
    {
      name: 'seconds',
      location: 'body',
      type: 'string',
      descriptionKey: 'Same as duration, as a string (OpenAI video format).',
    },
    {
      name: 'resolution',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Resolution tier such as 480p or 768p. Leave it empty for the workflow default (768p portrait for H3), billed at the default tier.',
    },
    {
      name: 'ratio',
      location: 'body',
      type: 'enum',
      enumValues: ['16:9', '9:16', '1:1'],
      defaultValue: '16:9',
      descriptionKey:
        'Aspect ratio: 16:9 landscape, 9:16 portrait or 1:1 square. Only used together with resolution.',
    },
    {
      name: 'size',
      location: 'body',
      type: 'string',
      descriptionKey:
        'Alternative to resolution and ratio: WIDTHxHEIGHT such as 864x480. The short edge picks the tier and the shape picks the orientation.',
    },
    {
      name: 'seed',
      location: 'body',
      type: 'integer',
      descriptionKey:
        'Random seed. The same inputs and seed give similar results.',
    },
    {
      name: 'mode',
      location: 'body',
      type: 'enum',
      enumValues: ['fast', 'quality'],
      defaultValue: 'quality',
      descriptionKey:
        'Speed mode: fast renders quicker, quality gives finer detail; both cost the same. Only some models accept it (see the channel note); others reject it with 400. 高速模式 and 高质模式 are also accepted.',
    },
    ...mediaRoleParams({
      firstFrame: MEDIA_FIRST_FRAME,
      lastFrame: MEDIA_LAST_FRAME,
      referenceImages: MEDIA_REFERENCE_IMAGES,
      referenceAudios: MEDIA_REFERENCE_AUDIOS,
      ranges: { referenceImages: '≤ 9', referenceAudios: '≤ 3' },
    }),
  ],
  samples: [
    {
      titleKey: 'Text to video',
      body: {
        model: 'MiniMax-H3-Turbo',
        prompt: '一只橘猫在洒满阳光的窗台上伸懒腰，镜头缓缓推近',
        resolution: '480p',
        ratio: '9:16',
        duration: 5,
      },
      noteKey: AUTODL_H3_WORKFLOW_NOTE,
    },
    {
      titleKey: 'Image to video (first and last frame)',
      body: {
        model: 'MiniMax-H3-Turbo',
        prompt: '镜头从清晨的街道平滑过渡到夜晚的灯火',
        first_frame_url: 'https://example.com/morning.png',
        last_frame_url: 'https://example.com/night.png',
        resolution: '768p',
        duration: 5,
      },
      noteKey: MEDIA_FRAMES_OR_REFERENCES_NOTE,
    },
    {
      titleKey: 'Reference to video',
      body: {
        model: 'MiniMax-H3-Turbo',
        prompt: '角色对着镜头说：欢迎来到我们的频道',
        reference_images: ['https://example.com/role.png'],
        audio_urls: ['https://example.com/voice.mp3'],
        resolution: '480p',
        ratio: '16:9',
        duration: 5,
      },
      noteKey: MEDIA_FRAMES_OR_REFERENCES_NOTE,
    },
  ],
  applyMatrix: (params, table) =>
    withMatrixResolution(params, table, 'resolution'),
}

// ---------------------------------------------------------------------------
// 腾讯 TokenHub IndexTTS-2 同步语音合成（按万字符计费，字符为 UTF-8 字节）
// 依据：relay/channel/tencent/tokenhub_speech.go。只走 OpenAI 兼容入口，直接返回 WAV。
// ---------------------------------------------------------------------------

const TOKENHUB_INDEXTTS: ModelApiSpec = {
  family: 'tokenhub-indextts',
  match: /^index-?tts/i,
  endpointPath: '/v1/audio/speech',
  response: 'audio-binary',
  params: [
    {
      name: 'model',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        'Model name, e.g. indextts-2. Billed per 10,000 characters counted in UTF-8 bytes: a Chinese character counts as 3, a letter or digit as 1.',
    },
    {
      name: 'input',
      location: 'body',
      type: 'string',
      required: true,
      range: '≤ 10000',
      descriptionKey:
        'Text to synthesize, at most 10,000 bytes in UTF-8 (about 3,300 Chinese characters).',
    },
    {
      name: 'voice',
      location: 'body',
      type: 'string',
      required: true,
      descriptionKey:
        "Voice reference audio as an http(s) URL; the speech keeps this speaker's timbre. data URIs are not accepted.",
    },
    {
      name: 'response_format',
      location: 'body',
      type: 'enum',
      defaultValue: 'wav',
      enumValues: ['wav'],
      descriptionKey:
        'Audio format. This model only returns 22050 Hz mono WAV, so other formats are rejected.',
    },
  ],
  samples: [
    {
      titleKey: 'Clone a voice',
      body: {
        model: 'indextts-2',
        input: '今天天气不错，我们一起去公园散步吧。',
        voice: 'https://example.com/voice.wav',
        response_format: 'wav',
      },
      outputFile: 'speech.wav',
      noteKey:
        'Returns the WAV audio directly. Emotion controls are not available on this route.',
    },
  ],
}

const SPECS: ModelApiSpec[] = [
  MINIMAX_VIDEO,
  KLING_VIDEO,
  VIDU_REFERENCE,
  VIDU_IMG2VIDEO,
  SEEDANCE_VIDEO,
  HAPPYHORSE_VIDEO,
  WAN3_VIDEO,
  IMAGE_GENERATION,
  SEED_AUDIO,
  SEED_TTS_TASK,
  INDEXTTS2_TASK,
  TOKENHUB_INDEXTTS,
  AUTODL_H3_VIDEO,
  QWEN_AUDIO_TTS,
  QWEN_AUDIO_NEXT,
  MINIMAX_SPEECH,
]

/**
 * 返回命中的族定义；没有专属定义时返回 undefined，由调用方回落到通用逻辑。
 * workflowApi 为 true 时只在工作流族里找，否则只在其余族里找。
 */
export function findModelApiSpec(
  modelName: string,
  workflowApi = false
): ModelApiSpec | undefined {
  const name = modelName.trim()
  if (!name) return undefined
  return SPECS.find(
    (spec) =>
      Boolean(spec.workflowOnly) === workflowApi && spec.match.test(name)
  )
}

/**
 * 解析出该模型的参数表。命中专属族时用族定义；否则对话类模型回落到真实的
 * 通用表。返回 undefined 表示这类模型（图片、向量等）暂无专属定义，
 * 由调用方回落到上游原有逻辑。
 */
export function resolveModelApiParams(
  model: PricingModel
): ModelApiParam[] | undefined {
  const spec = findModelApiSpec(model.model_name, model.workflow_api)
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
