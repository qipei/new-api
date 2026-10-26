package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
)

type ThinkingContentInfo struct {
	IsFirstThinkingContent  bool
	SendLastThinkingContent bool
	HasSentThinkingContent  bool
}

const (
	LastMessageTypeNone     = convmeta.LastMessageTypeNone
	LastMessageTypeText     = convmeta.LastMessageTypeText
	LastMessageTypeTools    = convmeta.LastMessageTypeTools
	LastMessageTypeThinking = convmeta.LastMessageTypeThinking
)

// ClaudeConvertInfo now lives with the converters (convmeta); the alias keeps
// host code and adaptors compiling unchanged.
type ClaudeConvertInfo = convmeta.ClaudeConvertInfo

type RerankerInfo struct {
	Documents       []any
	ReturnDocuments bool
}

type BuildInToolInfo struct {
	ToolName          string
	CallCount         int
	SearchContextSize string
}

type ResponsesUsageInfo struct {
	BuiltInTools map[string]*BuildInToolInfo
}

type ChannelMeta struct {
	ChannelType          int
	ChannelId            int
	ChannelIsMultiKey    bool
	ChannelMultiKeyIndex int
	ChannelBaseUrl       string
	ApiType              int
	ApiVersion           string
	ApiKey               string
	Organization         string
	ChannelCreateTime    int64
	ParamOverride        map[string]interface{}
	HeadersOverride      map[string]interface{}
	ChannelSetting       dto.ChannelSettings
	ChannelOtherSettings dto.ChannelOtherSettings
	UpstreamModelName    string
	IsModelMapped        bool
	SupportStreamOptions bool // 是否支持流式选项
}

type TokenCountMeta struct {
	//promptTokens int
	estimatePromptTokens int
}

type RelayInfo struct {
	TokenId           int
	TokenKey          string
	TokenGroup        string
	UserId            int
	UsingGroup        string // 使用的分组，当auto跨分组重试时，会变动
	UserGroup         string // 用户所在分组
	TokenUnlimited    bool
	StartTime         time.Time
	FirstResponseTime time.Time
	isFirstResponse   bool
	//SendLastReasoningResponse bool
	IsStream               bool
	IsGeminiBatchEmbedding bool
	IsPlayground           bool
	UsePrice               bool
	RelayMode              int
	OriginModelName        string
	RequestURLPath         string
	RequestHeaders         map[string]string
	ShouldIncludeUsage     bool
	DisablePing            bool // 是否禁止向下游发送自定义 Ping
	ClientWs               *websocket.Conn
	TargetWs               *websocket.Conn
	InputAudioFormat       string
	OutputAudioFormat      string
	RealtimeTools          []dto.RealTimeTool
	IsFirstRequest         bool
	AudioUsage             bool
	ReasoningEffort        string
	UserSetting            dto.UserSetting
	UserEmail              string
	UserQuota              int
	RelayFormat            types.RelayFormat
	SendResponseCount      int
	ReceivedResponseCount  int
	FinalPreConsumedQuota  int // 最终预消耗的配额
	// ForcePreConsume 为 true 时禁用 BillingSession 的信任额度旁路，
	// 强制预扣全额。用于异步任务（视频/音乐生成等），因为请求返回后任务仍在运行，
	// 必须在提交前锁定全额。
	ForcePreConsume bool
	// Billing 是计费会话，封装了预扣费/结算/退款的统一生命周期。
	// 初始免费组可为 nil；若 auto 重试切换到付费组，会在发送前创建。
	Billing BillingSettler
	// BillingSource indicates whether this request is billed from wallet quota or subscription.
	// "" or "wallet" => wallet; "subscription" => subscription
	BillingSource string
	// SubscriptionId is the user_subscriptions.id used when BillingSource == "subscription"
	SubscriptionId int
	// SubscriptionPreConsumed is the amount pre-consumed on subscription item (quota units or 1)
	SubscriptionPreConsumed int64
	// SubscriptionPostDelta is the post-consume delta applied to amount_used (quota units; can be negative).
	SubscriptionPostDelta int64
	// SubscriptionPlanId / SubscriptionPlanTitle are used for logging/UI display.
	SubscriptionPlanId    int
	SubscriptionPlanTitle string
	// RequestId is used for idempotent pre-consume/refund
	RequestId string
	// SubscriptionAmountTotal / SubscriptionAmountUsedAfterPreConsume are used to compute remaining in logs.
	SubscriptionAmountTotal               int64
	SubscriptionAmountUsedAfterPreConsume int64
	IsClaudeBetaQuery                     bool // /v1/messages?beta=true
	IsChannelTest                         bool // channel test request
	RetryIndex                            int
	LastError                             *types.NewAPIError
	RuntimeHeadersOverride                map[string]interface{}
	UseRuntimeHeadersOverride             bool
	ParamOverrideAudit                    []string

	PriceData hosttypes.PriceData

	// QuotaClamp is set (non-nil) when a quota conversion saturated at the
	// supported single-request bound (or NaN fallback) while computing this request's charge.
	// It is surfaced onto the consume/task log's admin_info for auditing.
	QuotaClamp *common.QuotaClamp

	// TieredBillingSnapshot captures tiered billing rules at pre-consume time.
	// Auto-group retries refresh its group-dependent fields before each attempt
	// and again before settlement. Non-nil only when billing mode is "tiered_expr".
	TieredBillingSnapshot *billingexpr.BillingSnapshot
	BillingRequestInput   *billingexpr.RequestInput

	Request dto.Request

	// RequestConversionChain records request format conversions in order, e.g.
	// ["openai", "openai_responses"] or ["openai", "claude"].
	RequestConversionChain []types.RelayFormat
	// 最终请求到上游的格式。可由 adaptor 显式设置；
	// 若为空，调用 GetFinalRequestRelayFormat 会回退到 RequestConversionChain 的最后一项或 RelayFormat。
	FinalRequestRelayFormat types.RelayFormat

	StreamStatus *StreamStatus

	// convOptions caches the converter settings snapshot (see ConvOptions).
	convOptions *convmeta.Options

	ThinkingContentInfo
	TokenCountMeta
	*ClaudeConvertInfo
	*RerankerInfo
	*ResponsesUsageInfo
	*ChannelMeta
	*TaskRelayInfo
}

