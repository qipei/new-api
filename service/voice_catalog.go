// CUSTOM: 模型广场的预置音色目录（fork 扩展），设计说明见 setting/voice_catalog。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/volcopenapi"
	"github.com/QuantumNous/new-api/setting/voice_catalog"
)

// 模型音色目录的三种形态：有预置音色列表；只能用参考音频；只能用自己复刻的音色。
const (
	VoiceCatalogKindVoices         = "voices"
	VoiceCatalogKindReferenceAudio = "reference_audio"
	VoiceCatalogKindClonedVoices   = "cloned_voices"
)

const (
	bailianVoiceDocURL      = "https://docs.bailian.console.aliyun.com/zh/model-studio/qwen-audio-tts-voice-list"
	bailianAudioGenDocURL   = "https://docs.bailian.console.aliyun.com/zh/model-studio/audio-generation-api"
	miniMaxVoiceDocURL      = "https://platform.minimax.cn/docs/faq/system-voice-id"
	volcVoiceLibraryURL     = "https://console.volcengine.com/speech/new/voices"
	volcVoiceCloneDocURL    = "https://docs.volcengine.com/docs/DoubaoVoice/tone-training-http?lang=zh"
	volcSpeakerPageSize     = 100
	volcSpeakerMaxPages     = 50
	voiceCatalogCacheTTL    = 10 * time.Minute
	volcListSpeakersTimeout = 30 * time.Second
)

// voiceCatalogRule 把平台模型名对应到音色来源。按模型名匹配，和接入示例的做法一致。
type voiceCatalogRule struct {
	pattern  *regexp.Regexp
	provider string
	// source 百炼为清单中的模型名，火山为 ListSpeakers 的 ResourceID，MiniMax 四个模型
	// 共用一份清单（任意非空值）；无预置音色时为空。
	source  string
	kind    string
	docsURL string
	// noteKey 页面上额外提示的文案（前端按 i18n key 翻译），没有时为空。
	noteKey string
}

// volcListSpeakersEndpoint 火山 OpenAPI 的 ListSpeakers 地址，测试时替换为本地服务。
var volcListSpeakersEndpoint = "https://open.volcengineapi.com/?Action=ListSpeakers&Version=2025-05-20"

var voiceCatalogRules = []voiceCatalogRule{
	{regexp.MustCompile(`(?i)^qwen-audio-3\.1-tts-plus`), voice_catalog.ProviderBailian, "qwen-audio-3.1-tts-plus", VoiceCatalogKindVoices, bailianVoiceDocURL, ""},
	{regexp.MustCompile(`(?i)^qwen-audio-3\.1-tts-flash`), voice_catalog.ProviderBailian, "qwen-audio-3.1-tts-flash", VoiceCatalogKindVoices, bailianVoiceDocURL, ""},
	{regexp.MustCompile(`(?i)^qwen-audio-[\d.]+-tts-next`), voice_catalog.ProviderBailian, "", VoiceCatalogKindReferenceAudio, bailianAudioGenDocURL, ""},
	{regexp.MustCompile(`(?i)^seed-tts-2\.0`), voice_catalog.ProviderVolc, "seed-tts-2.0", VoiceCatalogKindVoices, volcVoiceLibraryURL, ""},
	// seed-audio 没有独立的音色列表；实测抽样 15 个豆包 2.0 音色有 14 个可用。
	{regexp.MustCompile(`(?i)^seed-audio-`), voice_catalog.ProviderVolc, "seed-tts-2.0", VoiceCatalogKindVoices, volcVoiceLibraryURL,
		"These are the Doubao 2.0 voices. Most of them work with this model, but a few are not supported; confirm with a test call."},
	{regexp.MustCompile(`(?i)^seed-icl-`), voice_catalog.ProviderVolc, "", VoiceCatalogKindClonedVoices, volcVoiceCloneDocURL, ""},
	{regexp.MustCompile(`(?i)^(minimax/)?speech-[\d.]+-(hd|turbo)`), voice_catalog.ProviderMiniMax, "system", VoiceCatalogKindVoices, miniMaxVoiceDocURL, ""},
}

