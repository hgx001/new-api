package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openQuotaControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	model.DB = db
	model.LOG_DB = db

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

type quotaResponse struct {
	Success bool   `json:"success"`
	Data    struct {
		User struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
		Token struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"token"`
		Quota struct {
			RemainUser      int   `json:"remain_user"`
			UsedUser        int   `json:"used_user"`
			RemainToken     *int  `json:"remain_token"`
			UsedToken       int   `json:"used_token"`
			TokenUnlimited  bool  `json:"token_unlimited"`
			EffectiveRemain int   `json:"effective_remain"`
			QuotaPerUnit    float64 `json:"quota_per_unit"`
		} `json:"quota"`
	} `json:"data"`
}

func setupQuotaFixture(t *testing.T) {
	t.Helper()
	db := openQuotaControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "owner", AffCode: "owner-aff", Quota: 34913880, UsedQuota: 75423270, Status: common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.User{
		Id: 2, Username: "other", AffCode: "other-aff", Quota: 999, UsedQuota: 1, Status: common.UserStatusEnabled,
	}).Error)
	// unlimited 令牌：remain_quota 为负记账垃圾
	require.NoError(t, db.Create(&model.Token{
		Id: 11, UserId: 1, Key: "sk-owner-unlimited", Name: "auto",
		UnlimitedQuota: true, RemainQuota: -64201877, UsedQuota: 64105986, Status: common.TokenStatusEnabled,
	}).Error)
	// 非 unlimited 令牌：余额少于用户钱包
	require.NoError(t, db.Create(&model.Token{
		Id: 22, UserId: 1, Key: "sk-owner-limited", Name: "limited",
		RemainQuota: 300, UsedQuota: 100, Status: common.TokenStatusEnabled,
	}).Error)
}

func callGetQuota(t *testing.T, url string, userId, tokenId int, tokenKey string, unlimited bool) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, url, nil)
	ctx.Set("id", userId)
	ctx.Set("token_id", tokenId)
	ctx.Set("token_key", tokenKey)
	ctx.Set("token_name", "auto")
	ctx.Set("token_unlimited_quota", unlimited)
	GetQuota(ctx)
	return recorder
}

func TestGetQuotaUnlimitedTokenReturnsWalletAsEffective(t *testing.T) {
	setupQuotaFixture(t)

	// 带 user_id=2 试图越权：必须被忽略，返回调用者自己的余额
	recorder := callGetQuota(t, "/v1/dashboard/billing/quota?user_id=2", 1, 11, "sk-owner-unlimited", true)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))

	var payload quotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	assert.Equal(t, 1, payload.Data.User.ID)
	assert.Equal(t, "owner", payload.Data.User.Username)
	assert.Equal(t, 11, payload.Data.Token.ID)
	assert.Equal(t, "auto", payload.Data.Token.Name)
	assert.Equal(t, 34913880, payload.Data.Quota.RemainUser)
	assert.Equal(t, 75423270, payload.Data.Quota.UsedUser)
	assert.Nil(t, payload.Data.Quota.RemainToken)
	assert.Equal(t, 64105986, payload.Data.Quota.UsedToken)
	assert.True(t, payload.Data.Quota.TokenUnlimited)
	assert.Equal(t, 34913880, payload.Data.Quota.EffectiveRemain)
}

func TestGetQuotaLimitedTokenUsesMinOfUserAndToken(t *testing.T) {
	setupQuotaFixture(t)

	recorder := callGetQuota(t, "/v1/dashboard/billing/quota", 1, 22, "sk-owner-limited", false)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload quotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.NotNil(t, payload.Data.Quota.RemainToken)
	assert.Equal(t, 300, *payload.Data.Quota.RemainToken)
	assert.False(t, payload.Data.Quota.TokenUnlimited)
	// 钱包 34913880，令牌 300 → 真正能花的是 300
	assert.Equal(t, 300, payload.Data.Quota.EffectiveRemain)
}

func TestGetQuotaRejectsMissingAuthContext(t *testing.T) {
	setupQuotaFixture(t)

	// TokenAuth 未写入 context（理论上到不了这里）：按未鉴权处理
	recorder := callGetQuota(t, "/v1/dashboard/billing/quota", 0, 0, "", false)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestGetQuotaReturnsServerErrorOnDatabaseFailure(t *testing.T) {
	setupQuotaFixture(t)

	// 临时把 model.DB 换成已关闭的连接，模拟查询失败；cleanup 恢复，不影响其他用例
	broken, err := gorm.Open(sqlite.Open("file:quota-broken?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := broken.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	oldDB := model.DB
	model.DB = broken
	t.Cleanup(func() { model.DB = oldDB })

	recorder := callGetQuota(t, "/v1/dashboard/billing/quota", 1, 11, "sk-owner-unlimited", true)

	// 服务端故障必须是真实 5xx + OpenAI 风格错误体，不能是 200 success:false
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	assert.Equal(t, "server_error", payload.Error.Type)
	assert.NotEmpty(t, payload.Error.Message)
}
