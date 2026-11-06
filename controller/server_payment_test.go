package controller

import (
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 服务器订单的支付宝官方直连回调测试。
//
// 这一层验的是「支付宝通知进来 → RSA2 验签 → app_id 比对 → 金额校验
// → 开机器」整条链路。任何一环松了，钱和货就会对不上。
//
// 复用 topup_payment_hardening_test.go 的 setupAlipayNotifyConfig 生成
// 临时 RSA 密钥对，以及 topup_alipay_test.go 的 signAlipayNotify 按官方
// SDK 口径签名，保证测的是真验签而不是 mock 掉校验。

const serverAlipayTestAppID = "2026110600000001"

// setupServerPaymentDB 为服务器支付用例准备一个独立的内存数据库。
//
// 每个用例一份独立库，用例之间互不污染（订单表、实例表都是全局的）。
func setupServerPaymentDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.ServerOrder{},
		&model.ServerInstance{},
		&model.ServerInstanceEvent{},
		&model.Log{},
	))

	oldDB, oldLogDB := model.DB, model.LOG_DB
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})
	return db
}

// truncateServerTables 清空服务器相关表。
//
// 独立内存库已经按用例隔离了，这里再清一次是为了同一个用例内
// 「下单 → 回调 → 再回调」这类多阶段流程不互相干扰。
func truncateServerTables(t *testing.T) {
	t.Helper()
	model.DB.Exec("DELETE FROM server_instance_events")
	model.DB.Exec("DELETE FROM server_instances")
	model.DB.Exec("DELETE FROM server_orders")
	model.DB.Exec("DELETE FROM logs")
	model.DB.Exec("DELETE FROM users")
}

// createServerPayUser 建一个带余额的用户。
func createServerPayUser(t *testing.T, quota int) *model.User {
	t.Helper()
	user := &model.User{
		Username: "server_pay_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, model.DB.Create(user).Error)
	return user
}

// setupServerAlipay 装好支付宝直连配置，返回给通知签名的私钥。
func setupServerAlipay(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	return setupAlipayNotifyConfig(t, serverAlipayTestAppID)
}

// serverAlipayNotifyForm 构造一份指向服务器订单的支付宝成功通知。
func serverAlipayNotifyForm(
	t *testing.T,
	key *rsa.PrivateKey,
	tradeNo string,
	amount string,
) url.Values {
	t.Helper()
	form := url.Values{}
	form.Set("notify_time", "2026-11-06 10:00:00")
	form.Set("notify_type", "trade_status_sync")
	form.Set("notify_id", "SRV-NOTIFY-ID")
	form.Set("app_id", serverAlipayTestAppID)
	form.Set("charset", "utf-8")
	form.Set("version", "1.0")
	form.Set("sign_type", "RSA2")
	form.Set("trade_no", "2026110622000000000000000")
	form.Set("out_trade_no", tradeNo)
	form.Set("seller_id", "2088000000000000")
	form.Set("buyer_id", "2088000000000001")
	form.Set("total_amount", amount)
	form.Set("receipt_amount", amount)
	form.Set("buyer_pay_amount", amount)
	form.Set("subject", "云服务器")
	form.Set("gmt_payment", "2026-11-06 10:00:00")
	form.Set("trade_status", "TRADE_SUCCESS")
	signAlipayNotify(t, form, key)
	return form
}

// callServerAlipayNotify 模拟支付宝回调，返回响应体（success / fail）。
func callServerAlipayNotify(t *testing.T, form url.Values) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(
		http.MethodPost,
		"https://example.test/api/server/pay/alipay/notify",
		strings.NewReader(form.Encode()),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "203.0.113.10:4321"
	c.Request = request

	ServerAlipayNotify(c)
	return recorder.Body.String()
}

// serverOrderMoney 把订单金额格式化成支付宝通知里的金额字符串。
func serverOrderMoney(order *model.ServerOrder) string {
	return fmt.Sprintf("%.2f", order.Amount)
}

// createServerOrderForNotify 建一笔 pending 的支付宝直连订单。
func createServerOrderForNotify(t *testing.T, userId int) *model.ServerOrder {
	t.Helper()
	config := setting.ServerConfig{
		Region:     "guangzhou",
		Plan:       "lite",
		SpecId:     "lite-2c2g",
		Mbps:       5,
		DurationId: "1mo",
	}
	quote, err := setting.ComputeServerQuote(config)
	require.NoError(t, err)

	order, _, err := model.PurchaseServerInstanceWithProvider(
		userId, config, quote, model.PaymentMethodAlipay, model.PaymentProviderAlipay, "",
	)
	require.NoError(t, err)
	return order
}

func TestServerAlipayNotifyProvisionsInstanceOnSuccess(t *testing.T) {
	setupServerPaymentDB(t)
	key := setupServerAlipay(t)
	truncateServerTables(t)

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)
	require.Equal(t, model.PaymentProviderAlipay, order.PaymentProvider)

	form := serverAlipayNotifyForm(t, key, order.TradeNo, serverOrderMoney(order))
	assert.Equal(t, "success", callServerAlipayNotify(t, form))

	var updated model.ServerOrder
	require.NoError(t, model.DB.Where("id = ?", order.Id).First(&updated).Error)
	assert.Equal(t, common.TopUpStatusSuccess, updated.Status)
	assert.NotZero(t, updated.InstanceId, "回调成功后必须开通实例")

	var instance model.ServerInstance
	require.NoError(t, model.DB.Where("order_id = ?", order.Id).First(&instance).Error)
	assert.Equal(t, model.ServerInstanceStatusRunning, instance.Status)
	assert.NotEmpty(t, instance.Password)

	// 下单阶段不扣额度；在线支付的钱不进站内余额。
	var after model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&after).Error)
	assert.Equal(t, 100_000_000, after.Quota, "在线支付不应扣站内额度")
}

