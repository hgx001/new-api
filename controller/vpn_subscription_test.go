package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupVpnSubscriptionTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.VpnOrder{}, &model.VpnCredential{}, &model.VpnCredentialAccess{}))
	oldDB := model.DB
	model.DB = db
	oldAddr := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Setenv(VpnNodeAgentTokenEnv, "test-agent-token")
	t.Cleanup(func() {
		model.DB = oldDB
		system_setting.ServerAddress = oldAddr
	})
}

func seedVpnSubscriptionOrder(t *testing.T, tradeNo string, newRule bool) *model.VpnOrder {
	t.Helper()
	o := &model.VpnOrder{
		UserId: 61, TradeNo: tradeNo, Plan: "year", Money: 199,
		PaymentMethod: model.PaymentMethodAlipay, PaymentProvider: model.PaymentProviderAlipay,
		CreateTime: time.Now().Unix(), Status: common.TopUpStatusSuccess,
		CompleteTime: time.Now().Unix(), ClientIp: "203.0.113.7",
	}
	if newRule {
		require.NoError(t, model.CreateVpnOrderWithCredential(o))
	} else {
		o.NodeKey = vpnNodeKey
		require.NoError(t, model.DB.Create(o).Error)
	}
	return o
}

func serveVpnHandler(t *testing.T, method, path string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.Handle(method, path, func(c *gin.Context) {
		c.Set("id", 61)
		handler(c)
	})
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestBuildVpnShareLinkOnlyReplacesUUID 新链接必须保留节点全部 Reality 参数，只换 UUID。
func TestBuildVpnShareLinkOnlyReplacesUUID(t *testing.T) {
	template, err := url.Parse(vpnNodeKey)
	require.NoError(t, err)
	link, err := buildVpnShareLink("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	require.NoError(t, err)
	got, err := url.Parse(link)
	require.NoError(t, err)
	assert.Equal(t, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", got.User.Username())
	assert.Equal(t, template.Host, got.Host)
	assert.Equal(t, template.RawQuery, got.RawQuery)
	assert.Equal(t, template.Fragment, got.Fragment)
	assert.NotContains(t, link, template.User.Username())
}

// TestGetVpnMyKeyKeepsLegacyOrdersUnchanged 老订单照旧拿明文密钥，新订单只拿订阅地址。
func TestGetVpnMyKeyKeepsLegacyOrdersUnchanged(t *testing.T) {
	setupVpnSubscriptionTest(t)
	seedVpnSubscriptionOrder(t, "VPN-LEGACY", false)
	w := serveVpnHandler(t, http.MethodGet, "/api/user/vpn/key", GetVpnMyKey)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"mode":"legacy"`)
	template, err := url.Parse(vpnNodeKey)
	require.NoError(t, err)
	assert.Contains(t, w.Body.String(), template.User.Username())
	assert.NotContains(t, w.Body.String(), "subscribe_url")

	modern := seedVpnSubscriptionOrder(t, "VPN-MODERN", true)
	// GetLatestPaidVpnOrder 按 complete_time 倒序取第一笔，必须让新订单明确更新，
	// 否则同一秒下单时 SQLite 会命中老订单。
	require.NoError(t, model.DB.Model(modern).
		Update("complete_time", time.Now().Unix()+60).Error)
	w = serveVpnHandler(t, http.MethodGet, "/api/user/vpn/key", GetVpnMyKey)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"mode":"subscription"`)
	assert.Contains(t, w.Body.String(), "/api/user/vpn/sub/"+modern.SubToken)
	assert.NotContains(t, w.Body.String(), "vless://", "订阅模式下不能再把明文密钥下发到页面")
}

// TestGetVpnSubscriptionIssuesIndependentLinkAndFreezes 订阅只发该订单自己的 UUID。
func TestGetVpnSubscriptionIssuesIndependentLinkAndFreezes(t *testing.T) {
	setupVpnSubscriptionTest(t)
	o := seedVpnSubscriptionOrder(t, "VPN-SUB", true)
	cred, err := model.GetVpnCredentialByOrder(o.Id)
	require.NoError(t, err)
	template, err := url.Parse(vpnNodeKey)
	require.NoError(t, err)
	legacyUUID := template.User.Username()

	path := "/api/user/vpn/sub/" + o.SubToken
	r := gin.New()
	r.GET("/api/user/vpn/sub/:token", GetVpnSubscription)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "198.51.100.20:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "vless://"+cred.Uuid+"@")
	assert.NotContains(t, w.Body.String(), legacyUUID, "新订阅不能夹带历史共享 UUID")

	_, err = model.FreezeVpnCredential(cred, time.Now().Unix())
	require.NoError(t, err)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.NotContains(t, w.Body.String(), "vless://")
}

// TestVpnNodeManifestRequiresAgentToken 节点清单接口不能被匿名或普通用户读到。
func TestVpnNodeManifestRequiresAgentToken(t *testing.T) {
	setupVpnSubscriptionTest(t)
	o := seedVpnSubscriptionOrder(t, "VPN-MANIFEST", true)
	cred, err := model.GetVpnCredentialByOrder(o.Id)
	require.NoError(t, err)

	r := gin.New()
	r.GET("/api/vpn/node/manifest", VpnNodeManifest)
	req := httptest.NewRequest(http.MethodGet, "/api/vpn/node/manifest", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/vpn/node/manifest", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/vpn/node/manifest", nil)
	req.Header.Set("Authorization", "Bearer test-agent-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), cred.Uuid)
	assert.NotContains(t, w.Body.String(), "node_key")
}

// TestVpnNodeConnectionsFreezesSharedCredential 节点上报 4 个不同源 IP 后自动冻结。
func TestVpnNodeConnectionsFreezesSharedCredential(t *testing.T) {
	setupVpnSubscriptionTest(t)
	o := seedVpnSubscriptionOrder(t, "VPN-ABUSE", true)
	cred, err := model.GetVpnCredentialByOrder(o.Id)
	require.NoError(t, err)

	body := `{"events":[`
	for i := 1; i <= 4; i++ {
		if i > 1 {
			body += ","
		}
		body += fmt.Sprintf(`{"name":"%s","ip":"203.0.113.%d","ts":%d}`, cred.Name, i, time.Now().Unix())
	}
	body += `]}`
	r := gin.New()
	r.POST("/api/vpn/node/connections", VpnNodeConnections)
	req := httptest.NewRequest(http.MethodPost, "/api/vpn/node/connections", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-agent-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), cred.Name)

	got, err := model.GetVpnCredentialByOrder(o.Id)
	require.NoError(t, err)
	assert.Equal(t, model.VpnCredentialFrozen, got.Status)
}
