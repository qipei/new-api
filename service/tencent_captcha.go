package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	captcha "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/captcha/v20190722"
	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

// TencentCaptchaCredentials is shared by protected actions. Callers select
// trusted server-side credentials; clients never supply application secrets.
type TencentCaptchaCredentials struct {
	AppID, AppSecretKey, SecretID, SecretKey string
}

const tencentCaptchaTypeSlide = 9
const tencentCaptchaPassCode = 1

// ErrCaptchaNotConfigured indicates missing or invalid server-side credentials.
var ErrCaptchaNotConfigured = errors.New("captcha is not configured")

var usedCaptchaTickets = struct {
	sync.Mutex
	expires map[string]time.Time
}{expires: make(map[string]time.Time)}

// VerifyTencentCaptcha verifies Web/App or native mini-program tickets, then
// consumes the ticket once across protected actions. Provider failures and
// unavailable shared replay storage fail closed.
func VerifyTencentCaptcha(ctx context.Context, credentials TencentCaptchaCredentials, clientType, ticket, randstr, userIP string) error {
	if clientType != "web" && clientType != "mini_program" {
		return errors.New("unsupported captcha client")
	}
	appID, err := strconv.ParseUint(strings.TrimSpace(credentials.AppID), 10, 64)
	if err != nil || appID == 0 || strings.TrimSpace(credentials.AppSecretKey) == "" ||
		strings.TrimSpace(credentials.SecretID) == "" || strings.TrimSpace(credentials.SecretKey) == "" {
		return ErrCaptchaNotConfigured
	}
	ticket, randstr = strings.TrimSpace(ticket), strings.TrimSpace(randstr)
	if ticket == "" || strings.HasPrefix(ticket, "trerror_") || len(ticket) > 8192 || len(randstr) > 1024 || (clientType == "web" && randstr == "") {
		return errors.New("invalid captcha ticket")
	}
	clientProfile := profile.NewClientProfile()
	clientProfile.HttpProfile.ReqTimeout = 10
	client, err := captcha.NewClient(tccommon.NewCredential(strings.TrimSpace(credentials.SecretID), strings.TrimSpace(credentials.SecretKey)), "", clientProfile)
	if err != nil {
		return err
	}
	if clientType == "mini_program" {
		request := captcha.NewDescribeCaptchaMiniResultRequest()
		request.CaptchaType = tccommon.Uint64Ptr(tencentCaptchaTypeSlide)
		request.Ticket = tccommon.StringPtr(ticket)
		request.UserIp = tccommon.StringPtr(userIP)
		request.CaptchaAppId = tccommon.Uint64Ptr(appID)
		request.AppSecretKey = tccommon.StringPtr(strings.TrimSpace(credentials.AppSecretKey))
		response, err := client.DescribeCaptchaMiniResultWithContext(ctx, request)
		if err != nil {
			return err
		}
		if response == nil || response.Response == nil || response.Response.CaptchaCode == nil || *response.Response.CaptchaCode != tencentCaptchaPassCode {
			return errors.New("captcha verification failed")
		}
	} else {
		request := captcha.NewDescribeCaptchaResultRequest()
		request.CaptchaType = tccommon.Uint64Ptr(tencentCaptchaTypeSlide)
		request.Ticket = tccommon.StringPtr(ticket)
		request.Randstr = tccommon.StringPtr(randstr)
		request.UserIp = tccommon.StringPtr(userIP)
		request.CaptchaAppId = tccommon.Uint64Ptr(appID)
		request.AppSecretKey = tccommon.StringPtr(strings.TrimSpace(credentials.AppSecretKey))
		response, err := client.DescribeCaptchaResultWithContext(ctx, request)
		if err != nil {
			return err
		}
		if response == nil || response.Response == nil || response.Response.CaptchaCode == nil || *response.Response.CaptchaCode != tencentCaptchaPassCode ||
			(response.Response.EvilLevel != nil && *response.Response.EvilLevel == 100) {
			return errors.New("captcha verification failed")
		}
	}
	// Tencent tickets expire after five minutes. Keep a longer replay marker;
	// atomic insertion also prevents concurrent reuse across users or actions.
	key := fmt.Sprintf("captcha:used:v1:%x", sha256.Sum256([]byte(strconv.FormatUint(appID, 10)+":"+ticket)))
	const ttl = 10 * time.Minute
	if common.RedisEnabled {
		ok, err := common.RDB.SetNX(ctx, key, "1", ttl).Result()
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("captcha ticket already used")
		}
		return nil
	}
	usedCaptchaTickets.Lock()
	defer usedCaptchaTickets.Unlock()
	now := time.Now()
	for usedKey, expires := range usedCaptchaTickets.expires {
		if !expires.After(now) {
			delete(usedCaptchaTickets.expires, usedKey)
		}
	}
	if _, exists := usedCaptchaTickets.expires[key]; exists {
		return errors.New("captcha ticket already used")
	}
	usedCaptchaTickets.expires[key] = now.Add(ttl)
	return nil
}
