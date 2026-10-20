package controller

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/iGoogle-ink/gopay"
	"github.com/iGoogle-ink/gopay/alipay"
	"github.com/shopspring/decimal"
)

func isAlipayTopUpEnabled() bool {
	return operation_setting.AlipayEnabled &&
		strings.TrimSpace(operation_setting.AlipayAppID) != "" &&
		strings.TrimSpace(operation_setting.AlipayPrivateKey) != "" &&
		strings.TrimSpace(operation_setting.AlipayPublicKey) != ""
}

func normalizeAlipayPrivateKey(value string) string {
	value = strings.TrimSpace(value)
	var privateKeyDER []byte
	if block, _ := pem.Decode([]byte(value)); block != nil {
		privateKeyDER = block.Bytes
	} else {
		compact := strings.Join(strings.Fields(value), "")
		decoded, err := base64.StdEncoding.DecodeString(compact)
		if err != nil {
			return compact
		}
		privateKeyDER = decoded
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(privateKeyDER)
	if err != nil {
		parsedKey, parseErr := x509.ParsePKCS8PrivateKey(privateKeyDER)
		rsaKey, isRSA := parsedKey.(*rsa.PrivateKey)
		if parseErr != nil || !isRSA {
			return base64.StdEncoding.EncodeToString(privateKeyDER)
		}
		privateKey = rsaKey
	}
	return base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(privateKey))
}

func normalizeAlipayPublicKey(value string) string {
	value = strings.TrimSpace(value)
	if block, _ := pem.Decode([]byte(value)); block != nil {
		return base64.StdEncoding.EncodeToString(block.Bytes)
	}
	return strings.Join(strings.Fields(value), "")
}

// parseAlipayPublicKey decodes the stored Alipay public key (base64 DER or PEM) into an RSA key.
func parseAlipayPublicKey(value string) (*rsa.PublicKey, error) {
	value = strings.TrimSpace(value)
	var der []byte
	if block, _ := pem.Decode([]byte(value)); block != nil {
		der = block.Bytes
	} else {
		compact := strings.Join(strings.Fields(value), "")
		decoded, err := base64.StdEncoding.DecodeString(compact)
		if err != nil {
			return nil, fmt.Errorf("支付宝公钥不是合法 base64: %w", err)
		}
		der = decoded
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("支付宝公钥解析失败: %w", err)
	}
	publicKey, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("支付宝公钥不是 RSA 公钥")
	}
	return publicKey, nil
}

// verifyAlipayNotifySignature verifies the asynchronous notification with the raw form
// values, following the official Alipay SDK (rsaCheckV1): every received parameter except
// sign / sign_type, sorted by key, joined as "k=v&k=v", then RSA2 (SHA256) verified with
// the Alipay public key.
//
// It deliberately does NOT use gopay's struct-based VerifySign: gopay re-serializes the
// parsed NotifyRequest struct, and `omitempty` silently drops empty arrays such as
// "voucher_detail_list=[]". Alipay signs the raw transmitted value, so the two signing
// sources differ by that field and verification fails on every real notification
// (payment succeeds, quota is never credited).
func verifyAlipayNotifySignature(form url.Values, alipayPublicKey string) (bool, error) {
	signValue := strings.TrimSpace(form.Get("sign"))
	if signValue == "" {
		return false, errors.New("通知缺少 sign")
	}
	publicKey, err := parseAlipayPublicKey(alipayPublicKey)
	if err != nil {
		return false, err
	}
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
	source := strings.Join(parts, "&")
	signature, err := base64.StdEncoding.DecodeString(signValue)
	if err != nil {
		return false, fmt.Errorf("通知 sign 不是合法 base64: %w", err)
	}
	digest := sha256.Sum256([]byte(source))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature); err != nil {
		return false, err
	}
	return true, nil
}

func getAlipayClient() *alipay.Client {
	if !isAlipayTopUpEnabled() {
		return nil
	}
	client := alipay.NewClient(
		strings.TrimSpace(operation_setting.AlipayAppID),
		normalizeAlipayPrivateKey(operation_setting.AlipayPrivateKey),
		operation_setting.AlipayProduction,
	)
	client.SetCharset("utf-8").SetSignType("RSA2")
	return client
}

