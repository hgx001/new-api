package controller

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/iGoogle-ink/gopay"
	"github.com/iGoogle-ink/gopay/alipay"
	"github.com/stretchr/testify/require"
)

func testRSA2PrivateKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
}

func testRSA2PKCS8PrivateKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	}))
}

func TestAlipayClientBuildsComputerWebsitePaymentURL(t *testing.T) {
	originalEnabled := operation_setting.AlipayEnabled
	originalAppID := operation_setting.AlipayAppID
	originalPrivateKey := operation_setting.AlipayPrivateKey
	originalPublicKey := operation_setting.AlipayPublicKey
	originalProduction := operation_setting.AlipayProduction
	originalNotifyURL := operation_setting.AlipayNotifyURL
	originalReturnURL := operation_setting.AlipayReturnURL
	t.Cleanup(func() {
		operation_setting.AlipayEnabled = originalEnabled
		operation_setting.AlipayAppID = originalAppID
		operation_setting.AlipayPrivateKey = originalPrivateKey
		operation_setting.AlipayPublicKey = originalPublicKey
		operation_setting.AlipayProduction = originalProduction
		operation_setting.AlipayNotifyURL = originalNotifyURL
		operation_setting.AlipayReturnURL = originalReturnURL
	})

	operation_setting.AlipayEnabled = true
	operation_setting.AlipayAppID = "2026101600000000"
	operation_setting.AlipayPublicKey = "test-public-key"
	operation_setting.AlipayProduction = false
	operation_setting.AlipayNotifyURL = "https://example.test/api/user/alipay/notify"
	operation_setting.AlipayReturnURL = "https://example.test/console/log"

	for _, testCase := range []struct {
		name string
		key  string
	}{
		{name: "PKCS1", key: testRSA2PrivateKey(t)},
		{name: "PKCS8", key: testRSA2PKCS8PrivateKey(t)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			operation_setting.AlipayPrivateKey = testCase.key
			client := getAlipayClient()
			require.NotNil(t, client)
			client.SetNotifyUrl(operation_setting.AlipayNotifyURL).SetReturnUrl(operation_setting.AlipayReturnURL)
			body := make(gopay.BodyMap)
			body.Set("subject", "New API test top-up")
			body.Set("out_trade_no", "USR1ALIPAYE2E")
			body.Set("total_amount", "0.01")
			body.Set("product_code", "FAST_INSTANT_TRADE_PAY")

			payURL, err := client.TradePagePay(body)
			require.NoError(t, err)
			require.Contains(t, payURL, "alipay.trade.page.pay")
			require.Contains(t, payURL, "USR1ALIPAYE2E")
			require.Contains(t, payURL, "FAST_INSTANT_TRADE_PAY")
			require.True(t, strings.Contains(payURL, "notify_url=") || strings.Contains(payURL, "notify_url%3D"))
		})
	}
}

// alipayNotifySignSource builds the official signing source of an asynchronous
// notification: every received parameter except sign / sign_type, sorted by key.
func alipayNotifySignSource(form url.Values) string {
	keys := make([]string, 0, len(form))
	for key := range form {
		if key == "sign" || key == "sign_type" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+form.Get(key))
	}
	return strings.Join(parts, "&")
}

func signAlipayNotify(t *testing.T, form url.Values, key *rsa.PrivateKey) {
	t.Helper()
	digest := sha256.Sum256([]byte(alipayNotifySignSource(form)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	require.NoError(t, err)
	form.Set("sign", base64.StdEncoding.EncodeToString(signature))
}

// buildAlipayNotifyForm mirrors the raw parameters Alipay posts on a successful
// computer-website-payment notification, including the empty voucher list.
func buildAlipayNotifyForm(t *testing.T, key *rsa.PrivateKey) (url.Values, string) {
	t.Helper()
	form := url.Values{}
	form.Set("notify_time", "2026-10-20 21:38:45")
	form.Set("notify_type", "trade_status_sync")
	form.Set("notify_id", "PROBE-NOTIFY-ID")
	form.Set("app_id", "2021007109672654")
	form.Set("charset", "utf-8")
	form.Set("version", "1.0")
	form.Set("sign_type", "RSA2")
	form.Set("trade_no", "2026102022000000000000000")
	form.Set("out_trade_no", "USR28ALIPAYTEST")
	form.Set("seller_id", "2088000000000000")
	form.Set("buyer_id", "2088000000000001")
	form.Set("fund_bill_list", `[{"amount":"0.01","fundChannel":"AlipayAccount"}]`)
	form.Set("voucher_detail_list", `[]`)
	form.Set("total_amount", "0.01")
	form.Set("receipt_amount", "0.01")
	form.Set("buyer_pay_amount", "0.01")
	form.Set("point_amount", "0.00")
	form.Set("refund_fee", "0.00")
	form.Set("subject", "New API 账户充值 1")
	form.Set("gmt_create", "2026-10-20 21:38:45")
	form.Set("gmt_payment", "2026-10-20 21:38:45")
	form.Set("trade_status", "TRADE_SUCCESS")

	publicKey, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	encodedPublicKey := base64.StdEncoding.EncodeToString(publicKey)

	signAlipayNotify(t, form, key)
	return form, encodedPublicKey
}

func TestVerifyAlipayNotifySignatureAcceptsRawForm(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	form, encodedPublicKey := buildAlipayNotifyForm(t, key)

	verified, err := verifyAlipayNotifySignature(form, encodedPublicKey)
	require.NoError(t, err)
	require.True(t, verified)
}

func TestVerifyAlipayNotifySignatureRejectsTamperedAmount(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	form, encodedPublicKey := buildAlipayNotifyForm(t, key)

	form.Set("total_amount", "1000.00")
	verified, err := verifyAlipayNotifySignature(form, encodedPublicKey)
	require.Error(t, err)
	require.False(t, verified)
}

// TestAlipayGopayStructVerificationDropsEmptyVoucherList documents why the
// notification must be verified from the raw form: gopay re-serializes the parsed
// struct and `omitempty` drops "voucher_detail_list=[]", so its signing source no
// longer matches what Alipay signed and every real notification is rejected.
func TestAlipayGopayStructVerificationDropsEmptyVoucherList(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	form, encodedPublicKey := buildAlipayNotifyForm(t, key)

	request, err := http.NewRequest("POST", "https://example.test/api/user/alipay/notify", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	notify, err := alipay.ParseNotifyResult(request)
	require.NoError(t, err)

	gopayVerified, err := alipay.VerifySign(encodedPublicKey, notify)
	require.Error(t, err)
	require.False(t, gopayVerified)

	verified, err := verifyAlipayNotifySignature(request.Form, encodedPublicKey)
	require.NoError(t, err)
	require.True(t, verified)
}
