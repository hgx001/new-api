package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// TestPricingIncludesKimiWhenMoonshotChannelEnabled verifies that Kimi models
// appear in the Model Square (/api/pricing) only when the Moonshot channel is
// enabled and has those models configured. It also verifies that MiniMax models
// remain visible independently.
func TestPricingIncludesKimiWhenMoonshotChannelEnabled(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	minimaxChannel := &model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeMiniMax,
		Key:    "minimax-test-key",
		Name:   "MiniMax Test",
		Status: common.ChannelStatusEnabled,
		Models: "abab6.5-chat,abab6.5s-chat",
		Group:  "default",
	}
	moonshotChannel := &model.Channel{
		Id:     2,
		Type:   constant.ChannelTypeMoonshot,
		Key:    "moonshot-test-key",
		Name:   "Moonshot Test",
		Status: common.ChannelStatusEnabled,
		Models: "kimi-k2.5,kimi-k2-turbo-preview",
		Group:  "default",
	}

	require.NoError(t, db.Create(minimaxChannel).Error)
	require.NoError(t, minimaxChannel.UpdateAbilities(nil))
	require.NoError(t, db.Create(moonshotChannel).Error)
	require.NoError(t, moonshotChannel.UpdateAbilities(nil))

	model.InvalidatePricingCache()
	pricingByName := pricingByModelName(model.GetPricing())

	require.Contains(t, pricingByName, "abab6.5-chat", "MiniMax model should appear in pricing")
	require.Contains(t, pricingByName, "kimi-k2.5", "Kimi model should appear when Moonshot channel is enabled")
	require.Contains(t, pricingByName, "kimi-k2-turbo-preview", "Kimi model should appear when Moonshot channel is enabled")

	// Disable Moonshot channel and verify Kimi models disappear while MiniMax stays.
	moonshotChannel.Status = common.ChannelStatusManuallyDisabled
	require.NoError(t, db.Model(moonshotChannel).Update("status", moonshotChannel.Status).Error)
	require.NoError(t, moonshotChannel.UpdateAbilities(nil))

	model.InvalidatePricingCache()
	pricingByName = pricingByModelName(model.GetPricing())

	require.Contains(t, pricingByName, "abab6.5-chat", "MiniMax model should still appear after Moonshot is disabled")
	require.NotContains(t, pricingByName, "kimi-k2.5", "Kimi model should disappear when Moonshot channel is disabled")
	require.NotContains(t, pricingByName, "kimi-k2-turbo-preview", "Kimi model should disappear when Moonshot channel is disabled")
}

// TestPricingOmitsKimiWhenMoonshotModelsEmpty verifies that an enabled Moonshot
// channel without any configured models does not contribute Kimi entries to the
// Model Square.
func TestPricingOmitsKimiWhenMoonshotModelsEmpty(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	moonshotChannel := &model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeMoonshot,
		Key:    "moonshot-test-key",
		Name:   "Moonshot Empty Models",
		Status: common.ChannelStatusEnabled,
		Models: "",
		Group:  "default",
	}

	require.NoError(t, db.Create(moonshotChannel).Error)
	require.NoError(t, moonshotChannel.UpdateAbilities(nil))

	model.InvalidatePricingCache()
	pricingByName := pricingByModelName(model.GetPricing())

	for _, modelName := range []string{"kimi-k2.5", "kimi-k2-turbo-preview", "moonshot"} {
		require.NotContains(t, pricingByName, modelName, "Empty Moonshot channel should not produce pricing entries")
	}
}

// TestPricingOmitsKimiWhenModelMetadataDisabled verifies that even if an
// enabled ability exists for a Kimi model, the Model Square hides it when the
// corresponding model metadata row has status != 1.
func TestPricingOmitsKimiWhenModelMetadataDisabled(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	moonshotChannel := &model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeMoonshot,
		Key:    "moonshot-test-key",
		Name:   "Moonshot Test",
		Status: common.ChannelStatusEnabled,
		Models: "kimi-k2.5",
		Group:  "default",
	}
	require.NoError(t, db.Create(moonshotChannel).Error)
	require.NoError(t, moonshotChannel.UpdateAbilities(nil))

	// Insert an explicit model metadata row with disabled status.
	disabledMeta := &model.Model{
		ModelName: "kimi-k2.5",
		Status:    0,
		NameRule:  model.NameRuleExact,
	}
	require.NoError(t, disabledMeta.Insert())

	model.InvalidatePricingCache()
	pricingByName := pricingByModelName(model.GetPricing())
	require.NotContains(t, pricingByName, "kimi-k2.5", "Disabled model metadata should hide the model from pricing")
}