// ModelVoiceCatalog 某个模型可用的预置音色。
type ModelVoiceCatalog struct {
	Model     string                `json:"model"`
	Provider  string                `json:"provider"`
	Kind      string                `json:"kind"`
	DocsURL   string                `json:"docs_url"`
	NoteKey   string                `json:"note_key,omitempty"`
	UpdatedAt string                `json:"updated_at,omitempty"`
	Voices    []voice_catalog.Voice `json:"voices"`
}

type cachedVoices struct {
	voices    []voice_catalog.Voice
	updatedAt string
	loadedAt  time.Time
}

var (
	volcVoiceCacheMu sync.Mutex
	volcVoiceCache   = map[string]cachedVoices{}
)

// GetModelVoiceCatalog 返回模型的音色目录；模型不是语音模型时 ok 为 false。
func GetModelVoiceCatalog(modelName string) (catalog ModelVoiceCatalog, ok bool, err error) {
	for _, rule := range voiceCatalogRules {
		if !rule.pattern.MatchString(modelName) {
			continue
		}
		catalog = ModelVoiceCatalog{Model: modelName, Provider: rule.provider, Kind: rule.kind, DocsURL: rule.docsURL, NoteKey: rule.noteKey, Voices: []voice_catalog.Voice{}}
		if rule.source == "" {
			return catalog, true, nil
		}
		if rule.provider == voice_catalog.ProviderBailian {
			voices, _, updatedAt := voice_catalog.BailianVoices(rule.source)
			catalog.Voices, catalog.UpdatedAt = voices, updatedAt
			return catalog, true, nil
		}
		if rule.provider == voice_catalog.ProviderMiniMax {
			catalog.Voices, catalog.UpdatedAt = voice_catalog.MiniMaxVoices()
			return catalog, true, nil
		}
		voices, updatedAt, err := volcVoices(rule.source)
		if err != nil {
			return catalog, true, err
		}
		catalog.Voices, catalog.UpdatedAt = voices, updatedAt
		return catalog, true, nil
	}
	return catalog, false, nil
}

// volcVoices 读取已同步的火山音色。缓存带过期时间：多实例部署时其他实例完成的同步
// 也能在一个周期内生效。
func volcVoices(resourceID string) ([]voice_catalog.Voice, string, error) {
	volcVoiceCacheMu.Lock()
	cached, hit := volcVoiceCache[resourceID]
	volcVoiceCacheMu.Unlock()
	if hit && time.Since(cached.loadedAt) < voiceCatalogCacheTTL {
		return cached.voices, cached.updatedAt, nil
	}

	entries, err := model.ListVoiceCatalog(voice_catalog.ProviderVolc, resourceID)
	if err != nil {
		return nil, "", err
	}
	voices := make([]voice_catalog.Voice, 0, len(entries))
	var updatedAt int64
	for _, entry := range entries {
		var voice voice_catalog.Voice
		if err := common.UnmarshalJsonStr(entry.Data, &voice); err != nil {
			continue
		}
		voices = append(voices, voice)
		updatedAt = max(updatedAt, entry.UpdatedAt)
	}
	updated := ""
	if updatedAt > 0 {
		updated = time.Unix(updatedAt, 0).Format("2006-01-02 15:04")
	}
	volcVoiceCacheMu.Lock()
	volcVoiceCache[resourceID] = cachedVoices{voices: voices, updatedAt: updated, loadedAt: time.Now()}
	volcVoiceCacheMu.Unlock()
	return voices, updated, nil
}

// VoiceCatalogSyncResult 一次同步的结果，记在系统任务里供后台查看。
type VoiceCatalogSyncResult struct {
	Counts map[string]int    `json:"counts"`
	Errors map[string]string `json:"errors,omitempty"`
}

