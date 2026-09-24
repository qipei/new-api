package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileUpdateCannotOverwriteNewCheckinCaptchaTrust(t *testing.T) {
	truncateTables(t)
	user := User{Username: "captcha-profile", CheckinCaptchaVerifiedAt: 100}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, RecordCheckinCaptchaVerification(user.Id, 200))
	user.DisplayName = "Updated profile"
	require.NoError(t, user.UpdateWithTx(DB, false))
	verified, err := GetCheckinCaptchaVerifiedAt(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(200), verified)
	assert.Equal(t, "Updated profile", user.DisplayName)
	require.NoError(t, RecordCheckinCaptchaVerification(user.Id, 150))
	verified, err = GetCheckinCaptchaVerifiedAt(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(200), verified, "late verification writes cannot move trust backwards")
}
