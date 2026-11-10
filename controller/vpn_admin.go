package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// AdminGetVpnOrders 管理端 VPN 套餐订单只读视图（AdminAuth 由路由层保证）。
//
// VPN 年费套餐与充值额度是两套账：这里只消费 vpn_orders 的既有状态，
// 不改订单、不加额度、不写 top_ups。节点密钥属于交付凭据，永不进入响应。
func AdminGetVpnOrders(c *gin.Context) {
	status := c.Query("status")
	if !model.IsValidVpnOrderStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid status",
		})
		return
	}

	pageInfo := common.GetPageQuery(c)
	query := model.VpnOrderAdminQuery{
		Page:     pageInfo.GetPage(),
		PageSize: pageInfo.GetPageSize(),
		Keyword:  c.Query("keyword"),
		Status:   status,
	}
	orders, total, err := model.SearchVpnOrders(query)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	stats, err := model.GetVpnOrderStats(time.Now())
	if err != nil {
		common.ApiError(c, err)
		return
	}

	ids := make([]int, 0, len(orders))
	seen := make(map[int]bool, len(orders))
	for _, order := range orders {
		if !seen[order.UserId] {
			seen[order.UserId] = true
			ids = append(ids, order.UserId)
		}
	}
	usernames := model.GetVpnUsernames(ids)
	orderIds := make([]int, 0, len(orders))
	for _, order := range orders {
		orderIds = append(orderIds, order.Id)
	}
	credentials := model.GetVpnCredentialInfos(orderIds, time.Now().Unix())

	items := make([]gin.H, 0, len(orders))
	for _, order := range orders {
		items = append(items, gin.H{
			"id":               order.Id,
			"user_id":          order.UserId,
			"username":         usernames[order.UserId],
			"trade_no":         order.TradeNo,
			"plan":             order.Plan,
			"money":            order.Money,
			"payment_method":   order.PaymentMethod,
			"payment_provider": order.PaymentProvider,
			"status":           order.Status,
			"create_time":      order.CreateTime,
			"complete_time":    order.CompleteTime,
			"client_ip":        order.ClientIp,
			"credential":       credentials[order.Id],
		})
	}

	common.ApiSuccess(c, gin.H{
		"items":     items,
		"total":     total,
		"page":      query.Page,
		"page_size": query.PageSize,
		"stats":     stats,
	})
}
