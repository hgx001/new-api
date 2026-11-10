package controller

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// VpnMaxDistinctIPs 单条凭据 24h 内允许出现的不同源 IP 数。
// 留了手机 + 电脑 + 临时换网的余量；超了不是拉黑用户，而是自动换一把新钥匙，
// 让转发出去的旧配置失效，本人点一次「重置」即可继续用。
const VpnMaxDistinctIPs = 3

// VpnNodeAgentTokenEnv 节点 agent 访问内部接口用的共享令牌，只在服务器环境里配置。
const VpnNodeAgentTokenEnv = "VPN_NODE_AGENT_TOKEN"

// vpnSubscribeBaseURL 订阅地址与支付回调共用同一个对外站点根地址。
func vpnSubscribeBaseURL() string {
	base := strings.TrimRight(service.GetCallbackAddress(), "/")
	if base == "" {
		base = strings.TrimRight(system_setting.ServerAddress, "/")
	}
	return strings.TrimRight(base, "/")
}

func vpnSubscribeURL(token string) string {
	return vpnSubscribeBaseURL() + "/api/user/vpn/sub/" + token
}

// buildVpnShareLink 复用现有节点的 Reality 参数模板，只替换 VLESS UUID。
// vpnNodeKey 因此从「所有人共享的交付密钥」降级为「节点参数来源」，
// 历史订单仍从库里读它，新订单不再拿到它。
func buildVpnShareLink(uuid string) (string, error) {
	u, err := url.Parse(vpnNodeKey)
	if err != nil {
		return "", err
	}
	if uuid == "" {
		return "", errors.New("VPN 凭据 UUID 为空")
	}
	u.User = url.User(uuid)
	return u.String(), nil
}

// GetVpnSubscription v2rayN 订阅入口（匿名，靠随机令牌寻址）。
// 令牌绑定订单而不是凭据，所以「重置」换 UUID 后订阅地址不变，用户无需重新导入。
func GetVpnSubscription(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	if token == "" {
		c.String(http.StatusNotFound, "not found")
		return
	}
	order, err := model.GetVpnOrderBySubToken(token)
	if err != nil || order == nil || order.Status != common.TopUpStatusSuccess {
		c.String(http.StatusNotFound, "subscription not found")
		return
	}
	if time.Now().Unix() > vpnExpireTime(order) {
		c.String(http.StatusForbidden, "subscription expired")
		return
	}
	cred, err := model.GetVpnCredentialByOrder(order.Id)
	if err != nil || cred == nil {
		c.String(http.StatusNotFound, "subscription not ready")
		return
	}
	now := time.Now().Unix()
	ip := c.ClientIP()
	// 订阅拉取 IP 只留痕，不参与共享判定：用户可能在手机上拉订阅、在电脑上连，
	// 两个 IP 都是本人。判定只认真实连上节点的出口 IP。
	if err := model.RecordVpnCredentialAccess(cred, ip, model.VpnAccessSourceSub, now); err != nil {
		logger.LogError(c.Request.Context(), "VPN 订阅访问记录失败: "+err.Error())
	}
	// 冻结后重新读一次，避免同一请求里用缓存状态把已冻结的凭据又发出去。
	cred, err = model.GetVpnCredentialByOrder(order.Id)
	if err != nil || cred == nil {
		c.String(http.StatusNotFound, "subscription not ready")
		return
	}
	if cred.Status == model.VpnCredentialFrozen {
		c.String(http.StatusForbidden, "检测到多地点同时使用，请在 VPN 页面点击「重置密钥」")
		return
	}
	if cred.Status != model.VpnCredentialActive {
		c.String(http.StatusForbidden, "subscription unavailable")
		return
	}
	link, err := buildVpnShareLink(cred.Uuid)
	if err != nil {
		logger.LogError(c.Request.Context(), "VPN 订阅链接生成失败: "+err.Error())
		c.String(http.StatusInternalServerError, "subscription error")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.String(http.StatusOK, link+"\n")
}

// ResetVpnSubscription 用户自助换钥匙。旧凭据作废旧 UUID 立即从节点清单消失，
// 分享出去的配置失效；订阅地址保持不变。
func ResetVpnSubscription(c *gin.Context) {
	userId := c.GetInt("id")
	order, err := model.GetLatestPaidVpnOrder(userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "尚未购买 VPN 套餐或订单未支付"})
		return
	}
	if order.SubToken == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "当前套餐为旧版密钥，不支持自助重置"})
		return
	}
	if time.Now().Unix() > vpnExpireTime(order) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "套餐已到期，续费后可继续使用"})
		return
	}
	cred, err := model.GetVpnCredentialByOrder(order.Id)
	if err != nil || cred == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "凭据不存在"})
		return
	}
	if time.Now().Unix()-cred.CreateTime < 60 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "刚刚已重置过，请稍后再试"})
		return
	}
	next, err := model.RotateVpnCredential(cred)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	logger.LogInfo(c.Request.Context(), "VPN 凭据已重置 user_id="+strconv.Itoa(userId)+
		" order_id="+strconv.Itoa(order.Id)+" old="+cred.Name+" new="+next.Name)
	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"message":       "已重置，请在客户端更新一次订阅",
		"subscribe_url": vpnSubscribeURL(order.SubToken),
	})
}

