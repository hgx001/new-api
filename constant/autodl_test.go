package constant

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMigrateAutoDLModelNamesReplacesLegacyNamesAndDeduplicates(t *testing.T) {
	models, changed := MigrateAutoDLModelNames([]string{
		"autodl:multiaudio-video-1",
		"autodl:multiaudio-video-2",
		"autodl:multiaudio-video-3",
		"autodl:multiaudio-video-4",
		"autodl:audio-sync-video",
		" autodl:minimax-h3-u24 ",
		"",
	})

	assert.True(t, changed)
	assert.Equal(t, []string{
		"autodl:minimax-h3-u24",
		"autodl:minimax-h3-u08",
		"autodl:minimax-h3-image-audio-10s",
		"autodl:minimax-h3-image-audio-15s",
		"autodl:minimax-h3-lipsync",
	}, models)

	migratedAgain, changedAgain := MigrateAutoDLModelNames(models)
	assert.False(t, changedAgain)
	assert.Equal(t, models, migratedAgain)
}

func TestMigrateAutoDLModelNamesRenamesLiveChannelAliases(t *testing.T) {
	models, changed := MigrateAutoDLModelNames([]string{
		"autodl:h3-video",
		"autodl:multiref-video-1",
		"autodl:multiref-video-2",
		"autodl:multiref-video-3",
	})

	assert.True(t, changed)
	assert.Equal(t, []string{
		"autodl:minimax-h3-text-to-video",
		"autodl:minimax-h3-lightx2v-v5",
		"autodl:minimax-h3-lightx2v-v5-15s",
		"autodl:minimax-h3-b99-12s",
	}, models)
}
