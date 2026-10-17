package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/iGoogle-ink/gopay"
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
