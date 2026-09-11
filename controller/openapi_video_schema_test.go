package controller

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoOpenAPISchemaKeepsModelSpecificParametersGeneric(t *testing.T) {
	data, err := os.ReadFile("../docs/openapi/relay.json")
	require.NoError(t, err)

	var document map[string]any
	require.NoError(t, common.Unmarshal(data, &document))

	paths, ok := document["paths"].(map[string]any)
	require.True(t, ok)
	videoPath, ok := paths["/v1/videos"].(map[string]any)
	require.True(t, ok)
	post, ok := videoPath["post"].(map[string]any)
	require.True(t, ok)
	requestBody, ok := post["requestBody"].(map[string]any)
	require.True(t, ok)
	content, ok := requestBody["content"].(map[string]any)
	require.True(t, ok)
	multipart, ok := content["multipart/form-data"].(map[string]any)
	require.True(t, ok)
	schema, ok := multipart["schema"].(map[string]any)
	require.True(t, ok)
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	seed, ok := properties["seed"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, seed, "minimum")
	images, ok := properties["images"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, images, "minItems")
	imageItems, ok := images["items"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, imageItems, "format")
	audio, ok := properties["audio"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, audio, "format")
	audioDuration, ok := properties["audio_duration"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "integer", audioDuration["type"])

	components, ok := document["components"].(map[string]any)
	require.True(t, ok)
	schemas, ok := components["schemas"].(map[string]any)
	require.True(t, ok)
	videoRequest, ok := schemas["VideoRequest"].(map[string]any)
	require.True(t, ok)
	componentProperties, ok := videoRequest["properties"].(map[string]any)
	require.True(t, ok)
	componentAudioDuration, ok := componentProperties["audio_duration"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "integer", componentAudioDuration["type"])
	componentAudio, ok := componentProperties["audio"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, componentAudio, "format")
}
