package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// 测试基座
// ---------------------------------------------------------------------------

// setupPaymentHardeningDB 为支付加固用例准备一个独立的内存数据库。
func setupPaymentHardeningDB(t *testing.T) *gorm.DB {
	t.Helper()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))

	oldDB, oldLogDB := model.DB, model.LOG_DB
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
	})
	return db
}

// setupAlipayNotifyConfig 配置一套可用的支付宝直连参数，并返回签名用的私钥。
func setupAlipayNotifyConfig(t *testing.T, appID string) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)

	originalEnabled := operation_setting.AlipayEnabled
	originalAppID := operation_setting.AlipayAppID
	originalPrivateKey := operation_setting.AlipayPrivateKey
	originalPublicKey := operation_setting.AlipayPublicKey
	originalProduction := operation_setting.AlipayProduction
	t.Cleanup(func() {
		operation_setting.AlipayEnabled = originalEnabled
		operation_setting.AlipayAppID = originalAppID
		operation_setting.AlipayPrivateKey = originalPrivateKey
		operation_setting.AlipayPublicKey = originalPublicKey
		operation_setting.AlipayProduction = originalProduction
	})

	operation_setting.AlipayEnabled = true
	operation_setting.AlipayAppID = appID
	operation_setting.AlipayPrivateKey = string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
	operation_setting.AlipayPublicKey = base64.StdEncoding.EncodeToString(publicDER)
	operation_setting.AlipayProduction = false

	confirmPaymentComplianceForTest(t)
	return key
}

// callAlipayNotify 把一个表单 POST 到 AlipayNotify，返回响应体。
func callAlipayNotify(t *testing.T, form url.Values) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "https://example.test/api/user/alipay/notify", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "203.0.113.10:4321"
	c.Request = request

	AlipayNotify(c)
	return recorder.Body.String()
}

// ---------------------------------------------------------------------------
// F1：支付金额必须「算一次、下发与校验共用同一份」
// ---------------------------------------------------------------------------

