package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_billing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 配了价格矩阵的模型经对话接口调用时返回 400，并按模型类型指向正确的接口；对话模型不受影响。
func TestMediaModelOnChatEndpointError(t *testing.T) {
	settings, ok := config.GlobalConfig.Get("video_billing").(*video_billing.VideoBillingSettings)
	require.True(t, ok)
	original := settings.PriceTables
	tier := []video_billing.PriceTier{{Price: 0.1}}
	settings.PriceTables = map[string]video_billing.ModelPriceTable{
		"MiniMax-H3":          {Unit: video_billing.UnitPerSecond, Tiers: tier},
		"Doubao-Seedance-2.0": {Unit: video_billing.UnitPerMillionToken, Tiers: tier},
		"qwen-image-3.0":      {Unit: video_billing.UnitPerImage, Tiers: tier},
		"seed-tts-2.0":        {Unit: video_billing.UnitPer10kChars, Tiers: tier},
		"seed-audio-1.0":      {Unit: video_billing.UnitPerSecond, Tiers: tier},
	}
	t.Cleanup(func() { settings.PriceTables = original })

	tests := []struct {
		model    string
		endpoint string
	}{
		{"MiniMax-H3", "POST /v1/video/generations"},
		{"Doubao-Seedance-2.0", "POST /v1/video/generations"},
		{"qwen-image-3.0", "POST /v1/images/generations"},
		{"seed-tts-2.0", "POST /v1/audio/speech"},
		{"seed-audio-1.0", "POST /v1/audio/speech"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			err := MediaModelOnChatEndpointError(tt.model)
			require.NotNil(t, err)
			assert.Equal(t, http.StatusBadRequest, err.StatusCode)
			assert.Contains(t, err.Error(), tt.model)
			assert.Contains(t, err.Error(), tt.endpoint)
		})
	}

	assert.Nil(t, MediaModelOnChatEndpointError("gpt-4o"))
}
