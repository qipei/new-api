// CUSTOM: 语音模型的预置音色目录（fork 扩展）。
//
// 三个来源：
//   - 百炼 Qwen-Audio-TTS 没有查询音色的接口，按官方音色文档整理成静态清单随代码发布
//     （bailian_voices.json，每个音色都在对应模型上实测合成过）；
//   - 百炼 MiniMax 语音同样没有查询接口，按 MiniMax 开放平台的系统音色表整理成静态清单
//     （minimax_voices.json，四个模型共用）；
//   - 火山豆包语音用 ListSpeakers 接口定时同步入库，需要后台配置火山 OpenAPI 的 AK/SK
//     （与语音的 X-Api-Key 不是同一套凭据）。
//
// 试听只保存厂商的音频地址，由浏览器直接播放，不转存音频文件。
package voice_catalog

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ProviderBailian = "bailian"
	ProviderMiniMax = "minimax"
	ProviderVolc    = "volcengine"
)

// Sample 一段试听音频。Label 是试听的说明（例如方言或语种），可以为空。
type Sample struct {
	Label string `json:"label,omitempty"`
	Text  string `json:"text,omitempty"`
	URL   string `json:"url"`
}

type Emotion struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Features 百炼音色的能力标记。
type Features struct {
	SSML        bool `json:"ssml"`
	Instruction bool `json:"instruction"`
	Timestamp   bool `json:"timestamp"`
}

// Voice 对外展示的统一音色结构，两家缺失的字段留空。
type Voice struct {
	Voice       string    `json:"voice"`
	Name        string    `json:"name"`
	Gender      string    `json:"gender,omitempty"`
	Age         string    `json:"age,omitempty"`
	Category    string    `json:"category,omitempty"`
	Accent      string    `json:"accent,omitempty"`
	Traits      []string  `json:"traits,omitempty"`
	Scenes      []string  `json:"scenes,omitempty"`
	Labels      []string  `json:"labels,omitempty"`
	Languages   []string  `json:"languages,omitempty"`
	Emotions    []Emotion `json:"emotions,omitempty"`
	Features    *Features `json:"features,omitempty"`
	Samples     []Sample  `json:"samples,omitempty"`
	Description string    `json:"description,omitempty"`
	Avatar      string    `json:"avatar,omitempty"`
}

// VoiceCatalogSettings 由 config.GlobalConfig 管理，DB key 前缀 voice_catalog.。
type VoiceCatalogSettings struct {
	VolcAccessKeyID     string   `json:"volc_access_key_id"`
	VolcSecretAccessKey string   `json:"volc_secret_access_key"`
	VolcResourceIDs     []string `json:"volc_resource_ids"`
	SyncIntervalHours   int      `json:"sync_interval_hours"`
}

// SensitiveOptionKeys 不能下发给前端的配置项（键名命中不了通用的敏感后缀规则）。
var SensitiveOptionKeys = []string{
	"voice_catalog.volc_access_key_id",
	"voice_catalog.volc_secret_access_key",
}

var settings = VoiceCatalogSettings{
	VolcResourceIDs:   []string{"seed-tts-2.0"},
	SyncIntervalHours: 24,
}

func init() {
	config.GlobalConfig.Register("voice_catalog", &settings)
}

func GetSettings() *VoiceCatalogSettings {
	return &settings
}

// VolcConfigured 是否配置了火山同步所需的凭据。
func (s *VoiceCatalogSettings) VolcConfigured() bool {
	return strings.TrimSpace(s.VolcAccessKeyID) != "" && strings.TrimSpace(s.VolcSecretAccessKey) != ""
}

// ResourceIDs 返回去重、去空白后的同步模型列表；未配置时默认 seed-tts-2.0。
func (s *VoiceCatalogSettings) ResourceIDs() []string {
	seen := map[string]bool{}
	ids := make([]string, 0, len(s.VolcResourceIDs))
	for _, id := range s.VolcResourceIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		ids = append(ids, "seed-tts-2.0")
	}
	return ids
}

// SyncIntervalHoursOrDefault 同步间隔，限制在 1 小时到 7 天之间。
func (s *VoiceCatalogSettings) SyncIntervalHoursOrDefault() int {
	return min(max(s.SyncIntervalHours, 1), 24*7)
}

//go:embed bailian_voices.json
var bailianVoicesJSON []byte

// BailianCatalog 百炼静态音色清单。
type BailianCatalog struct {
	SourceURL string `json:"source_url"`
	UpdatedAt string `json:"updated_at"`
	Voices    []struct {
		Model string `json:"model"`
		Voice
	} `json:"voices"`
}

var (
	bailianOnce    sync.Once
	bailianCatalog BailianCatalog
)

func loadBailian() BailianCatalog {
	bailianOnce.Do(func() {
		if err := common.Unmarshal(bailianVoicesJSON, &bailianCatalog); err != nil {
			common.SysError(fmt.Sprintf("load bailian voice catalog failed: %v", err))
		}
	})
	return bailianCatalog
}

// BailianVoices 返回指定百炼模型的系统音色，以及清单的来源与整理日期。
func BailianVoices(model string) (voices []Voice, sourceURL, updatedAt string) {
	catalog := loadBailian()
	for _, entry := range catalog.Voices {
		if entry.Model == model {
			voices = append(voices, entry.Voice)
		}
	}
	return voices, catalog.SourceURL, catalog.UpdatedAt
}

//go:embed minimax_voices.json
var miniMaxVoicesJSON []byte

// MiniMaxCatalog MiniMax 系统音色静态清单，四个 MiniMax 语音模型共用。
type MiniMaxCatalog struct {
	SourceURL string  `json:"source_url"`
	UpdatedAt string  `json:"updated_at"`
	Voices    []Voice `json:"voices"`
}

var (
	miniMaxOnce    sync.Once
	miniMaxCatalog MiniMaxCatalog
)

// MiniMaxVoices 返回 MiniMax 系统音色，以及清单的整理日期。
func MiniMaxVoices() (voices []Voice, updatedAt string) {
	miniMaxOnce.Do(func() {
		if err := common.Unmarshal(miniMaxVoicesJSON, &miniMaxCatalog); err != nil {
			common.SysError(fmt.Sprintf("load minimax voice catalog failed: %v", err))
		}
	})
	return miniMaxCatalog.Voices, miniMaxCatalog.UpdatedAt
}
