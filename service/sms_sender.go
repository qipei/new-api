package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dysmsapi "github.com/alibabacloud-go/dysmsapi-20170525/v4/client"
	"github.com/alibabacloud-go/tea/tea"
)

// ErrSMSNotConfigured 表示短信通道还没有配置齐全，调用方应提示管理员去系统设置里补全。
var ErrSMSNotConfigured = errors.New("sms channel is not configured")

// normalizeSMSEndpoint 把管理员填写的完整 URL 收敛成 SDK 需要的主机名。
// 配置项里习惯写 https://dysmsapi.aliyuncs.com/，而 SDK 只接受 dysmsapi.aliyuncs.com。
func normalizeSMSEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimSuffix(endpoint, "/")
	if endpoint == "" {
		return "dysmsapi.aliyuncs.com"
	}
	return endpoint
}

// SendSMSCode 通过阿里云短信服务下发验证码。调试模式（LocalOnly）下不调用阿里云，
// 由调用方直接把配置里的固定验证码返回给登录流程。
func SendSMSCode(phone string, code string) error {
	settings := system_setting.GetSMSSettings()
	if !settings.Configured() {
		return ErrSMSNotConfigured
	}
	if settings.LocalOnly {
		common.SysLog(fmt.Sprintf("SMS local-only mode: skip sending code to %s", model.MaskPhone(phone)))
		return nil
	}

	client, err := dysmsapi.NewClient(&openapi.Config{
		AccessKeyId:     tea.String(strings.TrimSpace(settings.AccessKeyId)),
		AccessKeySecret: tea.String(strings.TrimSpace(settings.AccessKeySecret)),
		Endpoint:        tea.String(normalizeSMSEndpoint(settings.Endpoint)),
	})
	if err != nil {
		return err
	}

	templateParam, err := common.Marshal(map[string]string{"code": code})
	if err != nil {
		return err
	}

	response, err := client.SendSms(&dysmsapi.SendSmsRequest{
		PhoneNumbers:  tea.String(phone),
		SignName:      tea.String(strings.TrimSpace(settings.SignName)),
		TemplateCode:  tea.String(strings.TrimSpace(settings.TemplateCode)),
		TemplateParam: tea.String(string(templateParam)),
	})
	if err != nil {
		return err
	}
	if response == nil || response.Body == nil {
		return errors.New("empty response from sms provider")
	}
	if tea.StringValue(response.Body.Code) != "OK" {
		return fmt.Errorf("sms provider rejected the request: %s %s",
			tea.StringValue(response.Body.Code), tea.StringValue(response.Body.Message))
	}
	return nil
}
