type MediaInput =
  | { type: "url"; url: string; mimeType?: string; name?: string }
  | { type: "base64"; data: string; mimeType: string; name?: string }
  | { type: "binary"; data: Uint8Array; mimeType: string; name?: string };

type MediaAsset = MediaInput & { mediaType: "image" | "video" | "audio" };

type ImageRequest = {
  model: string;
  prompt: string;
  images?: MediaInput[];
  mask?: MediaInput;
  n?: number;
  ratio?: string;
  size?: string;
  quality?: string;
  outputFormat?: string;
  other?: Record<string, unknown>;
};

type VideoRequest = {
  model: string;
  prompt: string;
  mode?: unknown;
  images?: MediaInput[];
  videos?: MediaInput[];
  audios?: MediaInput[];
  firstFrame?: MediaInput;
  lastFrame?: MediaInput;
  duration?: number;
  ratio?: string;
  resolution?: string;
  generateAudio?: boolean;
  watermark?: boolean;
  other?: Record<string, unknown>;
};

type Context = {
  config: { apiKey?: string };
  signal?: AbortSignal;
  tool: { fetch: typeof fetch };
};

// 视频模型族：决定比例、清晰度、配音、水印各写在请求体的哪个位置。
type VideoFamily = "minimax" | "kling" | "viduReference" | "viduImage" | "seedance" | "happyhorse" | "wan" | "workflow";

type VideoCapability = {
  family: VideoFamily;
  text: boolean;
  singleImage: boolean;
  startEnd: boolean;
  maxImages: number;
  maxVideos: number;
  maxAudios: number;
  minDuration: number;
  maxDuration: number;
  resolutions: string[];
  ratios: string[];
  audio: boolean;
  watermark: boolean;
  fixedDuration?: boolean;
};

const BASE_URL = "https://www.token01.net";
const SUBMIT_TIMEOUT_MS = 180000;
const VIDEO_TIMEOUT_MS = 3 * 60 * 60 * 1000;
const IMAGE_TIMEOUT_MS = 10 * 60 * 1000;
const POLL_INTERVAL_MS = 10000;

const IMAGE_MODELS: Record<string, { sizes: string[]; edit: boolean }> = {
  "vidu-q2": { sizes: ["1K", "2K", "4K"], edit: true },
  "wan2.7-image": { sizes: ["1K"], edit: true },
  "qwen-image-2.0-pro": { sizes: ["1K"], edit: true },
  "qwen-image-2.0": { sizes: ["1K"], edit: true },
  "qwen-image-3.0": { sizes: ["1K", "2K"], edit: true },
  "qwen-image-3.0-pro": { sizes: ["1K", "2K"], edit: true },
  "z-image-turbo": { sizes: ["1K"], edit: false },
  "Doubao-Seedream-4.0": { sizes: ["1K"], edit: true },
  "Doubao-Seedream-4.5": { sizes: ["1K"], edit: true },
  "Doubao-Seedream-5.0-pro": { sizes: ["1K"], edit: true },
  "Doubao-Seedream-5.0-lite": { sizes: ["1K"], edit: true },
  "Kling-3.0-image": { sizes: ["1K", "2K", "4K"], edit: true },
};

const IMAGE_RATIOS = ["1:1", "16:9", "9:16", "4:3", "3:4"];

const MINIMAX_RATIOS = ["16:9", "9:16", "1:1", "4:3", "3:4", "21:9"];
const KLING_RATIOS = ["16:9", "9:16", "1:1"];
const SEEDANCE_RATIOS = ["16:9", "9:16", "1:1", "4:3", "3:4", "21:9"];
const WAN_RATIOS = ["16:9", "9:16", "1:1", "4:3", "3:4"];
const WORKFLOW_RATIOS = ["16:9", "9:16", "1:1"];

