package controller

import (
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// 测试基座
// ---------------------------------------------------------------------------

// setupVpnOrderPaymentDB 为 VPN 支付链路用例准备独立内存库（只迁移 vpn_orders）。
func setupVpnOrderPaymentDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.VpnOrder{}))

	oldDB, oldLogDB := model.DB, model.LOG_DB
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
	})
}

// insertPendingVpnOrder 造一笔待支付的年费订单（199 元，与 VpnPlanYearPrice 一致）。
func insertPendingVpnOrder(t *testing.T, tradeNo string, provider string) *model.VpnOrder {
	t.Helper()
	order := &model.VpnOrder{
		UserId:          7,
		TradeNo:         tradeNo,
		Plan:            "year",
		Money:           VpnPlanYearPrice,
		PaymentMethod:   model.PaymentMethodAlipay,
		PaymentProvider: provider,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
		NodeKey:         "vless://test-node-key",
	}
	require.NoError(t, model.DB.Create(order).Error)
	return order
}

// vpnAlipayNotifyForm 构造一份指向 VPN 订单的支付宝通知；amount 为通知里上报的金额。
func vpnAlipayNotifyForm(t *testing.T, key *rsa.PrivateKey, appID string, tradeNo string, amount string) url.Values {
	t.Helper()
	form := url.Values{}
	form.Set("notify_time", "2026-10-25 10:00:00")
	form.Set("notify_type", "trade_status_sync")
	form.Set("notify_id", "VPN-NOTIFY-ID")
	form.Set("app_id", appID)
	form.Set("charset", "utf-8")
	form.Set("version", "1.0")
	form.Set("sign_type", "RSA2")
	form.Set("trade_no", "2026102522000000000000000")
	form.Set("out_trade_no", tradeNo)
	form.Set("seller_id", "2088000000000000")
	form.Set("buyer_id", "2088000000000001")
	form.Set("total_amount", amount)
	form.Set("receipt_amount", amount)
	form.Set("buyer_pay_amount", amount)
	form.Set("subject", "VPN年费套餐")
	form.Set("gmt_payment", "2026-10-25 10:00:00")
	form.Set("trade_status", "TRADE_SUCCESS")
	signAlipayNotify(t, form, key)
	return form
}

func callVpnAlipayNotify(t *testing.T, form url.Values) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "https://example.test/api/user/vpn/alipay/notify", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "203.0.113.10:4321"
	c.Request = request

	VpnAlipayNotify(c)
	return recorder.Body.String()
}

func callVpnEpayNotify(t *testing.T, params map[string]string) string {
	t.Helper()
	form := url.Values{}
	for key, value := range params {
		form.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "https://example.test/api/user/vpn/epay/notify", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "198.51.100.7:4321"
	c.Request = request

	VpnEpayNotify(c)
	return recorder.Body.String()
}

// vpnEpayNotifyParams 构造一份合法签名的易支付回调（金额默认与订单一致）。
func vpnEpayNotifyParams(tradeNo string, money string) map[string]string {
	params := map[string]string{
		"pid":          "1001",
		"type":         "alipay",
		"out_trade_no": tradeNo,
		"trade_no":     "EPAYTRADE" + tradeNo,
		"trade_status": epay.StatusTradeSuccess,
		"name":         "VPN年费套餐",
		"money":        money,
	}
	return params
}

func callGetVpnMyKey(t *testing.T, userId int) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/vpn/key", nil)
	c.Set("id", userId)

	GetVpnMyKey(c)
	var body map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	return recorder.Code, body
}

func callGetVpnMyOrders(t *testing.T, userId int) (int, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/vpn/orders", nil)
	c.Set("id", userId)

	GetVpnMyOrders(c)
	return recorder.Code, recorder.Body.String()
}

func callRequestVpnOrder(t *testing.T, userId int, paymentMethod string) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/vpn/order",
		strings.NewReader(fmt.Sprintf(`{"payment_method":%q}`, paymentMethod)),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", userId)

	RequestVpnOrder(c)
	var body map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	return recorder.Code, body
}

