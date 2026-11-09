package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupVpnAdminDB 给管理端订单视图准备独立内存库。
// users 只需要 id/username 两列：用户名展示与搜索走批量查询，不依赖 User 全字段迁移。
func setupVpnAdminDB(t *testing.T) time.Time {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.VpnOrder{}))
	require.NoError(t, db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT)").Error)
	require.NoError(t, db.Exec("INSERT INTO users (id, username) VALUES (61, 'alice'), (31, 'bob'), (28, 'admin')").Error)

	oldDB, oldLogDB := model.DB, model.LOG_DB
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
	})

	now := time.Date(2026, 11, 9, 22, 0, 0, 0, time.Local)
	todayStart := time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local).Unix()
	seed := []model.VpnOrder{
		{UserId: 61, TradeNo: "VPN-A-ALIPAY", Plan: "year", Money: 199, PaymentMethod: "alipay", PaymentProvider: "alipay", Status: common.TopUpStatusSuccess, CreateTime: todayStart + 100, CompleteTime: todayStart + 200, NodeKey: "vless://secret-a"},
		{UserId: 31, TradeNo: "VPN-B-EPAY", Plan: "year", Money: 199, PaymentMethod: "alipay", PaymentProvider: "epay", Status: common.TopUpStatusSuccess, CreateTime: todayStart + 90, CompleteTime: todayStart + 190, NodeKey: "vless://secret-b"},
		{UserId: 28, TradeNo: "VPN-C-ALIPAY", Plan: "year", Money: 199, PaymentMethod: "alipay", PaymentProvider: "alipay", Status: common.TopUpStatusSuccess, CreateTime: todayStart - 3*86400, CompleteTime: todayStart - 3*86400 + 10, NodeKey: "vless://secret-c"},
		{UserId: 28, TradeNo: "VPN-D-PENDING", Plan: "year", Money: 199, PaymentMethod: "alipay", PaymentProvider: "epay", Status: common.TopUpStatusPending, CreateTime: todayStart - 86400, NodeKey: "vless://secret-d"},
		{UserId: 28, TradeNo: "VPN-E-PENDING", Plan: "year", Money: 199, PaymentMethod: "alipay", PaymentProvider: "alipay", Status: common.TopUpStatusPending, CreateTime: todayStart - 2*86400, NodeKey: "vless://secret-e"},
	}
	for i := range seed {
		require.NoError(t, db.Create(&seed[i]).Error)
	}
	return now
}

func callAdminVpnOrders(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.GET("/api/vpn/admin/orders", AdminGetVpnOrders)
	req := httptest.NewRequest(http.MethodGet, "/api/vpn/admin/orders"+query, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestAdminVpnOrdersFiltersAndPaging 管理端列表：分页、状态、订单号/用户ID/用户名搜索。
func TestAdminVpnOrdersFiltersAndPaging(t *testing.T) {
	setupVpnAdminDB(t)

	cases := []struct {
		name      string
		query     model.VpnOrderAdminQuery
		wantTotal int64
		wantFirst string
	}{
		{"all first page", model.VpnOrderAdminQuery{Page: 1, PageSize: 2}, 5, "VPN-A-ALIPAY"},
		{"status success", model.VpnOrderAdminQuery{Page: 1, PageSize: 10, Status: common.TopUpStatusSuccess}, 3, "VPN-A-ALIPAY"},
		{"status pending", model.VpnOrderAdminQuery{Page: 1, PageSize: 10, Status: common.TopUpStatusPending}, 2, "VPN-D-PENDING"},
		{"keyword trade_no", model.VpnOrderAdminQuery{Page: 1, PageSize: 10, Keyword: "VPN-B"}, 1, "VPN-B-EPAY"},
		{"keyword user_id", model.VpnOrderAdminQuery{Page: 1, PageSize: 10, Keyword: "61"}, 1, "VPN-A-ALIPAY"},
		{"keyword username", model.VpnOrderAdminQuery{Page: 1, PageSize: 10, Keyword: "bob"}, 1, "VPN-B-EPAY"},
		{"status plus keyword", model.VpnOrderAdminQuery{Page: 1, PageSize: 10, Status: common.TopUpStatusPending, Keyword: "admin"}, 2, "VPN-D-PENDING"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, total, err := model.SearchVpnOrders(tc.query)
			require.NoError(t, err)
			assert.Equal(t, tc.wantTotal, total)
			require.Len(t, items, int(min(tc.wantTotal, int64(tc.query.PageSize))))
			assert.Equal(t, tc.wantFirst, items[0].TradeNo)
			assert.GreaterOrEqual(t, tc.query.PageSize, len(items))
		})
	}
}

// TestAdminVpnOrderStats 汇总必须只统计 success 订单，金额用十进制字符串，避免浮点误差。
func TestAdminVpnOrderStats(t *testing.T) {
	now := setupVpnAdminDB(t)

	stats, err := model.GetVpnOrderStats(now)
	require.NoError(t, err)
	assert.Equal(t, int64(3), stats.PaidCount)
	assert.Equal(t, "597.00", stats.PaidAmount)
	assert.Equal(t, int64(2), stats.PendingCount)
	assert.Equal(t, int64(2), stats.TodayPaidCount)
	assert.Equal(t, "398.00", stats.TodayPaidAmount)

	require.Len(t, stats.ByProvider, 2)
	byProvider := map[string]model.VpnProviderStat{}
	for _, row := range stats.ByProvider {
		byProvider[row.Provider] = row
	}
	assert.Equal(t, int64(2), byProvider["alipay"].PaidCount)
	assert.Equal(t, "398.00", byProvider["alipay"].PaidAmount)
	assert.Equal(t, int64(1), byProvider["epay"].PaidCount)
	assert.Equal(t, "199.00", byProvider["epay"].PaidAmount)
}

// TestAdminGetVpnOrdersResponse 接口层：DTO 不得出现节点密钥，非法状态必须拒绝。
func TestAdminGetVpnOrdersResponse(t *testing.T) {
	setupVpnAdminDB(t)

	w := callAdminVpnOrders(t, "?p=1&page_size=10")
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"success":true`)
	assert.Contains(t, body, `"paid_amount":"597.00"`)
	assert.Contains(t, body, `"username":"alice"`)
	assert.NotContains(t, body, "node_key")
	assert.NotContains(t, body, "vless://")

	w = callAdminVpnOrders(t, "?status=not_a_status")
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"success":false`)
}
