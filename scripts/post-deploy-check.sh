#!/usr/bin/env bash
# 部署后检查：用一个测试 API Key 实际调用一遍各类接口，逐项输出 PASS / FAIL。
# 清单与手动检查项见 docs/deploy-checklist.md。
#
# 用法：
#   API_KEY=sk-xxx scripts/post-deploy-check.sh
#   API_KEY=sk-xxx SKIP_VIDEO=1 scripts/post-deploy-check.sh      # 跳过最贵、最慢的视频
#
# 可选环境变量（括号内为默认值）：
#   BASE_URL     站点地址（https://www.token01.net）
#   TEXT_MODEL   文本模型（qwen3.8-flash）
#   IMAGE_MODEL  图片模型（qwen-image-3.0）
#   AUDIO_MODEL  语音模型（speech-2.8-turbo），AUDIO_VOICE 音色（male-qn-qingse）
#   VIDEO_MODEL  视频模型（MiniMax-H3）
#   SKIP_VIDEO / SKIP_UPLOAD  设为 1 跳过对应检查
#
# 会产生真实费用（文本、图片、语音、一个 4 秒 768P 视频，约 1–2 元），依赖 curl、jq；
# 装了 ffprobe 时会额外核对视频的实际时长。API Key 只放在请求头里，不会打印。

set -u

BASE_URL="${BASE_URL:-https://www.token01.net}"
BASE_URL="${BASE_URL%/}"
TEXT_MODEL="${TEXT_MODEL:-qwen3.8-flash}"
IMAGE_MODEL="${IMAGE_MODEL:-qwen-image-3.0}"
AUDIO_MODEL="${AUDIO_MODEL:-speech-2.8-turbo}"
AUDIO_VOICE="${AUDIO_VOICE:-male-qn-qingse}"
VIDEO_MODEL="${VIDEO_MODEL:-MiniMax-H3}"

if [ -z "${API_KEY:-}" ]; then
  echo "请设置 API_KEY 环境变量（建议用一个限额的测试 Key）" >&2
  exit 2
fi
for tool in curl jq; do
  command -v "$tool" >/dev/null 2>&1 || { echo "缺少 $tool" >&2; exit 2; }
done

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
AUTH="Authorization: Bearer ${API_KEY}"
PASS=0
FAIL=0
FAILED_ITEMS=""