func (info *RelayInfo) InitChannelMeta(c *gin.Context) {
	channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType)
	paramOverride := common.GetContextKeyStringMap(c, constant.ContextKeyChannelParamOverride)
	headerOverride := common.GetContextKeyStringMap(c, constant.ContextKeyChannelHeaderOverride)
	apiType, _ := common.ChannelType2APIType(channelType)
	channelMeta := &ChannelMeta{
		ChannelType:          channelType,
		ChannelId:            common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		ChannelIsMultiKey:    common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey),
		ChannelMultiKeyIndex: common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex),
		ChannelBaseUrl:       common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl),
		ApiType:              apiType,
		ApiVersion:           c.GetString("api_version"),
		ApiKey:               common.GetContextKeyString(c, constant.ContextKeyChannelKey),
		Organization:         c.GetString("channel_organization"),
		ChannelCreateTime:    c.GetInt64("channel_create_time"),
		ParamOverride:        paramOverride,
		HeadersOverride:      headerOverride,
		UpstreamModelName:    common.GetContextKeyString(c, constant.ContextKeyOriginalModel),
		IsModelMapped:        false,
		SupportStreamOptions: false,
	}

	if channelType == constant.ChannelTypeAzure {
		channelMeta.ApiVersion = GetAPIVersion(c)
	}
	if channelType == constant.ChannelTypeVertexAi {
		channelMeta.ApiVersion = c.GetString("region")
	}

	channelSetting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	if ok {
		channelMeta.ChannelSetting = channelSetting
	}

	channelOtherSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	if ok {
		channelMeta.ChannelOtherSettings = channelOtherSettings
	}

	if streamSupportedChannels[channelMeta.ChannelType] {
		channelMeta.SupportStreamOptions = true
	}

	info.ChannelMeta = channelMeta

	// Channel identity feeds the converter options snapshot (e.g.
	// OpenRouterDialect); drop the cache so a cross-channel retry rebuilds it.
	info.convOptions = nil
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || channelMeta.ChannelSetting.PassThroughBodyEnabled {
		info.ReasoningEffort = ""
	} else {
		info.ReasoningEffort = reasoningEffortFromRequest(info.Request)
	}

	// reset some fields based on channel meta
	// 重置某些字段，例如模型名称等
	if info.Request != nil {
		info.Request.SetModelName(info.OriginModelName)
	}
}

func (info *RelayInfo) ToString() string {
	if info == nil {
		return "RelayInfo<nil>"
	}

	// Basic info
	b := &strings.Builder{}
	fmt.Fprintf(b, "RelayInfo{ ")
	fmt.Fprintf(b, "RelayFormat: %s, ", info.RelayFormat)
	fmt.Fprintf(b, "RelayMode: %d, ", info.RelayMode)
	fmt.Fprintf(b, "IsStream: %t, ", info.IsStream)
	fmt.Fprintf(b, "IsPlayground: %t, ", info.IsPlayground)
	fmt.Fprintf(b, "RequestURLPath: %q, ", info.RequestURLPath)
	fmt.Fprintf(b, "OriginModelName: %q, ", info.OriginModelName)
	fmt.Fprintf(b, "EstimatePromptTokens: %d, ", info.estimatePromptTokens)
	fmt.Fprintf(b, "ShouldIncludeUsage: %t, ", info.ShouldIncludeUsage)
	fmt.Fprintf(b, "DisablePing: %t, ", info.DisablePing)
	fmt.Fprintf(b, "SendResponseCount: %d, ", info.SendResponseCount)
	fmt.Fprintf(b, "FinalPreConsumedQuota: %d, ", info.FinalPreConsumedQuota)

	// User & token info (mask secrets)
	fmt.Fprintf(b, "User{ Id: %d, Email: %q, Group: %q, UsingGroup: %q, Quota: %d }, ",
		info.UserId, common.MaskEmail(info.UserEmail), info.UserGroup, info.UsingGroup, info.UserQuota)
	fmt.Fprintf(b, "Token{ Id: %d, Unlimited: %t, Key: ***masked*** }, ", info.TokenId, info.TokenUnlimited)

	// Time info
	latencyMs := info.FirstResponseTime.Sub(info.StartTime).Milliseconds()
	fmt.Fprintf(b, "Timing{ Start: %s, FirstResponse: %s, LatencyMs: %d }, ",
		info.StartTime.Format(time.RFC3339Nano), info.FirstResponseTime.Format(time.RFC3339Nano), latencyMs)

	// Audio / realtime
	if info.InputAudioFormat != "" || info.OutputAudioFormat != "" || len(info.RealtimeTools) > 0 || info.AudioUsage {
		fmt.Fprintf(b, "Realtime{ AudioUsage: %t, InFmt: %q, OutFmt: %q, Tools: %d }, ",
			info.AudioUsage, info.InputAudioFormat, info.OutputAudioFormat, len(info.RealtimeTools))
	}

	// Reasoning
	if info.ReasoningEffort != "" {
		fmt.Fprintf(b, "ReasoningEffort: %q, ", info.ReasoningEffort)
	}

	// Price data (non-sensitive)
	if info.PriceData.UsePrice {
		fmt.Fprintf(b, "PriceData{ %s }, ", info.PriceData.ToSetting())
	}

	// Channel metadata (mask ApiKey)
	if info.ChannelMeta != nil {
		cm := info.ChannelMeta
		fmt.Fprintf(b, "ChannelMeta{ Type: %d, Id: %d, IsMultiKey: %t, MultiKeyIndex: %d, BaseURL: %q, ApiType: %d, ApiVersion: %q, Organization: %q, CreateTime: %d, UpstreamModelName: %q, IsModelMapped: %t, SupportStreamOptions: %t, ApiKey: ***masked*** }, ",
			cm.ChannelType, cm.ChannelId, cm.ChannelIsMultiKey, cm.ChannelMultiKeyIndex, cm.ChannelBaseUrl, cm.ApiType, cm.ApiVersion, cm.Organization, cm.ChannelCreateTime, cm.UpstreamModelName, cm.IsModelMapped, cm.SupportStreamOptions)
	}

	// Responses usage info (non-sensitive)
	if info.ResponsesUsageInfo != nil && len(info.ResponsesUsageInfo.BuiltInTools) > 0 {
		fmt.Fprintf(b, "ResponsesTools{ ")
		first := true
		for name, tool := range info.ResponsesUsageInfo.BuiltInTools {
			if !first {
				fmt.Fprintf(b, ", ")
			}
			first = false
			if tool != nil {
				fmt.Fprintf(b, "%s: calls=%d", name, tool.CallCount)
			} else {
				fmt.Fprintf(b, "%s: calls=0", name)
			}
		}
		fmt.Fprintf(b, " }, ")
	}

	fmt.Fprintf(b, "}")
	return b.String()
}

