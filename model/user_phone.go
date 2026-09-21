package model

import (
	"errors"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// 手机号登录面向中国大陆号码，存储时统一为 11 位纯数字，不带 +86 前缀。
var chinaMobilePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

// NormalizePhone 去掉用户输入里的分隔符、国际区号和空白，得到可直接比较和存储的号码。
func NormalizePhone(phone string) string {
	var builder strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			builder.WriteRune(r)
		}
	}
	digits := builder.String()
	digits = strings.TrimPrefix(digits, "0086")
	if len(digits) == 13 && strings.HasPrefix(digits, "86") {
		digits = digits[2:]
	}
	return digits
}

// IsValidPhone 校验规范化后的号码是否是合法的中国大陆手机号。
func IsValidPhone(phone string) bool {
	return chinaMobilePattern.MatchString(phone)
}

// phoneQuery 只匹配未注销的账号：用户自助注销（软删除）后号码即释放，
// 可以重新注册或被其他账号绑定；注销记录上的号码保留用于审计。
func phoneQuery(tx *gorm.DB, phone string) *gorm.DB {
	if tx == nil {
		tx = DB
	}
	return tx.Model(&User{}).Where("phone = ?", NormalizePhone(phone))
}

func ensurePhoneAvailableWithTx(tx *gorm.DB, phone string, excludeUserID int) error {
	phone = NormalizePhone(phone)
	if phone == "" {
		return nil
	}
	query := phoneQuery(tx, phone)
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrPhoneAlreadyTaken
	}
	return nil
}

// EnsurePhoneAvailable 在事务外检查手机号是否已被别的账号占用。
func EnsurePhoneAvailable(phone string, excludeUserID int) error {
	return ensurePhoneAvailableWithTx(DB, phone, excludeUserID)
}

// withPhoneLock 串行化同一手机号上的“先查后写”，避免两个并发请求把同一个号码
// 绑到两个账号上。锁的粒度与语义与邮箱的 withNormalizedEmailLock 一致：
//
//   - PostgreSQL：事务级 advisory 锁。
//   - MySQL：对 phone 索引取 next-key/gap 锁，阻塞同值的并发插入。
//   - SQLite：单写模型天然串行，无需显式加锁。
func withPhoneLock(tx *gorm.DB, phone string, fn func(tx *gorm.DB) error) error {
	phone = NormalizePhone(phone)
	if phone == "" {
		return fn(tx)
	}
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "phone:"+phone).Error; err != nil {
			return err
		}
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		var ids []int
		if err := tx.Raw("SELECT id FROM users WHERE phone = ? FOR UPDATE", phone).Scan(&ids).Error; err != nil {
			return err
		}
	}
	return fn(tx)
}

// GetUserByPhone 按手机号查找启用中的账号，未注册时返回 gorm.ErrRecordNotFound。
func GetUserByPhone(phone string) (*User, error) {
	phone = NormalizePhone(phone)
	if phone == "" {
		return nil, ErrPhoneInvalid
	}
	var user User
	if err := DB.Where("phone = ?", phone).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// BindPhoneToUser 原子地检查手机号可用性并写入用户，绑定冲突时返回 ErrPhoneAlreadyTaken。
func BindPhoneToUser(user *User, phone string) error {
	phone = NormalizePhone(phone)
	if !IsValidPhone(phone) {
		return ErrPhoneInvalid
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return withPhoneLock(tx, phone, func(tx *gorm.DB) error {
			if err := ensurePhoneAvailableWithTx(tx, phone, user.Id); err != nil {
				return err
			}
			if err := tx.Model(&User{}).Where("id = ?", user.Id).Update("phone", phone).Error; err != nil {
				return err
			}
			return tx.First(user, user.Id).Error
		})
	}); err != nil {
		return err
	}
	return updateUserCache(*user)
}

// RegisterUserByPhone 为一个尚未注册的手机号创建账号。用户名由手机号派生，
// 冲突时追加随机后缀；整个创建过程在手机号锁内完成，避免并发重复注册。
// 并发下第二个请求会拿到先创建出来的账号，isNew 为 false。
func RegisterUserByPhone(phone string, inviterId int) (user *User, isNew bool, err error) {
	phone = NormalizePhone(phone)
	if !IsValidPhone(phone) {
		return nil, false, ErrPhoneInvalid
	}

	if err := DB.Transaction(func(tx *gorm.DB) error {
		return withPhoneLock(tx, phone, func(tx *gorm.DB) error {
			existing := &User{}
			err := tx.Where("phone = ?", phone).First(existing).Error
			if err == nil {
				user = existing
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}

			if err := ensurePhoneAvailableWithTx(tx, phone, 0); err != nil {
				return err
			}

			username, err := generatePhoneUsername(tx, phone)
			if err != nil {
				return err
			}
			newUser := &User{
				Username:    username,
				DisplayName: MaskPhone(phone),
				Phone:       phone,
				InviterId:   inviterId,
				Role:        common.RoleCommonUser,
				Status:      common.UserStatusEnabled,
			}
			if err := newUser.InsertWithTx(tx, inviterId); err != nil {
				return err
			}
			user = newUser
			isNew = true
			return nil
		})
	}); err != nil {
		return nil, false, err
	}

	// 赠送额度、邀请奖励和默认边栏配置必须等事务提交后再写，
	// 与 OAuth 首次登录建号走同一条收尾路径。
	if isNew {
		user.FinalizeOAuthUserCreation(inviterId)
	}
	return user, isNew, nil
}

// generatePhoneUsername 以「u + 手机号」作为用户名。该用户名已被占用时（例如同一号码的
// 旧账号已注销，注销记录仍保留用户名）追加 4 位随机后缀重试。
func generatePhoneUsername(tx *gorm.DB, phone string) (string, error) {
	candidate := "u" + phone
	for attempt := 0; attempt < 8; attempt++ {
		if attempt > 0 {
			candidate = "u" + phone + "_" + strings.ToLower(common.GetRandomString(4))
		}
		var count int64
		if err := tx.Unscoped().Model(&User{}).Where("username = ?", candidate).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
	return "", errors.New("failed to allocate username for phone")
}

// MaskPhone 隐藏号码中间四位，用于默认显示名和日志，避免完整号码被直接展示或落盘。
func MaskPhone(phone string) string {
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}