func TestServerAlipayNotifyIsIdempotent(t *testing.T) {
	setupServerPaymentDB(t)
	key := setupServerAlipay(t)
	truncateServerTables(t)

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)
	form := serverAlipayNotifyForm(t, key, order.TradeNo, serverOrderMoney(order))

	assert.Equal(t, "success", callServerAlipayNotify(t, form))
	// 支付宝重放同一份通知：只能开一台机器。
	assert.Equal(t, "success", callServerAlipayNotify(t, form))
	assert.Equal(t, "success", callServerAlipayNotify(t, form))

	var count int64
	require.NoError(t, model.DB.Model(&model.ServerInstance{}).Count(&count).Error)
	assert.Equal(t, int64(1), count, "重复回调不得开两台机器")
}

func TestServerAlipayNotifyRejectsUnderpaidAmount(t *testing.T) {
	setupServerPaymentDB(t)
	key := setupServerAlipay(t)
	truncateServerTables(t)

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)
	// 订单实价几十元，通知只报 ¥0.01，且签名是合法的 —— 防的是
	// 「拿真实密钥伪造低价通知」而不是单纯篡改报文。
	form := serverAlipayNotifyForm(t, key, order.TradeNo, "0.01")

	assert.Equal(t, "fail", callServerAlipayNotify(t, form))

	var count int64
	require.NoError(t, model.DB.Model(&model.ServerInstance{}).Count(&count).Error)
	assert.Equal(t, int64(0), count, "少付款绝不能发货")

	var updated model.ServerOrder
	require.NoError(t, model.DB.Where("id = ?", order.Id).First(&updated).Error)
	assert.Equal(t, common.TopUpStatusPending, updated.Status, "订单必须保持待支付")
}

func TestServerAlipayNotifyRejectsBadSignature(t *testing.T) {
	setupServerPaymentDB(t)
	key := setupServerAlipay(t)
	truncateServerTables(t)

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)
	form := serverAlipayNotifyForm(t, key, order.TradeNo, serverOrderMoney(order))
	// 签名合法但报文被改过：金额改小后签名失效。
	form.Set("total_amount", "0.01")
	form.Set("receipt_amount", "0.01")

	assert.Equal(t, "fail", callServerAlipayNotify(t, form))

	var count int64
	require.NoError(t, model.DB.Model(&model.ServerInstance{}).Count(&count).Error)
	assert.Equal(t, int64(0), count, "验签失败绝不能发货")
}

