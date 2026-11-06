package controller

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

// 云服务器租赁接口。
//
// 金额只由服务端计算：下单请求只带「地区/机型/规格/带宽/时长」五个选择，
// 不带价格。前端展示价为同一套公式的镜像，改价必须两边一起改（各有测试锁定）。

type serverPurchaseRequest struct {
	setting.ServerConfig
	ClientRequestId string `json:"client_request_id"`
	// PaymentMethod 支付方式：balance（站内余额，默认）或第三方渠道（alipay/wxpay…）。
	PaymentMethod string `json:"payment_method"`
}

type serverRenewRequest struct {
	DurationId      string `json:"duration_id"`
	ClientRequestId string `json:"client_request_id"`
}

type serverPasswordRequest struct {
	Password string `json:"password"`
}

// serverError 返回带稳定错误码的失败响应。
//
// code 让前端能把常见失败翻成用户自己的语言（余额不足/已到期/并发冲突），
// message 是服务端的中文兜底，未知错误直接展示它。
func serverError(c *gin.Context, code string, message string) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"code":    code,
		"message": message,
	})
}

func serverErrorFrom(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrServerQuotaInsufficient):
		serverError(c, "insufficient_quota", "余额不足，请先充值")
	case errors.Is(err, model.ErrServerInstanceNotFound):
		serverError(c, "instance_not_found", "实例不存在")
	case errors.Is(err, model.ErrServerInstanceReleased):
		serverError(c, "instance_released", "实例已释放")
	case errors.Is(err, model.ErrServerInstanceExpired):
		serverError(c, "instance_expired", "实例已到期，请先续费")
	case errors.Is(err, model.ErrServerInstanceNotActive):
		serverError(c, "instance_not_active", "实例当前状态不支持该操作")
	case errors.Is(err, model.ErrServerConcurrentUpdate):
		serverError(c, "concurrent_update", "实例状态已变化，请刷新后重试")
	default:
		serverError(c, "server_error", err.Error())
	}
}

// GetServerCatalog 下发价格目录（公开，无需登录）。
func GetServerCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    setting.GetServerCatalog(),
	})
}

// QuoteServerPrice 服务端报价（公开）。下单时服务端会用同一函数再算一次。
func QuoteServerPrice(c *gin.Context) {
	var config setting.ServerConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		serverError(c, "invalid_config", "参数错误")
		return
	}
	quote, err := setting.ComputeServerQuote(config)
	if err != nil {
		serverError(c, "invalid_config", err.Error())
		return
	}
	common.ApiSuccess(c, quote)
}

func serverWalletSnapshot(userId int) gin.H {
	quota, err := model.GetUserQuota(userId, true)
	if err != nil {
		quota = 0
	}
	return gin.H{
		"quota":          quota,
		"balance_amount": setting.ServerAmountFromQuota(quota),
	}
}

// PurchaseServer 下单购买一台服务器。
//
// 两种支付方式：
//   - balance（默认）：同步扣站内额度，下单即开通，返回 instance；
//   - 第三方渠道（alipay/wxpay…）：只落一张 pending 订单，返回 trade_no，
//     前端拿它去拉起收银台；机器在网关回调成功后才开通。
//
// 返回的 data 里 pay_mode 告诉前端该怎么走：balance 直接展示凭据，
// epay 需要跳去收银台。
func PurchaseServer(c *gin.Context) {
	userId := c.GetInt("id")
	var req serverPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		serverError(c, "invalid_config", "参数错误")
		return
	}
	quote, err := setting.ComputeServerQuote(req.ServerConfig)
	if err != nil {
		serverError(c, "invalid_config", err.Error())
		return
	}

	paymentMethod := strings.TrimSpace(req.PaymentMethod)
	if paymentMethod == "" {
		paymentMethod = model.PaymentMethodBalance
	}
	provider := model.PaymentProviderBalance
	if paymentMethod != model.PaymentMethodBalance {
		// 第三方渠道：校验渠道确实可用，避免下单成功却拉不起收银台。
		if !serverPaymentMethodAvailable(paymentMethod) {
			serverError(c, "payment_method_invalid", "支付方式不可用")
			return
		}
		provider = model.PaymentProviderEpay
	}

	order, instance, err := model.PurchaseServerInstanceWithProvider(
		userId, req.ServerConfig, quote, paymentMethod, provider,
		strings.TrimSpace(req.ClientRequestId),
	)
	if err != nil {
		serverErrorFrom(c, err)
		return
	}

	now := common.GetTimestamp()
	data := gin.H{
		"order": order,
		"pay_mode": provider,
	}
	if instance != nil {
		data["instance"] = instance.ToView(now)
	}
	for key, value := range serverWalletSnapshot(userId) {
		data[key] = value
	}
	common.ApiSuccess(c, data)
}

