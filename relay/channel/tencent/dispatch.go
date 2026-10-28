package tencent

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
)

const tokenHubBaseURL = "https://tokenhub.tencentmaas.com"

// DispatchAdaptor 按密钥格式分流:三段式 ak/sk 走原生 TC3,单段 TokenHub key 走 OpenAI 兼容。
type DispatchAdaptor struct {
	channel.Adaptor
}

func (a *DispatchAdaptor) Init(info *relaycommon.RelayInfo) {
	if strings.Contains(info.ApiKey, "|") {
		a.Adaptor = &Adaptor{}
	} else {
		a.Adaptor = &openai.Adaptor{}
		// CUSTOM: IndexTTS-2 语音合成不是 OpenAI 兼容协议，单独换算（fork 扩展）。
		if info.RelayMode == relayconstant.RelayModeAudioSpeech && IsTokenHubSpeechModel(info.UpstreamModelName) {
			a.Adaptor = &tokenHubSpeechAdaptor{}
		}
		if info.ChannelBaseUrl == "" || info.ChannelBaseUrl == constant.ChannelBaseURLs[constant.ChannelTypeTencent] {
			info.ChannelBaseUrl = tokenHubBaseURL
		}
	}
	a.Adaptor.Init(info)
}

func (a *DispatchAdaptor) GetModelList() []string {
	return ModelList
}

func (a *DispatchAdaptor) GetChannelName() string {
	return ChannelName
}
