package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iGoogle-ink/gopay"
	"github.com/iGoogle-ink/gopay/alipay"
	"github.com/shopspring/decimal"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// 云服务器的在线支付链路 —— 与全站统一，走**支付宝官方直连**。
//
// 曾经这里接的是易支付网关（表单 POST 到第三方收银台）。用户要求全站统一
// 到官方支付宝，所以改为复用 getAlipayClient / verifyAlipayNotifySignature
// 这套充值与 VPN 已在用的直连能力，不再读 PayMethods、不再依赖易支付凭据。
//
// 与「站内余额」的区别：余额是同步的（点一下就扣完、机器立刻开），
// 在线支付是**异步**的 ——
//
//	下单 → 落一张 pending 订单 → 整页跳转到支付宝 → 用户付款
//	     → 支付宝异步回调（RSA2 验签）→ 校验金额 → 置成功 + 开通实例
//
// 因此实例是「回调时」才创建的，不是「下单时」。下单那一刻机器还不存在，
// 订单快照（配置/规格/价格）必须存在订单上，回调时才知道开什么机器。

// serverPayMethod 收银台上的一个支付方式。
type serverPayMethod struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Icon  string `json:"icon"`
	Price string `json:"price"`
}

// serverPaymentMethodAvailable 判断在线支付是否可用。
//
// 全站统一口径：只认支付宝官方直连。`isAlipayTopUpEnabled()` 内部已经
// 检查了支付合规闸门 + AlipayEnabled + AppID/私钥/公钥三项凭据。
//
// 这里**不看** operation_setting.PayMethods：那是易支付网关的渠道配置，
// 与官方直连是两回事。管理员在 PayMethods 里配了什么，都不影响直连。
func serverPaymentMethodAvailable(paymentMethod string) bool {
	return paymentMethod == model.PaymentMethodAlipay && isAlipayTopUpEnabled()
}

// serverPaymentProviders 把可用的在线支付渠道列给前端。
//
// 直连凭据不全或合规未确认时返回空列表 —— 不能给用户看一个点了会失败的
// 收银台。只下发一个「支付宝」，与站内充值的收款通道保持一致。
func serverPaymentProviders() []serverPayMethod {
	if !serverPaymentMethodAvailable(model.PaymentMethodAlipay) {
		return []serverPayMethod{}
	}
	return []serverPayMethod{
		{
			Name: "支付宝",
			Type: model.PaymentMethodAlipay,
			Icon: "SiAlipay",
		},
	}
}

// GetServerPayMethods 返回服务器租赁可用的支付方式（登录后）。
//
// 前端用它渲染「支付方式」列表：站内余额永远有，在线支付按服务端配置下发。
func GetServerPayMethods(c *gin.Context) {
	methods := []serverPayMethod{
		{
			Name: "余额支付",
			Type: model.PaymentMethodBalance,
			Icon: "LuWallet",
		},
	}
	methods = append(methods, serverPaymentProviders()...)

	quota, err := model.GetUserQuota(c.GetInt("id"), true)
	if err != nil {
		quota = 0
	}
	common.ApiSuccess(c, gin.H{
		"methods":       methods,
		"balance":       setting.ServerAmountFromQuota(quota),
		"balance_quota": quota,
	})
}

// serverAlipayPayRequest 拉起在线支付的请求。
type serverAlipayPayRequest struct {
	// TradeNo 是下单时生成的订单号；续费/释放等操作另有接口。
	TradeNo string `json:"trade_no"`
	// PaymentMethod 支付方式，当前只接受 alipay。
	PaymentMethod string `json:"payment_method"`
}

// serverAlipayNotifyURL 服务器订单的支付宝异步通知地址。
//
// 与充值/VPN 的通知分开，便于对账与排障；也避免改动已经稳定运行的
// 充值回调。地址在下单时随 TradePagePay 下发给支付宝。
func serverAlipayNotifyURL() string {
	base := strings.TrimRight(service.GetCallbackAddress(), "/")
	if base == "" {
		base = strings.TrimRight(system_setting.ServerAddress, "/")
	}
	return base + "/api/server/pay/alipay/notify"
}

