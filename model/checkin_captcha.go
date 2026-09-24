package model

// GetCheckinCaptchaVerifiedAt deliberately reads the primary database: wallet
// cache entries must not carry or overwrite the CAPTCHA trust timestamp.
func GetCheckinCaptchaVerifiedAt(userID int) (int64, error) {
	var user User
	err := DB.Select("checkin_captcha_verified_at").First(&user, userID).Error
	return user.CheckinCaptchaVerifiedAt, err
}

func RecordCheckinCaptchaVerification(userID int, verifiedAt int64) error {
	// Concurrent successful verifications must never move trust backwards.
	return DB.Model(&User{}).Where("id = ? AND checkin_captcha_verified_at < ?", userID, verifiedAt).
		Update("checkin_captcha_verified_at", verifiedAt).Error
}
