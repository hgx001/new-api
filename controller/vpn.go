package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/gin-gonic/gin"
	"github.com/iGoogle-ink/gopay"
	"github.com/iGoogle-ink/gopay/alipay"
	"github.com/shopspring/decimal"
)

// VpnPlanYearPrice VPN 年费套餐价格（人民币元）
const VpnPlanYearPrice = 199.00

// VpnPlanDays 年费套餐有效期（天）
const VpnPlanDays = 365

// 前端据此决定怎么拉起支付：
//   - VpnPayModeForm：易支付网关需要浏览器 POST 一张表单（submitPaymentForm）
//   - VpnPayModeRedirect：支付宝官方直连只需要整页跳转到支付宝收银台
const (
	VpnPayModeForm     = "form"
	VpnPayModeRedirect = "redirect"
)

// vpnExpireTime 计算套餐到期时间：以支付完成时间起算，缺省回落至下单时间
func vpnExpireTime(order *model.VpnOrder) int64 {
	base := order.CompleteTime
	if base <= 0 {
		base = order.CreateTime
	}
	return base + int64(VpnPlanDays*24*time.Hour/time.Second)
}

// vpnNodeKey 支付成功后下发的节点导入密钥（VLESS Reality）
const vpnNodeKey = "vless://d316bab5-3a45-4630-abdc-66c178532b2e@43.167.248.149:8443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.apple.com&fp=random&pbk=qBs6CoSgvHwIBtPM87AgY5UOOEKEwUMfzOSDb5TPTFU&sid=6ba85179e30d4fc2&type=tcp&headerType=none#%E6%97%A5%E6%9C%AC%E4%B8%9C%E4%BA%AC"

type VpnOrderRequest struct {
	PaymentMethod string `json:"payment_method"`
}

// RequestVpnOrder 创建 VPN 年费套餐订单并拉起支付。
//
// 路由规则与充值链路 (/api/user/pay) 保持一致：payment_method=alipay 且支付宝官方直连
// 已开通时走官方直连（返回收银台 URL，前端整页跳转）；其余方式走易支付网关（表单 POST）。
//
// 之前 VPN 只有易支付一条路，而「alipay」这个名字在易支付里指的是**网关的支付宝通道**，
// 收款方并不是支付宝官方：页面写着「支付宝」、钱却进易支付商户，对账/退款/风控全在别人手里。
func RequestVpnOrder(c *gin.Context) {
	var req VpnOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	req.PaymentMethod = strings.TrimSpace(req.PaymentMethod)
	if req.PaymentMethod == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付方式不存在"})
		return
	}
	// 支付方式白名单：官方直连的 alipay 不强制写进 PayMethods（与充值链路同规则），
	// 其余方式必须在 PayMethods 里配置过，避免前端随便传一个 type 就去拉起网关。
	if !operation_setting.ContainsPayMethod(req.PaymentMethod) &&
		!(req.PaymentMethod == model.PaymentMethodAlipay && isAlipayTopUpEnabled()) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付方式不存在"})
		return
	}

	id := c.GetInt("id")
	if req.PaymentMethod == model.PaymentMethodAlipay && isAlipayTopUpEnabled() {
		requestVpnOrderAlipay(c, id)
		return
	}
	requestVpnOrderEpay(c, id, req.PaymentMethod)
}