// RequestServerAlipayPay 拉起支付宝官方收银台支付服务器订单。
//
// 前端流程：
//  1. POST /api/server/orders   → 拿到 pending 订单 + trade_no
//  2. POST /api/server/pay/alipay → 拿到官方收银台 URL，整页跳转
//  3. 轮询 GET /api/server/orders/:trade_no → 支付成功后展示凭据
func RequestServerAlipayPay(c *gin.Context) {
	userId := c.GetInt("id")
	var req serverAlipayPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		serverError(c, "invalid_config", "参数错误")
		return
	}
	req.TradeNo = strings.TrimSpace(req.TradeNo)
	if req.TradeNo == "" {
		serverError(c, "invalid_config", "缺少订单号")
		return
	}
	req.PaymentMethod = strings.TrimSpace(req.PaymentMethod)
	if req.PaymentMethod == "" {
		req.PaymentMethod = model.PaymentMethodAlipay
	}
	if !serverPaymentMethodAvailable(req.PaymentMethod) {
		serverError(c, "payment_method_invalid", "支付方式不可用")
		return
	}

	order := model.GetServerOrderByTradeNo(req.TradeNo)
	if order == nil {
		serverError(c, "order_not_found", "订单不存在")
		return
	}
	// 只能支付自己的订单。
	if order.UserId != userId {
		serverError(c, "order_not_found", "订单不存在")
		return
	}
	if order.Status == common.TopUpStatusSuccess {
		serverError(c, "order_paid", "订单已支付")
		return
	}
	if order.Status != common.TopUpStatusPending {
		serverError(c, "order_not_payable", "订单状态不可支付")
		return
	}
	if order.PaymentProvider != model.PaymentProviderAlipay {
		serverError(c, "order_not_payable", "该订单不支持在线支付")
		return
	}
	if order.TradeNoExpires > 0 && common.GetTimestamp() > order.TradeNoExpires {
		serverError(c, "order_expired", "订单已超时，请重新下单")
		return
	}

	client := getAlipayClient()
	if client == nil {
		serverError(c, "payment_not_configured", "支付宝官方直连尚未完成配置")
		return
	}

	amount := decimal.NewFromFloat(order.Amount).StringFixed(2)
	client.SetNotifyUrl(serverAlipayNotifyURL()).
		SetReturnUrl(paymentReturnPath("/servers?paid=1"))
	body := make(gopay.BodyMap)
	body.Set("subject", fmt.Sprintf("云服务器 %s", order.SpecId))
	body.Set("out_trade_no", order.TradeNo)
	// 下发给支付宝的金额必须已经舍入到分；回调校验用的是同一个口径
	// （见 model.CompleteServerOrderPaid 的按分比对）。
	body.Set("total_amount", amount)
	body.Set("product_code", "FAST_INSTANT_TRADE_PAY")
	payURL, err := client.TradePagePay(body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"支付宝 服务器订单拉起失败 user_id=%d trade_no=%s amount=%s error=%q",
			userId, order.TradeNo, amount, err.Error(),
		))
		serverError(c, "payment_launch_failed", "拉起支付宝支付失败")
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf(
		"支付宝 服务器订单拉起成功 user_id=%d trade_no=%s amount=%s",
		userId, order.TradeNo, amount,
	))
	common.ApiSuccess(c, gin.H{
		"pay_mode": "redirect",
		"url":      payURL,
		"trade_no": order.TradeNo,
	})
}

