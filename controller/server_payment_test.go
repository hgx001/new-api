package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
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

// 服务器订单的第三方支付回调测试。
//
// 这一层验的是「网关报文进来 → 验签 → 金额校验 → 开机器」整条链路。
// 任何一环松了，钱和货就会对不上。

const serverEpayTestKey = "server-epay-test-key"

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

// setupServerEpay 装好易支付网关配置与支付合规状态。
func setupServerEpay(t *testing.T) {
	t.Helper()
	confirmPaymentComplianceForTest(t)

	originalAddress := operation_setting.PayAddress
	originalID := operation_setting.EpayId
	originalKey := operation_setting.EpayKey
	originalMethods := operation_setting.PayMethods

	operation_setting.PayAddress = "https://pay.example.test"
	operation_setting.EpayId = "1001"
	operation_setting.EpayKey = serverEpayTestKey
	operation_setting.PayMethods = []map[string]string{
		{"name": "支付宝", "icon": "SiAlipay", "type": "alipay"},
	}

	t.Cleanup(func() {
		operation_setting.PayAddress = originalAddress
		operation_setting.EpayId = originalID
		operation_setting.EpayKey = originalKey
		operation_setting.PayMethods = originalMethods
	})
}

// callServerEpayNotify 模拟网关回调，返回响应体（success / fail）。
func callServerEpayNotify(t *testing.T, params map[string]string) string {
	t.Helper()
	form := url.Values{}
	for key, value := range params {
		form.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(
		http.MethodPost,
		"https://example.test/api/server/pay/epay/notify",
		strings.NewReader(form.Encode()),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "198.51.100.7:4321"
	c.Request = request

	ServerEpayNotify(c)
	return recorder.Body.String()
}

// serverEpayNotifyParams 构造一份合法签名的回调报文。
func serverEpayNotifyParams(tradeNo string, money string) map[string]string {
	return map[string]string{
		"pid":          "1001",
		"type":         "alipay",
		"out_trade_no": tradeNo,
		"trade_no":     "EPAY" + tradeNo,
		"trade_status": epay.StatusTradeSuccess,
		"name":         "云服务器",
		"money":        money,
	}
}

// serverOrderMoney 把订单金额格式化成网关回调里的金额字符串。
func serverOrderMoney(order *model.ServerOrder) string {
	return fmt.Sprintf("%.2f", order.Amount)
}

// createServerOrderForNotify 建一笔 pending 的第三方支付订单。
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
		userId, config, quote, model.PaymentMethodAlipay, model.PaymentProviderEpay, "",
	)
	require.NoError(t, err)
	return order
}

func TestServerEpayNotifyProvisionsInstanceOnSuccess(t *testing.T) {
	setupServerPaymentDB(t)
	setupServerEpay(t)
	truncateServerTables(t)

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)

	params := serverEpayNotifyParams(order.TradeNo, serverOrderMoney(order))
	epay.GenerateParams(params, serverEpayTestKey)

	assert.Equal(t, "success", callServerEpayNotify(t, params))

	var updated model.ServerOrder
	require.NoError(t, model.DB.Where("id = ?", order.Id).First(&updated).Error)
	assert.Equal(t, common.TopUpStatusSuccess, updated.Status)
	assert.NotZero(t, updated.InstanceId, "回调成功后必须开通实例")

	var instance model.ServerInstance
	require.NoError(t, model.DB.Where("order_id = ?", order.Id).First(&instance).Error)
	assert.Equal(t, model.ServerInstanceStatusRunning, instance.Status)
	assert.NotEmpty(t, instance.Password)

	// 下单阶段不扣额度；第三方支付的钱不进站内余额。
	var after model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&after).Error)
	assert.Equal(t, 100_000_000, after.Quota, "第三方支付不应扣站内额度")
}

func TestServerEpayNotifyIsIdempotent(t *testing.T) {
	setupServerPaymentDB(t)
	setupServerEpay(t)
	truncateServerTables(t)

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)

	params := serverEpayNotifyParams(order.TradeNo, serverOrderMoney(order))
	epay.GenerateParams(params, serverEpayTestKey)

	assert.Equal(t, "success", callServerEpayNotify(t, params))
	// 网关重放同一份报文：只能开一台机器。
	assert.Equal(t, "success", callServerEpayNotify(t, params))

	var count int64
	require.NoError(t, model.DB.Model(&model.ServerInstance{}).Count(&count).Error)
	assert.Equal(t, int64(1), count, "重复回调不得开两台机器")
}

