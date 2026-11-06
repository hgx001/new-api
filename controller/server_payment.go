package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/samber/lo"
)

// 云服务器的第三方支付链路。
//
// 与「站内余额」的区别：余额是同步的（点一下就扣完、机器立刻开），
// 第三方支付是**异步**的 ——
//
//	下单 → 落一张 pending 订单 → 拉起网关收银台 → 用户付款
//	     → 网关异步回调（验签）→ 校验金额 → 置成功 + 开通实例
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

// serverPaymentMethodAvailable 判断某个第三方支付方式当前是否可用。
//
// 三个条件缺一不可：合规已确认、渠道在 PayMethods 里、网关凭据齐全。
// 少任何一个都不该让用户点下去 —— 点了只会失败。
//
// 注意这里**不看** isAlipayTopUpEnabled：那是「支付宝直连」通道，
// 与易支付网关里的「支付宝」通道是两回事。管理员在 PayMethods 里配了
// alipay，说明走的是易支付的支付宝通道，只要易支付凭据齐全就该放行。
func serverPaymentMethodAvailable(paymentMethod string) bool {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		return false
	}
	if !operation_setting.ContainsPayMethod(paymentMethod) {
		return false
	}
	return isEpayWebhookConfigured()
}

// serverPaymentProviders 把可用的支付渠道列给前端。
//
// 只有「网关凭据齐全 + 合规已确认 + 该渠道在 PayMethods 里」三者同时满足
// 才会下发 —— 不能给用户看一个点了会失败的收银台。
func serverPaymentProviders() []serverPayMethod {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		return []serverPayMethod{}
	}
	providers := make([]serverPayMethod, 0, len(operation_setting.PayMethods))
	for _, method := range operation_setting.PayMethods {
		methodType := method["type"]
		if methodType == "" {
			continue
		}
		if !serverPaymentMethodAvailable(methodType) {
			continue
		}
		providers = append(providers, serverPayMethod{
			Name: method["name"],
			Type: methodType,
			Icon: method["icon"],
		})
	}
	return providers
}

// GetServerPayMethods 返回服务器租赁可用的支付方式（登录后）。
//
// 前端用它渲染「支付方式」列表：站内余额永远有，第三方渠道按服务端配置下发。
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

// serverEpayPayRequest 拉起第三方收银台的请求。
type serverEpayPayRequest struct {
	// TradeNo 是下单时生成的订单号；续费/释放等操作另有接口。
	TradeNo string `json:"trade_no"`
	// PaymentMethod 是支付方式（alipay / wxpay 等）。
	PaymentMethod string `json:"payment_method"`
}

// RequestServerEpayPay 拉起第三方收银台支付服务器订单。
//
// 前端流程：
//  1. POST /api/server/orders  → 拿到 pending 订单 + trade_no
//  2. POST /api/server/pay/epay → 拿到网关参数，前端提交表单跳转
//  3. 轮询 GET /api/server/orders/:trade_no → 支付成功后展示凭据
func RequestServerEpayPay(c *gin.Context) {
	userId := c.GetInt("id")
	var req serverEpayPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		serverError(c, "invalid_config", "参数错误")
		return
	}
	req.TradeNo = strings.TrimSpace(req.TradeNo)
	if req.TradeNo == "" {
		serverError(c, "invalid_config", "缺少订单号")
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
	if order.PaymentProvider != model.PaymentProviderEpay {
		serverError(c, "order_not_payable", "该订单不支持在线支付")
		return
	}
	if order.TradeNoExpires > 0 && common.GetTimestamp() > order.TradeNoExpires {
		serverError(c, "order_expired", "订单已超时，请重新下单")
		return
	}
	if !operation_setting.ContainsPayMethod(req.PaymentMethod) {
		serverError(c, "payment_method_invalid", "支付方式不存在")
		return
	}

	client := GetEpayClient()
	if client == nil {
		serverError(c, "payment_not_configured", "当前管理员未配置支付信息")
		return
	}

	callBackAddress := service.GetCallbackAddress()
	returnUrl, err := url.Parse(paymentReturnPath("/servers?paid=1"))
	if err != nil {
		serverError(c, "payment_not_configured", "回调地址配置错误")
		return
	}
	notifyUrl, err := url.Parse(callBackAddress + "/api/server/pay/epay/notify")
	if err != nil {
		serverError(c, "payment_not_configured", "回调地址配置错误")
		return
	}

	uri, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           req.PaymentMethod,
		ServiceTradeNo: order.TradeNo,
		Name:           fmt.Sprintf("云服务器 %s", order.SpecId),
		Money:          strconv.FormatFloat(order.Amount, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyUrl,
		ReturnUrl:      returnUrl,
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"易支付 服务器订单拉起失败 user_id=%d trade_no=%s payment_method=%s amount=%.2f error=%q",
			userId, order.TradeNo, req.PaymentMethod, order.Amount, err.Error(),
		))
		serverError(c, "payment_launch_failed", "拉起支付失败")
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf(
		"易支付 服务器订单拉起成功 user_id=%d trade_no=%s payment_method=%s amount=%.2f",
		userId, order.TradeNo, req.PaymentMethod, order.Amount,
	))
	common.ApiSuccess(c, gin.H{
		"pay_mode": "form",
		"url":      uri,
		"params":   params,
		"trade_no": order.TradeNo,
	})
}

