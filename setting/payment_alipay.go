package setting

// 支付宝「电脑网站支付」直连配置。
// 单价与最低充值额沿用 operation_setting 里的 Price / MinTopUp（同为人民币收款），
// 不单独设置，避免出现两套互相打架的价格口径。
var (
	// AlipayEnabled 运营开关。直连凭证通常长期保留，需要能在不清空私钥的前提下临时停用。
	AlipayEnabled bool

	AlipayAppID string
	// AlipayPrivateKey 应用 RSA2 私钥（PKCS#1 或 PKCS#8）。
	AlipayPrivateKey string
	// AlipayPublicKey 支付宝公钥，用于异步通知验签。
	AlipayPublicKey string
	// AlipaySellerID 收款方账号 ID（2088 开头 16 位）。
	// 支付宝要求异步通知必须校验该字段，见 docs 第 07 节的五项校验。
	AlipaySellerID string
	// AlipaySandbox 指向沙箱网关。沙箱仅支持余额支付，且业务逻辑最终以生产为准。
	AlipaySandbox bool
)