// ---------------------------------------------------------------------------
// 支付宝官方直连回调：验签 / app_id / 金额三道闸
// ---------------------------------------------------------------------------

// TestVpnAlipayNotifyRejectsWhenChannelDisabled 直连未开通时回调一律拒绝，
// 订单保持 pending（管理员可补单），绝不静默入账。
func TestVpnAlipayNotifyRejectsWhenChannelDisabled(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	const appID = "2021007109672654"
	key := setupAlipayNotifyConfig(t, appID)
	operation_setting.AlipayEnabled = false
	order := insertPendingVpnOrder(t, "VPN7ALIPAYDISABLED", model.PaymentProviderAlipay)

	form := vpnAlipayNotifyForm(t, key, appID, order.TradeNo, "199.00")
	assert.Equal(t, "fail", callVpnAlipayNotify(t, form))

	got := model.GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusPending, got.Status)
}

// TestVpnAlipayNotifyRejectsTamperedAmount 签名之后改金额 → 验签失败 → 拒绝。
// 签名只能证明报文没被改过；被改过的报文必须整个拒掉。
func TestVpnAlipayNotifyRejectsTamperedAmount(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	const appID = "2021007109672654"
	key := setupAlipayNotifyConfig(t, appID)
	order := insertPendingVpnOrder(t, "VPN7ALIPAYTAMPER", model.PaymentProviderAlipay)

	form := vpnAlipayNotifyForm(t, key, appID, order.TradeNo, "199.00")
	form.Set("total_amount", "1.00") // 签名后篡改，不重新签名
	assert.Equal(t, "fail", callVpnAlipayNotify(t, form))

	got := model.GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusPending, got.Status)
}

// TestVpnAlipayNotifyRejectsWrongAppID 别的应用的付款通知不得入账到我们头上。
func TestVpnAlipayNotifyRejectsWrongAppID(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	const appID = "2021007109672654"
	key := setupAlipayNotifyConfig(t, appID)
	order := insertPendingVpnOrder(t, "VPN7ALIPAYAPPID", model.PaymentProviderAlipay)

	form := vpnAlipayNotifyForm(t, key, "2099999999999999", order.TradeNo, "199.00")
	assert.Equal(t, "fail", callVpnAlipayNotify(t, form))

	got := model.GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusPending, got.Status)
}

// TestVpnAlipayNotifyRejectsUnderpaidAmount 「0.01 元买 199 元年费」必须被拒，
// 否则签名合法就能少付钱拿套餐。
func TestVpnAlipayNotifyRejectsUnderpaidAmount(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	const appID = "2021007109672654"
	key := setupAlipayNotifyConfig(t, appID)
	order := insertPendingVpnOrder(t, "VPN7ALIPAYCHEAP", model.PaymentProviderAlipay)

	form := vpnAlipayNotifyForm(t, key, appID, order.TradeNo, "0.01")
	assert.Equal(t, "fail", callVpnAlipayNotify(t, form))

	got := model.GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusPending, got.Status)
	assert.Equal(t, int64(0), got.CompleteTime, "金额不符不得写完成时间")
}

// TestVpnAlipayNotifyCompletesPaidOrder 合法付款通知 → 入账并回 success。
func TestVpnAlipayNotifyCompletesPaidOrder(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	const appID = "2021007109672654"
	key := setupAlipayNotifyConfig(t, appID)
	order := insertPendingVpnOrder(t, "VPN7ALIPAYOK", model.PaymentProviderAlipay)

	form := vpnAlipayNotifyForm(t, key, appID, order.TradeNo, "199.00")
	assert.Equal(t, "success", callVpnAlipayNotify(t, form))

	got := model.GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusSuccess, got.Status)
	assert.Greater(t, got.CompleteTime, int64(0))
}