const VIDEO_MODELS: Record<string, VideoCapability> = {
  "MiniMax-H3": { family: "minimax", text: true, singleImage: true, startEnd: true, maxImages: 9, maxVideos: 3, maxAudios: 3, minDuration: 4, maxDuration: 15, resolutions: ["768p", "2k"], ratios: MINIMAX_RATIOS, audio: false, watermark: true },
  "MiniMax-H3-Highspeed": { family: "minimax", text: true, singleImage: true, startEnd: true, maxImages: 9, maxVideos: 3, maxAudios: 3, minDuration: 4, maxDuration: 15, resolutions: ["768p"], ratios: MINIMAX_RATIOS, audio: false, watermark: true },
  "MiniMax-H3-Lite": { family: "minimax", text: true, singleImage: true, startEnd: true, maxImages: 9, maxVideos: 3, maxAudios: 3, minDuration: 4, maxDuration: 15, resolutions: ["480p", "768p"], ratios: MINIMAX_RATIOS, audio: false, watermark: true },
  "MiniMax-H3-Max": { family: "minimax", text: true, singleImage: true, startEnd: true, maxImages: 9, maxVideos: 3, maxAudios: 3, minDuration: 5, maxDuration: 15, resolutions: ["480p", "768p"], ratios: MINIMAX_RATIOS, audio: false, watermark: true },
  "MiniMax-H3-Fast": { family: "workflow", text: true, singleImage: false, startEnd: true, maxImages: 9, maxVideos: 0, maxAudios: 3, minDuration: 1, maxDuration: 15, resolutions: ["480p", "768p", "1088p", "1440p"], ratios: WORKFLOW_RATIOS, audio: false, watermark: false },
  "kling-3.0-video": { family: "kling", text: true, singleImage: true, startEnd: true, maxImages: 0, maxVideos: 0, maxAudios: 0, minDuration: 3, maxDuration: 15, resolutions: ["720p", "1080p", "2k", "4k"], ratios: KLING_RATIOS, audio: true, watermark: true },
  "kling-3.0-Omni": { family: "kling", text: true, singleImage: true, startEnd: true, maxImages: 7, maxVideos: 1, maxAudios: 0, minDuration: 3, maxDuration: 15, resolutions: ["720p", "1080p", "2k", "4k"], ratios: KLING_RATIOS, audio: true, watermark: true },
  "vidu-q3-ad": { family: "viduReference", text: false, singleImage: false, startEnd: false, maxImages: 7, maxVideos: 0, maxAudios: 0, minDuration: 3, maxDuration: 15, resolutions: ["720p", "1080p"], ratios: [], audio: false, watermark: false },
  "vidu-q3-drama": { family: "viduReference", text: false, singleImage: false, startEnd: false, maxImages: 7, maxVideos: 0, maxAudios: 0, minDuration: 2, maxDuration: 15, resolutions: ["720p", "1080p"], ratios: [], audio: false, watermark: false },
  "vidu-q3-pro": { family: "viduImage", text: false, singleImage: true, startEnd: false, maxImages: 0, maxVideos: 0, maxAudios: 0, minDuration: 1, maxDuration: 16, resolutions: ["540p", "720p", "1080p"], ratios: [], audio: true, watermark: false },
  "vidu-q3-pro-fast": { family: "viduImage", text: false, singleImage: true, startEnd: false, maxImages: 0, maxVideos: 0, maxAudios: 0, minDuration: 1, maxDuration: 16, resolutions: ["720p", "1080p"], ratios: [], audio: true, watermark: false },
  "vidu-q3-turbo": { family: "viduImage", text: false, singleImage: true, startEnd: false, maxImages: 0, maxVideos: 0, maxAudios: 0, minDuration: 1, maxDuration: 16, resolutions: ["540p", "720p", "1080p"], ratios: [], audio: true, watermark: false },
  "Doubao-Seedance-2.0": { family: "seedance", text: true, singleImage: true, startEnd: true, maxImages: 9, maxVideos: 3, maxAudios: 3, minDuration: 4, maxDuration: 15, resolutions: ["480p", "720p", "1080p", "4k"], ratios: SEEDANCE_RATIOS, audio: true, watermark: true },
  "Doubao-Seedance-2.0-fast": { family: "seedance", text: true, singleImage: true, startEnd: true, maxImages: 9, maxVideos: 3, maxAudios: 3, minDuration: 4, maxDuration: 15, resolutions: ["480p", "720p"], ratios: SEEDANCE_RATIOS, audio: true, watermark: true },
  "Doubao-Seedance-2.0-mini": { family: "seedance", text: true, singleImage: true, startEnd: true, maxImages: 9, maxVideos: 3, maxAudios: 3, minDuration: 4, maxDuration: 15, resolutions: ["480p", "720p"], ratios: SEEDANCE_RATIOS, audio: true, watermark: true },
  "Doubao-Seedance-2.5": { family: "seedance", text: true, singleImage: true, startEnd: true, maxImages: 30, maxVideos: 10, maxAudios: 10, minDuration: 4, maxDuration: 30, resolutions: ["480p", "720p", "1080p"], ratios: SEEDANCE_RATIOS, audio: true, watermark: true },
  "happyhorse-1.1-t2v": { family: "happyhorse", text: true, singleImage: false, startEnd: false, maxImages: 0, maxVideos: 0, maxAudios: 0, minDuration: 3, maxDuration: 15, resolutions: ["480p", "720p", "1080p"], ratios: WAN_RATIOS, audio: false, watermark: false },
  "happyhorse-1.1-i2v": { family: "happyhorse", text: false, singleImage: true, startEnd: false, maxImages: 0, maxVideos: 0, maxAudios: 0, minDuration: 3, maxDuration: 15, resolutions: ["480p", "720p", "1080p"], ratios: [], audio: false, watermark: false },
  "happyhorse-1.1-r2v": { family: "happyhorse", text: false, singleImage: false, startEnd: false, maxImages: 9, maxVideos: 0, maxAudios: 0, minDuration: 3, maxDuration: 15, resolutions: ["480p", "720p", "1080p"], ratios: WAN_RATIOS, audio: false, watermark: false },
  "happyhorse-1.0-video-edit": { family: "happyhorse", text: false, singleImage: false, startEnd: false, maxImages: 5, maxVideos: 1, maxAudios: 0, minDuration: 3, maxDuration: 15, resolutions: ["720p", "1080p"], ratios: [], audio: false, watermark: false, fixedDuration: true },
  "wan3.0-video": { family: "wan", text: true, singleImage: true, startEnd: true, maxImages: 10, maxVideos: 5, maxAudios: 5, minDuration: 2, maxDuration: 30, resolutions: ["480p", "720p", "1080p"], ratios: WAN_RATIOS, audio: true, watermark: true },
  "wan3.0-video-prime": { family: "wan", text: true, singleImage: true, startEnd: true, maxImages: 10, maxVideos: 5, maxAudios: 5, minDuration: 2, maxDuration: 30, resolutions: ["480p", "720p", "1080p"], ratios: WAN_RATIOS, audio: true, watermark: true },
};