// 定义支持流式选项的通道类型
var streamSupportedChannels = map[int]bool{
	constant.ChannelTypeOpenAI:         true,
	constant.ChannelTypeAnthropic:      true,
	constant.ChannelTypeAws:            true,
	constant.ChannelTypeGemini:         true,
	constant.ChannelCloudflare:         true,
	constant.ChannelTypeAzure:          true,
	constant.ChannelTypeVolcEngine:     true,
	constant.ChannelTypeOllama:         true,
	constant.ChannelTypeXai:            true,
	constant.ChannelTypeDeepSeek:       true,
	constant.ChannelTypeBaiduV2:        true,
	constant.ChannelTypeZhipu_v4:       true,
	constant.ChannelTypeAli:            true,
	constant.ChannelTypeSubmodel:       true,
	constant.ChannelTypeCodex:          true,
	constant.ChannelTypeMoonshot:       true,
	constant.ChannelTypeMiniMax:        true,
	constant.ChannelTypeSiliconFlow:    true,
	constant.ChannelTypeAdvancedCustom: true,
	constant.ChannelTypeSub2API:        true,
	constant.ChannelTypeNewAPI:         true,
	constant.ChannelTypeTencent:        true,
}

func GenRelayInfoWs(c *gin.Context, ws *websocket.Conn) *RelayInfo {
	info := genBaseRelayInfo(c, nil)
	info.RelayFormat = types.RelayFormatOpenAIRealtime
	info.ClientWs = ws
	info.InputAudioFormat = "pcm16"
	info.OutputAudioFormat = "pcm16"
	info.IsFirstRequest = true
	return info
}

func GenRelayInfoClaude(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatClaude
	info.ShouldIncludeUsage = false
	info.ClaudeConvertInfo = &ClaudeConvertInfo{
		LastMessagesType: LastMessageTypeNone,
	}
	info.IsClaudeBetaQuery = c.Query("beta") == "true"
	return info
}

func GenRelayInfoRerank(c *gin.Context, request *dto.RerankRequest) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayMode = relayconstant.RelayModeRerank
	info.RelayFormat = types.RelayFormatRerank
	info.RerankerInfo = &RerankerInfo{
		Documents:       request.Documents,
		ReturnDocuments: request.GetReturnDocuments(),
	}
	return info
}

func GenRelayInfoOpenAIAudio(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatOpenAIAudio
	return info
}

func GenRelayInfoEmbedding(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatEmbedding
	return info
}

func GenRelayInfoResponses(c *gin.Context, request *dto.OpenAIResponsesRequest) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayMode = relayconstant.RelayModeResponses
	info.RelayFormat = types.RelayFormatOpenAIResponses

	info.ResponsesUsageInfo = &ResponsesUsageInfo{
		BuiltInTools: make(map[string]*BuildInToolInfo),
	}
	if len(request.Tools) > 0 {
		for _, tool := range request.GetToolsMap() {
			toolType := common.Interface2String(tool["type"])
			info.ResponsesUsageInfo.BuiltInTools[toolType] = &BuildInToolInfo{
				ToolName:  toolType,
				CallCount: 0,
			}
			switch toolType {
			case dto.BuildInToolWebSearchPreview:
				searchContextSize := common.Interface2String(tool["search_context_size"])
				if searchContextSize == "" {
					searchContextSize = "medium"
				}
				info.ResponsesUsageInfo.BuiltInTools[toolType].SearchContextSize = searchContextSize
			}
		}
	}
	return info
}

func GenRelayInfoGemini(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatGemini
	info.ShouldIncludeUsage = false

	return info
}

func GenRelayInfoImage(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatOpenAIImage
	return info
}

func GenRelayInfoOpenAI(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatOpenAI
	return info
}

func reasoningEffortFromRequest(request dto.Request) string {
	var effort string
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if req == nil {
			return ""
		}
		effort = req.ReasoningEffort
		if strings.TrimSpace(effort) == "" && len(req.Reasoning) > 0 {
			value := gjson.GetBytes(req.Reasoning, "effort")
			if value.Type == gjson.String {
				effort = value.String()
			}
		}
	case *dto.OpenAIResponsesRequest:
		if req != nil && req.Reasoning != nil {
			effort = req.Reasoning.Effort
		}
	case *dto.ClaudeRequest:
		if req != nil {
			effort = req.GetEfforts()
		}
	case *dto.GeminiChatRequest:
		if req != nil && req.GenerationConfig.ThinkingConfig != nil {
			effort = req.GenerationConfig.ThinkingConfig.ThinkingLevel
		}
	}
	return strings.TrimSpace(effort)
}

func genBaseRelayInfo(c *gin.Context, request dto.Request) *RelayInfo {

	//channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType)
	//channelId := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	//paramOverride := common.GetContextKeyStringMap(c, constant.ContextKeyChannelParamOverride)

	tokenGroup := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	// 当令牌分组为空时，表示使用用户分组
	if tokenGroup == "" {
		tokenGroup = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	}

	startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}

	isStream := false

	if request != nil {
		isStream = request.IsStream(c.Request)
	}
	c.Set(string(constant.ContextKeyIsStream), isStream)

	// firstResponseTime = time.Now() - 1 second

	reqId := common.GetContextKeyString(c, common.RequestIdKey)
	if reqId == "" {
		reqId = common.NewRequestId()
	}
	reasoningEffort := reasoningEffortFromRequest(request)
	info := &RelayInfo{
		Request:         request,
		ReasoningEffort: reasoningEffort,

		RequestId:  reqId,
		UserId:     common.GetContextKeyInt(c, constant.ContextKeyUserId),
		UsingGroup: common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		UserGroup:  common.GetContextKeyString(c, constant.ContextKeyUserGroup),
		UserQuota:  common.GetContextKeyInt(c, constant.ContextKeyUserQuota),
		UserEmail:  common.GetContextKeyString(c, constant.ContextKeyUserEmail),

		OriginModelName: common.GetContextKeyString(c, constant.ContextKeyOriginalModel),

		TokenId:        common.GetContextKeyInt(c, constant.ContextKeyTokenId),
		TokenKey:       common.GetContextKeyString(c, constant.ContextKeyTokenKey),
		TokenUnlimited: common.GetContextKeyBool(c, constant.ContextKeyTokenUnlimited),
		TokenGroup:     tokenGroup,

		isFirstResponse: true,
		RelayMode:       relayconstant.Path2RelayMode(c.Request.URL.Path),
		RequestURLPath:  c.Request.URL.String(),
		RequestHeaders:  cloneRequestHeaders(c),
		IsStream:        isStream,

		StartTime:         startTime,
		FirstResponseTime: startTime.Add(-time.Second),
		ThinkingContentInfo: ThinkingContentInfo{
			IsFirstThinkingContent:  true,
			SendLastThinkingContent: false,
		},
		TokenCountMeta: TokenCountMeta{
			//promptTokens: common.GetContextKeyInt(c, constant.ContextKeyPromptTokens),
			estimatePromptTokens: common.GetContextKeyInt(c, constant.ContextKeyEstimatedTokens),
		},
	}

	if info.RelayMode == relayconstant.RelayModeUnknown {
		info.RelayMode = c.GetInt("relay_mode")
	}

	if strings.HasPrefix(c.Request.URL.Path, "/pg") {
		info.IsPlayground = true
		info.RequestURLPath = strings.TrimPrefix(info.RequestURLPath, "/pg")
		info.RequestURLPath = "/v1" + info.RequestURLPath
	}

	userSetting, ok := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting)
	if ok {
		info.UserSetting = userSetting
	}

	return info
}