// TestVpnAlipayNotifyReplayIsIdempotent 同一笔通知重放：仍然回 success（告知
// 支付宝别再推），但不得改写完成时间、不得重复入账。
func TestVpnAlipayNotifyReplayIsIdempotent(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	const appID = "2021007109672654"
	key := setupAlipayNotifyConfig(t, appID)
	order := insertPendingVpnOrder(t, "VPN7ALIPAYREPLAY", model.PaymentProviderAlipay)

	form := vpnAlipayNotifyForm(t, key, appID, order.TradeNo, "199.00")
	require.Equal(t, "success", callVpnAlipayNotify(t, form))
	first := model.GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, first)

	assert.Equal(t, "success", callVpnAlipayNotify(t, form))
	second := model.GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, second)
	assert.Equal(t, first.CompleteTime, second.CompleteTime, "重放不得改写完成时间")
}

// TestVpnAlipayNotifyAcksNonSuccessTradeStatus 非成功终态（如 TRADE_CLOSED）
// 回 success 让支付宝停止重推，但订单必须仍停在 pending。
func TestVpnAlipayNotifyAcksNonSuccessTradeStatus(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	const appID = "2021007109672654"
	key := setupAlipayNotifyConfig(t, appID)
	order := insertPendingVpnOrder(t, "VPN7ALIPAYCLOSED", model.PaymentProviderAlipay)

	form := vpnAlipayNotifyForm(t, key, appID, order.TradeNo, "199.00")
	form.Set("trade_status", "TRADE_CLOSED")
	signAlipayNotify(t, form, key)
	assert.Equal(t, "success", callVpnAlipayNotify(t, form))

	got := model.GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusPending, got.Status)
}

// TestVpnAlipayNotifyRejectsUnknownOrder 订单不存在 → 回 fail（让支付宝重试/报警），
// 绝不能回 success 把一笔无人认领的付款吞掉。
func TestVpnAlipayNotifyRejectsUnknownOrder(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	const appID = "2021007109672654"
	key := setupAlipayNotifyConfig(t, appID)

	form := vpnAlipayNotifyForm(t, key, appID, "VPN7ALIPAYNOSUCHORDER", "199.00")
	assert.Equal(t, "fail", callVpnAlipayNotify(t, form))
}

// ---------------------------------------------------------------------------
// 易支付回调：金额校验 / 跨通道拒绝 / 实际支付方式回写
// ---------------------------------------------------------------------------

// TestVpnEpayNotifyRejectsUnknownOrder 订单不存在必须回 fail。
func TestVpnEpayNotifyRejectsUnknownOrder(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	setupEpayNotifyConfig(t, "vpn-epay-key")

	assert.Equal(t, "fail", callVpnEpayNotify(t, epay.GenerateParams(
		vpnEpayNotifyParams("VPN7EPAYNOSUCHORDER", "199.00"), "vpn-epay-key")))
}

// TestVpnEpayNotifyRejectsUnderpaidAmount 少付款不得置成功。
func TestVpnEpayNotifyRejectsUnderpaidAmount(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	setupEpayNotifyConfig(t, "vpn-epay-key")
	order := insertPendingVpnOrder(t, "VPN7EPAYCHEAP", model.PaymentProviderEpay)

	assert.Equal(t, "fail", callVpnEpayNotify(t, epay.GenerateParams(
		vpnEpayNotifyParams(order.TradeNo, "0.01"), "vpn-epay-key")))

	got := model.GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusPending, got.Status)
}

// TestVpnEpayNotifyRejectsOtherProviderOrder 官方直连的订单不得被易支付回调入账。
func TestVpnEpayNotifyRejectsOtherProviderOrder(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	setupEpayNotifyConfig(t, "vpn-epay-key")
	order := insertPendingVpnOrder(t, "VPN7EPAYCROSS", model.PaymentProviderAlipay)

	assert.Equal(t, "fail", callVpnEpayNotify(t, epay.GenerateParams(
		vpnEpayNotifyParams(order.TradeNo, "199.00"), "vpn-epay-key")))

	got := model.GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusPending, got.Status)
}