// GetServerInstances 列出当前用户的实例；include_released=true 时含已释放记录。
func GetServerInstances(c *gin.Context) {
	userId := c.GetInt("id")
	now := common.GetTimestamp()
	// 先把到期实例翻成 expired：否则定时任务两次扫描之间会显示「运行中但已过期」。
	if _, _, err := model.ExpireServerInstances(now); err != nil {
		common.SysError("failed to expire server instances: " + err.Error())
	}
	includeReleased := c.Query("include_released") == "true"
	instances, err := model.ListServerInstances(userId, includeReleased)
	if err != nil {
		serverErrorFrom(c, err)
		return
	}
	views := make([]model.ServerInstanceView, 0, len(instances))
	for _, instance := range instances {
		views = append(views, instance.ToView(now))
	}
	data := gin.H{"items": views, "total": len(views)}
	for key, value := range serverWalletSnapshot(userId) {
		data[key] = value
	}
	common.ApiSuccess(c, data)
}

// GetServerInstance 返回单个实例详情。
func GetServerInstance(c *gin.Context) {
	userId := c.GetInt("id")
	instanceId, ok := parseServerInstanceId(c)
	if !ok {
		return
	}
	instance, err := model.GetServerInstance(userId, instanceId)
	if err != nil {
		serverErrorFrom(c, err)
		return
	}
	common.ApiSuccess(c, instance.ToView(common.GetTimestamp()))
}

// GetServerInstanceMetrics 返回实例最近 24 小时的监控数据。
func GetServerInstanceMetrics(c *gin.Context) {
	userId := c.GetInt("id")
	instanceId, ok := parseServerInstanceId(c)
	if !ok {
		return
	}
	instance, err := model.GetServerInstance(userId, instanceId)
	if err != nil {
		serverErrorFrom(c, err)
		return
	}
	common.ApiSuccess(c, model.BuildServerMetrics(instance, common.GetTimestamp(), 12))
}

// GetServerInstanceEvents 返回实例的操作记录（开关机、改密、续费、释放）。
func GetServerInstanceEvents(c *gin.Context) {
	userId := c.GetInt("id")
	instanceId, ok := parseServerInstanceId(c)
	if !ok {
		return
	}
	events, err := model.ListServerInstanceEvents(userId, instanceId, 50)
	if err != nil {
		serverErrorFrom(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": events})
}

// StartServerInstance / StopServerInstance 开关机。
func StartServerInstance(c *gin.Context) {
	setServerPower(c, true)
}

func StopServerInstance(c *gin.Context) {
	setServerPower(c, false)
}

func setServerPower(c *gin.Context, start bool) {
	userId := c.GetInt("id")
	instanceId, ok := parseServerInstanceId(c)
	if !ok {
		return
	}
	instance, err := model.SetServerInstancePower(userId, instanceId, start)
	if err != nil {
		serverErrorFrom(c, err)
		return
	}
	common.ApiSuccess(c, instance.ToView(common.GetTimestamp()))
}

func RebootServerInstance(c *gin.Context) {
	userId := c.GetInt("id")
	instanceId, ok := parseServerInstanceId(c)
	if !ok {
		return
	}
	instance, err := model.RebootServerInstance(userId, instanceId)
	if err != nil {
		serverErrorFrom(c, err)
		return
	}
	common.ApiSuccess(c, instance.ToView(common.GetTimestamp()))
}

// ResetServerInstancePassword 重置登录密码，新密码只在本次响应里回一次。
func ResetServerInstancePassword(c *gin.Context) {
	userId := c.GetInt("id")
	instanceId, ok := parseServerInstanceId(c)
	if !ok {
		return
	}
	var req serverPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		serverError(c, "invalid_config", "参数错误")
		return
	}
	if len(req.Password) < 8 {
		serverError(c, "password_too_short", "密码至少 8 位")
		return
	}
	if _, err := model.ResetServerInstancePassword(userId, instanceId, req.Password); err != nil {
		serverErrorFrom(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"password": req.Password})
}

// RenewServerInstance 续费。
//
// 续费价按**当前目录价**重算；如果规格已下架（历史实例），回退到实例下单时
// 记录的单价 —— 不能因为下架就让人无法续费，也不能按新档位偷换配置。
func RenewServerInstance(c *gin.Context) {
	userId := c.GetInt("id")
	instanceId, ok := parseServerInstanceId(c)
	if !ok {
		return
	}
	var req serverRenewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		serverError(c, "invalid_config", "参数错误")
		return
	}
	instance, err := model.GetServerInstance(userId, instanceId)
	if err != nil {
		serverErrorFrom(c, err)
		return
	}
	quote, err := serverRenewQuote(instance, strings.TrimSpace(req.DurationId))
	if err != nil {
		serverError(c, "invalid_config", err.Error())
		return
	}

	order, updated, err := model.RenewServerInstance(
		userId, instanceId, quote, model.PaymentMethodBalance, strings.TrimSpace(req.ClientRequestId),
	)
	if err != nil {
		serverErrorFrom(c, err)
		return
	}
	data := gin.H{
		"order":    order,
		"instance": updated.ToView(common.GetTimestamp()),
	}
	for key, value := range serverWalletSnapshot(userId) {
		data[key] = value
	}
	common.ApiSuccess(c, data)
}