pass() { PASS=$((PASS + 1)); printf '  \033[32mPASS\033[0m %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAILED_ITEMS="${FAILED_ITEMS}\n  - $1"; printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
section() { printf '\n== %s\n' "$1"; }

# 发请求，正文写到 $WORK/$1，返回 "状态码 耗时秒"。
request() {
  local name="$1"
  shift
  curl -s -m 180 -o "$WORK/$name" -w '%{http_code} %{time_total}' "$@"
}

section "服务状态"
read -r code took < <(request status "$BASE_URL/api/status")
if [ "$code" = 200 ] && [ "$(jq -r '.success' "$WORK/status")" = true ]; then
  pass "/api/status 正常（版本 $(jq -r '.data.version' "$WORK/status")，${took}s）"
else
  fail "/api/status 返回 $code"
fi

section "前端首屏"
read -r code took < <(request index.html "$BASE_URL/")
main_js="$(grep -oE 'src="/static/js/index\.[a-z0-9]+\.js"' "$WORK/index.html" | head -1 | sed 's/src="//;s/"$//')"
if [ "$code" = 200 ] && [ -n "$main_js" ]; then
  size=$(curl -s -m 60 -H 'Accept-Encoding: gzip' -o /dev/null -w '%{size_download}' "$BASE_URL$main_js")
  # 语言包全部打进主包时约 1.3 MB；按需加载后约 0.2 MB。超过 0.5 MB 说明首屏又变重了。
  if [ "$size" -gt 0 ] && [ "$size" -lt 524288 ]; then
    pass "首页可访问，主脚本压缩后 $((size / 1024)) KB"
  else
    fail "主脚本压缩后 $((size / 1024)) KB，超过 500 KB，首屏可能又变重了（检查语言包、图标库是否被打进主包）"
  fi
else
  fail "首页返回 $code 或找不到主脚本"
fi

section "模型列表"
read -r code took < <(request models "$BASE_URL/v1/models" -H "$AUTH")
if [ "$code" = 200 ]; then
  missing=""
  for model in "$TEXT_MODEL" "$IMAGE_MODEL" "$AUDIO_MODEL" "$VIDEO_MODEL"; do
    jq -e --arg m "$model" '.data | any(.id == $m)' "$WORK/models" >/dev/null || missing="$missing $model"
  done
  if [ -z "$missing" ]; then pass "/v1/models 包含全部测试模型"; else fail "这个 Key 看不到模型:$missing"; fi
else
  fail "/v1/models 返回 $code（Key 无效或已停用？）"
fi

section "文本 ($TEXT_MODEL)"
read -r code took < <(request text "$BASE_URL/v1/chat/completions" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"model\":\"$TEXT_MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"用一句话回答：1+1 等于几？\"}],\"max_tokens\":60}")
content="$(jq -r '.choices[0].message.content // empty' "$WORK/text" 2>/dev/null)"
if [ "$code" = 200 ] && [ -n "$content" ]; then pass "非流式 ${took}s：$content"; else fail "非流式返回 $code：$(head -c 200 "$WORK/text")"; fi

read -r code took < <(request stream "$BASE_URL/v1/chat/completions" -N -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"model\":\"$TEXT_MODEL\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"用一句话回答：天空为什么是蓝色？\"}],\"max_tokens\":80}")
chunks=$(grep -c '^data: {' "$WORK/stream")
if [ "$code" = 200 ] && [ "$chunks" -gt 1 ] && grep -q '\[DONE\]' "$WORK/stream"; then
  pass "流式 ${took}s，$chunks 段数据，正常结束"
else
  fail "流式返回 $code，$chunks 段数据，$(grep -c '\[DONE\]' "$WORK/stream") 个 [DONE]"
fi

section "图片 ($IMAGE_MODEL)"
# 故意用百炼原生的 1024*1024：OpenAI 兼容的中转线路只认 1024x1024，网关要负责转换。
read -r code took < <(request image "$BASE_URL/v1/images/generations" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"model\":\"$IMAGE_MODEL\",\"prompt\":\"一个红色的苹果放在白色桌面上\",\"size\":\"1024*1024\",\"n\":1}")
image_url="$(jq -r '.data[0].url // empty' "$WORK/image" 2>/dev/null)"
if [ "$code" = 200 ] && [ -n "$image_url" ]; then
  dl=$(curl -s -m 60 -o "$WORK/image.bin" -w '%{http_code}' "$image_url")
  magic="$(head -c 4 "$WORK/image.bin" | od -An -tx1 | tr -d ' \n')"
  case "$magic" in 89504e47 | ffd8ff* | 52494646) is_image=1 ;; *) is_image=0 ;; esac
  if [ "$dl" = 200 ] && [ "$is_image" = 1 ]; then
    pass "生成并下载图片成功，$(($(wc -c <"$WORK/image.bin") / 1024)) KB（${took}s）"
  else
    fail "图片链接下载返回 $dl 或不是图片"
  fi
else
  fail "图片生成返回 $code：$(head -c 200 "$WORK/image")"
fi

section "语音 ($AUDIO_MODEL)"
read -r code took < <(request audio "$BASE_URL/v1/audio/speech" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"model\":\"$AUDIO_MODEL\",\"input\":\"您好，这是一次部署后检查。\",\"voice\":\"$AUDIO_VOICE\"}")
audio_head="$(head -c 3 "$WORK/audio" | od -An -tx1 | tr -d ' \n')"
if [ "$code" = 200 ] && { [ "$audio_head" = "494433" ] || [ "${audio_head:0:3}" = "fff" ]; } && [ "$(wc -c <"$WORK/audio")" -gt 1000 ]; then
  pass "合成 MP3 $(($(wc -c <"$WORK/audio") / 1024)) KB（${took}s）"
else
  fail "语音返回 $code：$(head -c 200 "$WORK/audio")"
fi