function readApiKey(ctx: Context): string {
  const apiKey = ctx.config.apiKey?.trim();
  if (!apiKey) throw new Error("请先在供应商配置里填写 token01 的令牌（sk- 开头）");
  return apiKey;
}

function combinedSignal(ctx: Context, timeoutMs: number): AbortSignal {
  const signals = [AbortSignal.timeout(timeoutMs)];
  if (ctx.signal) signals.push(ctx.signal);
  return AbortSignal.any(signals);
}

// 参考素材统一转成 token01 接受的字符串：网络地址原样传，其余转成 data URI。
function mediaToString(input: MediaInput, label: string): string {
  if (input.type === "url") {
    if (!/^https?:\/\//i.test(input.url) && !/^data:/i.test(input.url)) {
      throw new Error(label + "必须是 http(s) 网络地址");
    }
    return input.url;
  }
  if (!input.mimeType) throw new Error(label + "缺少文件类型");
  const data = input.type === "base64" ? input.data.replace(/^data:[^,]*,/, "") : Buffer.from(input.data).toString("base64");
  return "data:" + input.mimeType + ";base64," + data;
}

async function readError(response: Response): Promise<string> {
  const text = await response.text().catch(() => "");
  try {
    const body = JSON.parse(text) as { error?: { message?: unknown } | string; message?: unknown };
    if (body && typeof body.error === "object" && body.error && typeof body.error.message === "string") return body.error.message;
    if (body && typeof body.error === "string") return body.error;
    if (body && typeof body.message === "string" && body.message) return body.message;
  } catch {
    // 不是 JSON 时直接截取原文
  }
  return text.slice(0, 300) || "HTTP " + response.status;
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(signal.reason ?? new Error("已取消"));
      return;
    }
    const onAbort = () => {
      clearTimeout(timer);
      reject(signal.reason ?? new Error("已取消"));
    };
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

// 按比例和 1K/2K/4K 档位算出「宽x高」，长边等于档位，短边按比例取 16 的倍数。
function imageSizeToPixels(size: string | undefined, ratio: string | undefined): string {
  if (size && /^\d+\s*[x*×]\s*\d+$/i.test(size)) return size.replace(/\s*[*×X]\s*/, "x");
  const tier = (size ?? "1K").toUpperCase();
  const longEdge = tier === "4K" ? 4096 : tier === "2K" ? 2048 : 1024;
  const match = /^(\d+):(\d+)$/.exec(ratio ?? "1:1");
  if (!match) throw new Error("不支持的图片比例：" + ratio);
  const w = Number(match[1]);
  const h = Number(match[2]);
  if (w >= h) return longEdge + "x" + Math.round((longEdge * h) / w / 16) * 16;
  return Math.round((longEdge * w) / h / 16) * 16 + "x" + longEdge;
}

