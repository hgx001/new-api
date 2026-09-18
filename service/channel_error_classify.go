package service

import (
	"net/http"
	"strings"
)

// upstreamBalanceKeywords 命中即判定为上游渠道账户自身不可用（余额/额度耗尽）。
// 注意：下游用户额度不足发生在选渠道之前的预扣费阶段，走不到重试循环，
// 因此这里只收录描述「上游账户」的短语，避免把普通参数错误误判为可切换。
var upstreamBalanceKeywords = []string{
	"余额不足",
	"余额耗尽",
	"可用额度不足",
	"剩余额度",
	"欠费",
	"请充值",
	"充值",
	"账户余额",
	"insufficient balance",
	"insufficient_user_quota",
	"insufficient quota",
	"quota exceeded",
	"quota_exceeded",
	"out of credit",
	"balance not enough",
	"balance insufficient",
	"payment required",
	"arrears",
}

// IsUpstreamAccountBalanceError 判断上游失败是否源于渠道账户余额/额度耗尽。
// 此类错误必须切换到同模型下一优先级的备选渠道重试，而不是直接返回失败；
// 配合 AutoBan 可将耗尽的渠道暂时下线，避免每个请求都先撞一次坏渠道交延迟税。
func IsUpstreamAccountBalanceError(statusCode int, message string) bool {
	if statusCode == http.StatusPaymentRequired {
		return true
	}
	lowerMessage := strings.ToLower(strings.TrimSpace(message))
	if lowerMessage == "" {
		return false
	}
	for _, keyword := range upstreamBalanceKeywords {
		if strings.Contains(lowerMessage, keyword) {
			return true
		}
	}
	return false
}