// TestVpnEpayNotifyWritesBackActualPaymentMethod 下单选支付宝、实际用微信付：
// 入账成功，且把网关回传的实际支付方式写回订单（对账用）。
func TestVpnEpayNotifyWritesBackActualPaymentMethod(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	setupEpayNotifyConfig(t, "vpn-epay-key")
	order := insertPendingVpnOrder(t, "VPN7EPAYWX", model.PaymentProviderEpay)

	params := vpnEpayNotifyParams(order.TradeNo, "199.00")
	params["type"] = "wxpay"
	assert.Equal(t, "success", callVpnEpayNotify(t, epay.GenerateParams(params, "vpn-epay-key")))

	got := model.GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusSuccess, got.Status)
	assert.Equal(t, "wxpay", got.PaymentMethod, "实际支付方式应回写订单")
}

// TestVpnEpayNotifyIgnoresNonSuccessTradeStatus 非成功终态回 success 但不入账。
func TestVpnEpayNotifyIgnoresNonSuccessTradeStatus(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	setupEpayNotifyConfig(t, "vpn-epay-key")
	order := insertPendingVpnOrder(t, "VPN7EPAYCLOSED", model.PaymentProviderEpay)

	params := vpnEpayNotifyParams(order.TradeNo, "199.00")
	params["trade_status"] = "TRADE_CLOSED"
	assert.Equal(t, "success", callVpnEpayNotify(t, epay.GenerateParams(params, "vpn-epay-key")))

	got := model.GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusPending, got.Status)
}

// ---------------------------------------------------------------------------
// 密钥下发：未支付 / 已过期一律不给 key
// ---------------------------------------------------------------------------

// TestGetVpnMyKeyRejectsUnpaidOrder 订单没付钱就想要密钥 → 拒绝。
func TestGetVpnMyKeyRejectsUnpaidOrder(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	order := insertPendingVpnOrder(t, "VPN7KEYUNPAID", model.PaymentProviderAlipay)

	code, body := callGetVpnMyKey(t, order.UserId)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, false, body["success"])
	assert.NotContains(t, body, "data", "未支付不得下发任何密钥数据")
}

// TestGetVpnMyKeyRejectsExpiredOrder 套餐到期 → 提示续费，不下发 key。
func TestGetVpnMyKeyRejectsExpiredOrder(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	order := insertPendingVpnOrder(t, "VPN7KEYEXPIRED", model.PaymentProviderAlipay)
	order.Status = common.TopUpStatusSuccess
	order.CompleteTime = time.Now().Unix() - int64(VpnPlanDays+1)*24*3600
	require.NoError(t, model.DB.Save(order).Error)

	code, body := callGetVpnMyKey(t, order.UserId)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, false, body["success"])
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "到期响应应带 data")
	assert.Equal(t, true, data["expired"])
	assert.NotContains(t, data, "key", "已到期不得下发密钥")
}

// TestGetVpnMyKeyReturnsKeyForPaidOrder 已支付且未到期 → 下发密钥与到期时间。
func TestGetVpnMyKeyReturnsKeyForPaidOrder(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	order := insertPendingVpnOrder(t, "VPN7KEYPAID", model.PaymentProviderAlipay)
	order.Status = common.TopUpStatusSuccess
	order.CompleteTime = common.GetTimestamp()
	require.NoError(t, model.DB.Save(order).Error)

	code, body := callGetVpnMyKey(t, order.UserId)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, body["success"])
	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, order.NodeKey, data["key"])
	assert.Equal(t, float64(order.CompleteTime+VpnPlanDays*24*3600), data["expire_time"])
}

// TestGetVpnMyOrdersHidesNodeKey 订单列表是给前端展示的，绝不能带出节点密钥。
func TestGetVpnMyOrdersHidesNodeKey(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	paid := insertPendingVpnOrder(t, "VPN7LISTPAID", model.PaymentProviderAlipay)
	paid.Status = common.TopUpStatusSuccess
	paid.CompleteTime = common.GetTimestamp()
	require.NoError(t, model.DB.Save(paid).Error)
	insertPendingVpnOrder(t, "VPN7LISTPENDING", model.PaymentProviderEpay)

	code, body := callGetVpnMyOrders(t, paid.UserId)
	assert.Equal(t, http.StatusOK, code)
	assert.NotContains(t, body, paid.NodeKey, "订单列表不得泄漏节点密钥")
	assert.NotContains(t, body, "node_key")
	assert.Contains(t, body, paid.TradeNo)
}

