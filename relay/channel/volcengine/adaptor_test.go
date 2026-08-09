package volcengine

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertImageRequestPreservesArkParametersWithoutInternalN(t *testing.T) {
	request := dto.ImageRequest{
		Model:  "doubao-seedream",
		Prompt: "cat",
		Size:   "2048x2048",
		N:      common.GetPointer(uint(4)),
		Extra: map[string]json.RawMessage{
			"seed":                                json.RawMessage(`0`),
			"sequential_image_generation":         json.RawMessage(`"auto"`),
			"sequential_image_generation_options": json.RawMessage(`{"max_images":4}`),
			"guidance_scale":                      json.RawMessage(`5.5`),
		},
	}
	converted, err := (&Adaptor{}).ConvertImageRequest(nil, &relaycommon.RelayInfo{RelayMode: constant.RelayModeImagesGenerations}, request)
	require.NoError(t, err)
	payload, ok := converted.(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, payload, "n")
	assert.Equal(t, float64(0), payload["seed"])
	assert.Equal(t, "auto", payload["sequential_image_generation"])
	options, ok := payload["sequential_image_generation_options"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(4), options["max_images"])
}
