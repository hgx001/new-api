package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/gin-gonic/gin"
)

// VpnPlanYearPrice VPN 年费套餐价格（人民币元）
const VpnPlanYearPrice = 199.00

// VpnPlanDays 年费套餐有效期（天）
const VpnPlanDays = 365

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

// RequestVpnOrder 创建 VPN 年费套餐订单并拉起易支付
func RequestVpnOrder(c *gin.Context) {
	var req VpnOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if !operation_setting.ContainsPayMethod(req.PaymentMethod) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付方式不存在"})
		return
	}
	id := c.GetInt("id")

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
		Type:           req.PaymentMethod,
		ServiceTradeNo: tradeNo,
		Name:           "VPN年费套餐",
		Money:          strconv.FormatFloat(VpnPlanYearPrice, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyUrl,
		ReturnUrl:      returnUrl,
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 VPN 拉起支付失败 user_id=%d trade_no=%s payment_method=%s error=%q", id, tradeNo, req.PaymentMethod, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	order := &model.VpnOrder{
		UserId:          id,
		TradeNo:         tradeNo,
		Plan:            "year",
		Money:           VpnPlanYearPrice,
		PaymentMethod:   req.PaymentMethod,
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
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 VPN 订单创建成功 user_id=%d trade_no=%s payment_method=%s money=%.2f", id, tradeNo, req.PaymentMethod, VpnPlanYearPrice))
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": params, "url": uri})
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
	_, _ = c.Writer.Write([]byte("success"))

	if verifyInfo.TradeStatus != epay.StatusTradeSuccess {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 VPN webhook 忽略事件 trade_no=%s trade_status=%s", verifyInfo.ServiceTradeNo, verifyInfo.TradeStatus))
		return
	}
	LockOrder(verifyInfo.ServiceTradeNo)
	defer UnlockOrder(verifyInfo.ServiceTradeNo)
	order := model.GetVpnOrderByTradeNo(verifyInfo.ServiceTradeNo)
	if order == nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 VPN 回调订单不存在 trade_no=%s client_ip=%s", verifyInfo.ServiceTradeNo, c.ClientIP()))
		return
	}
	if order.Status == common.TopUpStatusPending {
		if order.PaymentMethod != verifyInfo.Type {
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 VPN 实际支付方式与订单不同 trade_no=%s order_payment_method=%s actual_type=%s", order.TradeNo, order.PaymentMethod, verifyInfo.Type))
			order.PaymentMethod = verifyInfo.Type
		}
		order.Status = common.TopUpStatusSuccess
		order.CompleteTime = time.Now().Unix()
		if err := order.Update(); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 VPN 更新订单失败 trade_no=%s user_id=%d error=%q", order.TradeNo, order.UserId, err.Error()))
			return
		}
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 VPN 支付成功 trade_no=%s user_id=%d money=%.2f client_ip=%s", order.TradeNo, order.UserId, order.Money, c.ClientIP()))
	}
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