// VpnNodeManifest 节点 agent 拉取有效凭据清单。
// 节点本地基础配置里的 legacy 用户（没有 name）由 agent 原样保留，不经过这里。
func VpnNodeManifest(c *gin.Context) {
	if !vpnNodeAgentAuthorized(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "unauthorized"})
		return
	}
	now := time.Now().Unix()
	cutoff := now - int64(VpnPlanDays*24*time.Hour/time.Second)
	manifest, err := model.ListVpnNodeManifest(cutoff, now)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, manifest)
}

// VpnNodeConnectionEvent 节点日志里的一条已认证连接。
type VpnNodeConnectionEvent struct {
	Name string `json:"name"`
	Ip   string `json:"ip"`
	Ts   int64  `json:"ts"`
}

// VpnNodeConnections 节点 agent 上报连接源 IP，网站做共享判定。
// 上报失败不影响节点本地运行，agent 会在下一轮重试。
func VpnNodeConnections(c *gin.Context) {
	if !vpnNodeAgentAuthorized(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "unauthorized"})
		return
	}
	var body struct {
		Events []VpnNodeConnectionEvent `json:"events"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid body"})
		return
	}
	if len(body.Events) > 1000 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "too many events"})
		return
	}
	now := time.Now().Unix()
	frozen := make([]string, 0)
	seen := make(map[string]bool)
	for _, event := range body.Events {
		name := strings.TrimSpace(event.Name)
		ip := strings.TrimSpace(event.Ip)
		if name == "" || ip == "" || !strings.HasPrefix(name, "v") {
			continue
		}
		cred, err := model.GetVpnCredentialByName(name)
		if err != nil || cred == nil || cred.Status == model.VpnCredentialRevoked {
			continue
		}
		if err := model.RecordVpnCredentialAccess(cred, ip, model.VpnAccessSourceNode, now); err != nil {
			logger.LogError(c.Request.Context(), "VPN 节点连接记录失败: "+err.Error())
			continue
		}
		ok, _, err := model.EvaluateVpnCredentialAbuse(cred, model.VpnAccessSourceNode, VpnMaxDistinctIPs, now)
		if err != nil {
			logger.LogError(c.Request.Context(), "VPN 节点共享检测失败: "+err.Error())
			continue
		}
		if ok && !seen[name] {
			seen[name] = true
			frozen = append(frozen, name)
			logger.LogWarn(c.Request.Context(), "VPN 凭据因多源 IP 被冻结 name="+name)
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "frozen": frozen})
}

func vpnNodeAgentAuthorized(c *gin.Context) bool {
	expected := strings.TrimSpace(os.Getenv(VpnNodeAgentTokenEnv))
	if expected == "" {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}