func TestServerAlipayNotifyRejectsWrongAppID(t *testing.T) {
	setupServerPaymentDB(t)
	key := setupServerAlipay(t)
	truncateServerTables(t)

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)
	form := serverAlipayNotifyForm(t, key, order.TradeNo, serverOrderMoney(order))
	// 换一个 app_id 后重新签名（否则会先被验签挡住，测不到 app_id 这道闸门）。
	form.Set("app_id", "2026000000000999")
	form.Del("sign")
	signAlipayNotify(t, form, key)

	assert.Equal(t, "fail", callServerAlipayNotify(t, form))

	var count int64
	require.NoError(t, model.DB.Model(&model.ServerInstance{}).Count(&count).Error)
	assert.Equal(t, int64(0), count, "串应用的通知绝不能发货")
}

func TestServerAlipayNotifyRejectsUnknownOrder(t *testing.T) {
	setupServerPaymentDB(t)
	key := setupServerAlipay(t)
	truncateServerTables(t)

	form := serverAlipayNotifyForm(t, key, "SRV-NOT-EXIST", "68.00")
	assert.Equal(t, "fail", callServerAlipayNotify(t, form))
}

func TestServerAlipayNotifyIgnoresNonSuccessStatus(t *testing.T) {
	setupServerPaymentDB(t)
	key := setupServerAlipay(t)
	truncateServerTables(t)

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)
	form := serverAlipayNotifyForm(t, key, order.TradeNo, serverOrderMoney(order))
	form.Set("trade_status", "TRADE_CLOSED")
	form.Del("sign")
	signAlipayNotify(t, form, key)

	// 非成功终态回 success（让支付宝停止重试），但不开机器。
	assert.Equal(t, "success", callServerAlipayNotify(t, form))

	var count int64
	require.NoError(t, model.DB.Model(&model.ServerInstance{}).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

// 「通道没开就绝不开货」：合规/凭据关掉后，即使签名合法也不发货。
func TestServerAlipayNotifyRejectsDisabledChannel(t *testing.T) {
	setupServerPaymentDB(t)
	key := setupServerAlipay(t)
	truncateServerTables(t)

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)
	form := serverAlipayNotifyForm(t, key, order.TradeNo, serverOrderMoney(order))

	originalEnabled := operation_setting.AlipayEnabled
	operation_setting.AlipayEnabled = false
	t.Cleanup(func() { operation_setting.AlipayEnabled = originalEnabled })

	assert.Equal(t, "fail", callServerAlipayNotify(t, form))

	var count int64
	require.NoError(t, model.DB.Model(&model.ServerInstance{}).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

// 支付方式列表：直连凭据齐全才下发「支付宝」，且**不读**易支付的 PayMethods。
func TestServerPayMethodsHidesUnavailableChannels(t *testing.T) {
	setupServerPaymentDB(t)
	truncateServerTables(t)

	// 未确认合规 + 无凭据：在线渠道一个都不该出现。
	assert.Empty(t, serverPaymentProviders())

	confirmPaymentComplianceForTest(t)
	// 只配易支付网关（PayMethods），没有直连凭据 —— 仍然不下发。
	operation_setting.PayMethods = []map[string]string{
		{"name": "支付宝", "icon": "SiAlipay", "type": "alipay"},
	}
	originalMethods := operation_setting.PayMethods
	t.Cleanup(func() { operation_setting.PayMethods = originalMethods })
	assert.Empty(t, serverPaymentProviders())

	// 打开直连：下发且只下发一个支付宝。
	setupServerAlipay(t)
	providers := serverPaymentProviders()
	require.Len(t, providers, 1)
	assert.Equal(t, model.PaymentMethodAlipay, providers[0].Type)
	assert.Equal(t, "支付宝", providers[0].Name)
}
