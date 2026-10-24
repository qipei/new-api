package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/voice_catalog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetVoiceCatalog(t *testing.T) {
	t.Helper()
	model.DB.Exec("DELETE FROM voice_catalog_entries")
	volcVoiceCacheMu.Lock()
	volcVoiceCache = map[string]cachedVoices{}
	volcVoiceCacheMu.Unlock()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM voice_catalog_entries")
		volcVoiceCacheMu.Lock()
		volcVoiceCache = map[string]cachedVoices{}
		volcVoiceCacheMu.Unlock()
	})
}

func withVolcSettings(t *testing.T, resourceIDs ...string) {
	t.Helper()
	settings := voice_catalog.GetSettings()
	original := *settings
	settings.VolcAccessKeyID = "AKLTtest"
	settings.VolcSecretAccessKey = "secret"
	settings.VolcResourceIDs = resourceIDs
	t.Cleanup(func() { *settings = original })
}

// volcSpeakerServer 模拟 ListSpeakers：按页返回 total 个音色，并校验请求已签名。
func volcSpeakerServer(t *testing.T, total int, failResource string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "ListSpeakers", r.URL.Query().Get("Action"))
		assert.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "HMAC-SHA256 Credential=AKLTtest/"))
		assert.Contains(t, r.Header.Get("Authorization"), "/cn-beijing/speech_saas_prod/request")
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			ResourceIDs []string `json:"ResourceIDs"`
			Page        int      `json:"Page"`
			Limit       int      `json:"Limit"`
		}
		require.NoError(t, common.Unmarshal(raw, &req))
		if req.ResourceIDs[0] == failResource {
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"InvalidParameter","Message":"bad resource"}}}`))
			return
		}
		assert.Equal(t, volcSpeakerPageSize, req.Limit, "Limit must be sent as a number")
		limit := volcSpeakerPageSize
		var speakers []string
		if req.ResourceIDs[0] == "with-offline-and-duplicate" && req.Page == 1 {
			speakers = append(speakers,
				`{"VoiceType":"voice_0","Name":"重复","Status":"online"}`,
				`{"VoiceType":"gone","Name":"下线","Status":"offline"}`)
		}
		for i := (req.Page - 1) * limit; i < min(req.Page*limit, total); i++ {
			speakers = append(speakers, fmt.Sprintf(`{"VoiceType":"voice_%d","Name":"音色%d","Gender":"女","Age":"青年",
				"Categories":[{"Categories":["多语种","通用"]}],"NormalLabels":["温柔"],"SpecialLabels":[{"Label":"热门"}],
				"TrialURL":"https://cdn/voice_%d.mp3","Status":"online","Languages":[{"Language":"zh-cn","Text":"你好"}],
				"Emotions":[{"Label":"开心","Value":"happy"}],"ResourceID":"%s"}`, i, i, i, req.ResourceIDs[0]))
		}
		_, _ = fmt.Fprintf(w, `{"Result":{"Total":%d,"Speakers":[%s]}}`, total, strings.Join(speakers, ","))
	}))
	original := volcListSpeakersEndpoint
	volcListSpeakersEndpoint = server.URL + "/?Action=ListSpeakers&Version=2025-05-20"
	t.Cleanup(func() {
		volcListSpeakersEndpoint = original
		server.Close()
	})
	return server
}

func TestVoiceCatalogRulesPerModel(t *testing.T) {
	resetVoiceCatalog(t)
	tests := []struct {
		model      string
		wantKind   string
		wantVoices int
		provider   string
	}{
		{"qwen-audio-3.1-tts-plus", VoiceCatalogKindVoices, 4, voice_catalog.ProviderBailian},
		{"qwen-audio-3.1-tts-flash", VoiceCatalogKindVoices, 68, voice_catalog.ProviderBailian},
		{"qwen-audio-3.1-tts-next", VoiceCatalogKindReferenceAudio, 0, voice_catalog.ProviderBailian},
		{"seed-icl-2.0", VoiceCatalogKindClonedVoices, 0, voice_catalog.ProviderVolc},
		{"seed-tts-2.0", VoiceCatalogKindVoices, 0, voice_catalog.ProviderVolc},
		{"seed-audio-1.0", VoiceCatalogKindVoices, 0, voice_catalog.ProviderVolc},
		// MiniMax 四个模型共用一份系统音色清单，对外名带不带 MiniMax/ 前缀都能对上。
		{"MiniMax/speech-2.8-hd", VoiceCatalogKindVoices, 327, voice_catalog.ProviderMiniMax},
		{"speech-02-turbo", VoiceCatalogKindVoices, 327, voice_catalog.ProviderMiniMax},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			catalog, ok, err := GetModelVoiceCatalog(tt.model)
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, tt.wantKind, catalog.Kind)
			assert.Equal(t, tt.provider, catalog.Provider)
			assert.Len(t, catalog.Voices, tt.wantVoices)
			assert.NotEmpty(t, catalog.DocsURL)
		})
	}
	_, ok, err := GetModelVoiceCatalog("gpt-4o")
	require.NoError(t, err)
	assert.False(t, ok)
}

// 百炼 plus 只能用文档列出的 4 个音色（实测其余音色会被上游拒绝），清单不能混入 flash 的音色。
func TestBailianPlusCatalogMatchesVerifiedVoices(t *testing.T) {
	catalog, _, err := GetModelVoiceCatalog("qwen-audio-3.1-tts-plus")
	require.NoError(t, err)
	var ids []string
	for _, voice := range catalog.Voices {
		ids = append(ids, voice.Voice)
		assert.NotEmpty(t, voice.Samples, voice.Voice)
	}
	assert.ElementsMatch(t, []string{"longanlingxin_v3.1", "xunanchuan_v3.1", "longanhuan_v3.1", "longanfengyue_v3.1"}, ids)
	assert.Equal(t, "2026-10-23", catalog.UpdatedAt)
}

func TestSyncVolcVoiceCatalogPagesAndMaps(t *testing.T) {
	resetVoiceCatalog(t)
	withVolcSettings(t, "seed-tts-2.0")
	volcSpeakerServer(t, volcSpeakerPageSize+5, "")

	result, err := SyncVolcVoiceCatalog(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"seed-tts-2.0": volcSpeakerPageSize + 5}, result.Counts)

	catalog, _, err := GetModelVoiceCatalog("seed-tts-2.0")
	require.NoError(t, err)
	require.Len(t, catalog.Voices, volcSpeakerPageSize+5)
	first := catalog.Voices[0]
	assert.Equal(t, voice_catalog.Voice{
		Voice: "voice_0", Name: "音色0", Gender: "female", Age: "青年", Scenes: []string{"多语种", "通用"},
		Labels: []string{"温柔", "热门"}, Languages: []string{"zh-cn"},
		Emotions: []voice_catalog.Emotion{{Label: "开心", Value: "happy"}},
		Samples:  []voice_catalog.Sample{{Text: "你好", URL: "https://cdn/voice_0.mp3"}},
	}, first)
	assert.NotEmpty(t, catalog.UpdatedAt)
}

// 同步是整体替换：下架的音色被删除；某个模型同步失败时保留它的旧数据。
func TestSyncVolcVoiceCatalogReplacesAndKeepsDataOnFailure(t *testing.T) {
	resetVoiceCatalog(t)
	withVolcSettings(t, "seed-tts-2.0", "seed-tts-1.0")
	volcSpeakerServer(t, 3, "")
	_, err := SyncVolcVoiceCatalog(context.Background())
	require.NoError(t, err)

	volcSpeakerServer(t, 2, "seed-tts-1.0")
	result, err := SyncVolcVoiceCatalog(context.Background())
	require.Error(t, err)
	assert.Equal(t, map[string]int{"seed-tts-2.0": 2}, result.Counts)
	assert.Contains(t, result.Errors["seed-tts-1.0"], "InvalidParameter")

	replaced, err := model.ListVoiceCatalog(voice_catalog.ProviderVolc, "seed-tts-2.0")
	require.NoError(t, err)
	assert.Len(t, replaced, 2, "voices removed upstream must be deleted")
	kept, err := model.ListVoiceCatalog(voice_catalog.ProviderVolc, "seed-tts-1.0")
	require.NoError(t, err)
	assert.Len(t, kept, 3, "a failed sync must keep the previous snapshot")
}

func TestSyncVolcVoiceCatalogRejectsEmptySnapshot(t *testing.T) {
	resetVoiceCatalog(t)
	withVolcSettings(t, "seed-tts-2.0")
	volcSpeakerServer(t, 3, "")
	_, err := SyncVolcVoiceCatalog(context.Background())
	require.NoError(t, err)

	volcSpeakerServer(t, 0, "")
	_, err = SyncVolcVoiceCatalog(context.Background())
	require.Error(t, err)
	kept, err := model.ListVoiceCatalog(voice_catalog.ProviderVolc, "seed-tts-2.0")
	require.NoError(t, err)
	assert.Len(t, kept, 3)
}

func TestSyncVolcVoiceCatalogRequiresCredentials(t *testing.T) {
	settings := voice_catalog.GetSettings()
	original := *settings
	settings.VolcAccessKeyID, settings.VolcSecretAccessKey = "", ""
	t.Cleanup(func() { *settings = original })
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()

	_, err := SyncVolcVoiceCatalog(context.Background())
	require.Error(t, err)
	assert.Zero(t, calls.Load())
}

// 实测上游会重复返回同一音色；下线的音色也不能展示。重复 ID 若写库会违反唯一索引导致整次同步失败。
func TestSyncVolcVoiceCatalogSkipsDuplicatesAndOffline(t *testing.T) {
	resetVoiceCatalog(t)
	withVolcSettings(t, "with-offline-and-duplicate")
	volcSpeakerServer(t, 3, "")

	result, err := SyncVolcVoiceCatalog(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"with-offline-and-duplicate": 3}, result.Counts)
	entries, err := model.ListVoiceCatalog(voice_catalog.ProviderVolc, "with-offline-and-duplicate")
	require.NoError(t, err)
	var ids []string
	for _, entry := range entries {
		ids = append(ids, entry.VoiceID)
	}
	assert.Equal(t, []string{"voice_0", "voice_1", "voice_2"}, ids)
}