// requestVpnOrderAlipay 走支付宝官方直连：落库 → 生成收银台 URL → 前端整页跳转。
func requestVpnOrderAlipay(c *gin.Context, userId int) {
	client := getAlipayClient()
	if client == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付宝官方直连尚未完成配置"})
		return
	}

	tradeNo := fmt.Sprintf("VPN%dALIPAY%s%d", userId, common.GetRandomString(16), time.Now().Unix())
	order := &model.VpnOrder{
		UserId:          userId,
		TradeNo:         tradeNo,
		Plan:            "year",
		Money:           VpnPlanYearPrice,
		PaymentMethod:   model.PaymentMethodAlipay,
		PaymentProvider: model.PaymentProviderAlipay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
		NodeKey:         vpnNodeKey,
	}
	// 先落库再拉起支付：支付宝的异步通知随时可能到，订单不存在就查不到 → 钱付了不发货。
	if err := order.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 VPN 创建订单失败 user_id=%d trade_no=%s error=%q", userId, tradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	client.SetNotifyUrl(vpnAlipayNotifyURL()).SetReturnUrl(paymentReturnPath("/vpn?paid=1"))
	body := make(gopay.BodyMap)
	body.Set("subject", "VPN年费套餐")
	body.Set("out_trade_no", tradeNo)
	// 下发给支付宝的金额与回调校验用的是同一份「舍入到分」的值（见 model.CompleteVpnOrderPaid）。
	body.Set("total_amount", decimal.NewFromFloat(VpnPlanYearPrice).StringFixed(2))
	body.Set("product_code", "FAST_INSTANT_TRADE_PAY")
	payURL, err := client.TradePagePay(body)
	if err != nil {
		// 拉起失败必须把这行订单标成 failed，否则它永远停在 pending：
		// 前端每 3 秒轮询一次 pending 订单，永远等不到密钥。
		order.Status = common.TopUpStatusFailed
		if updateErr := order.Update(); updateErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 VPN 订单失败状态更新失败 trade_no=%s error=%q", tradeNo, updateErr.Error()))
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 VPN 拉起支付失败 user_id=%d trade_no=%s error=%q", userId, tradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付宝支付失败"})
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 VPN 订单创建成功 user_id=%d trade_no=%s money=%.2f", userId, tradeNo, VpnPlanYearPrice))
	c.JSON(http.StatusOK, gin.H{
		"message":  "success",
		"data":     gin.H{"url": payURL, "trade_no": tradeNo},
		"url":      payURL,
		"pay_mode": VpnPayModeRedirect,
	})
}

// vpnAlipayNotifyURL VPN 订单的支付宝异步通知地址（与充值通知分开，便于对账与排障）。
func vpnAlipayNotifyURL() string {
	base := strings.TrimRight(service.GetCallbackAddress(), "/")
	if base == "" {
		base = strings.TrimRight(system_setting.ServerAddress, "/")
	}
	return base + "/api/user/vpn/alipay/notify"
}

// requestVpnOrderEpay 走易支付网关：落库 → 生成网关提交表单 → 前端 POST 表单。
func requestVpnOrderEpay(c *gin.Context, id int, paymentMethod string) {
	client := GetEpayClient()
	if client == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "当前管理员未配置支付信息"})
		return
	}

	// 已有生效订单的用户重复购买：直接允许（续费），不拦截
	callBackAddress := service.GetCallbackAddress()
	returnUrl, _ := url.Parse(paymentReturnPath("/vpn?paid=1"))
	notifyUrl, _ := url.Parse(callBackAddress + "/api/user/vpn/epay/notify")
	tradeNo := fmt.Sprintf("VPN%dNO%s%d", id, common.GetRandomString(6), time.Now().Unix())

	uri, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           paymentMethod,
		ServiceTradeNo: tradeNo,
		Name:           "VPN年费套餐",
		Money:          strconv.FormatFloat(VpnPlanYearPrice, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyUrl,
		ReturnUrl:      returnUrl,
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 VPN 拉起支付失败 user_id=%d trade_no=%s payment_method=%s error=%q", id, tradeNo, paymentMethod, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	order := &model.VpnOrder{
		UserId:          id,
		TradeNo:         tradeNo,
		Plan:            "year",
		Money:           VpnPlanYearPrice,
		PaymentMethod:   paymentMethod,
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
		NodeKey:         vpnNodeKey,
	}
	if err := order.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 VPN 创建订单失败 user_id=%d trade_no=%s error=%q", id, tradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 VPN 订单创建成功 user_id=%d trade_no=%s payment_method=%s money=%.2f", id, tradeNo, paymentMethod, VpnPlanYearPrice))
	c.JSON(http.StatusOK, gin.H{
		"message":  "success",
		"data":     params,
		"url":      uri,
		"pay_mode": VpnPayModeForm,
	})
}

