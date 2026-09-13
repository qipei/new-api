package setting

// 微信支付「Native 扫码」直连配置。
// 单价与最低充值额沿用 operation_setting 里的 Price / MinTopUp。
//
// 注意：申请微信支付公钥后，商户平台的「开启公钥切换」默认是关闭的。
// 未开启时微信仍用平台证书签名，公钥验签会全部失败。
var (
	// WechatPayEnabled 运营开关，语义同 AlipayEnabled。
	WechatPayEnabled bool

	// WechatPayAppID 已认证服务号 / 公众号 / 小程序 / 移动应用的 AppID，
	// 必须与 WechatPayMchID 完成授权绑定，否则下单返回 APPID_MCHID_NOT_MATCH。
	WechatPayAppID string
	WechatPayMchID string
	// WechatPayCertSerialNo 商户 API 证书序列号。
	WechatPayCertSerialNo string
	// WechatPayPrivateKey 商户 API 私钥（apiclient_key.pem 内容）。
	WechatPayPrivateKey string
	// WechatPayAPIv3Key APIv3 密钥。未配置时微信不会发送任何回调通知。
	WechatPayAPIv3Key string
	// WechatPayPublicKey 微信支付公钥内容，用于应答与回调验签。
	WechatPayPublicKey string
	// WechatPayPublicKeyID 微信支付公钥 ID，形如 PUB_KEY_ID_xxx。
	// 前缀不可省略，gopay 会用它匹配应答头中的 Wechatpay-Serial。
	WechatPayPublicKeyID string
)