function setNested(body: Record<string, unknown>, path: string[], value: unknown): void {
  let node = body;
  for (const key of path.slice(0, -1)) {
    const next = node[key];
    if (!next || typeof next !== "object") node[key] = {};
    node = node[key] as Record<string, unknown>;
  }
  node[path[path.length - 1]] = value;
}

function buildVideoBody(request: VideoRequest, cap: VideoCapability): Record<string, unknown> {
  const images = request.images ?? [];
  const videos = request.videos ?? [];
  const audios = request.audios ?? [];
  const hasFrames = Boolean(request.firstFrame || request.lastFrame);
  const hasReferences = images.length > 0 || videos.length > 0 || audios.length > 0;

  if (hasFrames && hasReferences) throw new Error("首尾帧不能和参考图、参考视频、参考音频同时使用");
  if (request.lastFrame && !request.firstFrame) throw new Error("传尾帧时必须同时传首帧");
  if (request.firstFrame && request.lastFrame && !cap.startEnd) throw new Error(request.model + " 不支持首尾帧");
  if (request.firstFrame && !request.lastFrame && !cap.singleImage) throw new Error(request.model + " 不支持单图生视频");
  if (images.length > cap.maxImages) throw new Error(request.model + (cap.maxImages ? " 最多 " + cap.maxImages + " 张参考图" : " 不支持参考图"));
  if (videos.length > cap.maxVideos) throw new Error(request.model + (cap.maxVideos ? " 最多 " + cap.maxVideos + " 段参考视频" : " 不支持参考视频"));
  if (audios.length > cap.maxAudios) throw new Error(request.model + (cap.maxAudios ? " 最多 " + cap.maxAudios + " 段参考音频" : " 不支持参考音频"));
  if (!hasFrames && !hasReferences && !cap.text) throw new Error(request.model + " 需要输入图片或参考素材，不能只用文字生成");
  if (cap.family === "viduReference" && images.length === 0) throw new Error(request.model + " 至少需要一张参考图");
  if (request.model === "happyhorse-1.1-r2v" && images.length === 0) throw new Error(request.model + " 至少需要一张参考图");
  if (request.model === "happyhorse-1.0-video-edit" && videos.length !== 1) throw new Error(request.model + " 需要且只能传一段待编辑的视频");
  if (request.generateAudio && !cap.audio) throw new Error(request.model + " 不支持生成配音");
  if (request.watermark && !cap.watermark) throw new Error(request.model + " 不支持添加水印");

  const body: Record<string, unknown> = { model: request.model, prompt: request.prompt };
  if (request.firstFrame) body.first_frame_url = mediaToString(request.firstFrame, "首帧图片");
  if (request.lastFrame) body.last_frame_url = mediaToString(request.lastFrame, "尾帧图片");
  if (images.length) body.reference_images = images.map((item, i) => mediaToString(item, "第 " + (i + 1) + " 张参考图"));
  if (videos.length) body.video_urls = videos.map((item, i) => mediaToString(item, "第 " + (i + 1) + " 段参考视频"));
  if (audios.length) body.audio_urls = audios.map((item, i) => mediaToString(item, "第 " + (i + 1) + " 段参考音频"));

  if (request.duration !== undefined && !cap.fixedDuration) {
    const duration = request.duration;
    if (!Number.isInteger(duration) || duration < cap.minDuration || duration > cap.maxDuration) {
      throw new Error(request.model + " 的时长需在 " + cap.minDuration + "～" + cap.maxDuration + " 秒之间");
    }
    body.duration = duration;
  }

  if (request.resolution !== undefined) {
    const resolution = request.resolution.toLowerCase();
    if (!cap.resolutions.includes(resolution)) {
      throw new Error(request.model + " 不支持清晰度 " + request.resolution + "，可选：" + cap.resolutions.join("、"));
    }
    if (cap.family === "workflow") body.resolution = resolution;
    else if (cap.family === "seedance" || cap.family === "kling") body.size = resolution;
    else body.size = resolution.toUpperCase();
  }

  // 比例：参考生和文生视频需要；以首帧决定画面的场景由上游按图片比例出片。
  const ratio = request.ratio && request.ratio !== "adaptive" ? request.ratio : undefined;
  if (ratio && cap.ratios.length && !cap.ratios.includes(ratio)) {
    throw new Error(request.model + " 不支持比例 " + ratio + "，可选：" + cap.ratios.join("、"));
  }
  if (ratio && cap.ratios.length) {
    if (cap.family === "minimax" || cap.family === "happyhorse" || cap.family === "wan") setNested(body, ["metadata", "parameters", "ratio"], ratio);
    else if (cap.family === "kling") setNested(body, ["metadata", "parameters", "aspect_ratio"], ratio);
    else if (cap.family === "seedance") setNested(body, ["metadata", "ratio"], ratio);
    else if (cap.family === "workflow") body.ratio = ratio;
  }

  if (request.generateAudio !== undefined && cap.audio) {
    if (cap.family === "seedance") setNested(body, ["metadata", "generate_audio"], request.generateAudio);
    else setNested(body, ["metadata", "parameters", "audio"], request.generateAudio);
  }
  if (request.watermark !== undefined && cap.watermark) {
    if (cap.family === "seedance") setNested(body, ["metadata", "watermark"], request.watermark);
    else setNested(body, ["metadata", "parameters", "watermark"], request.watermark);
  }
  return body;
}