// ServerEpayNotify 服务器订单的易支付异步通知。
//
// 口径与充值链路一致：**验签 → 校验金额 → 开通实例**，任何一步失败都回 fail
// 让网关重试。只有实例真的开出来了才回 success。
func ServerEpayNotify(c *gin.Context) {
	if !isEpayWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf(
			"易支付 服务器 webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s",
			c.Request.RequestURI, c.ClientIP(),
		))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	var params map[string]string
	if c.Request.Method == http.MethodPost {
		if err := c.Request.ParseForm(); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf(
				"易支付 服务器 webhook 表单解析失败 path=%q client_ip=%s error=%q",
				c.Request.RequestURI, c.ClientIP(), err.Error(),
			))
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
		params = lo.Reduce(lo.Keys(c.Request.PostForm), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.PostForm.Get(t)
			return r
		}, map[string]string{})
	} else {
		params = lo.Reduce(lo.Keys(c.Request.URL.Query()), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.URL.Query().Get(t)
			return r
		}, map[string]string{})
	}

	if len(params) == 0 {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf(
			"易支付 服务器 webhook 参数为空 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP(),
		))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	client := GetEpayClient()
	if client == nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"易支付 服务器 webhook client 未初始化 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP(),
		))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	verifyInfo, err := client.Verify(params)
	if err != nil || verifyInfo == nil || !verifyInfo.VerifyStatus {
		// 注意：err 可能为 nil（只是签名对不上），直接 err.Error() 会 panic。
		// 验签失败的报文一律当错误处理，但**不能崩** —— 崩了网关会收到
		// 500 而不是 fail，重试逻辑和日志都会乱。
		verifyError := "签名不匹配"
		if err != nil {
			verifyError = err.Error()
		}
		logger.LogWarn(c.Request.Context(), fmt.Sprintf(
			"易支付 服务器 webhook 验签失败 path=%q client_ip=%s verify_error=%q",
			c.Request.RequestURI, c.ClientIP(), verifyError,
		))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	if verifyInfo.TradeStatus != epay.StatusTradeSuccess {
		// 非成功事件（支付关闭等）无需开通，明确回 success 让网关停止重试。
		logger.LogInfo(c.Request.Context(), fmt.Sprintf(
			"易支付 服务器 webhook 忽略事件 trade_no=%s trade_status=%s",
			verifyInfo.ServiceTradeNo, verifyInfo.TradeStatus,
		))
		_, _ = c.Writer.Write([]byte("success"))
		return
	}

	// 金额必须与订单一致（按分比对）——否则 0.01 元也能买走一台机器。
	paidAmount, err := strconv.ParseFloat(verifyInfo.Money, 64)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"易支付 服务器 webhook 金额解析失败 trade_no=%s money=%q error=%q",
			verifyInfo.ServiceTradeNo, verifyInfo.Money, err.Error(),
		))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	LockOrder(verifyInfo.ServiceTradeNo)
	defer UnlockOrder(verifyInfo.ServiceTradeNo)

	completed, instance, err := model.CompleteServerOrderPaid(
		verifyInfo.ServiceTradeNo,
		model.PaymentProviderEpay,
		paidAmount,
		verifyInfo.Type,
	)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"易支付 服务器 webhook 开通失败 trade_no=%s client_ip=%s error=%q",
			verifyInfo.ServiceTradeNo, c.ClientIP(), err.Error(),
		))
		// 回 fail：网关会重试。回 success 会让用户付了钱却永远拿不到机器。
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if completed && instance != nil {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf(
			"易支付 服务器订单支付成功 trade_no=%s instance=%s user_id=%d amount=%.2f client_ip=%s",
			verifyInfo.ServiceTradeNo, instance.Name, instance.UserId, paidAmount, c.ClientIP(),
		))
	} else {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf(
			"易支付 服务器订单回调幂等命中 trade_no=%s client_ip=%s", verifyInfo.ServiceTradeNo, c.ClientIP(),
		))
	}
	_, _ = c.Writer.Write([]byte("success"))
}

// GetServerOrderStatus 查询订单支付状态（前端轮询用）。
//
// 前端在第三方收银台跳回来后，靠这个接口知道钱到没到、机器开没开。
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
		"paid": order.Status == common.TopUpStatusSuccess,
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
