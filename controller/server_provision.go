package controller

// 人工开台的管理端接口：待分配列表、角标计数、分配动作。
//
// 只挂 AdminAuth：分配意味着把一台真机的登录凭据写进用户的控制台，
// 权限必须收到管理员这一层。

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type serverAssignRequest struct {
	TradeNo    string `json:"trade_no"`
	IP         string `json:"ip"`
	SshPort    int    `json:"ssh_port"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
	Note       string `json:"note"`
}

// GetServerProvisionOrders 待分配 / 已分配的购买订单列表。
func GetServerProvisionOrders(c *gin.Context) {
	status := strings.TrimSpace(c.Query("status"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	orders, err := model.ListServerProvisionOrders(status, limit)
	if err != nil {
		serverError(c, "server_error", err.Error())
		return
	}
	awaiting, oldest, err := model.ServerProvisionSummary()
	if err != nil {
		serverError(c, "server_error", err.Error())
		return
	}
	common.ApiSuccess(c, gin.H{
		"orders":              orders,
		"awaiting":            awaiting,
		"oldest_wait_seconds": oldest,
	})
}

// GetServerProvisionSummary 侧边栏角标用：待办数量与最久等待时长。
//
// 单独一个轻接口，前端可以 30 秒轮询而不必拉整张列表。
func GetServerProvisionSummary(c *gin.Context) {
	awaiting, oldest, err := model.ServerProvisionSummary()
	if err != nil {
		serverError(c, "server_error", err.Error())
		return
	}
	common.ApiSuccess(c, gin.H{
		"awaiting":            awaiting,
		"oldest_wait_seconds": oldest,
	})
}

// AssignServerOrder 交付一台机器：写入 IP、端口、账号、密码/密钥并开通计时。
func AssignServerOrder(c *gin.Context) {
	adminId := c.GetInt("id")
	var req serverAssignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		serverError(c, "invalid_assign_input", "参数错误")
		return
	}
	order, instance, err := model.AssignServerOrder(
		strings.TrimSpace(req.TradeNo),
		adminId,
		model.ServerAssignInput{
			IP:         req.IP,
			SshPort:    req.SshPort,
			Username:   req.Username,
			Password:   req.Password,
			PrivateKey: req.PrivateKey,
			Note:       req.Note,
		},
	)
	if err != nil {
		serverErrorFrom(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"order":    order,
		"instance": instance.ToView(common.GetTimestamp()),
	})
}
