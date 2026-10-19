package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// quotaQueryError 返回 /v1 家族统一的 OpenAI 风格错误体。
// 查询接口给程序消费，服务端故障必须用真实 5xx，不能让下游 raise_for_status 漏掉后再解析 data 崩掉。
func quotaQueryError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
			"code":    "",
		},
	})
}

// GetQuota 返回调用方 key 自己的余额（GET /v1/dashboard/billing/quota，挂 TokenAuth）。
// 身份只由 Authorization 头里的 key 决定：user/token id 一律从 context 取，
// 不读 query / body，从接口形状上杜绝"查别人余额"。
// 零副作用：不记计费日志、不扣费、不建任务，仅读 Redis 实时字段与数据库当前值。
func GetQuota(c *gin.Context) {
	userId := c.GetInt("id")
	tokenId := c.GetInt("token_id")
	if userId == 0 || tokenId == 0 {
		quotaQueryError(c, http.StatusUnauthorized, "invalid_request_error", "Unauthorized")
		return
	}

	userCache, err := model.GetUserCache(userId)
	if err != nil {
		common.SysLog(fmt.Sprintf("GetQuota: failed to get user cache for user %d: %v", userId, err))
		quotaQueryError(c, http.StatusInternalServerError, "server_error", "查询余额失败")
		return
	}
	// 用户钱包剩余：Redis Quota 字段实时优先，DB 兜底
	remainUser, err := model.GetUserQuota(userId, false)
	if err != nil {
		common.SysLog(fmt.Sprintf("GetQuota: failed to get user quota for user %d: %v", userId, err))
		quotaQueryError(c, http.StatusInternalServerError, "server_error", "查询余额失败")
		return
	}
	// 令牌剩余：读字段级实时缓存；unlimited 令牌的 DB 值可能是负记账垃圾
	remainToken, err := model.GetTokenRemainQuota(tokenId, c.GetString("token_key"))
	if err != nil {
		common.SysLog(fmt.Sprintf("GetQuota: failed to get token quota for token %d: %v", tokenId, err))
		quotaQueryError(c, http.StatusInternalServerError, "server_error", "查询余额失败")
		return
	}

	usedUser, err := model.GetUserUsedQuota(userId)
	if err != nil {
		common.SysLog(fmt.Sprintf("GetQuota: failed to get user used quota for user %d: %v", userId, err))
		quotaQueryError(c, http.StatusInternalServerError, "server_error", "查询余额失败")
		return
	}
	usedToken, err := model.GetTokenUsedQuota(tokenId)
	if err != nil {
		common.SysLog(fmt.Sprintf("GetQuota: failed to get token used quota for token %d: %v", tokenId, err))
		quotaQueryError(c, http.StatusInternalServerError, "server_error", "查询余额失败")
		return
	}

	tokenUnlimited := c.GetBool("token_unlimited_quota")
	// 下游真正能花的钱 = min(用户钱包, 令牌剩余)；令牌不限量时只看钱包
	effectiveRemain := remainUser
	var remainTokenField any
	if tokenUnlimited {
		remainTokenField = nil
	} else {
		remainTokenField = remainToken
		if remainToken < effectiveRemain {
			effectiveRemain = remainToken
		}
	}

	userSetting := userCache.GetSetting()
	notifyType := userSetting.NotifyType
	threshold := common.QuotaRemindThreshold
	if userSetting.QuotaWarningThreshold != 0 {
		threshold = int(userSetting.QuotaWarningThreshold)
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"object": "billing.quota",
			"user": gin.H{
				"id":       userCache.Id,
				"username": userCache.Username,
			},
			"token": gin.H{
				"id":   tokenId,
				"name": c.GetString("token_name"),
			},
			"quota": gin.H{
				"remain_user":       remainUser,
				"used_user":         usedUser,
				"remain_token":      remainTokenField,
				"used_token":        usedToken,
				"token_unlimited":   tokenUnlimited,
				"effective_remain":  effectiveRemain,
				"unit":              "quota",
				"quota_per_unit":    common.QuotaPerUnit,
				"display_remain":    logger.FormatQuota(effectiveRemain),
			},
			"warning": gin.H{
				"enabled":     notifyType != "",
				"threshold":   threshold,
				"notify_type": notifyType,
			},
			"stale_seconds_max": common.BatchUpdateInterval,
		},
	})
}
