package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// TestPricingSquareOnlyShowsAllowlistedModels locks the Model Square contract
// introduced on 2026-08-28 ("restrict public model square models"): only models
// on the public allowlist (model.isAllowedPricingModel) reach the square, even
// when a channel serving them is enabled; allowlisted models still follow their
// own channel's enabled/disabled state.
//
// History: this case used to be TestPricingIncludesKimiWhenMoonshotChannelEnabled
// and asserted "Kimi shows up on the square once the Moonshot channel is enabled".
// After the allowlist shipped, Kimi/abab6.5-style models never reach the square at
// all, so the old assertions went red for two months (nobody ran the full suite).
func TestPricingSquareOnlyShowsAllowlistedModels(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	minimaxChannel := &model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeMiniMax,
		Key:    "minimax-test-key",
		Name:   "MiniMax Test",
		Status: common.ChannelStatusEnabled,
		Models: "MiniMax-M3,abab6.5-chat",
		Group:  "default",
	}
	moonshotChannel := &model.Channel{
		Id:     2,
		Type:   constant.ChannelTypeMoonshot,
		Key:    "moonshot-test-key",
		Name:   "Moonshot Test",
		Status: common.ChannelStatusEnabled,
		Models: "kimi-k2.5",
		Group:  "default",
	}

	require.NoError(t, db.Create(minimaxChannel).Error)
	require.NoError(t, minimaxChannel.UpdateAbilities(nil))
	require.NoError(t, db.Create(moonshotChannel).Error)
	require.NoError(t, moonshotChannel.UpdateAbilities(nil))

	model.InvalidatePricingCache()
	pricingByName := pricingByModelName(model.GetPricing())

	// Allowlisted + channel enabled → on the square.
	require.Contains(t, pricingByName, "MiniMax-M3", "allowlisted model on an enabled channel must show on the square")
	// Channel enabled but model not on the allowlist → stays hidden.
	require.NotContains(t, pricingByName, "abab6.5-chat", "unlisted model must stay hidden even with an enabled channel")
	require.NotContains(t, pricingByName, "kimi-k2.5", "unlisted model must stay hidden even with an enabled channel")

	// Disabling Moonshot leaves Kimi hidden and does not touch MiniMax.
	moonshotChannel.Status = common.ChannelStatusManuallyDisabled
	require.NoError(t, db.Model(moonshotChannel).Update("status", moonshotChannel.Status).Error)
	require.NoError(t, moonshotChannel.UpdateAbilities(nil))

	model.InvalidatePricingCache()
	pricingByName = pricingByModelName(model.GetPricing())

	require.Contains(t, pricingByName, "MiniMax-M3", "MiniMax model should still appear after Moonshot is disabled")
	require.NotContains(t, pricingByName, "kimi-k2.5", "Kimi model should stay hidden after Moonshot is disabled")

	// Allowlisted models follow their own channel: disabling MiniMax drops it.
	minimaxChannel.Status = common.ChannelStatusManuallyDisabled
	require.NoError(t, db.Model(minimaxChannel).Update("status", minimaxChannel.Status).Error)
	require.NoError(t, minimaxChannel.UpdateAbilities(nil))

	model.InvalidatePricingCache()
	pricingByName = pricingByModelName(model.GetPricing())

	require.NotContains(t, pricingByName, "MiniMax-M3", "allowlisted model must disappear once its channel is disabled")
}

// TestPricingOmitsEntriesWhenChannelHasNoModels verifies that an enabled channel
// without any configured models contributes nothing to the Model Square. The
// control channel proves the square itself still works, so a pass cannot come
// from "everything is broken".
func TestPricingOmitsEntriesWhenChannelHasNoModels(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	controlChannel := &model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeMiniMax,
		Key:    "minimax-test-key",
		Name:   "MiniMax Control",
		Status: common.ChannelStatusEnabled,
		Models: "MiniMax-M3",
		Group:  "default",
	}
	emptyModelsChannel := &model.Channel{
		Id:     2,
		Type:   constant.ChannelTypeMoonshot,
		Key:    "moonshot-test-key",
		Name:   "Moonshot Empty Models",
		Status: common.ChannelStatusEnabled,
		Models: "",
		Group:  "default",
	}

	require.NoError(t, db.Create(controlChannel).Error)
	require.NoError(t, controlChannel.UpdateAbilities(nil))
	require.NoError(t, db.Create(emptyModelsChannel).Error)
	require.NoError(t, emptyModelsChannel.UpdateAbilities(nil))

	model.InvalidatePricingCache()
	pricingByName := pricingByModelName(model.GetPricing())

	// Control: the allowlisted model of the configured channel is on the square.
	require.Contains(t, pricingByName, "MiniMax-M3", "control channel must still produce a square entry")
	// The empty-models channel contributes nothing at all.
	// 注意 "" 也在列：strings.Split("", ",") == [""]，所以 Models 为空的渠道
	// 真会写一条 model 为空的 ability 垃圾行（model/ability.go UpdateAbilities）。
	// 今天它被广场白名单和 HasModelBillingConfig 双重挡着，用户端看不见；
	// 哪天白名单放宽，这里会立刻红——那是信号，不是误报。
	for _, modelName := range []string{"kimi-k2.5", "kimi-k2-turbo-preview", "moonshot", ""} {
		require.NotContains(t, pricingByName, modelName, "channel without models must not produce pricing entries")
	}
	require.Len(t, pricingByName, 1, "only the control channel's model may reach the square")
}

// TestPricingHidesAllowlistedModelWhenMetadataDisabled verifies that an
// allowlisted model is hidden from the Model Square when its model metadata row
// has status != 1 (the meta.Status guard inside updatePricing).
func TestPricingHidesAllowlistedModelWhenMetadataDisabled(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	channel := &model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeMiniMax,
		Key:    "minimax-test-key",
		Name:   "MiniMax Test",
		Status: common.ChannelStatusEnabled,
		Models: "MiniMax-M3",
		Group:  "default",
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, channel.UpdateAbilities(nil))

	// Insert an explicit model metadata row with disabled status.
	disabledMeta := &model.Model{
		ModelName: "MiniMax-M3",
		Status:    0,
		NameRule:  model.NameRuleExact,
	}
	require.NoError(t, disabledMeta.Insert())

	model.InvalidatePricingCache()
	pricingByName := pricingByModelName(model.GetPricing())
	require.NotContains(t, pricingByName, "MiniMax-M3", "disabled model metadata must hide an allowlisted model from the square")
}
