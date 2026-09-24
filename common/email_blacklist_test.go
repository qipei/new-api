package common

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestEmailDomainBlacklist(t *testing.T) {
	OptionMapRWMutex.Lock()
	old := OptionMap
	OptionMap = map[string]string{"EmailDomainBlacklistEnabled": "true", "EmailDomainBlacklist": " maildrop.cc , BLOCKED.EXAMPLE. "}
	OptionMapRWMutex.Unlock()
	t.Cleanup(func() { OptionMapRWMutex.Lock(); OptionMap = old; OptionMapRWMutex.Unlock() })
	for _, tc := range []struct {
		email   string
		blocked bool
	}{
		{"jvii9y1blgo@maildrop.cc", true}, {"  User@MAILDROP.CC  ", true}, {"user@sub.maildrop.cc", true},
		{"a@maildrop.cc.", true}, {"  A@SUB.MAILDROP.CC.  ", true},
		{"a@maildrop.cc..", true}, {"a@notmaildrop.cc.", false}, {"a@maildrop.cc.example.", false},
		{"u@blocked.example", true}, {"u@notmaildrop.cc", false}, {"u@maildrop.cc.example", false}, {"u@qq.com", false}, {"", false},
	} {
		t.Run(tc.email, func(t *testing.T) { assert.Equal(t, tc.blocked, IsEmailDomainBlocked(tc.email)) })
	}
	OptionMapRWMutex.Lock()
	OptionMap["EmailDomainBlacklistEnabled"] = "false"
	OptionMapRWMutex.Unlock()
	assert.False(t, IsEmailDomainBlocked("u@maildrop.cc"))
}

func TestValidateEmailDomainBlacklist(t *testing.T) {
	for _, value := range []string{"", "maildrop.cc", " MAILDROP.CC , example.org "} {
		assert.NoError(t, ValidateEmailDomainBlacklist(value))
	}
	for _, value := range []string{"u@maildrop.cc", "https://maildrop.cc", "*.maildrop.cc", "maildrop", "-bad.com", "maildrop..cc"} {
		assert.Error(t, ValidateEmailDomainBlacklist(value), value)
	}
}
