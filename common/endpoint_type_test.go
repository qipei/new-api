package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"

	"github.com/stretchr/testify/assert"
)

// 语音模型挂在火山、阿里等渠道下，按渠道推断会被误标成对话端点；
// 模型广场要按「语音」单独筛选，所以只能标成语音端点。
func TestGetEndpointTypesMarksSpeechModels(t *testing.T) {
	speech := []constant.EndpointType{constant.EndpointTypeAudioSpeech}
	tests := []struct {
		channelType int
		model       string
		want        []constant.EndpointType
	}{
		{constant.ChannelTypeVolcEngine, "seed-audio-1.0", speech},
		{constant.ChannelTypeVolcEngine, "seed-tts-2.0", speech},
		{constant.ChannelTypeVolcEngine, "seed-icl-2.0", speech},
		{constant.ChannelTypeAli, "qwen-audio-3.1-tts-plus", speech},
		{constant.ChannelTypeAli, "qwen-audio-3.1-tts-next", speech},
		{constant.ChannelTypeAli, "qwen3-tts-flash", speech},
		{constant.ChannelTypeOpenAI, "tts-1", speech},
		{constant.ChannelTypeOpenAI, "tts-1-hd", speech},
		{constant.ChannelTypeOpenAI, "gpt-4o-mini-tts", speech},
		{constant.ChannelTypeNewAPI, "seed-tts-2.0", speech},
		// 不是语音合成的同族模型不能被误判。
		{constant.ChannelTypeVolcEngine, "doubao-seedance-2-0-260128", []constant.EndpointType{constant.EndpointTypeOpenAI}},
		{constant.ChannelTypeVolcEngine, "doubao-seed-1-6-thinking-250715", []constant.EndpointType{constant.EndpointTypeOpenAI}},
		{constant.ChannelTypeAli, "qwen-audio-asr", []constant.EndpointType{constant.EndpointTypeOpenAI}},
		{constant.ChannelTypeOpenAI, "gpt-4o-mini", []constant.EndpointType{constant.EndpointTypeOpenAI}},
		{constant.ChannelTypeOpenAI, "gpt-image-1", []constant.EndpointType{constant.EndpointTypeImageGeneration, constant.EndpointTypeOpenAI}},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.want, GetEndpointTypesByChannelType(tt.channelType, tt.model))
		})
	}
}

func TestAudioSpeechEndpointHasDefaultPath(t *testing.T) {
	info, ok := GetDefaultEndpointInfo(constant.EndpointTypeAudioSpeech)
	assert.True(t, ok)
	assert.Equal(t, EndpointInfo{Path: "/v1/audio/speech", Method: "POST"}, info)
}
