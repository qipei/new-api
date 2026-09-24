package model

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
)

func TestEmailBlacklistBlocksAccountCreationAndBinding(t *testing.T) {
	truncateTables(t)
	common.OptionMapRWMutex.Lock()
	old := common.OptionMap
	common.OptionMap = map[string]string{"EmailDomainBlacklistEnabled": "true", "EmailDomainBlacklist": "maildrop.cc"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); common.OptionMap = old; common.OptionMapRWMutex.Unlock() })
	for _, email := range []string{"USER@MAILDROP.CC", "user@maildrop.cc.", "user@sub.maildrop.cc."} {
		user := &User{Username: "blocked-signup", Email: email}
		assert.ErrorIs(t, user.Insert(0), common.ErrEmailDomainBlocked)
		assert.ErrorIs(t, DB.Transaction(func(tx *gorm.DB) error { return user.InsertWithTx(tx, 0) }), common.ErrEmailDomainBlocked)
	}
	var count int64
	require.NoError(t, DB.Model(&User{}).Count(&count).Error)
	assert.Zero(t, count)
	existing := &User{Username: "existing", Email: "existing@example.com"}
	require.NoError(t, DB.Create(existing).Error)
	for _, email := range []string{"u@maildrop.cc", "u@maildrop.cc.", "u@sub.maildrop.cc."} {
		assert.ErrorIs(t, BindEmailToUser(existing, email), common.ErrEmailDomainBlocked)
	}
	require.NoError(t, DB.First(existing, existing.Id).Error)
	assert.Equal(t, "existing@example.com", existing.Email)
	// Accounts without email (e.g. phone registration) remain supported.
	phoneUser := &User{Username: "phone-only"}
	require.NoError(t, phoneUser.InsertWithTx(DB, 0))
	originalQuota := phoneUser.Quota
	topup := &TopUp{UserId: phoneUser.Id, TradeNo: "blacklisted-email-payment", Amount: 10, PaymentProvider: PaymentProviderCreem, Status: common.TopUpStatusPending}
	require.NoError(t, DB.Create(topup).Error)
	require.NoError(t, RechargeCreem(topup.TradeNo, "payment@maildrop.cc.", "", "127.0.0.1"))
	require.NoError(t, DB.First(phoneUser, phoneUser.Id).Error)
	assert.Empty(t, phoneUser.Email)
	assert.Equal(t, originalQuota+10, phoneUser.Quota)
	require.NoError(t, DB.First(topup, topup.Id).Error)
	assert.Equal(t, common.TopUpStatusSuccess, topup.Status)
}

func TestEmailBlacklistOptionFailureDoesNotPublish(t *testing.T) {
	truncateTables(t)
	common.OptionMapRWMutex.Lock()
	old := common.OptionMap
	common.OptionMap = map[string]string{"EmailDomainBlacklistEnabled": "false", "EmailDomainBlacklist": ""}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); common.OptionMap = old; common.OptionMapRWMutex.Unlock() })
	writeError := errors.New("settings storage unavailable")
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("test:reject_blacklist_save", func(tx *gorm.DB) {
		if tx.Statement.Table == "options" {
			tx.AddError(writeError)
		}
	}))
	t.Cleanup(func() { require.NoError(t, DB.Callback().Update().Remove("test:reject_blacklist_save")) })
	for key, value := range map[string]string{"EmailDomainBlacklist": "maildrop.cc", "EmailDomainBlacklistEnabled": "true"} {
		assert.ErrorIs(t, UpdateOption(key, value), writeError)
	}
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, "false", common.OptionMap["EmailDomainBlacklistEnabled"])
	assert.Empty(t, common.OptionMap["EmailDomainBlacklist"])
	common.OptionMapRWMutex.RUnlock()
	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key IN ?", []string{"EmailDomainBlacklist", "EmailDomainBlacklistEnabled"}).Count(&count).Error)
	assert.Zero(t, count)
}