// SyncVolcVoiceCatalog 按配置的模型逐个拉取全部分页，成功后整体替换入库。
// 某个模型失败时保留它的旧数据，不影响其他模型。
func SyncVolcVoiceCatalog(ctx context.Context) (VoiceCatalogSyncResult, error) {
	settings := voice_catalog.GetSettings()
	result := VoiceCatalogSyncResult{Counts: map[string]int{}, Errors: map[string]string{}}
	if !settings.VolcConfigured() {
		return result, errors.New("volcengine access key is not configured")
	}
	cred := volcopenapi.Credential{
		AccessKeyID:     strings.TrimSpace(settings.VolcAccessKeyID),
		SecretAccessKey: strings.TrimSpace(settings.VolcSecretAccessKey),
		Region:          "cn-beijing",
		Service:         "speech_saas_prod",
	}
	for _, resourceID := range settings.ResourceIDs() {
		voices, err := listVolcSpeakers(ctx, cred, resourceID)
		if err == nil {
			err = storeVolcVoices(resourceID, voices)
		}
		if err != nil {
			result.Errors[resourceID] = err.Error()
			continue
		}
		result.Counts[resourceID] = len(voices)
	}
	if len(result.Errors) > 0 {
		return result, fmt.Errorf("voice catalog sync failed for %d resource(s)", len(result.Errors))
	}
	return result, nil
}

func storeVolcVoices(resourceID string, voices []voice_catalog.Voice) error {
	entries := make([]model.VoiceCatalogEntry, 0, len(voices))
	for _, voice := range voices {
		data, err := common.Marshal(voice)
		if err != nil {
			return err
		}
		entries = append(entries, model.VoiceCatalogEntry{VoiceID: voice.Voice, Data: string(data)})
	}
	if err := model.ReplaceVoiceCatalog(voice_catalog.ProviderVolc, resourceID, entries); err != nil {
		return err
	}
	volcVoiceCacheMu.Lock()
	delete(volcVoiceCache, resourceID)
	volcVoiceCacheMu.Unlock()
	return nil
}