func cloneRequestHeaders(c *gin.Context) map[string]string {
	if c == nil || c.Request == nil {
		return nil
	}
	if len(c.Request.Header) == 0 {
		return nil
	}
	headers := make(map[string]string, len(c.Request.Header))
	for key := range c.Request.Header {
		value := strings.TrimSpace(c.Request.Header.Get(key))
		if value == "" {
			continue
		}
		headers[key] = value
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

func GenRelayInfo(c *gin.Context, relayFormat types.RelayFormat, request dto.Request, ws *websocket.Conn) (*RelayInfo, error) {
	var info *RelayInfo
	var err error
	switch relayFormat {
	case types.RelayFormatOpenAI:
		info = GenRelayInfoOpenAI(c, request)
	case types.RelayFormatOpenAIAudio:
		info = GenRelayInfoOpenAIAudio(c, request)
	case types.RelayFormatOpenAIImage:
		info = GenRelayInfoImage(c, request)
	case types.RelayFormatOpenAIRealtime:
		info = GenRelayInfoWs(c, ws)
	case types.RelayFormatClaude:
		info = GenRelayInfoClaude(c, request)
	case types.RelayFormatRerank:
		if request, ok := request.(*dto.RerankRequest); ok {
			info = GenRelayInfoRerank(c, request)
			break
		}
		err = errors.New("request is not a RerankRequest")
	case types.RelayFormatGemini:
		info = GenRelayInfoGemini(c, request)
	case types.RelayFormatEmbedding:
		info = GenRelayInfoEmbedding(c, request)
	case types.RelayFormatOpenAIResponses:
		if request, ok := request.(*dto.OpenAIResponsesRequest); ok {
			info = GenRelayInfoResponses(c, request)
			break
		}
		err = errors.New("request is not a OpenAIResponsesRequest")
	case types.RelayFormatOpenAIResponsesCompaction:
		if request, ok := request.(*dto.OpenAIResponsesCompactionRequest); ok {
			return GenRelayInfoResponsesCompaction(c, request), nil
		}
		return nil, errors.New("request is not a OpenAIResponsesCompactionRequest")
	case types.RelayFormatOpenAIAlphaSearch:
		if request, ok := request.(*dto.AlphaSearchRequest); ok {
			return GenRelayInfoAlphaSearch(c, request), nil
		}
		return nil, errors.New("request is not a AlphaSearchRequest")
	case types.RelayFormatTask:
		info = genBaseRelayInfo(c, nil)
		info.TaskRelayInfo = &TaskRelayInfo{}
	case types.RelayFormatMjProxy:
		info = genBaseRelayInfo(c, nil)
		info.TaskRelayInfo = &TaskRelayInfo{}
	default:
		err = errors.New("invalid relay format")
	}

	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, errors.New("failed to build relay info")
	}

	info.InitRequestConversionChain()
	return info, nil
}

func (info *RelayInfo) InitRequestConversionChain() {
	if info == nil {
		return
	}
	if len(info.RequestConversionChain) > 0 {
		return
	}
	if info.RelayFormat == "" {
		return
	}
	info.RequestConversionChain = []types.RelayFormat{info.RelayFormat}
}

func (info *RelayInfo) AppendRequestConversion(format types.RelayFormat) {
	if info == nil {
		return
	}
	if format == "" {
		return
	}
	if len(info.RequestConversionChain) == 0 {
		info.RequestConversionChain = []types.RelayFormat{format}
		return
	}
	last := info.RequestConversionChain[len(info.RequestConversionChain)-1]
	if last == format {
		return
	}
	info.RequestConversionChain = append(info.RequestConversionChain, format)
}

func (info *RelayInfo) GetFinalRequestRelayFormat() types.RelayFormat {
	if info == nil {
		return ""
	}
	if info.FinalRequestRelayFormat != "" {
		return info.FinalRequestRelayFormat
	}
	if n := len(info.RequestConversionChain); n > 0 {
		return info.RequestConversionChain[n-1]
	}
	return info.RelayFormat
}

func GenRelayInfoResponsesCompaction(c *gin.Context, request *dto.OpenAIResponsesCompactionRequest) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	if info.RelayMode == relayconstant.RelayModeUnknown {
		info.RelayMode = relayconstant.RelayModeResponsesCompact
	}
	info.RelayFormat = types.RelayFormatOpenAIResponsesCompaction
	return info
}

func GenRelayInfoAlphaSearch(c *gin.Context, request *dto.AlphaSearchRequest) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	if info.RelayMode == relayconstant.RelayModeUnknown {
		info.RelayMode = relayconstant.RelayModeAlphaSearch
	}
	info.RelayFormat = types.RelayFormatOpenAIAlphaSearch
	info.ResponsesUsageInfo = &ResponsesUsageInfo{
		BuiltInTools: map[string]*BuildInToolInfo{
			dto.BuildInToolWebSearchPreview: {
				ToolName:  dto.BuildInToolWebSearchPreview,
				CallCount: 0,
			},
		},
	}
	return info
}

//func (info *RelayInfo) SetPromptTokens(promptTokens int) {
//	info.promptTokens = promptTokens
//}

func (info *RelayInfo) SetEstimatePromptTokens(promptTokens int) {
	if info == nil {
		return
	}
	info.estimatePromptTokens = promptTokens
}