// TestGetPayMoneyUsesSingleRoundingRule 锁定支付金额只有一套舍入口径。
//
// 历史 bug：建单时用 strconv.FormatFloat(money,'f',2,64)（按 float64 的二进制精确值舍入）
// 下发给网关，回调时用 decimal.NewFromFloat(money).Round(2)（按最短十进制表示舍入）
// 校验。两者在第三位小数正好是 5 时会差 0.01（7.3 × 0.95 = 6.935 → 下发 6.93 / 校验 6.94），
// 回调会被判「金额不一致」→ 钱扣了、额度不加、订单永远 pending。
func TestGetPayMoneyUsesSingleRoundingRule(t *testing.T) {
	originalPrice := operation_setting.Price
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	t.Cleanup(func() {
		operation_setting.Price = originalPrice
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	})
	operation_setting.Price = 7.3
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}

	// 先把旧实现的两种口径差异钉死：这正是丢单的根因
	raw := decimal.NewFromFloat(7.3).Mul(decimal.NewFromFloat(0.95))
	legacySent := strconv.FormatFloat(raw.InexactFloat64(), 'f', 2, 64)
	legacyExpected := decimal.NewFromFloat(raw.InexactFloat64()).Round(2).StringFixed(2)
	require.Equal(t, "6.93", legacySent)
	require.Equal(t, "6.94", legacyExpected)
	require.NotEqual(t, legacySent, legacyExpected, "旧实现的两种舍入口径会差 0.01")

	// 新实现：统一算一次并舍入到分
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":0.95}`))
	t.Cleanup(func() {
		_ = common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1,"svip":1}`)
	})

	money := getPayMoney(1, "default")
	require.Equal(t, "6.94", money.StringFixed(2), "统一舍入后应得到 6.94")

	// 不变量：下发给网关的金额 == 回调校验用的金额 == 存库后读回的金额
	for _, ratio := range []float64{0.95, 0.9, 0.85, 0.99, 1, 1.05} {
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(fmt.Sprintf(`{"default":%v}`, ratio)))
		for amount := int64(1); amount <= 200; amount++ {
			m := getPayMoney(amount, "default")
			sent := m.StringFixed(2)

			// 回调侧拿到网关回传的金额，解析后必须与建单值完全相等
			callbackAmount, err := decimal.NewFromString(sent)
			require.NoError(t, err)
			require.True(t, callbackAmount.Equal(m),
				"ratio=%v amount=%d：网关收到 %s，回调校验却算出 %s", ratio, amount, sent, m.String())

			// 存库（float64 列）后读回也必须还原成同一个分，否则回调会判金额不一致
			require.True(t, decimal.NewFromFloat(payMoneyToFloat(m)).Round(2).Equal(m),
				"ratio=%v amount=%d：金额经 float64 存读后发生了漂移", ratio, amount)
		}
	}
}

// ---------------------------------------------------------------------------
// F2：支付宝路径必须和其它网关一样受支付合规闸门约束
// ---------------------------------------------------------------------------

func TestAlipayTopUpEnabledRequiresPaymentCompliance(t *testing.T) {
	originalEnabled := operation_setting.AlipayEnabled
	originalAppID := operation_setting.AlipayAppID
	originalPrivateKey := operation_setting.AlipayPrivateKey
	originalPublicKey := operation_setting.AlipayPublicKey
	t.Cleanup(func() {
		operation_setting.AlipayEnabled = originalEnabled
		operation_setting.AlipayAppID = originalAppID
		operation_setting.AlipayPrivateKey = originalPrivateKey
		operation_setting.AlipayPublicKey = originalPublicKey
	})

	operation_setting.AlipayEnabled = true
	operation_setting.AlipayAppID = "2021007109672654"
	operation_setting.AlipayPrivateKey = "private"
	operation_setting.AlipayPublicKey = "public"

	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})

	// 合规未确认：不允许走支付宝收款
	paymentSetting.ComplianceConfirmed = false
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	require.False(t, isAlipayTopUpEnabled(), "合规未确认时支付宝通道必须关闭")

	// 合规条款版本对不上：同样关闭
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = "v0"
	require.False(t, isAlipayTopUpEnabled(), "合规条款版本不匹配时支付宝通道必须关闭")

	// 合规确认且配置齐全：才允许收款
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	require.True(t, isAlipayTopUpEnabled())
}

// ---------------------------------------------------------------------------
// F10：回调的 app_id 必须与本应用一致（缺失即拒）
// ---------------------------------------------------------------------------

func TestAlipayNotifyRejectsNotificationWithWrongAppID(t *testing.T) {
	setupPaymentHardeningDB(t)
	const appID = "2021007109672654"
	key := setupAlipayNotifyConfig(t, appID)

	user := model.User{Id: 1, Username: "alipay-appid-user", AffCode: "alipay-appid-user", Password: "password", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)

	newOrder := func(tradeNo string) {
		topUp := model.TopUp{
			UserId:          user.Id,
			Amount:          10,
			Money:           73,
			TradeNo:         tradeNo,
			PaymentMethod:   model.PaymentMethodAlipay,
			PaymentProvider: model.PaymentProviderAlipay,
			CreateTime:      common.GetTimestamp(),
			Status:          common.TopUpStatusPending,
		}
		require.NoError(t, model.DB.Create(&topUp).Error)
	}

	// 1) app_id 正确 → 正常入账
	newOrder("USR1ALIPAYAPPIDOK")
	form := buildAlipayNotifyFormForTrade(t, key, appID, "USR1ALIPAYAPPIDOK")
	require.Equal(t, "success", callAlipayNotify(t, form))
	var gotUser model.User
	require.NoError(t, model.DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota)

	// 2) app_id 缺失 → 拒绝，不入账
	newOrder("USR1ALIPAYAPPIDMISSING")
	form = buildAlipayNotifyFormForTrade(t, key, appID, "USR1ALIPAYAPPIDMISSING")
	form.Del("app_id")
	signAlipayNotify(t, form, key)
	require.Equal(t, "fail", callAlipayNotify(t, form), "app_id 缺失的通知必须被拒绝")
	require.NoError(t, model.DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota, "被拒绝的通知不得加额度")

	// 3) app_id 属于另一个应用 → 拒绝，不入账
	newOrder("USR1ALIPAYAPPIDOTHER")
	form = buildAlipayNotifyFormForTrade(t, key, appID, "USR1ALIPAYAPPIDOTHER")
	form.Set("app_id", "2021007109672655")
	signAlipayNotify(t, form, key)
	require.Equal(t, "fail", callAlipayNotify(t, form), "app_id 不匹配的通知必须被拒绝")
	require.NoError(t, model.DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota, "被拒绝的通知不得加额度")

	// 4) 金额被篡改 → 拒绝，不入账
	newOrder("USR1ALIPAYAPPIDTAMPER")
	form = buildAlipayNotifyFormForTrade(t, key, appID, "USR1ALIPAYAPPIDTAMPER")
	form.Set("total_amount", "1.00")
	signAlipayNotify(t, form, key)
	require.Equal(t, "fail", callAlipayNotify(t, form))
	require.NoError(t, model.DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota, "金额被篡改的通知不得加额度")
}

// buildAlipayNotifyFormForTrade 构造一份指向指定订单的合法签名通知。
func buildAlipayNotifyFormForTrade(t *testing.T, key *rsa.PrivateKey, appID string, outTradeNo string) url.Values {
	t.Helper()
	form := url.Values{}
	form.Set("notify_time", "2026-10-20 22:00:00")
	form.Set("notify_type", "trade_status_sync")
	form.Set("notify_id", "HARDEN-NOTIFY-ID")
	form.Set("app_id", appID)
	form.Set("charset", "utf-8")
	form.Set("version", "1.0")
	form.Set("sign_type", "RSA2")
	form.Set("trade_no", "2026102022000000000000000")
	form.Set("out_trade_no", outTradeNo)
	form.Set("seller_id", "2088000000000000")
	form.Set("buyer_id", "2088000000000001")
	form.Set("total_amount", "73.00")
	form.Set("receipt_amount", "73.00")
	form.Set("buyer_pay_amount", "73.00")
	form.Set("subject", "账户充值 10")
	form.Set("gmt_payment", "2026-10-20 22:00:00")
	form.Set("trade_status", "TRADE_SUCCESS")
	signAlipayNotify(t, form, key)
	return form
}

// ---------------------------------------------------------------------------
// F4：易支付回调只有在额度真正入账后才回 success
// ---------------------------------------------------------------------------

func setupEpayNotifyConfig(t *testing.T, epayKey string) {
	t.Helper()
	originalAddress := operation_setting.PayAddress
	originalID := operation_setting.EpayId
	originalKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() {
		operation_setting.PayAddress = originalAddress
		operation_setting.EpayId = originalID
		operation_setting.EpayKey = originalKey
		operation_setting.PayMethods = originalPayMethods
	})
	operation_setting.PayAddress = "https://pay.example.test"
	operation_setting.EpayId = "1001"
	operation_setting.EpayKey = epayKey
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	confirmPaymentComplianceForTest(t)
}

// callEpayNotify 把一个表单 POST 到 EpayNotify，返回响应体。
func callEpayNotify(t *testing.T, params map[string]string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	form := url.Values{}
	for key, value := range params {
		form.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "https://example.test/api/user/epay/notify", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "198.51.100.7:4321"
	c.Request = request

	EpayNotify(c)
	return recorder.Body.String()
}

// TestEpayNotifyRespondsSuccessOnlyAfterCredit 锁定「先入账、后回 success」。
//
// 旧实现验签通过就立刻回 success，之后入账失败网关也不会再重试 → 已付款不发货。
func TestEpayNotifyRespondsSuccessOnlyAfterCredit(t *testing.T) {
	setupPaymentHardeningDB(t)
	const epayKey = "epay-test-key"
	setupEpayNotifyConfig(t, epayKey)

	user := model.User{Id: 1, Username: "epay-notify-user", AffCode: "epay-notify-user", Password: "password", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)

	buildParams := func(tradeNo string) map[string]string {
		params := map[string]string{
			"pid":          "1001",
			"type":         "alipay",
			"out_trade_no": tradeNo,
			"trade_no":     "EPAYTRADE" + tradeNo,
			"trade_status": epay.StatusTradeSuccess,
			"name":         "账户充值",
			"money":        "73.00",
		}
		return epay.GenerateParams(params, epayKey)
	}

	// 1) 订单不存在 → 必须回 fail（让网关继续重试/报警），不能回 success
	require.Equal(t, "fail", callEpayNotify(t, buildParams("USR1EPAYNOSUCHORDER")))

	// 2) 订单存在且是易支付订单 → 入账成功并回 success
	topUp := model.TopUp{
		UserId:          user.Id,
		Amount:          10,
		Money:           73,
		TradeNo:         "USR1EPAYOK",
		PaymentMethod:   model.PaymentMethodAlipay,
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	require.Equal(t, "success", callEpayNotify(t, buildParams(topUp.TradeNo)))
	var gotUser model.User
	require.NoError(t, model.DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota)

	// 3) 订单属于别的支付网关 → 必须回 fail（跨网关回调不得入账）
	otherTopUp := model.TopUp{
		UserId:          user.Id,
		Amount:          10,
		Money:           73,
		TradeNo:         "USR1EPAYOTHERPROVIDER",
		PaymentMethod:   model.PaymentMethodAlipay,
		PaymentProvider: model.PaymentProviderAlipay,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&otherTopUp).Error)
	require.Equal(t, "fail", callEpayNotify(t, buildParams(otherTopUp.TradeNo)))
	require.NoError(t, model.DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota, "跨网关回调不得加额度")

	// 4) 同一笔订单重放 → 幂等回 success，且不重复加额度
	require.Equal(t, "success", callEpayNotify(t, buildParams(topUp.TradeNo)))
	require.NoError(t, model.DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota, "重放回调不得重复加额度")
}
