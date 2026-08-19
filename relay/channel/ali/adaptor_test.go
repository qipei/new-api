package ali

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAliUnifiedImageRoutingAndConversion(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:       constant.RelayModeImagesGenerations,
		OriginModelName: "vidu-q2",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://workspace.cn-beijing.maas.aliyuncs.com",
			UpstreamModelName: "vidu/vidu-image_reference2image",
		},
	}
	adaptor := &Adaptor{}
	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://workspace.cn-beijing.maas.aliyuncs.com/api/v1/services/aigc/image-generation/generation", requestURL)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", bytes.NewBuffer(nil))
	converted, err := adaptor.ConvertImageRequest(c, info, dto.ImageRequest{
		Model:  "vidu-q2",
		Prompt: "flower shop",
		Size:   "1920x1088",
		Image:  json.RawMessage(`"https://example.com/ref.png"`),
	})
	require.NoError(t, err)
	aliRequest, ok := converted.(*AliImageRequest)
	require.True(t, ok)
	assert.Equal(t, "vidu/vidu-image_reference2image", aliRequest.Model)
	assert.Equal(t, "1920*1088", aliRequest.Parameters.Size)
	input, ok := aliRequest.Input.(AliImageInput)
	require.True(t, ok)
	require.Len(t, input.Messages, 1)
	contents, ok := input.Messages[0].Content.([]AliMediaContent)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/ref.png", contents[0].Image)
	assert.Equal(t, "flower shop", contents[1].Text)
}

func TestAliImageChoicesPreserveEveryGeneratedImage(t *testing.T) {
	output := AliOutput{}
	output.Choices = append(output.Choices, struct {
		FinishReason string `json:"finish_reason,omitempty"`
		Message      struct {
			Role             string            `json:"role,omitempty"`
			Content          []AliMediaContent `json:"content,omitempty"`
			ReasoningContent string            `json:"reasoning_content,omitempty"`
		} `json:"message,omitempty"`
	}{})
	output.Choices[0].Message.Content = []AliMediaContent{
		{Text: "revised"},
		{Image: "https://example.com/1.png"},
		{Image: "https://example.com/2.png"},
	}

	images := output.ChoicesToOpenAIImageDate(nil, "url")
	require.Len(t, images, 2)
	assert.Equal(t, "https://example.com/1.png", images[0].Url)
	assert.Equal(t, "https://example.com/2.png", images[1].Url)
}

func TestConvertOpenAIRequestFiltersThinkingBudgetByUpstreamModel(t *testing.T) {
	tests := []struct {
		name          string
		requestModel  string
		upstreamModel string
		budget        string
		wantBudget    bool
		wantValue     int64
	}{
		{
			name:          "qwen",
			requestModel:  "qwen-plus",
			upstreamModel: "qwen-plus",
			budget:        "128",
			wantBudget:    true,
			wantValue:     128,
		},
		{
			name:          "qwq explicit zero",
			requestModel:  "qwq-32b",
			upstreamModel: "qwq-32b",
			budget:        "0",
			wantBudget:    true,
			wantValue:     0,
		},
		{
			name:          "unsupported upstream overrides qwen request",
			requestModel:  "qwen-plus",
			upstreamModel: "deepseek-r1",
			budget:        "128",
			wantBudget:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &dto.GeneralOpenAIRequest{
				Model:          tt.requestModel,
				EnableThinking: json.RawMessage(`true`),
				ThinkingBudget: json.RawMessage(tt.budget),
			}
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: tt.upstreamModel,
				},
			}

			convertedValue, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
			require.NoError(t, err)
			converted, ok := convertedValue.(*dto.GeneralOpenAIRequest)
			require.True(t, ok)

			if tt.wantBudget {
				assert.Equal(t, tt.budget, string(converted.ThinkingBudget))
			} else {
				assert.Nil(t, converted.ThinkingBudget)
			}

			encoded, err := common.Marshal(converted)
			require.NoError(t, err)

			assert.True(t, gjson.GetBytes(encoded, "enable_thinking").Bool())
			value := gjson.GetBytes(encoded, "thinking_budget")
			assert.Equal(t, tt.wantBudget, value.Exists())
			if tt.wantBudget {
				assert.Equal(t, tt.wantValue, value.Int())
			}
		})
	}
}

func TestConvertOpenAIRequestPreservesExplicitZeroForMappedQwenModel(t *testing.T) {
	const (
		clientModel   = "customer-model"
		upstreamModel = "Qwen/Qwen3-235B-A22B-Thinking-2507"
	)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("model_mapping", `{"customer-model":"Qwen/Qwen3-235B-A22B-Thinking-2507"}`)

	request := &dto.GeneralOpenAIRequest{
		Model:          clientModel,
		EnableThinking: json.RawMessage(`true`),
		ThinkingBudget: json.RawMessage(`0`),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: clientModel,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: clientModel,
		},
	}

	err := relayhelper.ModelMappedHelper(c, info, request)
	require.NoError(t, err)
	assert.True(t, info.IsModelMapped)
	assert.Equal(t, upstreamModel, info.UpstreamModelName)
	assert.Equal(t, upstreamModel, request.Model)

	convertedValue, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, request)
	require.NoError(t, err)
	converted, ok := convertedValue.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	assert.Equal(t, json.RawMessage(`0`), converted.ThinkingBudget)

	encoded, err := common.Marshal(converted)
	require.NoError(t, err)

	value := gjson.GetBytes(encoded, "thinking_budget")
	assert.True(t, value.Exists())
	assert.Equal(t, int64(0), value.Int())
}

func TestMappedAliImageModelUsesUpstreamProtocol(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       constant.RelayModeImagesGenerations,
		OriginModelName: "customer-image-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://dashscope.aliyuncs.com",
			UpstreamModelName: "qwen-image-3.0-pro",
		},
	}

	adaptor := &Adaptor{}
	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation", url)

	header := http.Header{}
	require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))
	assert.Empty(t, header.Get("X-DashScope-Async"))

	converted, err := adaptor.ConvertImageRequest(c, info, dto.ImageRequest{
		Model:  info.UpstreamModelName,
		Prompt: "poster",
	})
	require.NoError(t, err)
	assert.True(t, adaptor.IsSyncImageModel)
	assert.IsType(t, &AliImageRequest{}, converted)
}