// VpnAlipayNotify VPN 订单的支付宝官方直连异步通知（唯一的入账来源）。
//
// 口径与充值回调一致：原始表单验签 + app_id 比对 + 金额按「舍入到分」比对，
// 任何一项不符都拒绝（订单保持 pending，管理员可补单），绝不「先置成功再说」。
func VpnAlipayNotify(c *gin.Context) {
	if !isAlipayTopUpEnabled() {
		logger.LogWarn(c.Request.Context(), "支付宝 VPN webhook 被拒绝 reason=disabled")
		c.String(http.StatusOK, "fail")
		return
	}

	notify, err := alipay.ParseNotifyResult(c.Request)
	if err != nil || notify == nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝 VPN 回调解析失败 error=%v", err))
		c.String(http.StatusOK, "fail")
		return
	}
	verified, err := verifyAlipayNotifySignature(c.Request.Form, operation_setting.AlipayPublicKey)
	if err != nil || !verified {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝 VPN 回调验签失败 trade_no=%s error=%v", notify.OutTradeNo, err))
		c.String(http.StatusOK, "fail")
		return
	}
	// app_id 必须与本应用一致：防止「公钥配错 / 串应用」的通知被当成本应用的付款入账。
	configuredAppID := strings.TrimSpace(operation_setting.AlipayAppID)
	if notify.AppId == "" || notify.AppId != configuredAppID {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝 VPN 回调 APPID 不匹配 trade_no=%s notify_app_id=%q configured_app_id=%q", notify.OutTradeNo, notify.AppId, configuredAppID))
		c.String(http.StatusOK, "fail")
		return
	}
	if notify.TradeStatus != "TRADE_SUCCESS" && notify.TradeStatus != "TRADE_FINISHED" {
		c.String(http.StatusOK, "success")
		return
	}
	totalAmount, err := decimal.NewFromString(notify.TotalAmount)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝 VPN 回调金额解析失败 trade_no=%s total_amount=%s", notify.OutTradeNo, notify.TotalAmount))
		c.String(http.StatusOK, "fail")
		return
	}

	LockOrder(notify.OutTradeNo)
	defer UnlockOrder(notify.OutTradeNo)
	completed, err := model.CompleteVpnOrderPaid(
		notify.OutTradeNo,
		model.PaymentProviderAlipay,
		totalAmount,
		time.Now().Unix(),
		nil,
	)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 VPN 回调入账失败 trade_no=%s error=%q", notify.OutTradeNo, err.Error()))
		c.String(http.StatusOK, "fail")
		return
	}
	if completed {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 VPN 支付成功 trade_no=%s money=%s client_ip=%s", notify.OutTradeNo, totalAmount.StringFixed(2), c.ClientIP()))
	} else {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 VPN 回调幂等命中 trade_no=%s client_ip=%s", notify.OutTradeNo, c.ClientIP()))
	}
	c.String(http.StatusOK, "success")
}