func (info *RelayInfo) GetEstimatePromptTokens() int {
	if info == nil {
		return 0
	}
	return info.estimatePromptTokens
}

// ---------------------------------------------------------------------------
// convmeta.Meta implementation — the view format converters see. Keep these
// thin: they only expose protocol state, never billing/user fields.
// ---------------------------------------------------------------------------

var _ convmeta.Meta = (*RelayInfo)(nil)

func (info *RelayInfo) GetOriginModelName() string {
	if info == nil {
		return ""
	}
	return info.OriginModelName
}

func (info *RelayInfo) GetUpstreamModelName() string {
	if info == nil || info.ChannelMeta == nil {
		return ""
	}
	return info.UpstreamModelName
}

func (info *RelayInfo) HasChannelMeta() bool { return info != nil && info.ChannelMeta != nil }

func (info *RelayInfo) GetChannelID() int {
	if info == nil || info.ChannelMeta == nil {
		return 0
	}
	return info.ChannelId
}

func (info *RelayInfo) GetChannelType() int {
	if info == nil || info.ChannelMeta == nil {
		return 0
	}
	return info.ChannelType
}

func (info *RelayInfo) GetIsStream() bool {
	return info != nil && info.IsStream
}

func (info *RelayInfo) GetReasoningEffort() string {
	if info == nil {
		return ""
	}
	return info.ReasoningEffort
}

func (info *RelayInfo) SetReasoningEffort(effort string) {
	if info == nil {
		return
	}
	info.ReasoningEffort = strings.TrimSpace(effort)
}

func (info *RelayInfo) EnsureClaudeConvertInfo() *convmeta.ClaudeConvertInfo {
	if info == nil {
		return &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		}
	}
	if info.ClaudeConvertInfo == nil {
		info.ClaudeConvertInfo = &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		}
	}
	return info.ClaudeConvertInfo
}

func (info *RelayInfo) GetSendResponseCount() int {
	if info == nil {
		return 0
	}
	return info.SendResponseCount
}

func (info *RelayInfo) IncrSendResponseCount() {
	if info == nil {
		return
	}
	info.SendResponseCount++
}

// ConvOptions snapshots host settings for the converters. Rebuilt on each
// call site's first use; cached so one relay session sees one snapshot.
func (info *RelayInfo) ConvOptions() *convmeta.Options {
	if info != nil && info.convOptions != nil {
		return info.convOptions
	}

	claudeSettings := model_setting.GetClaudeSettings()
	geminiSettings := model_setting.GetGeminiSettings()
	options := &convmeta.Options{
		Claude: convmeta.ClaudeOptions{
			ThinkingAdapterEnabled:                claudeSettings.ThinkingAdapterEnabled,
			ThinkingAdapterBudgetTokensPercentage: claudeSettings.ThinkingAdapterBudgetTokensPercentage,
			DefaultMaxTokens:                      claudeSettings.GetDefaultMaxTokens,
		},
		Gemini: convmeta.GeminiOptions{
			ThinkingAdapterEnabled:                geminiSettings.ThinkingAdapterEnabled,
			ThinkingAdapterBudgetTokensPercentage: geminiSettings.ThinkingAdapterBudgetTokensPercentage,
			FunctionCallThoughtSignatureEnabled:   geminiSettings.FunctionCallThoughtSignatureEnabled,
			SupportsImagine:                       model_setting.IsGeminiModelSupportImagine,
			SafetySetting:                         model_setting.GetGeminiSafetySetting,
		},
		OpenRouterDialect:      info != nil && info.GetChannelType() == constant.ChannelTypeOpenRouter,
		PreserveThinkingSuffix: model_setting.ShouldPreserveThinkingSuffix,
	}
	if info != nil {
		info.convOptions = options
	}
	return options
}

func (info *RelayInfo) SetFirstResponseTime() {
	if info.isFirstResponse {
		info.FirstResponseTime = time.Now()
		info.isFirstResponse = false
	}
}

func (info *RelayInfo) HasSendResponse() bool {
	return info.FirstResponseTime.After(info.StartTime)
}

type TaskRelayInfo struct {
	Action       string
	OriginTaskID string
	// PublicTaskID 是提交时预生成的 task_xxxx 格式公开 ID，
	// 供 DoResponse 在返回给客户端时使用（避免暴露上游真实 ID）。
	PublicTaskID string

	ConsumeQuota bool

	// LockedChannel holds the full channel object when the request is bound to
	// a specific channel (e.g., remix on origin task's channel). Stored as any
	// to avoid an import cycle with model; callers type-assert to *model.Channel.
	LockedChannel any
}

type TaskSubmitReq struct {
	Prompt         string                 `json:"prompt"`
	Model          string                 `json:"model,omitempty"`
	Mode           string                 `json:"mode,omitempty"`
	Image          string                 `json:"image,omitempty"`
	Images         []string               `json:"images,omitempty"`
	Video          string                 `json:"video,omitempty"`
	Audio          string                 `json:"audio,omitempty"`
	Audios         []string               `json:"audios,omitempty"`
	Size           string                 `json:"size,omitempty"`
	Duration       int                    `json:"duration,omitempty"`
	Seconds        string                 `json:"seconds,omitempty"`
	InputReference string                 `json:"input_reference,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`

	// CUSTOM: 按用途区分的素材字段（fork 扩展）。与 image/images/video/audio(s) 冗余并存，
	// 各字段也可以写在 metadata 里（与 tokease 等平台的写法一致），网关两处都认。
	FirstFrameURL   string   `json:"first_frame_url,omitempty"`
	LastFrameURL    string   `json:"last_frame_url,omitempty"`
	ReferenceImages []string `json:"reference_images,omitempty"`
	VideoURL        string   `json:"video_url,omitempty"`
	VideoURLs       []string `json:"video_urls,omitempty"`
	AudioURL        string   `json:"audio_url,omitempty"`
	AudioURLs       []string `json:"audio_urls,omitempty"`

	// CUSTOM: 与火山、MiniMax 官方及 tokease 一致的顶层写法（fork 扩展），与 metadata 里的
	// 同名参数冗余并存：resolution/ratio 为输出规格，content 为带 role 的原生素材数组。
	Resolution string        `json:"resolution,omitempty"`
	Ratio      string        `json:"ratio,omitempty"`
	Content    []interface{} `json:"content,omitempty"`

	// legacyImages 调用方是否用了 image/images/input_reference 传图，在补齐旧字段之前记录。
	// 旧写法按张数推断首帧/参考图，和按用途区分的图片字段同时出现时用途说不清，需要拒绝；
	// 视频、音频只有「参考」一种用途，两种写法合并即可。
	legacyImages bool

	// bodyFields 调用方请求体顶层的全部字段（JSON 写法），供按上游官方协议透传参数的渠道
	// 读取未在本结构中声明的官方参数，例如火山方舟的 generate_audio、seed、tools。
	bodyFields map[string]interface{}
}

