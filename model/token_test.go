package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTokenRemainQuotaReadsDBWhenRedisDisabled(t *testing.T) {
	truncateTables(t)
	require.False(t, common.RedisEnabled)

	token := Token{
		Id:         1,
		UserId:     1,
		Key:        "sk-quota-read",
		Name:       "quota-read",
		RemainQuota: 123456,
		UsedQuota:  654321,
	}
	require.NoError(t, DB.Create(&token).Error)

	remain, err := GetTokenRemainQuota(token.Id, token.Key)
	require.NoError(t, err)
	assert.Equal(t, 123456, remain)

	used, err := GetTokenUsedQuota(token.Id)
	require.NoError(t, err)
	assert.Equal(t, 654321, used)
}

func TestGetTokenRemainQuotaReturnsNegativeForUnlimitedToken(t *testing.T) {
	truncateTables(t)

	// unlimited 令牌的 remain_quota 是从 0 起扣的负数记账垃圾，
	// model 层必须原样返回，unlimited 语义由 controller 解释。
	token := Token{
		Id:            2,
		UserId:        1,
		Key:           "sk-unlimited",
		Name:          "unlimited",
		RemainQuota:   -64201877,
		UsedQuota:     64105986,
		UnlimitedQuota: true,
	}
	require.NoError(t, DB.Create(&token).Error)

	remain, err := GetTokenRemainQuota(token.Id, token.Key)
	require.NoError(t, err)
	assert.Equal(t, -64201877, remain)
}
