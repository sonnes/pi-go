package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateEmitsPackageModelCatalog(t *testing.T) {
	provider := mdProvider{Models: map[string]mdModel{
		"model-1": {ID: "model-1", Name: "Model 1"},
	}}

	src, count := generate(
		target{key: "fake", pkg: "fake"},
		provider,
	)
	require.Equal(t, 1, count)

	generated := string(src)
	assert.Contains(t, generated, "func Models() []ai.Model")
	assert.NotContains(t, generated, "func (p *Provider) Models()")
	assert.True(t, strings.Contains(generated, "func (p *Provider) LanguageModel"))
}