// BodyField 读取请求体顶层的字段，未传时第二个返回值为 false。
func (t *TaskSubmitReq) BodyField(key string) (interface{}, bool) {
	value, ok := t.bodyFields[key]
	return value, ok
}

// TaskMedia 按用途区分的输入素材：首帧、尾帧、参考图、参考视频、参考音频。
type TaskMedia struct {
	FirstFrame      string
	LastFrame       string
	ReferenceImages []string
	ReferenceVideos []string
	ReferenceAudios []string
}

func (m TaskMedia) IsEmpty() bool {
	return m.FirstFrame == "" && m.LastFrame == "" && len(m.ReferenceImages) == 0 &&
		len(m.ReferenceVideos) == 0 && len(m.ReferenceAudios) == 0
}

// RoleMedia 汇总按用途区分的素材：请求体顶层与 metadata 里的同名字段都认（单数与复数
// 形式合并），去掉空白与重复。参考视频、参考音频再并入旧写法 video/audio/audios（排在前面），
// 两种写法同时出现时一并送出。供按角色组装素材的渠道使用。
func (t *TaskSubmitReq) RoleMedia() TaskMedia {
	media := t.roleMedia(true)
	media.ReferenceVideos = uniqueNonBlank([]string{t.Video}, media.ReferenceVideos)
	media.ReferenceAudios = uniqueNonBlank([]string{t.Audio}, t.Audios, media.ReferenceAudios)
	return media
}

// UsesRoleMedia 是否按用途组装素材：用了按用途区分的字段且没用旧写法传图。
// 旧写法传图时仍按各渠道原有的张数规则推断，已有请求的行为不变。
func (t *TaskSubmitReq) UsesRoleMedia() bool {
	return !t.legacyImages && !t.roleMedia(true).IsEmpty()
}

// HasLegacyImages 调用方是否用了 image/images/input_reference 传图（不含网关补齐的部分）。
func (t *TaskSubmitReq) HasLegacyImages() bool {
	return t.legacyImages
}

// NativeContent 返回调用方直接给出的原生 content 数组：metadata.content 优先，其次顶层 content。
func (t *TaskSubmitReq) NativeContent() ([]interface{}, bool) {
	if content, ok := t.Metadata["content"].([]interface{}); ok {
		return content, true
	}
	if _, ok := t.Metadata["content"]; ok {
		return nil, true
	}
	if len(t.Content) > 0 {
		return t.Content, true
	}
	return nil, false
}

// roleMedia 的 withMetadataSingular 控制是否读取 metadata 里的 first_frame_url、
// last_frame_url、video_url、audio_url。百炼老版万相的原生参数在 metadata 顶层也用这些
// 名字，补旧字段、混用检查只看不会和原生参数撞名的来源，免得改变已有请求的行为。
func (t *TaskSubmitReq) roleMedia(withMetadataSingular bool) TaskMedia {
	singular := func(key string) []string {
		if !withMetadataSingular {
			return nil
		}
		return metadataStrings(t.Metadata, key)
	}
	return TaskMedia{
		FirstFrame:      firstNonBlank(append([]string{t.FirstFrameURL}, singular("first_frame_url")...)),
		LastFrame:       firstNonBlank(append([]string{t.LastFrameURL}, singular("last_frame_url")...)),
		ReferenceImages: uniqueNonBlank(t.ReferenceImages, metadataStrings(t.Metadata, "reference_images")),
		ReferenceVideos: uniqueNonBlank([]string{t.VideoURL}, t.VideoURLs,
			singular("video_url"), metadataStrings(t.Metadata, "video_urls")),
		ReferenceAudios: uniqueNonBlank([]string{t.AudioURL}, t.AudioURLs,
			singular("audio_url"), metadataStrings(t.Metadata, "audio_urls")),
	}
}

// metadataStrings 读取 metadata 里的字符串或字符串数组。
func metadataStrings(metadata map[string]interface{}, key string) []string {
	switch value := metadata[key].(type) {
	case string:
		return []string{value}
	case []interface{}:
		values := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return values
	}
	return nil
}

