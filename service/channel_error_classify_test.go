package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsUpstreamAccountBalanceError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		message    string
		want       bool
	}{
		{name: "402 always account error", statusCode: http.StatusPaymentRequired, message: "anything", want: true},
		{name: "400 chinese balance", statusCode: http.StatusBadRequest, message: "账户余额不足，请充值", want: true},
		{name: "403 english balance", statusCode: http.StatusForbidden, message: "Insufficient balance, please top up", want: true},
		{name: "400 upstream quota id", statusCode: http.StatusBadRequest, message: "insufficient_user_quota, 用户剩余额度: 0.5", want: true},
		{name: "502 gateway without balance words", statusCode: http.StatusBadGateway, message: "<html>502 Bad Gateway</html>", want: false},
		{name: "400 plain bad request", statusCode: http.StatusBadRequest, message: "prompt is required", want: false},
		{name: "400 invalid model", statusCode: http.StatusBadRequest, message: "model_not_found", want: false},
		{name: "400 empty message", statusCode: http.StatusBadRequest, message: "   ", want: false},
		{name: "200 empty message", statusCode: http.StatusOK, message: "", want: false},
		{name: "500 internal error", statusCode: http.StatusInternalServerError, message: "internal error", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUpstreamAccountBalanceError(tt.statusCode, tt.message)
			if tt.want {
				require.True(t, got, "status=%d message=%q should be account balance error", tt.statusCode, tt.message)
			} else {
				assert.False(t, got, "status=%d message=%q should not be account balance error", tt.statusCode, tt.message)
			}
		})
	}
}