// ReleaseServerInstance 释放实例并按剩余时长退款。
func ReleaseServerInstance(c *gin.Context) {
	userId := c.GetInt("id")
	instanceId, ok := parseServerInstanceId(c)
	if !ok {
		return
	}
	refundOrder, instance, err := model.ReleaseServerInstance(userId, instanceId)
	if err != nil {
		serverErrorFrom(c, err)
		return
	}
	data := gin.H{
		"instance": instance.ToView(common.GetTimestamp()),
		"refunded": 0.0,
	}
	if refundOrder != nil {
		data["refund_order"] = refundOrder
		data["refunded"] = -refundOrder.Amount
	}
	for key, value := range serverWalletSnapshot(userId) {
		data[key] = value
	}
	common.ApiSuccess(c, data)
}

// GetServerOrders 返回购买/续费/退款流水。
func GetServerOrders(c *gin.Context) {
	userId := c.GetInt("id")
	orders, err := model.ListServerOrders(userId, 100)
	if err != nil {
		serverErrorFrom(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": orders})
}

func parseServerInstanceId(c *gin.Context) (int, bool) {
	instanceId := 0
	for _, ch := range c.Param("id") {
		if ch < '0' || ch > '9' {
			serverError(c, "invalid_config", "实例 ID 不合法")
			return 0, false
		}
		instanceId = instanceId*10 + int(ch-'0')
		if instanceId > 1<<31-1 {
			serverError(c, "invalid_config", "实例 ID 不合法")
			return 0, false
		}
	}
	if instanceId <= 0 {
		serverError(c, "invalid_config", "实例 ID 不合法")
		return 0, false
	}
	return instanceId, true
}

// serverRenewQuote 计算续费报价。
//
// 只允许原计费口径的时长（按量的实例不能续一年）：
// 两种口径的单价含义不同（¥/小时 vs ¥/月），混用会让续费价差一个数量级。
func serverRenewQuote(instance *model.ServerInstance, durationId string) (*setting.ServerQuote, error) {
	duration, ok := setting.GetServerDurationById(durationId)
	if !ok {
		return nil, errors.New("未知租用时长")
	}
	if duration.Pricing != instance.Billing {
		return nil, errors.New("续费时长必须与原实例的计费方式一致")
	}

	var config setting.ServerConfig
	if instance.Config != "" {
		if err := common.UnmarshalJsonStr(instance.Config, &config); err != nil {
			return nil, err
		}
	}
	config.DurationId = durationId

	quote, err := setting.ComputeServerQuote(config)
	if err == nil {
		return quote, nil
	}

	// 规格/机型已下架：按实例下单时的单价续费，保证历史用户仍可续。
	machinePrice := instance.UnitPrice
	total := machinePrice * float64(duration.Amount) * duration.Discount
	total = float64(int(total*100+0.5)) / 100
	return &setting.ServerQuote{
		Billing:       duration.Pricing,
		Unit:          map[string]string{setting.ServerBillingHourly: "hour", setting.ServerBillingPrepaid: "month"}[duration.Pricing],
		Units:         duration.Amount,
		UnitPrice:     machinePrice,
		Discount:      duration.Discount,
		ListTotal:     float64(int(machinePrice*float64(duration.Amount)*100+0.5)) / 100,
		Total:         total,
		PeriodSeconds: setting.ServerPeriodSeconds(duration),
		Region:        instance.Region,
		Plan:          instance.Plan,
		SpecId:        instance.SpecId,
		Mbps:          instance.Mbps,
		DurationId:    duration.Id,
	}, nil
}
