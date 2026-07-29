package cmd

import (
	"testing"

	"github.com/dotcommander/distill/internal/actions/digest"
	"github.com/dotcommander/distill/internal/digestcache"
	"github.com/dotcommander/distill/internal/prompts"
	"github.com/stretchr/testify/assert"
)

func TestPopulateMergeDigestCacheInputs(t *testing.T) {
	t.Parallel()
	opts := digest.Options{
		MergeFacts:           true,
		MergeThreshold:       0.87,
		OutlineFromClusters:  true,
		MaxSections:          9,
		MinSectionFacts:      3,
		ClusterBalanceFactor: 1.75,
	}
	p := &prompts.Set{
		MergeFacts:    "resolved merge prompt",
		ClusterLabels: "resolved cluster labels prompt",
	}
	var got digestcache.KeyInputs

	populateMergeDigestCacheInputs(&got, opts, "resolved-embedding-model", p)

	assert.True(t, got.MergeFacts)
	assert.Equal(t, 0.87, got.MergeThreshold)
	assert.True(t, got.OutlineFromClusters)
	assert.Equal(t, 9, got.MaxSections)
	assert.Equal(t, 3, got.MinSectionFacts)
	assert.Equal(t, 1.75, got.ClusterBalanceFactor)
	assert.Equal(t, "resolved-embedding-model", got.EmbeddingModel)
	assert.Equal(t, "resolved merge prompt", got.MergeFactsPrompt)
	assert.Equal(t, "resolved cluster labels prompt", got.ClusterLabelsPrompt)
}