func firstNonBlank(values []string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func uniqueNonBlank(groups ...[]string) []string {
	var values []string
	seen := map[string]bool{}
	for _, group := range groups {
		for _, value := range group {
			value = strings.TrimSpace(value)
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			values = append(values, value)
		}
	}
	return values
}

// fillLegacyMedia 按种类补齐 image/images/video/audios：某一种素材只用了按用途区分的
// 新字段时补上对应的旧字段，让还没按角色适配的渠道也能拿到素材、计费也能识别图生/视频生。
// 只用于补空，调用方自己传的旧字段原样保留。只有尾帧没有首帧时旧字段无法表达，不补。
func (t *TaskSubmitReq) fillLegacyMedia() {
	t.legacyImages = strings.TrimSpace(t.Image) != "" || len(t.Images) > 0 || strings.TrimSpace(t.InputReference) != ""
	media := t.roleMedia(false)
	// OpenAI 视频接口的 input_reference 是首帧，只认 images 的渠道同样要拿到它。
	if len(t.Images) == 0 && strings.TrimSpace(t.Image) == "" && strings.TrimSpace(t.InputReference) != "" {
		t.Images = []string{strings.TrimSpace(t.InputReference)}
	}
	if !t.legacyImages {
		switch {
		case media.FirstFrame != "":
			t.Images = []string{media.FirstFrame}
			if media.LastFrame != "" {
				t.Images = append(t.Images, media.LastFrame)
			}
		case media.LastFrame == "" && len(media.ReferenceImages) > 0:
			t.Images = append([]string(nil), media.ReferenceImages...)
		}
	}
	if strings.TrimSpace(t.Video) == "" && len(media.ReferenceVideos) > 0 {
		t.Video = media.ReferenceVideos[0]
	}
	if strings.TrimSpace(t.Audio) == "" && len(t.Audios) == 0 && len(media.ReferenceAudios) > 0 {
		t.Audios = append([]string(nil), media.ReferenceAudios...)
	}
}

// aliImageMediaTypes 百炼原生 input.media 里属于图片的素材类型（各模型的命名不同）。
var aliImageMediaTypes = map[string]bool{
	"first_frame": true, "last_frame": true, "reference_image": true, "image_url": true, "refer": true, "image": true,
}

// InputImageCount 本次请求送给上游的输入图张数，首帧、尾帧、参考图都算，用于按张计费。
// 原生 content 数组按其中的 image_url 项计；百炼原生 metadata.input.media 按图片类素材计；
// 其余按网关字段计（旧写法传图时按 image/images/input_reference 去重后的张数）。
func (t *TaskSubmitReq) InputImageCount() int {
	if content, ok := t.NativeContent(); ok {
		count := 0
		for _, raw := range content {
			if item, ok := raw.(map[string]interface{}); ok && item["type"] == "image_url" {
				count++
			}
		}
		return count
	}
	if input, ok := t.Metadata["input"].(map[string]interface{}); ok {
		if media, ok := input["media"].([]interface{}); ok && len(media) > 0 {
			count := 0
			for _, raw := range media {
				if item, ok := raw.(map[string]interface{}); ok {
					if mediaType, _ := item["type"].(string); aliImageMediaTypes[mediaType] {
						count++
					}
				}
			}
			return count
		}
	}
	if t.legacyImages {
		return len(uniqueNonBlank([]string{t.Image}, t.Images, []string{t.InputReference}))
	}
	media := t.roleMedia(true)
	count := len(media.ReferenceImages)
	if media.FirstFrame != "" {
		count++
	}
	if media.LastFrame != "" {
		count++
	}
	return count
}

// MixesMediaStyles 是否同时用了旧写法传图（image/images/input_reference）与按用途区分的
// 图片字段（first_frame_url/last_frame_url/reference_images）。
func (t *TaskSubmitReq) MixesMediaStyles() bool {
	if !t.legacyImages {
		return false
	}
	media := t.roleMedia(false)
	return media.FirstFrame != "" || media.LastFrame != "" || len(media.ReferenceImages) > 0
}

// parseInputReference 读取 OpenAI 视频接口的 input_reference：字符串，或
// {"image_url": "..."} / {"file_id": "..."} 对象。只有 file_id 时网关无法取图，
// 留空交给透传渠道原样转发。
func parseInputReference(raw json.RawMessage) string {
	var text string
	if err := common.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var ref struct {
		ImageURL any `json:"image_url"`
	}
	if err := common.Unmarshal(raw, &ref); err != nil {
		return ""
	}
	switch value := ref.ImageURL.(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]interface{}:
		url, _ := value["url"].(string)
		return strings.TrimSpace(url)
	}
	return ""
}

func (t *TaskSubmitReq) GetPrompt() string {
	return t.Prompt
}

func (t *TaskSubmitReq) HasImage() bool {
	if len(t.Images) > 0 {
		return true
	}
	media := t.roleMedia(false)
	return media.FirstFrame != "" || media.LastFrame != "" || len(media.ReferenceImages) > 0
}

func (t *TaskSubmitReq) HasVideo() bool {
	return strings.TrimSpace(t.Video) != "" || len(t.roleMedia(false).ReferenceVideos) > 0
}

func (t *TaskSubmitReq) HasAudio() bool {
	return strings.TrimSpace(t.Audio) != "" || len(t.Audios) > 0 || len(t.roleMedia(false).ReferenceAudios) > 0
}