func alipayNotifyURL() string {
	if value := strings.TrimSpace(operation_setting.AlipayNotifyURL); value != "" {
		return value
	}
	return strings.TrimRight(service.GetCallbackAddress(), "/") + "/api/user/alipay/notify"
}

func alipayReturnURL() string {
	if value := strings.TrimSpace(operation_setting.AlipayReturnURL); value != "" {
		return value
	}
	return paymentReturnPath("/console/log")
}

// RequestAlipay creates a pending order and returns the official Alipay page URL.
// It is called from the legacy /api/user/pay endpoint when payment_method=alipay
// and official direct Alipay mode is enabled.
func RequestAlipay(c *gin.Context, amount int64, group string) {
	payMoney := getPayMoney(amount, group)
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	client := getAlipayClient()
	if client == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付宝官方直连尚未完成配置"})
		return
	}

	userID := c.GetInt("id")
	tradeNo := fmt.Sprintf("USR%dALIPAY%s", userID, common.GetRandomString(24))
	topUpAmount := amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		topUpAmount = decimal.NewFromInt(amount).Div(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	}
	topUp := &model.TopUp{
		UserId:          userID,
		Amount:          topUpAmount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodAlipay,
		PaymentProvider: model.PaymentProviderAlipay,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝创建充值订单失败 user_id=%d trade_no=%s error=%q", userID, tradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	client.SetNotifyUrl(alipayNotifyURL()).SetReturnUrl(alipayReturnURL())
	body := make(gopay.BodyMap)
	body.Set("subject", fmt.Sprintf("New API 账户充值 %d", amount))
	body.Set("out_trade_no", tradeNo)
	body.Set("total_amount", strconv.FormatFloat(payMoney, 'f', 2, 64))
	body.Set("product_code", "FAST_INSTANT_TRADE_PAY")
	payURL, err := client.TradePagePay(body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝拉起支付失败 user_id=%d trade_no=%s error=%q", userID, tradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付宝支付失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"url": payURL, "trade_no": tradeNo}})
}

// AlipayNotify is the authoritative asynchronous payment callback.
// It verifies the signature and amount, then completes the pending order once.
func AlipayNotify(c *gin.Context) {
	if !isAlipayTopUpEnabled() {
		c.String(http.StatusOK, "fail")
		return
	}
	notify, err := alipay.ParseNotifyResult(c.Request)
	if err != nil || notify == nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝回调解析失败 error=%v", err))
		c.String(http.StatusOK, "fail")
		return
	}
	verified, err := verifyAlipayNotifySignature(c.Request.Form, operation_setting.AlipayPublicKey)
	if err != nil || !verified {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝回调验签失败 trade_no=%s error=%v", notify.OutTradeNo, err))
		c.String(http.StatusOK, "fail")
		return
	}
	if notify.AppId != "" && notify.AppId != strings.TrimSpace(operation_setting.AlipayAppID) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝回调 APPID 不匹配 trade_no=%s app_id=%s", notify.OutTradeNo, notify.AppId))
		c.String(http.StatusOK, "fail")
		return
	}
	if notify.TradeStatus != "TRADE_SUCCESS" && notify.TradeStatus != "TRADE_FINISHED" {
		c.String(http.StatusOK, "success")
		return
	}
	totalAmount, err := decimal.NewFromString(notify.TotalAmount)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝回调金额解析失败 trade_no=%s total_amount=%s", notify.OutTradeNo, notify.TotalAmount))
		c.String(http.StatusOK, "fail")
		return
	}
	if err := model.RechargeAlipay(notify.OutTradeNo, totalAmount, c.ClientIP()); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝回调入账失败 trade_no=%s error=%q", notify.OutTradeNo, err.Error()))
		c.String(http.StatusOK, "fail")
		return
	}
	c.String(http.StatusOK, "success")
}

// AlipayReturn only returns the browser to the local payment history page.
// It deliberately does not credit quota; only AlipayNotify can do that.
func AlipayReturn(c *gin.Context) {
	c.Redirect(http.StatusFound, paymentReturnPath("/console/log"))
}