// ServerAlipayNotify 服务器订单的支付宝官方直连异步通知（唯一入账来源）。
//
// 口径与充值 / VPN 回调完全一致：原始表单 RSA2 验签 → app_id 比对 →
// 金额按「舍入到分」比对 → 开通实例。任何一步失败都回 fail 让支付宝重试；
// 只有实例真的开出来了才回 success，否则用户付了钱却永远拿不到机器。
func ServerAlipayNotify(c *gin.Context) {
	if !isAlipayTopUpEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf(
			"支付宝 服务器 webhook 被拒绝 reason=disabled path=%q client_ip=%s",
			c.Request.RequestURI, c.ClientIP(),
		))
		c.String(http.StatusOK, "fail")
		return
	}

	notify, err := alipay.ParseNotifyResult(c.Request)
	if err != nil || notify == nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf(
			"支付宝 服务器 webhook 表单解析失败 error=%v client_ip=%s", err, c.ClientIP(),
		))
		c.String(http.StatusOK, "fail")
		return
	}
	verified, err := verifyAlipayNotifySignature(c.Request.Form, operation_setting.AlipayPublicKey)
	if err != nil || !verified {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf(
			"支付宝 服务器 webhook 验签失败 trade_no=%s error=%v client_ip=%s",
			notify.OutTradeNo, err, c.ClientIP(),
		))
		c.String(http.StatusOK, "fail")
		return
	}
	// app_id 必须与本应用一致：防止「公钥配错 / 串应用」的通知被当成
	// 本应用的付款发货。
	configuredAppID := strings.TrimSpace(operation_setting.AlipayAppID)
	if notify.AppId == "" || notify.AppId != configuredAppID {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf(
			"支付宝 服务器 webhook APPID 不匹配 trade_no=%s notify_app_id=%q configured_app_id=%q",
			notify.OutTradeNo, notify.AppId, configuredAppID,
		))
		c.String(http.StatusOK, "fail")
		return
	}
	if notify.TradeStatus != "TRADE_SUCCESS" && notify.TradeStatus != "TRADE_FINISHED" {
		// 非成功事件（支付关闭等）无需开通，明确回 success 让支付宝停止重试。
		logger.LogInfo(c.Request.Context(), fmt.Sprintf(
			"支付宝 服务器 webhook 忽略事件 trade_no=%s trade_status=%s",
			notify.OutTradeNo, notify.TradeStatus,
		))
		c.String(http.StatusOK, "success")
		return
	}
	totalAmount, err := decimal.NewFromString(notify.TotalAmount)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf(
			"支付宝 服务器 webhook 金额解析失败 trade_no=%s total_amount=%s",
			notify.OutTradeNo, notify.TotalAmount,
		))
		c.String(http.StatusOK, "fail")
		return
	}
	paidAmount, err := strconv.ParseFloat(totalAmount.StringFixed(2), 64)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf(
			"支付宝 服务器 webhook 金额转换失败 trade_no=%s total_amount=%s error=%q",
			notify.OutTradeNo, notify.TotalAmount, err.Error(),
		))
		c.String(http.StatusOK, "fail")
		return
	}

	LockOrder(notify.OutTradeNo)
	defer UnlockOrder(notify.OutTradeNo)

	completed, instance, err := model.CompleteServerOrderPaid(
		notify.OutTradeNo,
		model.PaymentProviderAlipay,
		paidAmount,
		model.PaymentMethodAlipay,
	)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"支付宝 服务器 webhook 开通失败 trade_no=%s client_ip=%s error=%q",
			notify.OutTradeNo, c.ClientIP(), err.Error(),
		))
		// 回 fail：支付宝会重试。回 success 会让用户付了钱却永远拿不到机器。
		c.String(http.StatusOK, "fail")
		return
	}
	if completed && instance != nil {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf(
			"支付宝 服务器订单支付成功 trade_no=%s instance=%s user_id=%d amount=%s client_ip=%s",
			notify.OutTradeNo, instance.Name, instance.UserId, totalAmount.StringFixed(2), c.ClientIP(),
		))
	} else {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf(
			"支付宝 服务器订单回调幂等命中 trade_no=%s client_ip=%s",
			notify.OutTradeNo, c.ClientIP(),
		))
	}
	c.String(http.StatusOK, "success")
}

// GetServerOrderStatus 查询订单支付状态（前端轮询用）。
//
// 前端在支付宝收银台跳回来后，靠这个接口知道钱到没到、机器开没开。
func GetServerOrderStatus(c *gin.Context) {
	userId := c.GetInt("id")
	tradeNo := strings.TrimSpace(c.Param("trade_no"))
	if tradeNo == "" {
		serverError(c, "invalid_config", "缺少订单号")
		return
	}
	order := model.GetServerOrderByTradeNo(tradeNo)
	if order == nil || order.UserId != userId {
		serverError(c, "order_not_found", "订单不存在")
		return
	}
	now := common.GetTimestamp()
	data := gin.H{
		"order": order,
		"paid":  order.Status == common.TopUpStatusSuccess,
	}
	if order.InstanceId > 0 {
		if instance, err := model.GetServerInstance(userId, order.InstanceId); err == nil {
			data["instance"] = instance.ToView(now)
		}
	}
	common.ApiSuccess(c, data)
}

// StartServerOrderExpiryWorker 启动未支付订单的作废扫描。
func StartServerOrderExpiryWorker() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := model.ExpireServerOrders(common.GetTimestamp()); err != nil {
				common.SysError("failed to expire server orders: " + err.Error())
			}
		}
	}()
}
