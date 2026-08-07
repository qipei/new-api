package model

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initOptionMapForTest 保证 common.OptionMap 可写(UpdateOption 持久化游标时需要)
func initOptionMapForTest(t *testing.T) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMapRWMutex.Unlock()
}

func commissionTestSetting(cType string, value float64, limit int) operation_setting.CommissionSetting {
	return operation_setting.CommissionSetting{
		Enabled: true, Type: cType, Value: value, TopupCountLimit: limit,
	}
}

// 建一个邀请人 + 被邀请人, 返回两者 id
func setupInvitePair(t *testing.T) (inviterId, inviteeId int) {
	t.Helper()
	inviter := &User{Username: "inviter_u", Password: "pw12345678", Role: common.RoleCommonUser, AffCode: common.GetRandomString(8)}
	require.NoError(t, DB.Create(inviter).Error)
	invitee := &User{Username: "invitee_u", Password: "pw12345678", Role: common.RoleCommonUser, AffCode: common.GetRandomString(8), InviterId: inviter.Id}
	require.NoError(t, DB.Create(invitee).Error)
	return inviter.Id, invitee.Id
}

func createSuccessTopUp(t *testing.T, userId int, method string, amount int64, money float64, completeTime int64) *TopUp {
	t.Helper()
	topUp := &TopUp{
		UserId: userId, Amount: amount, Money: money,
		TradeNo:       common.GetRandomString(16),
		PaymentMethod: method, Status: common.TopUpStatusSuccess,
		CreateTime: completeTime - 60, CompleteTime: completeTime,
	}
	require.NoError(t, DB.Create(topUp).Error)
	return topUp
}

func TestProcessCommissionPercent(t *testing.T) {
	truncateTables(t)
	oldQPU := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQPU })

	inviterId, inviteeId := setupInvitePair(t)
	// epay 类订单: Amount=10 美元 → 到账 10*500000, 10% 佣金 = 500000
	topUp := createSuccessTopUp(t, inviteeId, "alipay", 10, 70.0, common.GetTimestamp())

	require.NoError(t, ProcessCommissionForTopUp(topUp, commissionTestSetting(operation_setting.CommissionTypePercent, 10, 0)))

	var record CommissionRecord
	require.NoError(t, DB.Where("topup_id = ?", topUp.Id).First(&record).Error)
	assert.Equal(t, 500000, record.CommissionQuota)
	assert.Equal(t, 5000000, record.CreditedQuota)
	assert.Equal(t, inviterId, record.InviterId)
	assert.Equal(t, inviteeId, record.InviteeId)

	inviter, err := GetUserById(inviterId, true)
	require.NoError(t, err)
	assert.Equal(t, 500000, inviter.AffQuota)
	assert.Equal(t, 500000, inviter.AffHistoryQuota)
}

func TestProcessCommissionFixed(t *testing.T) {
	truncateTables(t)
	inviterId, inviteeId := setupInvitePair(t)
	topUp := createSuccessTopUp(t, inviteeId, "alipay", 5, 35.0, common.GetTimestamp())

	require.NoError(t, ProcessCommissionForTopUp(topUp, commissionTestSetting(operation_setting.CommissionTypeFixed, 12345, 0)))

	inviter, err := GetUserById(inviterId, true)
	require.NoError(t, err)
	assert.Equal(t, 12345, inviter.AffQuota)
}

