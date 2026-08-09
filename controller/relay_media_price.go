// CUSTOM: 图片价格矩阵的提交期计费解析（fork 扩展，矩阵 v2）。
// 配置了 per_image 价格表的模型完全绕开"模型定价"取价，单次费用为加法组件：
//
//	费用 = 输出档价(模式×分辨率) × 张数 n + 输入图单价 × 输入图张数 + 输入token单价 × promptTokens/1M
//
// 输出价格由 n 倍率结算；输入图与提示词价格作为每请求固定组件单独结算，
// 避免上游返回的实际图片数覆盖 n 后重复放大一次性费用。
package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/video_billing"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// resolveRelayPriceData 同步中转的价格解析：图片价格矩阵优先，
// 其余情况回退到原有的 ModelPriceHelper 逻辑。
func resolveRelayPriceData(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta, request any) (hosttypes.PriceData, error) {
	imageRequest, isImage := request.(*dto.ImageRequest)
	if !isImage {
		return helper.ModelPriceHelper(c, info, promptTokens, meta)
	}
	if info.RelayMode != relayconstant.RelayModeImagesGenerations && info.RelayMode != relayconstant.RelayModeImagesEdits {
		return helper.ModelPriceHelper(c, info, promptTokens, meta)
	}
	table, ok := video_billing.GetPriceTable(info.OriginModelName)
	if !ok || table.Unit != video_billing.UnitPerImage {
		return helper.ModelPriceHelper(c, info, promptTokens, meta)
	}

	inputImages := countImageRequestInputImages(imageRequest)
	mode := video_billing.ModeTextToImage
	if inputImages > 0 || info.RelayMode == relayconstant.RelayModeImagesEdits {
		mode = video_billing.ModeImageToImage
	}

	outputPrice, _, matched := video_billing.GetPrice(info.OriginModelName, mode, imageRequest.Size, "")
	if !matched {
		return hosttypes.PriceData{}, fmt.Errorf(
			"image pricing for model %s has no tier matching mode=%s size=%s; add a default tier or the missing tier in Video Pricing settings",
			info.OriginModelName, mode, imageRequest.Size)
	}

	imageN := 1
	if imageRequest.N != nil && *imageRequest.N > 0 {
		imageN = int(*imageRequest.N)
	}

	extraCost := float64(inputImages)*table.InputImagePrice +
		float64(promptTokens)/1_000_000*table.InputTokenPrice

	groupRatioInfo := helper.HandleGroupRatio(c, info)
	priceData := hosttypes.PriceData{
		UsePrice:       true,
		ModelPrice:     outputPrice,
		FixedPrice:     extraCost,
		GroupRatioInfo: groupRatioInfo,
	}
	priceData.AddOtherRatio("n", float64(imageN))
	quota, err := common.QuotaFromFloatStrict((outputPrice*float64(imageN) + extraCost) * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	priceData.QuotaToPreConsume = quota
	info.PriceData = priceData
	return priceData, nil
}

// countImageRequestInputImages 统计请求中的输入参考图张数
// （image/images 字段：数组按元素数计，非空字符串计 1）。
func countImageRequestInputImages(request *dto.ImageRequest) int {
	count := 0
	for _, raw := range [][]byte{request.Image, request.Images} {
		if len(raw) == 0 {
			continue
		}
		var asList []any
		if err := common.Unmarshal(raw, &asList); err == nil {
			count += len(asList)
			continue
		}
		var asString string
		if err := common.Unmarshal(raw, &asString); err == nil && asString != "" {
			count++
		}
	}
	return count
}