type volcListSpeakersResponse struct {
	ResponseMetadata struct {
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"ResponseMetadata"`
	Result struct {
		Total    int           `json:"Total"`
		Speakers []volcSpeaker `json:"Speakers"`
	} `json:"Result"`
}

type volcSpeaker struct {
	VoiceType     string `json:"VoiceType"`
	Name          string `json:"Name"`
	Avatar        string `json:"Avatar"`
	Gender        string `json:"Gender"`
	Age           string `json:"Age"`
	Description   string `json:"Description"`
	TrialURL      string `json:"TrialURL"`
	Status        string `json:"Status"`
	ShortTrialURL string `json:"ShortTrialURL"`
	Categories    []struct {
		Categories []string `json:"Categories"`
	} `json:"Categories"`
	NormalLabels  []json.RawMessage `json:"NormalLabels"`
	SpecialLabels []json.RawMessage `json:"SpecialLabels"`
	Languages     []struct {
		Language string `json:"Language"`
		Text     string `json:"Text"`
	} `json:"Languages"`
	Emotions []struct {
		Label string `json:"Label"`
		Value string `json:"Value"`
	} `json:"Emotions"`
}

func listVolcSpeakers(ctx context.Context, cred volcopenapi.Credential, resourceID string) ([]voice_catalog.Voice, error) {
	var voices []voice_catalog.Voice
	// 实测上游会重复返回同一个音色（seed-tts-1.0 有重复的 VoiceType），按 ID 去重保留第一条。
	seen := map[string]bool{}
	for page := 1; page <= volcSpeakerMaxPages; page++ {
		resp, err := callVolcListSpeakers(ctx, cred, resourceID, page)
		if err != nil {
			return nil, err
		}
		for _, speaker := range resp.Result.Speakers {
			// 下线的音色不再展示；实测线上数据目前全是 online。
			if speaker.VoiceType == "" || seen[speaker.VoiceType] || (speaker.Status != "" && speaker.Status != "online") {
				continue
			}
			seen[speaker.VoiceType] = true
			voices = append(voices, volcSpeakerToVoice(speaker))
		}
		if len(resp.Result.Speakers) < volcSpeakerPageSize || page*volcSpeakerPageSize >= resp.Result.Total {
			if len(voices) == 0 {
				return nil, fmt.Errorf("ListSpeakers returned no voices for %s", resourceID)
			}
			return voices, nil
		}
	}
	return nil, fmt.Errorf("ListSpeakers for %s exceeded %d pages", resourceID, volcSpeakerMaxPages)
}

func callVolcListSpeakers(ctx context.Context, cred volcopenapi.Credential, resourceID string, page int) (*volcListSpeakersResponse, error) {
	body, err := common.Marshal(map[string]any{
		"ResourceIDs": []string{resourceID},
		"Page":        page,
		// 文档示例把 Limit 写成字符串，实测必须是数字，传字符串会报 InvalidParameter。
		"Limit": volcSpeakerPageSize,
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, volcListSpeakersTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, volcListSpeakersEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	volcopenapi.Sign(req, body, cred, time.Now())

	client, err := GetHttpClientWithProxy("")
	if err != nil {
		return nil, err
	}
	httpResp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var resp volcListSpeakersResponse
	if err := common.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("ListSpeakers returned HTTP %d: %s", httpResp.StatusCode, truncateForError(raw))
	}
	if apiErr := resp.ResponseMetadata.Error; apiErr != nil && apiErr.Code != "" {
		return nil, fmt.Errorf("ListSpeakers error %s: %s", apiErr.Code, apiErr.Message)
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ListSpeakers returned HTTP %d", httpResp.StatusCode)
	}
	return &resp, nil
}

func truncateForError(raw []byte) string {
	if len(raw) > 300 {
		raw = raw[:300]
	}
	return string(raw)
}

func volcSpeakerToVoice(speaker volcSpeaker) voice_catalog.Voice {
	voice := voice_catalog.Voice{
		Voice:       speaker.VoiceType,
		Name:        speaker.Name,
		Gender:      normalizeVoiceGender(speaker.Gender),
		Age:         speaker.Age,
		Description: speaker.Description,
		Avatar:      speaker.Avatar,
	}
	// 火山的分类是一组场景标签（一个音色常属于多个场景），作为场景展示和搜索。
	for _, group := range speaker.Categories {
		voice.Scenes = append(voice.Scenes, group.Categories...)
	}
	for _, raw := range append(speaker.NormalLabels, speaker.SpecialLabels...) {
		if label := volcLabelText(raw); label != "" {
			voice.Labels = append(voice.Labels, label)
		}
	}
	sampleText := ""
	for _, language := range speaker.Languages {
		if language.Language != "" {
			voice.Languages = append(voice.Languages, language.Language)
		}
		if sampleText == "" {
			sampleText = language.Text
		}
	}
	for _, emotion := range speaker.Emotions {
		voice.Emotions = append(voice.Emotions, voice_catalog.Emotion{Label: emotion.Label, Value: emotion.Value})
	}
	trialURL := speaker.TrialURL
	if trialURL == "" {
		trialURL = speaker.ShortTrialURL
	}
	if trialURL != "" {
		voice.Samples = []voice_catalog.Sample{{Text: sampleText, URL: trialURL}}
	}
	return voice
}

// volcLabelText 标签字段的结构文档没有写明，兼容字符串和带 Label/Name 的对象两种形态。
func volcLabelText(raw json.RawMessage) string {
	var text string
	if err := common.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var object map[string]any
	if err := common.Unmarshal(raw, &object); err != nil {
		return ""
	}
	for _, key := range []string{"Label", "Name", "Value", "Text"} {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeVoiceGender(gender string) string {
	switch strings.ToLower(strings.TrimSpace(gender)) {
	case "female", "女", "f":
		return "female"
	case "male", "男", "m":
		return "male"
	}
	return strings.TrimSpace(gender)
}
