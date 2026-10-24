package common

import (
	"regexp"
	"strings"
)

var (
	// OpenAIResponseOnlyModels is a list of models that are only available for OpenAI responses.
	OpenAIResponseOnlyModels = []string{
		"o3-pro",
		"o3-deep-research",
		"o4-mini-deep-research",
	}
	ImageGenerationModels = []string{
		"dall-e-3",
		"dall-e-2",
		"gpt-image-1",
		"prefix:imagen-",
		"flux-",
		"flux.1-",
	}
	OpenAITextModels = []string{
		"gpt-",
		"o1",
		"o3",
		"o4",
		"chatgpt",
	}
)

// CUSTOM: 语音合成与音频生成模型（fork 扩展）。这些模型挂在火山、阿里等渠道下，
// 按渠道推断会被误标成对话端点，模型广场需要按「语音」单独归类。
var audioSpeechModelPattern = regexp.MustCompile(`^(seed-(audio|tts|icl)-|tts-1(-hd)?$|tts-1-hd-|gpt-4o(-mini)?-tts|qwen-audio-[\d.]+-tts|qwen3?-tts|cosyvoice-|sambert-|(minimax/)?speech-[\d.]+-(hd|turbo))`)

func IsAudioSpeechModel(modelName string) bool {
	return audioSpeechModelPattern.MatchString(strings.ToLower(strings.TrimSpace(modelName)))
}

func IsOpenAIResponseOnlyModel(modelName string) bool {
	for _, m := range OpenAIResponseOnlyModels {
		if strings.Contains(modelName, m) {
			return true
		}
	}
	return false
}

func IsImageGenerationModel(modelName string) bool {
	modelName = strings.ToLower(modelName)
	for _, m := range ImageGenerationModels {
		if strings.Contains(modelName, m) {
			return true
		}
		if strings.HasPrefix(m, "prefix:") && strings.HasPrefix(modelName, strings.TrimPrefix(m, "prefix:")) {
			return true
		}
	}
	return false
}

func IsOpenAITextModel(modelName string) bool {
	modelName = strings.ToLower(modelName)
	for _, m := range OpenAITextModels {
		if strings.Contains(modelName, m) {
			return true
		}
	}
	return false
}