export default {
  id: "token01",
  label: "token01",
  version: "2.0.0",
  readme: "## token01 图片与视频\n\n接入 token01（https://www.token01.net）模型广场里的图片和视频模型。\n\n**配置**：在「API Key」里填写 token01 控制台「令牌管理」中创建的令牌（sk- 开头）。令牌的分组决定可用模型和价格，费用按模型广场标价从 token01 账户扣除。\n\n**使用说明**\n\n- 参考图、首尾帧推荐使用公网可访问的图片网址；本地图片会以 base64 发送，MiniMax-H3 的 5.9折分组不支持 base64，参考视频和参考音频请尽量使用网址。\n- 视频为异步生成，会在节点里等待完成，最长等待 3 小时。生成失败时 token01 会自动退款。\n- MiniMax-H3-Fast 由工作流提供，能用哪种玩法（文生、首尾帧、参考图）取决于令牌所在分组，不匹配时会提示错误。\n- Vidu 图生视频、HappyHorse 图生视频的画面比例跟随输入图片；HappyHorse 视频编辑的时长跟随输入视频。\n- 图片尺寸按比例和 1K/2K/4K 档位换算成像素，未标出 2K/4K 的模型只开放 1K。",
  rules: [
    {
      type: "input",
      field: "apiKey",
      title: "API Key",
      value: "",
      props: { type: "password", showPassword: true, autocomplete: "off", placeholder: "sk-..." },
      validate: [{ required: true, message: "请填写 token01 的令牌" }],
    },
  ],
  models: [
    { id: "Doubao-Seedream-5.0-pro", label: "豆包 Seedream 5.0 Pro", type: "image", mode: ["text", "singleImage"], imageSizes: ["1K"], imageRatios: ["1:1", "16:9", "9:16", "4:3", "3:4"] },
    { id: "Doubao-Seedream-5.0-lite", label: "豆包 Seedream 5.0 Lite", type: "image", mode: ["text", "singleImage"], imageSizes: ["1K"], imageRatios: ["1:1", "16:9", "9:16", "4:3", "3:4"] },
    { id: "Doubao-Seedream-4.5", label: "豆包 Seedream 4.5", type: "image", mode: ["text", "singleImage"], imageSizes: ["1K"], imageRatios: ["1:1", "16:9", "9:16", "4:3", "3:4"] },
    { id: "Doubao-Seedream-4.0", label: "豆包 Seedream 4.0", type: "image", mode: ["text", "singleImage"], imageSizes: ["1K"], imageRatios: ["1:1", "16:9", "9:16", "4:3", "3:4"] },
    { id: "qwen-image-3.0-pro", label: "千问图片 3.0 Pro", type: "image", mode: ["text", "singleImage"], imageSizes: ["1K", "2K"], imageRatios: ["1:1", "16:9", "9:16", "4:3", "3:4"] },
    { id: "qwen-image-3.0", label: "千问图片 3.0", type: "image", mode: ["text", "singleImage"], imageSizes: ["1K", "2K"], imageRatios: ["1:1", "16:9", "9:16", "4:3", "3:4"] },
    { id: "qwen-image-2.0-pro", label: "千问图片 2.0 Pro", type: "image", mode: ["text", "singleImage"], imageSizes: ["1K"], imageRatios: ["1:1", "16:9", "9:16", "4:3", "3:4"] },
    { id: "qwen-image-2.0", label: "千问图片 2.0", type: "image", mode: ["text", "singleImage"], imageSizes: ["1K"], imageRatios: ["1:1", "16:9", "9:16", "4:3", "3:4"] },
    { id: "wan2.7-image", label: "万相图片 2.7", type: "image", mode: ["text", "singleImage"], imageSizes: ["1K"], imageRatios: ["1:1", "16:9", "9:16", "4:3", "3:4"] },
    { id: "Kling-3.0-image", label: "可灵图片 3.0", type: "image", mode: ["text", "singleImage"], imageSizes: ["1K", "2K", "4K"], imageRatios: ["1:1", "16:9", "9:16", "4:3", "3:4"] },
    { id: "vidu-q2", label: "Vidu Q2 图片", type: "image", mode: ["text", "singleImage"], imageSizes: ["1K", "2K", "4K"], imageRatios: ["1:1", "16:9", "9:16", "4:3", "3:4"] },
    { id: "z-image-turbo", label: "Z-Image Turbo", type: "image", mode: ["text"], imageSizes: ["1K"], imageRatios: ["1:1", "16:9", "9:16", "4:3", "3:4"] },
    { id: "MiniMax-H3", label: "MiniMax 海螺 H3", type: "video", mode: ["text", "singleImage", "startEndRequired", ["imageReference:9", "videoReference:3", "audioReference:3"]], durationResolutionMap: [{ duration: [4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["768p", "2k"] }] },
    { id: "MiniMax-H3-Highspeed", label: "MiniMax 海螺 H3 Highspeed", type: "video", mode: ["text", "singleImage", "startEndRequired", ["imageReference:9", "videoReference:3", "audioReference:3"]], durationResolutionMap: [{ duration: [4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["768p"] }] },
    { id: "MiniMax-H3-Lite", label: "MiniMax 海螺 H3 Lite", type: "video", mode: ["text", "singleImage", "startEndRequired", ["imageReference:9", "videoReference:3", "audioReference:3"]], durationResolutionMap: [{ duration: [4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["480p", "768p"] }] },
    { id: "MiniMax-H3-Max", label: "MiniMax 海螺 H3 Max", type: "video", mode: ["text", "singleImage", "startEndRequired", ["imageReference:9", "videoReference:3", "audioReference:3"]], durationResolutionMap: [{ duration: [5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["480p", "768p"] }] },
    { id: "MiniMax-H3-Fast", label: "MiniMax 海螺 H3 Fast（工作流版）", type: "video", mode: ["text", "startEndRequired", ["imageReference:9", "audioReference:3"]], durationResolutionMap: [{ duration: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["480p", "768p", "1088p", "1440p"] }] },
    { id: "kling-3.0-video", label: "可灵 3.0 视频", type: "video", audio: "optional", mode: ["text", "singleImage", "startEndRequired"], durationResolutionMap: [{ duration: [3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["720p", "1080p", "2k", "4k"] }] },
    { id: "kling-3.0-Omni", label: "可灵 3.0 Omni", type: "video", audio: "optional", mode: ["text", "singleImage", "startEndRequired", ["imageReference:7", "videoReference:1"]], durationResolutionMap: [{ duration: [3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["720p", "1080p", "2k", "4k"] }] },
    { id: "Doubao-Seedance-2.5", label: "豆包 Seedance 2.5", type: "video", audio: "optional", mode: ["text", "singleImage", "startEndRequired", ["imageReference:30", "videoReference:10", "audioReference:10"]], durationResolutionMap: [{ duration: [4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30], resolution: ["480p", "720p", "1080p"] }] },
    { id: "Doubao-Seedance-2.0", label: "豆包 Seedance 2.0", type: "video", audio: "optional", mode: ["text", "singleImage", "startEndRequired", ["imageReference:9", "videoReference:3", "audioReference:3"]], durationResolutionMap: [{ duration: [4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["480p", "720p", "1080p", "4k"] }] },
    { id: "Doubao-Seedance-2.0-fast", label: "豆包 Seedance 2.0 Fast", type: "video", audio: "optional", mode: ["text", "singleImage", "startEndRequired", ["imageReference:9", "videoReference:3", "audioReference:3"]], durationResolutionMap: [{ duration: [4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["480p", "720p"] }] },
    { id: "Doubao-Seedance-2.0-mini", label: "豆包 Seedance 2.0 Mini", type: "video", audio: "optional", mode: ["text", "singleImage", "startEndRequired", ["imageReference:9", "videoReference:3", "audioReference:3"]], durationResolutionMap: [{ duration: [4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["480p", "720p"] }] },
    { id: "wan3.0-video", label: "万相 3.0 视频", type: "video", audio: "optional", mode: ["text", "singleImage", "startEndRequired", ["imageReference:10", "videoReference:5", "audioReference:5"]], durationResolutionMap: [{ duration: [2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30], resolution: ["480p", "720p", "1080p"] }] },
    { id: "wan3.0-video-prime", label: "万相 3.0 视频 Prime", type: "video", audio: "optional", mode: ["text", "singleImage", "startEndRequired", ["imageReference:10", "videoReference:5", "audioReference:5"]], durationResolutionMap: [{ duration: [2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30], resolution: ["480p", "720p", "1080p"] }] },
    { id: "happyhorse-1.1-t2v", label: "HappyHorse 1.1 文生视频", type: "video", mode: ["text"], durationResolutionMap: [{ duration: [3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["480p", "720p", "1080p"] }] },
    { id: "happyhorse-1.1-i2v", label: "HappyHorse 1.1 图生视频", type: "video", mode: ["singleImage"], durationResolutionMap: [{ duration: [3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["480p", "720p", "1080p"] }] },
    { id: "happyhorse-1.1-r2v", label: "HappyHorse 1.1 参考生视频", type: "video", mode: [["imageReference:9"]], durationResolutionMap: [{ duration: [3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["480p", "720p", "1080p"] }] },
    { id: "happyhorse-1.0-video-edit", label: "HappyHorse 1.0 视频编辑", type: "video", mode: [["videoReference:1", "imageReference:5"]], durationResolutionMap: [{ duration: [5], resolution: ["720p", "1080p"] }] },
    { id: "vidu-q3-ad", label: "Vidu Q3 广告参考生视频", type: "video", mode: [["imageReference:7"]], durationResolutionMap: [{ duration: [3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["720p", "1080p"] }] },
    { id: "vidu-q3-drama", label: "Vidu Q3 短剧参考生视频", type: "video", mode: [["imageReference:7"]], durationResolutionMap: [{ duration: [2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], resolution: ["720p", "1080p"] }] },
    { id: "vidu-q3-pro", label: "Vidu Q3 Pro 图生视频", type: "video", audio: "optional", mode: ["singleImage"], durationResolutionMap: [{ duration: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16], resolution: ["540p", "720p", "1080p"] }] },
    { id: "vidu-q3-pro-fast", label: "Vidu Q3 Pro Fast 图生视频", type: "video", audio: "optional", mode: ["singleImage"], durationResolutionMap: [{ duration: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16], resolution: ["720p", "1080p"] }] },
    { id: "vidu-q3-turbo", label: "Vidu Q3 Turbo 图生视频", type: "video", audio: "optional", mode: ["singleImage"], durationResolutionMap: [{ duration: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16], resolution: ["540p", "720p", "1080p"] }] },
  ],

  async generateImage(this: Context, request: ImageRequest): Promise<MediaAsset[]> {
    this.signal?.throwIfAborted();
    const apiKey = readApiKey(this);
    const model = IMAGE_MODELS[request.model];
    if (!model) throw new Error("token01 没有这个图片模型：" + request.model);
    if (!request.prompt?.trim()) throw new Error("请填写提示词");
    if (request.mask) throw new Error(request.model + " 不支持蒙版局部重绘");
    if (request.quality !== undefined && request.quality !== "") throw new Error(request.model + " 不支持选择画质档位");
    if (request.outputFormat !== undefined && request.outputFormat !== "") throw new Error(request.model + " 不支持指定输出格式");
    if (Object.keys(request.other ?? {}).length) throw new Error(request.model + " 没有额外参数");
    const images = request.images ?? [];
    if (images.length > 1) throw new Error(request.model + " 只支持一张参考图");
    if (images.length && !model.edit) throw new Error(request.model + " 不支持参考图");
    if (request.size && !/^\d+\s*[x*×]\s*\d+$/i.test(request.size) && !model.sizes.includes(request.size.toUpperCase())) {
      throw new Error(request.model + " 不支持尺寸 " + request.size + "，可选：" + model.sizes.join("、"));
    }
    if (request.ratio && !IMAGE_RATIOS.includes(request.ratio)) throw new Error(request.model + " 不支持比例 " + request.ratio);
    const n = request.n ?? 1;
    if (!Number.isInteger(n) || n < 1 || n > 4) throw new Error("生成数量需在 1～4 张之间");

    const body: Record<string, unknown> = {
      model: request.model,
      prompt: request.prompt,
      size: imageSizeToPixels(request.size, request.ratio),
      n,
    };
    if (images.length) body.image = mediaToString(images[0], "参考图");

    const signal = combinedSignal(this, IMAGE_TIMEOUT_MS);
    const response = await this.tool.fetch(BASE_URL + "/v1/images/generations", {
      method: "POST",
      headers: { Authorization: "Bearer " + apiKey, "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal,
    });
    if (!response.ok) throw new Error("图片生成失败：" + (await readError(response)));
    const result = (await response.json()) as { data?: { url?: string; b64_json?: string }[] };
    const assets: MediaAsset[] = [];
    for (const item of result.data ?? []) {
      if (item.url && /^https?:\/\//i.test(item.url)) assets.push({ mediaType: "image", type: "url", url: item.url });
      else if (item.b64_json) assets.push({ mediaType: "image", type: "base64", data: item.b64_json, mimeType: "image/png" });
    }
    if (!assets.length) throw new Error("token01 没有返回图片");
    return assets;
  },

  async generateVideo(this: Context, request: VideoRequest): Promise<MediaAsset[]> {
    this.signal?.throwIfAborted();
    const apiKey = readApiKey(this);
    const cap = VIDEO_MODELS[request.model];
    if (!cap) throw new Error("token01 没有这个视频模型：" + request.model);
    if (!request.prompt?.trim() && cap.family !== "viduImage") throw new Error("请填写提示词");
    if (Object.keys(request.other ?? {}).length) throw new Error(request.model + " 没有额外参数");
    const body = buildVideoBody(request, cap);
    const headers = { Authorization: "Bearer " + apiKey, "Content-Type": "application/json" };

    const submit = await this.tool.fetch(BASE_URL + "/v1/video/generations", {
      method: "POST",
      headers,
      body: JSON.stringify(body),
      signal: combinedSignal(this, SUBMIT_TIMEOUT_MS),
    });
    if (!submit.ok) throw new Error("视频任务提交失败：" + (await readError(submit)));
    const submitted = (await submit.json()) as { id?: string; task_id?: string };
    const taskId = submitted.id || submitted.task_id;
    if (!taskId) throw new Error("token01 没有返回任务编号");

    const signal = combinedSignal(this, VIDEO_TIMEOUT_MS);
    let resultUrl = "";
    for (;;) {
      await sleep(POLL_INTERVAL_MS, signal);
      const poll = await this.tool.fetch(BASE_URL + "/v1/video/generations/" + encodeURIComponent(taskId), {
        headers: { Authorization: "Bearer " + apiKey },
        signal,
      });
      if (!poll.ok) {
        // 查询偶发失败时继续等待；任务不存在等明确错误直接报出。
        if (poll.status === 400 || poll.status === 404) throw new Error("视频任务查询失败：" + (await readError(poll)));
        continue;
      }
      const result = (await poll.json()) as { data?: { status?: string; fail_reason?: string; result_url?: string } };
      const status = result.data?.status;
      if (status === "FAILURE") throw new Error("视频生成失败：" + (result.data?.fail_reason || "未知原因") + "（费用已自动退回）");
      if (status === "SUCCESS") {
        resultUrl = result.data?.result_url ?? "";
        break;
      }
    }
    if (!/^https?:\/\//i.test(resultUrl)) throw new Error("token01 没有返回视频地址");

    // token01 的视频地址需要令牌鉴权，在这里下载后以二进制返回；其他域名的地址不携带令牌。
    const sameHost = new URL(resultUrl).host === new URL(BASE_URL).host;
    const download = await this.tool.fetch(resultUrl, {
      headers: sameHost ? { Authorization: "Bearer " + apiKey } : {},
      signal,
    });
    if (!download.ok) throw new Error("视频下载失败：" + (await readError(download)));
    const data = new Uint8Array(await download.arrayBuffer());
    if (!data.length) throw new Error("下载到的视频为空");
    const mimeType = (download.headers.get("content-type") || "video/mp4").split(";")[0].trim() || "video/mp4";
    return [{ mediaType: "video", type: "binary", data, mimeType: mimeType.startsWith("video/") ? mimeType : "video/mp4" }];
  },
};