// TestVpnExpireTimeFallsBackToCreateTime 完成时间缺失（异常数据）时按下单时间
// 起算有效期，不能算出 1970 年那种「已过期」。
func TestVpnExpireTimeFallsBackToCreateTime(t *testing.T) {
	createTime := common.GetTimestamp()
	got := vpnExpireTime(&model.VpnOrder{CreateTime: createTime, CompleteTime: 0})
	assert.Equal(t, createTime+int64(VpnPlanDays*24*3600), got)
}

// ---------------------------------------------------------------------------
// 建单：支付方式白名单 + 先落库再拉起
// ---------------------------------------------------------------------------

// TestRequestVpnOrderRejectsEmptyPaymentMethod 空支付方式直接拒。
func TestRequestVpnOrderRejectsEmptyPaymentMethod(t *testing.T) {
	setupVpnOrderPaymentDB(t)

	_, body := callRequestVpnOrder(t, 7, "   ")
	assert.Equal(t, "error", body["message"])
	assert.Equal(t, "支付方式不存在", body["data"])
}

// TestRequestVpnOrderRejectsUnknownPaymentMethod 白名单之外的支付方式不得拉起网关。
func TestRequestVpnOrderRejectsUnknownPaymentMethod(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() { operation_setting.PayMethods = originalPayMethods })
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}

	_, body := callRequestVpnOrder(t, 7, "bitcoin")
	assert.Equal(t, "error", body["message"])
	assert.Equal(t, "支付方式不存在", body["data"])
}

// TestRequestVpnOrderAlipayCreatesOrderBeforePayment 官方直连：先落库（pending）
// 再返回收银台跳转链接。顺序反了会出现「钱付了查不到订单」。
func TestRequestVpnOrderAlipayCreatesOrderBeforePayment(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	const appID = "2021007109672654"
	setupAlipayNotifyConfig(t, appID)

	code, body := callRequestVpnOrder(t, 42, model.PaymentMethodAlipay)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "success", body["message"])
	assert.Equal(t, VpnPayModeRedirect, body["pay_mode"])

	payURL, _ := body["url"].(string)
	require.NotEmpty(t, payURL, "应返回支付宝收银台地址")
	assert.Contains(t, payURL, "alipay")

	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	tradeNo, _ := data["trade_no"].(string)
	require.NotEmpty(t, tradeNo)

	order := model.GetVpnOrderByTradeNo(tradeNo)
	require.NotNil(t, order, "返回支付链接前订单必须已落库")
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Equal(t, VpnPlanYearPrice, order.Money)
	assert.Equal(t, model.PaymentProviderAlipay, order.PaymentProvider)
	assert.Equal(t, 42, order.UserId)
	assert.NotEmpty(t, order.NodeKey, "订单应带节点密钥，支付成功后可直接下发")
}

// TestRequestVpnOrderEpayCreatesPendingOrder 易支付网关：落库 + 返回表单参数。
func TestRequestVpnOrderEpayCreatesPendingOrder(t *testing.T) {
	setupVpnOrderPaymentDB(t)
	setupEpayNotifyConfig(t, "vpn-epay-key")

	code, body := callRequestVpnOrder(t, 42, "alipay")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "success", body["message"])
	assert.Equal(t, VpnPayModeForm, body["pay_mode"])

	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	tradeNo, _ := data["out_trade_no"].(string)
	require.NotEmpty(t, tradeNo)

	order := model.GetVpnOrderByTradeNo(tradeNo)
	require.NotNil(t, order, "返回表单前订单必须已落库")
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Equal(t, model.PaymentProviderEpay, order.PaymentProvider)
	assert.Equal(t, 42, order.UserId)
}