if [ "${SKIP_UPLOAD:-0}" != 1 ]; then
  section "图片上传 (/v1/uploads/images)"
  # 生成一张 1x1 的 PNG 作为上传样本。
  printf '\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x35\x81\x84\x00\x00\x00\x00IEND\xaeB`\x82' >"$WORK/sample.png"
  read -r code took < <(request upload "$BASE_URL/v1/uploads/images" -H "$AUTH" -F "file=@$WORK/sample.png")
  upload_url="$(jq -r '.url // empty' "$WORK/upload" 2>/dev/null)"
  if [ "$code" = 200 ] && [ -n "$upload_url" ]; then
    curl -s -m 60 -o "$WORK/upload.bin" "$upload_url"
    if cmp -s "$WORK/sample.png" "$WORK/upload.bin"; then pass "上传成功，下载内容一致（${took}s）"; else fail "上传成功但下载内容不一致"; fi
  else
    fail "上传返回 $code：$(head -c 200 "$WORK/upload")（503 disabled 表示后台未开启文件存储，403 表示账户没有余额）"
  fi
  printf 'not an image' >"$WORK/fake.png"
  read -r code took < <(request upload_fake "$BASE_URL/v1/uploads/images" -H "$AUTH" -F "file=@$WORK/fake.png")
  if [ "$code" = 400 ]; then pass "非图片文件被拒绝（400）"; else fail "非图片文件返回 $code，应为 400"; fi
fi

if [ "${SKIP_VIDEO:-0}" != 1 ]; then
  section "视频 ($VIDEO_MODEL，768P 4 秒，最长等 15 分钟)"
  # 只传 seconds：经 TOK 等 Sora 格式线路时网关要补 duration，否则会按默认 5 秒出片和计费。
  read -r code took < <(request video "$BASE_URL/v1/video/generations" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"model\":\"$VIDEO_MODEL\",\"prompt\":\"清晨的湖面，薄雾缓缓飘动，一只白鹭掠过水面，鸟鸣声\",\"seconds\":\"4\",\"size\":\"768P\",\"metadata\":{\"parameters\":{\"ratio\":\"16:9\"}}}")
  task_id="$(jq -r '.task_id // .id // empty' "$WORK/video" 2>/dev/null)"
  if [ "$code" != 200 ] || [ -z "$task_id" ]; then
    fail "提交返回 $code：$(head -c 200 "$WORK/video")"
  else
    pass "提交成功（${took}s），任务 $task_id"
    status=""
    for _ in $(seq 1 60); do
      sleep 15
      curl -s -m 30 -o "$WORK/video_status" "$BASE_URL/v1/videos/$task_id" -H "$AUTH"
      status="$(jq -r '.status // empty' "$WORK/video_status" 2>/dev/null)"
      case "$status" in completed | failed | cancelled) break ;; esac
    done
    if [ "$status" != completed ]; then
      fail "任务状态 ${status:-超时未结束}：$(head -c 200 "$WORK/video_status")"
    else
      returned_seconds="$(jq -r '.seconds' "$WORK/video_status")"
      if [ "$returned_seconds" = 4 ]; then pass "出片完成，任务时长 4 秒"; else fail "请求 4 秒，任务显示 ${returned_seconds} 秒（时长字段没传到上游，会多收费）"; fi
      dl=$(curl -s -m 120 -o "$WORK/video.mp4" -w '%{http_code}' "$BASE_URL/v1/videos/$task_id/content" -H "$AUTH")
      if [ "$dl" = 200 ] && [ "$(wc -c <"$WORK/video.mp4")" -gt 10000 ]; then
        pass "通过网关下载视频 $(($(wc -c <"$WORK/video.mp4") / 1024)) KB"
        if command -v ffprobe >/dev/null 2>&1; then
          duration="$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$WORK/video.mp4")"
          if awk -v d="$duration" 'BEGIN { exit !(d >= 3.9 && d < 5) }'; then pass "实际时长 ${duration}s"; else fail "实际时长 ${duration}s，不是 4 秒档"; fi
        fi
      else
        fail "视频下载返回 $dl"
      fi
      # 完成后若出现「adaptor计费调整」，说明实际出片秒数与请求不一致被补扣了。
      curl -s -m 30 -o "$WORK/logs" "$BASE_URL/api/log/token" -H "$AUTH"
      if jq -e --arg id "$task_id" '[(.data | if type == "array" then . else (.items // []) end)[] | select((.other // "") | tostring | contains($id)) | select(.content == "adaptor计费调整")] | length == 0' "$WORK/logs" >/dev/null 2>&1; then
        pass "计费没有完成后补扣"
      else
        fail "出现了完成后补扣（按实际秒数多收了费），检查时长字段"
      fi
    fi
  fi
fi

section "汇总"
printf '  通过 %d 项，失败 %d 项\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  失败项：%b\n' "$FAILED_ITEMS"
  exit 1
fi
