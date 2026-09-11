package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateAutoDLModelNamesUpdatesChannelAndAbilities(t *testing.T) {
	truncateTables(t)

	testModel := "autodl:audio-sync-video"
	channel := &Channel{
		Type:      constant.ChannelTypeAutoDL,
		Models:    "autodl:multiaudio-video-1,autodl:multiaudio-video-2,autodl:audio-sync-video",
		Group:     "default",
		Status:    common.ChannelStatusEnabled,
		TestModel: &testModel,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

	require.NoError(t, migrateAutoDLModelNames())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, strings.Join([]string{
		"autodl:minimax-h3-u24",
		"autodl:minimax-h3-u08",
		"autodl:minimax-h3-lipsync",
	}, ","), stored.Models)
	require.NotNil(t, stored.TestModel)
	assert.Equal(t, "autodl:minimax-h3-lipsync", *stored.TestModel)

	var abilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	assert.Len(t, abilities, 3)
	abilityModels := make([]string, 0, len(abilities))
	for _, ability := range abilities {
		abilityModels = append(abilityModels, ability.Model)
	}
	assert.ElementsMatch(t, []string{
		"autodl:minimax-h3-u24",
		"autodl:minimax-h3-u08",
		"autodl:minimax-h3-lipsync",
	}, abilityModels)

	require.NoError(t, migrateAutoDLModelNames())
	var migratedAgain Channel
	require.NoError(t, DB.First(&migratedAgain, channel.Id).Error)
	assert.Equal(t, stored.Models, migratedAgain.Models)
}

func TestAutoDLChannelInsertNormalizesLegacyModelNames(t *testing.T) {
	truncateTables(t)

	channel := &Channel{
		Type:   constant.ChannelTypeAutoDL,
		Models: "autodl:multiaudio-video-3",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, channel.Insert())
	assert.Equal(t, "autodl:minimax-h3-image-audio-10s", channel.Models)

	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.Equal(t, "autodl:minimax-h3-image-audio-10s", ability.Model)
}

func TestAutoDLChannelUpdateNormalizesLegacyModelNamesWhenTypeIsOmitted(t *testing.T) {
	truncateTables(t)

	channel := &Channel{
		Type:   constant.ChannelTypeAutoDL,
		Models: "autodl:minimax-h3-u24",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, channel.Insert())

	patch := &Channel{Id: channel.Id, Models: "autodl:multiaudio-video-4"}
	require.NoError(t, patch.Update())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "autodl:minimax-h3-image-audio-15s", stored.Models)
}

func TestMigrateAutoDLLiveChannelAliases(t *testing.T) {
	truncateTables(t)

	channel := &Channel{
		Type:   constant.ChannelTypeAutoDL,
		Models: "autodl:h3-video,autodl:multiref-video-1,autodl:multiref-video-2,autodl:multiref-video-3",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

	require.NoError(t, migrateAutoDLModelNames())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, strings.Join([]string{
		"autodl:minimax-h3-text-to-video",
		"autodl:minimax-h3-lightx2v-v5",
		"autodl:minimax-h3-lightx2v-v5-15s",
		"autodl:minimax-h3-b99-12s",
	}, ","), stored.Models)

	var abilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	abilityModelNames := make([]string, 0, len(abilities))
	for _, ability := range abilities {
		abilityModelNames = append(abilityModelNames, ability.Model)
	}
	assert.ElementsMatch(t, []string{
		"autodl:minimax-h3-text-to-video",
		"autodl:minimax-h3-lightx2v-v5",
		"autodl:minimax-h3-lightx2v-v5-15s",
		"autodl:minimax-h3-b99-12s",
	}, abilityModelNames)
}