func (t *TaskSubmitReq) UnmarshalJSON(data []byte) error {
	type Alias TaskSubmitReq
	aux := &struct {
		Metadata       json.RawMessage `json:"metadata,omitempty"`
		Duration       json.RawMessage `json:"duration,omitempty"`
		InputReference json.RawMessage `json:"input_reference,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(t),
	}

	if err := common.Unmarshal(data, &aux); err != nil {
		return err
	}
	var bodyFields map[string]interface{}
	if err := common.Unmarshal(data, &bodyFields); err == nil {
		t.bodyFields = bodyFields
	}

	if len(aux.InputReference) > 0 {
		t.InputReference = parseInputReference(aux.InputReference)
	}

	if len(aux.Duration) > 0 {
		var durationInt int
		if err := common.Unmarshal(aux.Duration, &durationInt); err == nil {
			t.Duration = durationInt
		} else {
			var durationStr string
			if err := common.Unmarshal(aux.Duration, &durationStr); err == nil && durationStr != "" {
				if v, err := strconv.Atoi(durationStr); err == nil {
					t.Duration = v
				}
			}
		}
	}

	if len(aux.Metadata) > 0 {
		var metadataStr string
		if err := common.Unmarshal(aux.Metadata, &metadataStr); err == nil && metadataStr != "" {
			var metadataObj map[string]interface{}
			if err := common.Unmarshal([]byte(metadataStr), &metadataObj); err == nil {
				t.Metadata = metadataObj
				t.fillLegacyMedia()
				return nil
			}
		}

		var metadataObj map[string]interface{}
		if err := common.Unmarshal(aux.Metadata, &metadataObj); err == nil {
			t.Metadata = metadataObj
		}
	}

	t.fillLegacyMedia()
	return nil
}
func (t *TaskSubmitReq) UnmarshalMetadata(v any) error {
	metadata := t.Metadata
	if metadata != nil {
		metadataBytes, err := common.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata failed: %w", err)
		}
		err = common.Unmarshal(metadataBytes, v)
		if err != nil {
			return fmt.Errorf("unmarshal metadata to target failed: %w", err)
		}
	}
	return nil
}

type TaskInfo struct {
	Code             int     `json:"code"`
	TaskID           string  `json:"task_id"`
	Status           string  `json:"status"`
	Reason           string  `json:"reason,omitempty"`
	Url              string  `json:"url,omitempty"`
	RemoteUrl        string  `json:"remote_url,omitempty"`
	Progress         string  `json:"progress,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"` // 用于按倍率计费
	TotalTokens      int     `json:"total_tokens,omitempty"`      // 用于按倍率计费
	Duration         int     `json:"duration,omitempty"`          // 上游实际输出时长（秒）
	BillingDuration  float64 `json:"billing_duration,omitempty"`  // 上游用于计费的实际时长（秒，可含小数）
	// BillableImageCount 是上游按张计费的输入图数量，已扣除上游的免费额度
	// （百炼对输入图 5 张以内返回 0，超出时只返回超出部分）。只用于提交时没记录
	// 输入图张数的旧任务。
	BillableImageCount int `json:"billable_image_count,omitempty"`
	// InputVideoSeconds 是 BillingDuration 里属于输入（参考/待编辑）视频的秒数。价格表
	// 配了输入视频单价时这部分按它单独计费，否则与输出一起按档位单价计费。
	InputVideoSeconds float64 `json:"input_video_seconds,omitempty"`
	// InputImageCount 是上游报告的输入图总张数（未扣免费额度），同样只用于旧任务。
	InputImageCount int                `json:"input_image_count,omitempty"`
	QuotaClamp      *common.QuotaClamp `json:"-"` // 完成结算额度饱和审计标记
}

func FailTaskInfo(reason string) *TaskInfo {
	return &TaskInfo{
		Status: "FAILURE",
		Reason: reason,
	}
}

// RemoveDisabledFields 从请求 JSON 数据中移除渠道设置中禁用的字段
// service_tier: 服务层级字段，可能导致额外计费（OpenAI、Claude、Responses API 支持）
// inference_geo: Claude 数据驻留推理区域字段（仅 Claude 支持，默认过滤）
// speed: Claude 推理速度模式字段（仅 Claude 支持，默认过滤）
// store: 数据存储授权字段，涉及用户隐私（仅 OpenAI、Responses API 支持，默认允许透传，禁用后可能导致 Codex 无法使用）
// safety_identifier: 安全标识符，用于向 OpenAI 报告违规用户（仅 OpenAI 支持，涉及用户隐私）
// stream_options.include_obfuscation: 响应流混淆控制字段（仅 OpenAI Responses API 支持）
func RemoveDisabledFields(jsonData []byte, channelOtherSettings dto.ChannelOtherSettings, channelPassThroughEnabled bool) ([]byte, error) {
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || channelPassThroughEnabled {
		return jsonData, nil
	}
	if !hasRemovableDisabledField(jsonData, channelOtherSettings) {
		return jsonData, nil
	}

	var data map[string]interface{}
	if err := common.Unmarshal(jsonData, &data); err != nil {
		common.SysError("RemoveDisabledFields Unmarshal error :" + err.Error())
		return jsonData, nil
	}

	// 默认移除 service_tier，除非明确允许（避免额外计费风险）
	if !channelOtherSettings.AllowServiceTier {
		if _, exists := data["service_tier"]; exists {
			delete(data, "service_tier")
		}
	}

	// 默认移除 inference_geo，除非明确允许（避免在未授权情况下透传数据驻留区域）
	if !channelOtherSettings.AllowInferenceGeo {
		if _, exists := data["inference_geo"]; exists {
			delete(data, "inference_geo")
		}
	}

	// 默认移除 speed，除非明确允许（避免意外切换 Claude 推理速度模式）
	if !channelOtherSettings.AllowSpeed {
		if _, exists := data["speed"]; exists {
			delete(data, "speed")
		}
	}

	// 默认允许 store 透传，除非明确禁用（禁用可能影响 Codex 使用）
	if channelOtherSettings.DisableStore {
		if _, exists := data["store"]; exists {
			delete(data, "store")
		}
	}

	// 默认移除 safety_identifier，除非明确允许（保护用户隐私，避免向 OpenAI 报告用户信息）
	if !channelOtherSettings.AllowSafetyIdentifier {
		if _, exists := data["safety_identifier"]; exists {
			delete(data, "safety_identifier")
		}
	}

	// 默认移除 stream_options.include_obfuscation，除非明确允许（避免关闭响应流混淆保护）
	if !channelOtherSettings.AllowIncludeObfuscation {
		if streamOptionsAny, exists := data["stream_options"]; exists {
			if streamOptions, ok := streamOptionsAny.(map[string]interface{}); ok {
				if _, includeExists := streamOptions["include_obfuscation"]; includeExists {
					delete(streamOptions, "include_obfuscation")
				}
				if len(streamOptions) == 0 {
					delete(data, "stream_options")
				} else {
					data["stream_options"] = streamOptions
				}
			}
		}
	}

	jsonDataAfter, err := common.Marshal(data)
	if err != nil {
		common.SysError("RemoveDisabledFields Marshal error :" + err.Error())
		return jsonData, nil
	}
	return jsonDataAfter, nil
}

func hasRemovableDisabledField(jsonData []byte, channelOtherSettings dto.ChannelOtherSettings) bool {
	values := gjson.GetManyBytes(
		jsonData,
		"service_tier",
		"inference_geo",
		"speed",
		"store",
		"safety_identifier",
		"stream_options.include_obfuscation",
	)

	return (!channelOtherSettings.AllowServiceTier && values[0].Exists()) ||
		(!channelOtherSettings.AllowInferenceGeo && values[1].Exists()) ||
		(!channelOtherSettings.AllowSpeed && values[2].Exists()) ||
		(channelOtherSettings.DisableStore && values[3].Exists()) ||
		(!channelOtherSettings.AllowSafetyIdentifier && values[4].Exists()) ||
		(!channelOtherSettings.AllowIncludeObfuscation && values[5].Exists())
}

// RemoveGeminiDisabledFields removes disabled fields from Gemini request JSON data
// Currently supports removing functionResponse.id field which Vertex AI does not support
func RemoveGeminiDisabledFields(jsonData []byte) ([]byte, error) {
	if !model_setting.GetGeminiSettings().RemoveFunctionResponseIdEnabled {
		return jsonData, nil
	}

	var data map[string]interface{}
	if err := common.Unmarshal(jsonData, &data); err != nil {
		common.SysError("RemoveGeminiDisabledFields Unmarshal error: " + err.Error())
		return jsonData, nil
	}

	// Process contents array
	// Handle both camelCase (functionResponse) and snake_case (function_response)
	if contents, ok := data["contents"].([]interface{}); ok {
		for _, content := range contents {
			if contentMap, ok := content.(map[string]interface{}); ok {
				if parts, ok := contentMap["parts"].([]interface{}); ok {
					for _, part := range parts {
						if partMap, ok := part.(map[string]interface{}); ok {
							// Check functionResponse (camelCase)
							if funcResp, ok := partMap["functionResponse"].(map[string]interface{}); ok {
								delete(funcResp, "id")
							}
							// Check function_response (snake_case)
							if funcResp, ok := partMap["function_response"].(map[string]interface{}); ok {
								delete(funcResp, "id")
							}
						}
					}
				}
			}
		}
	}

	jsonDataAfter, err := common.Marshal(data)
	if err != nil {
		common.SysError("RemoveGeminiDisabledFields Marshal error: " + err.Error())
		return jsonData, nil
	}
	return jsonDataAfter, nil
}