func TestServerEpayNotifyRejectsUnderpaidAmount(t *testing.T) {
	setupServerPaymentDB(t)
	setupServerEpay(t)
	truncateServerTables(t)

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)

	// 订单实价几十元，回调只报 ¥0.01。
	params := serverEpayNotifyParams(order.TradeNo, "0.01")
	epay.GenerateParams(params, serverEpayTestKey)

	assert.Equal(t, "fail", callServerEpayNotify(t, params))

	var count int64
	require.NoError(t, model.DB.Model(&model.ServerInstance{}).Count(&count).Error)
	assert.Equal(t, int64(0), count, "少付款绝不能发货")

	var updated model.ServerOrder
	require.NoError(t, model.DB.Where("id = ?", order.Id).First(&updated).Error)
	assert.Equal(t, common.TopUpStatusPending, updated.Status, "订单必须保持待支付")
}

func TestServerEpayNotifyRejectsBadSignature(t *testing.T) {
	setupServerPaymentDB(t)
	setupServerEpay(t)
	truncateServerTables(t)

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)

	params := serverEpayNotifyParams(order.TradeNo, serverOrderMoney(order))
	epay.GenerateParams(params, "wrong-key")

	assert.Equal(t, "fail", callServerEpayNotify(t, params))

	var count int64
	require.NoError(t, model.DB.Model(&model.ServerInstance{}).Count(&count).Error)
	assert.Equal(t, int64(0), count, "验签失败绝不能发货")
}

func TestServerEpayNotifyRejectsUnknownOrder(t *testing.T) {
	setupServerPaymentDB(t)
	setupServerEpay(t)
	truncateServerTables(t)

	params := serverEpayNotifyParams("SRV-NOT-EXIST", "68.00")
	epay.GenerateParams(params, serverEpayTestKey)

	assert.Equal(t, "fail", callServerEpayNotify(t, params))
}

func TestServerEpayNotifyIgnoresNonSuccessStatus(t *testing.T) {
	setupServerPaymentDB(t)
	setupServerEpay(t)
	truncateServerTables(t)

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)

	params := serverEpayNotifyParams(order.TradeNo, serverOrderMoney(order))
	params["trade_status"] = "TRADE_CLOSED"
	epay.GenerateParams(params, serverEpayTestKey)

	// 非成功终态回 success（让网关停止重试），但不开机器。
	assert.Equal(t, "success", callServerEpayNotify(t, params))

	var count int64
	require.NoError(t, model.DB.Model(&model.ServerInstance{}).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

// TestServerEpayNotifyRejectsDisabledWebhook 确认「webhook 没开就绝不开货」。
func TestServerEpayNotifyRejectsDisabledWebhook(t *testing.T) {
	setupServerPaymentDB(t)
	setupServerEpay(t)
	truncateServerTables(t)

	// 把 PayMethods 清空 —— 等价于管理员关掉了这个收款通道。
	operation_setting.PayMethods = []map[string]string{}

	user := createServerPayUser(t, 100_000_000)
	order := createServerOrderForNotify(t, user.Id)

	params := serverEpayNotifyParams(order.TradeNo, serverOrderMoney(order))
	epay.GenerateParams(params, serverEpayTestKey)

	assert.Equal(t, "fail", callServerEpayNotify(t, params))

	var count int64
	require.NoError(t, model.DB.Model(&model.ServerInstance{}).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

// TestServerPayMethodsHidesUnavailableChannels 确认不会把「点了会失败」的渠道下发给前端。
func TestServerPayMethodsHidesUnavailableChannels(t *testing.T) {
	setupServerPaymentDB(t)
	truncateServerTables(t)

	// 未确认合规 + 无凭据：第三方渠道一个都不该出现。
	assert.Empty(t, serverPaymentProviders())

	confirmPaymentComplianceForTest(t)
	operation_setting.PayMethods = []map[string]string{
		{"name": "支付宝", "icon": "SiAlipay", "type": "alipay"},
	}
	// 凭据不全，仍然不下发。
	assert.Empty(t, serverPaymentProviders())

	operation_setting.PayAddress = "https://pay.example.test"
	operation_setting.EpayId = "1001"
	operation_setting.EpayKey = serverEpayTestKey
	t.Cleanup(func() {
		operation_setting.PayAddress = ""
		operation_setting.EpayId = ""
		operation_setting.EpayKey = ""
	})

	providers := serverPaymentProviders()
	require.Len(t, providers, 1)
	assert.Equal(t, "alipay", providers[0].Type)
	assert.Equal(t, "支付宝", providers[0].Name)
}