// VpnEpayNotify VPN 订单易支付回调（验签成功且交易成功则标记订单已支付）
func VpnEpayNotify(c *gin.Context) {
	if !isEpayWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 VPN webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	var params map[string]string
	if c.Request.Method == "POST" {
		if err := c.Request.ParseForm(); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 VPN webhook POST 表单解析失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
		params = map[string]string{}
		for k := range c.Request.PostForm {
			params[k] = c.Request.PostForm.Get(k)
		}
	} else {
		params = map[string]string{}
		for k := range c.Request.URL.Query() {
			params[k] = c.Request.URL.Query().Get(k)
		}
	}
	if len(params) == 0 {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 VPN webhook 参数为空 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	client := GetEpayClient()
	if client == nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 VPN client 未初始化 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	verifyInfo, err := client.Verify(params)
	if err != nil || !verifyInfo.VerifyStatus {
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 VPN webhook 验签失败 path=%q verify_error=%q", c.Request.RequestURI, err.Error()))
		} else {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 VPN webhook 验签失败 path=%q verify_status=false", c.Request.RequestURI))
		}
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if verifyInfo.TradeStatus != epay.StatusTradeSuccess {
		_, _ = c.Writer.Write([]byte("success"))
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 VPN webhook 忽略事件 trade_no=%s trade_status=%s", verifyInfo.ServiceTradeNo, verifyInfo.TradeStatus))
		return
	}
	// 金额必须与订单金额一致：签名只能证明报文来自网关，不能证明金额是 199 元。
	// 少了这一步，「用 0.01 元下单」也会被置成功 → 白送年费套餐。
	paidAmount, err := decimal.NewFromString(verifyInfo.Money)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 VPN webhook 金额解析失败 trade_no=%s money=%q", verifyInfo.ServiceTradeNo, verifyInfo.Money))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	LockOrder(verifyInfo.ServiceTradeNo)
	defer UnlockOrder(verifyInfo.ServiceTradeNo)
	order := model.GetVpnOrderByTradeNo(verifyInfo.ServiceTradeNo)
	if order == nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 VPN 回调订单不存在 trade_no=%s client_ip=%s", verifyInfo.ServiceTradeNo, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	extraUpdates := map[string]interface{}{}
	if order.PaymentMethod != verifyInfo.Type {
		// 回写网关回传的**实际**支付方式（下单时选的和实际付的可能不是同一个）
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 VPN 实际支付方式与订单不同 trade_no=%s order_payment_method=%s actual_type=%s", order.TradeNo, order.PaymentMethod, verifyInfo.Type))
		extraUpdates["payment_method"] = verifyInfo.Type
	}
	completed, err := model.CompleteVpnOrderPaid(
		verifyInfo.ServiceTradeNo,
		model.PaymentProviderEpay,
		paidAmount,
		time.Now().Unix(),
		extraUpdates,
	)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 VPN 更新订单失败 trade_no=%s user_id=%d error=%q", order.TradeNo, order.UserId, err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if completed {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 VPN 支付成功 trade_no=%s user_id=%d money=%.2f client_ip=%s", order.TradeNo, order.UserId, order.Money, c.ClientIP()))
	}
	_, _ = c.Writer.Write([]byte("success"))
}

// sanitizeVpnOrder 隐藏服务端内部字段
func sanitizeVpnOrder(o *model.VpnOrder) gin.H {
	return gin.H{
		"id":             o.Id,
		"trade_no":       o.TradeNo,
		"plan":           o.Plan,
		"money":          o.Money,
		"payment_method": o.PaymentMethod,
		"status":         o.Status,
		"create_time":    o.CreateTime,
		"complete_time":  o.CompleteTime,
	}
}

// GetVpnMyOrders 当前用户的 VPN 订单列表（不含密钥）
func GetVpnMyOrders(c *gin.Context) {
	userId := c.GetInt("id")
	orders, err := model.GetUserVpnOrders(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]gin.H, 0, len(orders))
	for _, o := range orders {
		items = append(items, sanitizeVpnOrder(o))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

// GetVpnMyKey 返回当前用户已支付订单的节点密钥（未支付或已过期一律不下发）
func GetVpnMyKey(c *gin.Context) {
	userId := c.GetInt("id")
	order, err := model.GetLatestPaidVpnOrder(userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "尚未购买 VPN 套餐或订单未支付"})
		return
	}
	expireAt := vpnExpireTime(order)
	if time.Now().Unix() > expireAt {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "套餐已到期，续费后可继续使用",
			"data": gin.H{
				"expired":     true,
				"expire_time": expireAt,
				"trade_no":    order.TradeNo,
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"trade_no":    order.TradeNo,
			"plan":        order.Plan,
			"pay_time":    order.CompleteTime,
			"expire_time": expireAt,
			"key":         order.NodeKey,
		},
	})
}