func TestProcessCommissionIdempotent(t *testing.T) {
	truncateTables(t)
	inviterId, inviteeId := setupInvitePair(t)
	topUp := createSuccessTopUp(t, inviteeId, "alipay", 10, 70.0, common.GetTimestamp())
	setting := commissionTestSetting(operation_setting.CommissionTypeFixed, 1000, 0)

	require.NoError(t, ProcessCommissionForTopUp(topUp, setting))
	require.NoError(t, ProcessCommissionForTopUp(topUp, setting)) // 重复处理

	var count int64
	require.NoError(t, DB.Model(&CommissionRecord{}).Where("topup_id = ?", topUp.Id).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	inviter, err := GetUserById(inviterId, true)
	require.NoError(t, err)
	assert.Equal(t, 1000, inviter.AffQuota) // 只发一次
}

func TestProcessCommissionSkipRules(t *testing.T) {
	truncateTables(t)
	_, inviteeId := setupInvitePair(t)
	setting := commissionTestSetting(operation_setting.CommissionTypeFixed, 1000, 0)

	// balance 订单不计佣
	balanceTopUp := createSuccessTopUp(t, inviteeId, PaymentMethodBalance, 10, 5.0, common.GetTimestamp())
	require.NoError(t, ProcessCommissionForTopUp(balanceTopUp, setting))

	// Amount=0 (订阅账单条目) 不计佣
	subTopUp := createSuccessTopUp(t, inviteeId, PaymentMethodStripe, 0, 20.0, common.GetTimestamp())
	require.NoError(t, ProcessCommissionForTopUp(subTopUp, setting))

	// 无邀请人的用户不计佣
	orphan := &User{Username: "orphan_u", Password: "pw12345678", Role: common.RoleCommonUser, AffCode: common.GetRandomString(8)}
	require.NoError(t, DB.Create(orphan).Error)
	orphanTopUp := createSuccessTopUp(t, orphan.Id, "alipay", 10, 70.0, common.GetTimestamp())
	require.NoError(t, ProcessCommissionForTopUp(orphanTopUp, setting))

	var count int64
	require.NoError(t, DB.Model(&CommissionRecord{}).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestProcessCommissionCountLimit(t *testing.T) {
	truncateTables(t)
	inviterId, inviteeId := setupInvitePair(t)
	setting := commissionTestSetting(operation_setting.CommissionTypeFixed, 1000, 2) // 仅前 2 笔

	now := common.GetTimestamp()
	t1 := createSuccessTopUp(t, inviteeId, "alipay", 1, 7.0, now)
	t2 := createSuccessTopUp(t, inviteeId, "alipay", 1, 7.0, now+1)
	t3 := createSuccessTopUp(t, inviteeId, "alipay", 1, 7.0, now+2)

	require.NoError(t, ProcessCommissionForTopUp(t1, setting))
	require.NoError(t, ProcessCommissionForTopUp(t2, setting))
	require.NoError(t, ProcessCommissionForTopUp(t3, setting)) // 第 3 笔超限

	var count int64
	require.NoError(t, DB.Model(&CommissionRecord{}).Count(&count).Error)
	assert.Equal(t, int64(2), count)

	inviter, err := GetUserById(inviterId, true)
	require.NoError(t, err)
	assert.Equal(t, 2000, inviter.AffQuota)
}

func TestScanAndProcessCommissionsBasic(t *testing.T) {
	truncateTables(t)
	initOptionMapForTest(t)
	inviterId, inviteeId := setupInvitePair(t)
	now := common.GetTimestamp()
	createSuccessTopUp(t, inviteeId, "alipay", 1, 7.0, now-10)

	setting := commissionTestSetting(operation_setting.CommissionTypeFixed, 1000, 0)
	setting.ScanCursor = now - 100

	processed, err := scanAndProcessCommissions(setting, now)
	require.NoError(t, err)
	assert.Equal(t, 1, processed)

	inviter, err := GetUserById(inviterId, true)
	require.NoError(t, err)
	assert.Equal(t, 1000, inviter.AffQuota)

	// 游标已持久化为本轮扫描时间
	var opt Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", "commission_setting.scan_cursor").First(&opt).Error)
	assert.Equal(t, strconv.FormatInt(now, 10), opt.Value)
}

func TestScanCompleteTimeZeroFallback(t *testing.T) {
	truncateTables(t)
	initOptionMapForTest(t)
	_, inviteeId := setupInvitePair(t)
	now := common.GetTimestamp()
	// 模拟易支付订单: complete_time 为 0, 只有 create_time
	topUp := &TopUp{
		UserId: inviteeId, Amount: 1, Money: 7.0,
		TradeNo: common.GetRandomString(16), PaymentMethod: "alipay",
		Status: common.TopUpStatusSuccess, CreateTime: now - 10, CompleteTime: 0,
	}
	require.NoError(t, DB.Create(topUp).Error)

	setting := commissionTestSetting(operation_setting.CommissionTypeFixed, 1000, 0)
	setting.ScanCursor = now - 100

	processed, err := scanAndProcessCommissions(setting, now)
	require.NoError(t, err)
	assert.Equal(t, 1, processed)
}

func TestScanCursorInitAndDisabled(t *testing.T) {
	truncateTables(t)
	initOptionMapForTest(t)
	_, inviteeId := setupInvitePair(t)
	now := common.GetTimestamp()
	createSuccessTopUp(t, inviteeId, "alipay", 1, 7.0, now-10)

	// 游标为 0(首次启动): 只初始化游标, 不发佣(历史订单不补发)
	setting := commissionTestSetting(operation_setting.CommissionTypeFixed, 1000, 0)
	setting.ScanCursor = 0
	processed, err := scanAndProcessCommissions(setting, now)
	require.NoError(t, err)
	assert.Equal(t, 0, processed)

	// 功能关闭: 不扫描但游标照常推进
	setting.Enabled = false
	setting.ScanCursor = now - 100
	processed, err = scanAndProcessCommissions(setting, now)
	require.NoError(t, err)
	assert.Equal(t, 0, processed)

	var count int64
	require.NoError(t, DB.Model(&CommissionRecord{}).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestScanWindowExcludesOldOrders(t *testing.T) {
	truncateTables(t)
	initOptionMapForTest(t)
	_, inviteeId := setupInvitePair(t)
	now := common.GetTimestamp()
	// 完成时间在窗口之前的订单不应被扫到
	createSuccessTopUp(t, inviteeId, "alipay", 1, 7.0, now-10000)

	setting := commissionTestSetting(operation_setting.CommissionTypeFixed, 1000, 0)
	setting.ScanCursor = now - 100

	processed, err := scanAndProcessCommissions(setting, now)
	require.NoError(t, err)
	assert.Equal(t, 0, processed)
}

func TestCreditedTopUpQuotaBranches(t *testing.T) {
	oldQPU := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQPU })

	// stripe: Money × QuotaPerUnit
	assert.Equal(t, 1000000, creditedTopUpQuota(&TopUp{PaymentMethod: PaymentMethodStripe, Money: 2, Amount: 2}))
	// creem: Amount 直接就是额度
	assert.Equal(t, 3000, creditedTopUpQuota(&TopUp{PaymentMethod: PaymentMethodCreem, Amount: 3000}))
	// 默认(epay/waffo): Amount × QuotaPerUnit
	assert.Equal(t, 2500000, creditedTopUpQuota(&TopUp{PaymentMethod: "alipay", Amount: 5}))
}
